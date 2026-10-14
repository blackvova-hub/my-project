package newsapi

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func terminalTestPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	dsn := os.Getenv("NEWS_TERMINAL_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("set NEWS_TERMINAL_TEST_DATABASE_URL to a migrated isolated QA database")
	}
	ctx := context.Background()
	admin, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal("cannot initialize QA database")
	}
	schema := fmt.Sprintf("news_terminal_test_%d", time.Now().UnixNano())
	name := pgx.Identifier{schema}.Sanitize()
	if _, err = admin.Exec(ctx, "CREATE SCHEMA "+name); err != nil {
		admin.Close()
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = admin.Exec(ctx, "DROP SCHEMA "+name+" CASCADE"); admin.Close() })
	if _, err = admin.Exec(ctx, "CREATE TABLE "+name+".news_items (LIKE public.news_items INCLUDING ALL)"); err != nil {
		t.Fatal(err)
	}
	if _, err = admin.Exec(ctx, "CREATE TABLE "+name+".news_terminal_source_health (LIKE public.news_terminal_source_health INCLUDING ALL)"); err != nil {
		t.Fatal(err)
	}
	config, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatal("invalid QA database configuration")
	}
	config.ConnConfig.RuntimeParams["search_path"] = schema + ",public"
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	return pool
}

func TestTerminalPostgresPersistenceDedupAndFilteredHistory(t *testing.T) {
	pool := terminalTestPool(t)
	s := newTerminalService(pool)
	s.now = func() time.Time { return terminalTestNow }
	ctx := context.Background()
	eth := terminalTestItem("eth", "Introducing zkAPI private usage credits", "crypto", "ethereum_foundation", 1, 0)
	eth.Summary = "A protocol update"
	enrichTerminalItem(&eth, true, terminalTestNow)
	macro := terminalTestItem("cpi", "Consumer price report published", "macro", "bls", 2, 0)
	enrichTerminalItem(&macro, true, terminalTestNow)
	duplicate := eth
	duplicate.URL += "?utm_source=duplicate"
	if err := s.persistItems(ctx, []TerminalNewsItem{eth, macro, duplicate}); err != nil {
		t.Fatal(err)
	}
	if err := s.persistItems(ctx, []TerminalNewsItem{eth}); err != nil {
		t.Fatal(err)
	}
	var count, published int
	if err := pool.QueryRow(ctx, `SELECT count(*),count(*) FILTER(WHERE publication_eligible) FROM news_items`).Scan(&count, &published); err != nil {
		t.Fatal(err)
	}
	if count != 2 || published != 0 {
		t.Fatalf("dedup or outbound safeguard failed: rows=%d approved=%d", count, published)
	}
	eth.Title = "Introducing zkAPI revised protocol announcement"
	if err := s.persistItems(ctx, []TerminalNewsItem{eth}); err != nil {
		t.Fatal(err)
	}
	var revised string
	if err := pool.QueryRow(ctx, `SELECT title FROM news_items WHERE url=$1`, eth.URL).Scan(&revised); err != nil || revised != eth.Title {
		t.Fatal("same-URL provider revision was lost")
	}
	if _, err := pool.Exec(ctx, `UPDATE news_items SET publication_eligible=TRUE WHERE url=$1`, eth.URL); err != nil {
		t.Fatal(err)
	}
	eth.Title = "Terminal attempt to overwrite moderated content"
	if err := s.persistItems(ctx, []TerminalNewsItem{eth}); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `SELECT title FROM news_items WHERE url=$1`, eth.URL).Scan(&revised); err != nil || revised == eth.Title {
		t.Fatal("terminal overwrote independently approved collector content")
	}
	for _, query := range []url.Values{{"asset": {"ETH"}}, {"q": {"inflation"}}, {"category": {"crypto"}, "source": {"ethereum_foundation"}}} {
		f, err := parseTerminalFilter(query, terminalTestNow)
		if err != nil {
			t.Fatal(err)
		}
		items, truncated, err := s.readStored(ctx, f)
		if err != nil || truncated {
			t.Fatalf("query=%v err=%v truncated=%v", query, err, truncated)
		}
		page, total, _ := terminalPage(deduplicateTerminal(items, terminalTestNow), f, terminalTestNow)
		if len(page) != 1 || total != 1 {
			t.Fatalf("filtered persistent history failed %v %+v", query, page)
		}
	}
	// A disabled publisher's legacy row is never laundered into the terminal.
	if _, err := pool.Exec(ctx, `INSERT INTO news_items(category,title,url,source,source_id,published_at,publication_eligible) VALUES('crypto','Unlicensed headline','https://coindesk.com/1','CoinDesk','coindesk',$1,TRUE)`, terminalTestNow.Add(-time.Minute)); err != nil {
		t.Fatal(err)
	}
	f, _ := parseTerminalFilter(nil, terminalTestNow)
	items, _, err := s.readStored(ctx, f)
	if err != nil || len(items) != 2 {
		t.Fatalf("disabled source leaked into stored feed: %d %v", len(items), err)
	}
	for i := range s.sources {
		if s.sources[i].ID == "bls" {
			s.sources[i].LastAttempt = nullableTerminalTime(terminalTestNow)
			s.sources[i].LastSuccess = nullableTerminalTime(terminalTestNow)
			s.sources[i].ItemCount = 1
			s.sources[i].etag = "verified"
			s.sources[i].nextAttempt = terminalTestNow.Add(time.Hour)
			s.persistHealth(ctx, s.sources[i])
		}
	}
	restarted := newTerminalService(pool)
	restarted.now = s.now
	restarted.hydrate(ctx)
	if len(restarted.items) != 2 || restarted.storage != "postgres" {
		t.Fatalf("restart lost history: %+v", restarted)
	}
	for _, source := range restarted.sources {
		if source.ID == "bls" && (source.etag != "verified" || source.ItemCount != 1 || source.LastSuccess == nil) {
			t.Fatal("source health and conditional request state lost on restart")
		}
	}
}

func TestTerminalIndependentProviderFailurePreservesSuccessfulFeed(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "failure") {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		fmt.Fprint(w, `<rss><channel><item><title>Independent Bitcoin protocol update</title><link>https://example.org/independent</link><pubDate>Wed, 14 Oct 2026 11:00:00 GMT</pubDate></item></channel></rss>`)
	}))
	defer server.Close()
	s := newTerminalService(nil)
	s.now = func() time.Time { return terminalTestNow }
	s.sources = []terminalSource{{TerminalSourceHealth: TerminalSourceHealth{ID: "good", Name: "Good source", URL: server.URL + "/good", Enabled: true, IntervalSeconds: 300}, kind: "rss", category: "crypto"}, {TerminalSourceHealth: TerminalSourceHealth{ID: "bad", Name: "Bad source", URL: server.URL + "/failure", Enabled: true, IntervalSeconds: 300}, kind: "rss", category: "crypto"}}
	s.synchronize()
	if len(s.items) != 1 || s.healthLocked(terminalTestNow)[0].Status != "ok" || s.healthLocked(terminalTestNow)[1].Status != "error" {
		t.Fatal("a failing independent provider erased/blocked the valid feed")
	}
}

func TestTerminalPersistedLowScoresAreRecomputed(t *testing.T) {
	pool := terminalTestPool(t)
	s := newTerminalService(pool)
	s.now = func() time.Time { return terminalTestNow }
	ctx := context.Background()
	if _, err := pool.Exec(ctx, `INSERT INTO news_items(category,title,url,source,source_id,published_at,importance,terminal_collected,publication_eligible)
	 VALUES('macro','Minutes of the Federal Open Market Committee, September 15-16, 2026','https://www.federalreserve.gov/newsevents/pressreleases/monetary20261007a.htm','Federal Reserve','federal_reserve',$1,15,TRUE,FALSE)`, terminalTestNow.Add(-time.Hour)); err != nil {
		t.Fatal(err)
	}
	f, _ := parseTerminalFilter(url.Values{"importance": {"high"}}, terminalTestNow)
	items, _, err := s.readStored(ctx, f)
	if err != nil {
		t.Fatal(err)
	}
	page, _, _ := terminalPage(deduplicateTerminal(items, terminalTestNow), f, terminalTestNow)
	if len(page) != 1 || page[0].Importance != "high" || page[0].ImportanceScore != 65 {
		t.Fatalf("old persisted score controlled current classification: %+v", page)
	}
}
