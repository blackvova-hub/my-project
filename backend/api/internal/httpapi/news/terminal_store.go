package newsapi

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strconv"
	"strings"
	"time"
)

func (s *terminalService) hydrate(ctx context.Context) {
	ctx, cancel := context.WithTimeout(ctx, 4*time.Second)
	defer cancel()
	s.mu.Lock()
	first := !s.hydrated
	s.hydrated, s.dbChecked = true, s.now()
	s.mu.Unlock()
	if s.db == nil {
		return
	}
	if first {
		rows, err := s.db.Query(ctx, `SELECT source_id,last_attempt,last_success,last_error,item_count,etag,last_modified,next_attempt FROM news_terminal_source_health`)
		if err == nil {
			for rows.Next() {
				var id, etag, modified string
				var attempt, success, next *time.Time
				var message *string
				var count int
				if rows.Scan(&id, &attempt, &success, &message, &count, &etag, &modified, &next) != nil {
					continue
				}
				s.mu.Lock()
				for i := range s.sources {
					h := &s.sources[i]
					if h.ID != id {
						continue
					}
					if attempt != nil {
						h.LastAttempt = nullableTerminalTime(*attempt)
					}
					if success != nil {
						h.LastSuccess = nullableTerminalTime(*success)
						if success.After(s.asOf) {
							s.asOf = *success
						}
					}
					h.LastError, h.ItemCount, h.etag, h.lastModified = message, count, etag, modified
					if next != nil {
						h.nextAttempt = *next
					}
				}
				s.mu.Unlock()
			}
			rows.Close()
		}
	}
	f, _ := parseTerminalFilter(nil, s.now())
	items, _, err := s.readStored(ctx, f)
	if err != nil {
		return
	}
	s.mu.Lock()
	s.items = deduplicateTerminal(append(items, s.items...), s.now())
	s.storage = "postgres"
	s.mu.Unlock()
}

// Filtering occurs before the bounded history scan, so a search or asset query
// never silently searches only the newest UI page. Parameters are always bound.
func (s *terminalService) readStored(ctx context.Context, f terminalFilter) ([]TerminalNewsItem, bool, error) {
	s.mu.RLock()
	allowed := make([]string, 0, len(s.sources))
	specs := map[string]terminalSource{}
	for _, source := range s.sources {
		if source.Enabled {
			allowed = append(allowed, source.ID)
			specs[source.ID] = source
		}
	}
	s.mu.RUnlock()
	query := `SELECT title,url,source,source_id,category,published_at,COALESCE(summary,''),COALESCE(original_language,''),assets,source_type FROM news_items
	 WHERE (terminal_collected=TRUE OR publication_eligible=TRUE) AND calendar_only=FALSE
	 AND source_id=ANY($1::text[]) AND published_at >= $2 AND published_at <= $3`
	args := []any{allowed, s.now().Add(-terminalRetention), f.asOf}
	add := func(clause string, value any) {
		args = append(args, value)
		query += fmt.Sprintf(clause, "$"+strconv.Itoa(len(args)))
	}
	if f.category != "all" {
		add(" AND category=%s", f.category)
	}
	if f.source != "" && f.source != "all" {
		add(" AND source_id=%s", f.source)
	}
	if !f.from.IsZero() {
		add(" AND published_at >= %s", f.from)
	}
	if !f.to.IsZero() {
		add(" AND published_at < %s", f.to)
	}
	if f.query != "" {
		query += " AND (FALSE"
		for _, term := range terminalSearchTerms(f.query) {
			add(" OR (title || ' ' || COALESCE(summary,'') || ' ' || assets::text) ILIKE %s", "%"+escapeTerminalLike(term)+"%")
		}
		query += ")"
	}
	if f.asset != "" {
		// Keyword enrichment can add assets absent from the legacy JSON. Include
		// its terms in the bound text predicate, then enforce exact enriched asset
		// membership in terminalPage.
		args = append(args, f.asset)
		assetArg := "$" + strconv.Itoa(len(args))
		query += " AND (assets @> jsonb_build_array(" + assetArg + "::text)"
		for _, sourceID := range terminalForcedAssetSources(f.asset) {
			add(" OR source_id=%s", sourceID)
		}
		for _, term := range terminalAssetTerms(f.asset) {
			add(" OR (title || ' ' || COALESCE(summary,'')) ILIKE %s", "%"+escapeTerminalLike(term)+"%")
		}
		query += ")"
	}
	query += " ORDER BY published_at DESC LIMIT 10001"
	rows, err := s.db.Query(ctx, query, args...)
	if err != nil {
		return nil, false, err
	}
	defer rows.Close()
	items := make([]TerminalNewsItem, 0)
	for rows.Next() {
		var item TerminalNewsItem
		var published time.Time
		var assets json.RawMessage
		var sourceType string
		if err := rows.Scan(&item.Title, &item.URL, &item.Source, &item.SourceID, &item.Category, &published, &item.Summary, &item.Language, &assets, &sourceType); err != nil {
			return nil, false, err
		}
		item.PublishedAt, item.TimestampBasis = published.UTC().Format(time.RFC3339), "published"
		if item.SourceID == "gdelt" {
			item.TimestampBasis = "indexed"
		}
		item.Title, item.Summary = boundedTerminalText(item.Title, 300), boundedTerminalText(item.Summary, 420)
		source := specs[item.SourceID]
		item.Attribution = source.Attribution
		enrichTerminalItem(&item, source.official, s.now())
		var storedAssets []string
		if json.Unmarshal(assets, &storedAssets) == nil {
			for _, asset := range storedAssets {
				asset = strings.ToUpper(strings.TrimSpace(asset))
				if newsAssetRe.MatchString(asset) && !terminalMember(asset, item.Assets...) {
					item.Assets = append(item.Assets, asset)
				}
			}
		}
		items = append(items, item)
	}
	if rows.Err() != nil {
		return nil, false, rows.Err()
	}
	return items, len(items) > 10000, nil
}

func (s *terminalService) persistItems(ctx context.Context, items []TerminalNewsItem) error {
	if len(items) == 0 {
		return nil
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	// Use the existing collector's lock so independent processes cannot race
	// URL/headline deduplication against each other.
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock($1)`, int64(0x4e45575344454455)); err != nil {
		return err
	}
	for _, item := range deduplicateTerminal(append([]TerminalNewsItem{}, items...), s.now()) {
		published, _ := time.Parse(time.RFC3339, item.PublishedAt)
		canonical := canonicalTerminalURL(item.URL)
		assets, _ := json.Marshal(item.Assets)
		kind := "rss"
		if terminalMember(item.SourceID, "federal_reserve", "bls", "ecb", "sec") {
			kind = "official_government"
		}
		if item.SourceID == "gdelt" {
			kind = "gdelt_metadata"
		}
		_, err := tx.Exec(ctx, `INSERT INTO news_items
		 (category,title,url,source,published_at,summary,original_title,original_text,original_language,source_id,source_type,event_type,importance,assets,title_norm,url_hash,terminal_collected,publication_eligible)
		 SELECT $1,$2,$3,$4,$5,NULLIF($6,''),$2,$6,$7,$8,$9,'market_news',$10,$11::jsonb,$12,$13,TRUE,FALSE
		 WHERE NOT EXISTS(SELECT 1 FROM news_items WHERE url=$3 OR url_hash=$13 OR (title_norm=$12 AND published_at BETWEEN $14 AND $15))
		 ON CONFLICT DO NOTHING`, item.Category, stripTerminalNUL(item.Title), item.URL, item.Source, published, stripTerminalNUL(item.Summary), item.Language, item.SourceID, kind, item.ImportanceScore, string(assets), normalizeTerminalHeadline(item.Title), terminalHash(canonical), published.Add(-terminalDedupWindow), published.Add(terminalDedupWindow))
		if err != nil {
			return err
		}
		// A publisher may revise a feed headline/excerpt without changing its
		// URL or publication timestamp. Refresh only our unmoderated cache rows;
		// independently reviewed collector content is never overwritten.
		_, err = tx.Exec(ctx, `UPDATE news_items SET title=$1,summary=NULLIF($2,''),original_title=$1,original_text=$2,
		 importance=$3,assets=$4::jsonb,title_norm=$5,published_at=$6
		 WHERE (url=$7 OR url_hash=$8) AND source_id=$9 AND terminal_collected=TRUE AND publication_eligible=FALSE`,
			stripTerminalNUL(item.Title), stripTerminalNUL(item.Summary), item.ImportanceScore, string(assets), normalizeTerminalHeadline(item.Title), published, item.URL, terminalHash(canonical), item.SourceID)
		if err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

func (s *terminalService) persistHealth(ctx context.Context, source terminalSource) {
	if s.db == nil {
		return
	}
	var attempt, success *time.Time
	if source.LastAttempt != nil {
		parsed, _ := time.Parse(time.RFC3339, *source.LastAttempt)
		attempt = &parsed
	}
	if source.LastSuccess != nil {
		parsed, _ := time.Parse(time.RFC3339, *source.LastSuccess)
		success = &parsed
	}
	_, err := s.db.Exec(ctx, `INSERT INTO news_terminal_source_health(source_id,last_attempt,last_success,last_error,item_count,etag,last_modified,next_attempt)
	 VALUES($1,$2,$3,$4,$5,$6,$7,$8) ON CONFLICT(source_id) DO UPDATE SET
	 last_attempt=EXCLUDED.last_attempt,last_success=EXCLUDED.last_success,last_error=EXCLUDED.last_error,item_count=EXCLUDED.item_count,
	 etag=EXCLUDED.etag,last_modified=EXCLUDED.last_modified,next_attempt=EXCLUDED.next_attempt,updated_at=now()`, source.ID, attempt, success, source.LastError, source.ItemCount, source.etag, source.lastModified, source.nextAttempt)
	if err != nil {
		log.Printf("news terminal source health storage unavailable: %v", err)
	}
}

func escapeTerminalLike(value string) string {
	return strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(value)
}
func stripTerminalNUL(value string) string { return strings.ReplaceAll(value, "\x00", "") }
func terminalAssetTerms(asset string) []string {
	switch asset {
	case "BTC":
		return []string{"bitcoin", "биткоин", "BTC"}
	case "ETH":
		return []string{"ethereum", "эфириум", "ETH"}
	case "SPX":
		return []string{"s&p 500"}
	case "NDX":
		return []string{"nasdaq"}
	case "XAU":
		return []string{"gold", "золот"}
	case "DXY":
		return []string{"dollar index", "dxy"}
	case "USD":
		return []string{"dollar", "federal reserve", "fomc", "доллар", "фрс"}
	case "EUR":
		return []string{"ecb", "european central bank", "euro", "ецб"}
	default:
		return []string{asset}
	}
}

func terminalForcedAssetSources(asset string) []string {
	switch asset {
	case "BTC":
		return []string{"bitcoin_core"}
	case "ETH":
		return []string{"ethereum_foundation"}
	case "USD":
		return []string{"federal_reserve"}
	case "EUR":
		return []string{"ecb"}
	default:
		return nil
	}
}

func terminalSearchTerms(query string) []string {
	terms := []string{query}
	for _, rule := range terminalTopicRules {
		if strings.Contains(rule.tag, strings.ToLower(query)) {
			terms = append(terms, rule.words...)
		}
	}
	return terms
}
