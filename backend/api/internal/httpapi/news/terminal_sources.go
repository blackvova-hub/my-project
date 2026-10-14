package newsapi

import (
	"context"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

func terminalSources() []terminalSource {
	makeSource := func(id, name, feed, terms, category, kind, attribution string, official bool, seconds int) terminalSource {
		return terminalSource{TerminalSourceHealth: TerminalSourceHealth{ID: id, Name: name, URL: feed, TermsURL: terms, Enabled: true, Status: "pending", IntervalSeconds: seconds, Attribution: attribution}, category: category, kind: kind, official: official}
	}
	gdeltQuery := `(bitcoin OR ethereum OR cryptocurrency OR "interest rates" OR inflation OR "stock market" OR tariffs OR sanctions) sourcelang:english -domain:coindesk.com -domain:cointelegraph.com -domain:decrypt.co -domain:cryptoslate.com -domain:bbc.com -domain:bbc.co.uk`
	gdelt := "https://api.gdeltproject.org/api/v2/doc/doc?" + url.Values{"query": {gdeltQuery}, "mode": {"artlist"}, "format": {"json"}, "maxrecords": {"250"}, "timespan": {"24h"}, "sort": {"datedesc"}}.Encode()
	sources := []terminalSource{
		makeSource("ethereum_foundation", "Ethereum Foundation", "https://blog.ethereum.org/feed.xml", "https://ethereum.org/terms-of-use/", "crypto", "rss", "Ethereum Foundation and named author. CC BY 4.0 (https://creativecommons.org/licenses/by/4.0/); excerpt shortened. Original article linked.", true, 900),
		makeSource("bitcoin_core", "Bitcoin Core", "https://bitcoincore.org/en/rss.xml", "https://github.com/bitcoin-core/bitcoincore.org/blob/master/LICENSE", "crypto", "rss", "Bitcoin Core / respective authors. Website documentation under MIT license: https://github.com/bitcoin-core/bitcoincore.org/blob/master/LICENSE. Excerpt shortened.", true, 1800),
		makeSource("federal_reserve", "Federal Reserve", "https://www.federalreserve.gov/feeds/press_all.xml", "https://www.federalreserve.gov/disclaimer.htm", "macro", "rss", "Federal Reserve Board — official releases", true, 300),
		makeSource("ecb", "European Central Bank", "https://www.ecb.europa.eu/rss/press.html", "https://www.ecb.europa.eu/services/using-our-site/disclaimer/html/index.en.html", "macro", "rss", "Source: European Central Bank. Information is freely available at ecb.europa.eu.", true, 600),
		makeSource("bls", "Bureau of Labor Statistics", "https://www.bls.gov/feed/bls_latest.rss", "https://www.bls.gov/bls/linksite.htm", "macro", "rss", "U.S. Bureau of Labor Statistics — official releases", true, 900),
		makeSource("sec", "U.S. Securities and Exchange Commission", "https://www.sec.gov/news/pressreleases.rss", "https://www.sec.gov/about/rss-feeds", "markets", "rss", "U.S. Securities and Exchange Commission — official releases", true, 600),
		makeSource("gdelt", "GDELT / global publishers", gdelt, "https://www.gdeltproject.org/data.html", "world", "gdelt", "Data from the GDELT Project (gdeltproject.org). Publisher articles remain the property of their publishers.", false, 900),
	}
	// These publishers provide accessible feeds, but public commercial reuse is
	// not granted by their current terms. Opt-in requires operator permission;
	// merely being enabled in the legacy collector does not grant that right.
	licensed := map[string]bool{}
	for _, id := range strings.Split(os.Getenv("NEWS_TERMINAL_LICENSED_SOURCES"), ",") {
		licensed[strings.TrimSpace(id)] = true
	}
	for _, spec := range []struct{ id, name, feed, terms string }{
		{"coindesk", "CoinDesk", "https://www.coindesk.com/arc/outboundfeeds/rss/", "https://www.coindesk.com/terms"},
		{"cointelegraph", "Cointelegraph", "https://cointelegraph.com/rss", "https://cointelegraph.com/terms-and-privacy"},
		{"decrypt", "Decrypt", "https://decrypt.co/feed", "https://decrypt.co/terms-of-service"},
		{"cryptoslate", "CryptoSlate", "https://cryptoslate.com/feed/", "https://cryptoslate.com/terms-and-conditions/"},
		{"bbc", "BBC Business", "https://feeds.bbci.co.uk/news/business/rss.xml", "https://www.bbc.co.uk/usingthebbc/terms/"},
	} {
		category := "crypto"
		if spec.id == "bbc" {
			category = "markets"
		}
		source := makeSource(spec.id, spec.name, spec.feed, spec.terms, category, "rss", spec.name+" — original publication linked", false, 600)
		source.Enabled = licensed[spec.id]
		if !source.Enabled {
			source.Status, source.Reason = "disabled", "Для публичного использования этого источника требуется разрешение правообладателя."
		}
		sources = append(sources, source)
	}
	return sources
}

func (s *terminalService) fetch(ctx context.Context, source terminalSource) ([]TerminalNewsItem, http.Header, bool, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, source.URL, nil)
	if err != nil {
		return nil, nil, false, err
	}
	req.Header.Set("User-Agent", "ShortLongNewsTerminal/1.0 (+https://short-and-long.ru)")
	req.Header.Set("Accept", "application/rss+xml, application/atom+xml, application/xml, application/json;q=0.9")
	if source.etag != "" {
		req.Header.Set("If-None-Match", source.etag)
	}
	if source.lastModified != "" {
		req.Header.Set("If-Modified-Since", source.lastModified)
	}
	response, err := s.client.Do(req)
	if err != nil {
		return nil, nil, false, fmt.Errorf("source_request_failed: %s", source.Name)
	}
	defer response.Body.Close()
	if response.StatusCode == http.StatusNotModified {
		return nil, response.Header, true, nil
	}
	if response.StatusCode != http.StatusOK {
		return nil, response.Header, false, fmt.Errorf("HTTP %d from %s", response.StatusCode, source.Name)
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, (4<<20)+1))
	if err != nil || len(body) > 4<<20 {
		return nil, response.Header, false, fmt.Errorf("source_response_invalid: %s", source.Name)
	}
	var items []TerminalNewsItem
	if source.kind == "gdelt" {
		items, err = parseTerminalGDELT(body, source, s.now())
	} else {
		items, err = parseTerminalRSS(body, source, s.now())
	}
	if err != nil {
		return nil, response.Header, false, fmt.Errorf("source_parse_failed: %s", source.Name)
	}
	return items, response.Header, false, nil
}

type terminalRSS struct {
	XMLName xml.Name
	Channel struct {
		Language string `xml:"language"`
		Items    []struct {
			Title       string `xml:"title"`
			Link        string `xml:"link"`
			Description string `xml:"description"`
			Author      string `xml:"creator"`
			Published   string `xml:"pubDate"`
			Date        string `xml:"date"`
		} `xml:"item"`
	} `xml:"channel"`
	Entries []struct {
		Title string `xml:"title"`
		Links []struct {
			Href string `xml:"href,attr"`
			Rel  string `xml:"rel,attr"`
		} `xml:"link"`
		Summary   string `xml:"summary"`
		Published string `xml:"published"`
		Updated   string `xml:"updated"`
	} `xml:"entry"`
}

func parseTerminalRSS(body []byte, source terminalSource, now time.Time) ([]TerminalNewsItem, error) {
	var feed terminalRSS
	// U.S. official feeds may prefix UTF-8 XML with a BOM.
	body = []byte(strings.TrimPrefix(string(body), "\ufeff"))
	if err := xml.Unmarshal(body, &feed); err != nil {
		return nil, err
	}
	if feed.XMLName.Local != "rss" && feed.XMLName.Local != "feed" {
		return nil, fmt.Errorf("not an RSS/Atom feed")
	}
	out := make([]TerminalNewsItem, 0, min(len(feed.Channel.Items)+len(feed.Entries), 200))
	add := func(title, link, description, date, language, author string) {
		published, ok := terminalPublishedDate(date)
		if !ok || published.Before(now.Add(-terminalRetention)) || published.After(now.Add(5*time.Minute)) {
			return
		}
		if canonicalTerminalURL(link) == "" || strings.TrimSpace(title) == "" {
			return
		}
		if language == "" {
			language = "en"
		}
		item := TerminalNewsItem{Title: boundedTerminalText(title, 300), URL: link, Source: source.Name, SourceID: source.ID, Category: source.category, PublishedAt: published.UTC().Format(time.RFC3339), TimestampBasis: "published", Summary: boundedTerminalText(description, 420), Language: language, Attribution: source.Attribution}
		if author != "" {
			item.Attribution = boundedTerminalText(author, 140) + ". " + item.Attribution
		}
		enrichTerminalItem(&item, source.official, now)
		out = append(out, item)
	}
	for _, row := range feed.Channel.Items {
		date := row.Published
		if date == "" {
			date = row.Date
		}
		add(row.Title, row.Link, row.Description, date, feed.Channel.Language, row.Author)
		if len(out) >= 200 {
			break
		}
	}
	for _, row := range feed.Entries {
		link := ""
		for _, candidate := range row.Links {
			if candidate.Rel == "alternate" || candidate.Rel == "" {
				link = candidate.Href
				break
			}
		}
		// Atom updated is an update time, so items without published are skipped.
		add(row.Title, link, row.Summary, row.Published, "en", "")
		if len(out) >= 200 {
			break
		}
	}
	return out, nil
}

func terminalPublishedDate(raw string) (time.Time, bool) {
	// Go's generic RFC parser assigns unknown zone abbreviations a zero offset.
	// Resolve common official US feed zones explicitly instead of shifting them.
	for zone, offset := range map[string]int{"EST": -5, "EDT": -4, "CST": -6, "CDT": -5, "MST": -7, "MDT": -6, "PST": -8, "PDT": -7} {
		if strings.HasSuffix(strings.TrimSpace(raw), " "+zone) {
			location := time.FixedZone(zone, offset*3600)
			for _, layout := range []string{time.RFC1123, time.RFC822, "Mon, 2 Jan 2006 15:04:05 MST"} {
				if date, err := time.ParseInLocation(layout, strings.TrimSpace(raw), location); err == nil {
					return date.UTC(), true
				}
			}
			return time.Time{}, false
		}
	}
	for _, layout := range []string{time.RFC3339Nano, time.RFC1123Z, time.RFC1123, time.RFC822Z, time.RFC822, "Mon, 2 Jan 2006 15:04:05 -0700", "Mon, 2 Jan 2006 15:04:05 MST", "20060102T150405Z"} {
		if date, err := time.Parse(layout, strings.TrimSpace(raw)); err == nil {
			return date.UTC(), true
		}
	}
	return time.Time{}, false
}

func parseTerminalGDELT(body []byte, source terminalSource, now time.Time) ([]TerminalNewsItem, error) {
	var result struct {
		Articles []struct {
			Title    string `json:"title"`
			URL      string `json:"url"`
			SeenDate string `json:"seendate"`
			Domain   string `json:"domain"`
			Language string `json:"language"`
		} `json:"articles"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, err
	}
	if result.Articles == nil {
		return nil, fmt.Errorf("missing articles")
	}
	out := make([]TerminalNewsItem, 0, len(result.Articles))
	for _, row := range result.Articles {
		date, ok := terminalPublishedDate(row.SeenDate)
		canonical := canonicalTerminalURL(row.URL)
		if !ok || canonical == "" || date.Before(now.Add(-terminalRetention)) || date.After(now.Add(5*time.Minute)) {
			continue
		}
		domain := strings.ToLower(row.Domain)
		if u, err := url.Parse(canonical); err == nil {
			domain = u.Hostname()
		}
		// GDELT metadata does not waive a publisher's restrictions. Exclude
		// publishers whose terms were explicitly reviewed as restrictive.
		if restrictedTerminalDomain(domain) {
			continue
		}
		item := TerminalNewsItem{Title: boundedTerminalText(row.Title, 300), URL: row.URL, Source: domain, SourceID: source.ID, Category: "world", PublishedAt: date.Format(time.RFC3339), TimestampBasis: "indexed", Language: row.Language, Attribution: source.Attribution}
		if item.Title == "" {
			continue
		}
		enrichTerminalItem(&item, false, now)
		out = append(out, item)
	}
	return out, nil
}

func restrictedTerminalDomain(domain string) bool {
	for _, restricted := range []string{"coindesk.com", "cointelegraph.com", "decrypt.co", "cryptoslate.com", "bbc.com", "bbc.co.uk"} {
		if domain == restricted || strings.HasSuffix(domain, "."+restricted) {
			return true
		}
	}
	return false
}

type terminalKeywordRule struct {
	tag   string
	words []string
	score int
}

var terminalTopicRules = []terminalKeywordRule{
	{"monetary-policy", []string{"fomc", "federal open market committee", "interest rate", "policy rate", "discount rate", "federal funds", "monetary policy", "rate decision", "rate hike", "rate cut", "комитет по открытым рынкам", "комитета по открытым рынкам", "комитет по операциям на открытом рынке", "комитета по операциям на открытом рынке", "процентная ставк", "ключевая ставк", "учетная ставк", "учётная ставк", "ставка дисконтирования", "денежно-кредит"}, 35},
	{"inflation", []string{"inflation", "consumer price", "cpi", "core cpi", "pce", "personal consumption expenditure", "инфляц", "индекс потребительских цен", "базовый ипц", "ипц", "расходы на личное потребление"}, 30},
	{"employment", []string{"unemployment", "nonfarm", "non-farm", "non farm", "nfp", "payroll", "labor market", "labour market", "employment situation", "безработ", "рынок труда", "несельскохозяйствен", "вне сельского хозяйства"}, 30},
	{"growth", []string{"gross domestic product", "gdp", "recession", "industrial production", "ввп", "валовой внутренний продукт", "рецесси", "промышленное производств"}, 30},
	{"regulation", []string{"regulation", "regulatory", "securities and exchange", "enforcement", "регулиров", "sec "}, 15},
	{"systemic-risk", []string{"bank failure", "bank run", "sovereign default", "debt default", "payment default", "bond default", "defaults on", "trading halt", "emergency rate", "emergency liquidity", "emergency lending", "emergency meeting", "банкрот", "дефолт"}, 35},
	{"geopolitics", []string{"sanction", "tariff", "war ", "sanкц", "санкци", "тариф", "войн"}, 20},
	{"etf", []string{"etf", "exchange-traded"}, 15},
	{"liquidity", []string{"liquidity", "balance sheet", "quantitative", "ликвидност", "баланс фрс"}, 20},
	{"protocol-upgrade", []string{"mainnet upgrade", "mainnet activation", "mainnet hard fork", "mainnet launch", "activation on mainnet", "upgrade on mainnet", "обновление основной сети", "хардфорк основной сети", "активация в основной сети"}, 20},
	{"protocol-security", []string{"security alert", "security advisory", "critical vulnerability", "client vulnerability", "security incident", "critical security update", "критическая уязвимост", "критическое обновление безопасности", "инцидент безопасности", "предупреждение об уязвимост"}, 20},
	{"crypto", []string{"bitcoin", "ethereum", "cryptocurrency", "crypto ", "blockchain", "stablecoin", "биткоин", "эфириум", "крипт", "стейблкоин"}, 0},
	{"markets", []string{"stock market", "s&p 500", "nasdaq", "equities", "treasury yield", "bond yield", "акци", "облигац"}, 0},
}

func enrichTerminalItem(item *TerminalNewsItem, official bool, now time.Time) {
	text := " " + strings.ToLower(item.Title+" "+item.Summary) + " "
	item.Tags, item.Assets, item.ImportanceReason = []string{}, []string{}, []string{}
	item.ImportanceScore = 15
	item.ImportanceReason = append(item.ImportanceReason, "Базовая оценка публикации: 15 баллов.")
	if official {
		item.ImportanceScore += 15
		item.ImportanceReason = append(item.ImportanceReason, "Официальный источник: +15 баллов.")
	}
	maxTopicScore := 0
	strongestTopic := ""
	provenancePolicy := terminalPolicyProvenance(*item)
	for _, rule := range terminalTopicRules {
		if (rule.tag == "protocol-upgrade" && terminalTestnetAnnouncement(item.Title)) || (strings.HasPrefix(rule.tag, "protocol-") && item.Category != "crypto" && !terminalMember(item.SourceID, "ethereum_foundation", "bitcoin_core")) {
			continue
		}
		if terminalTopicMatches(text, rule.words...) || (rule.tag == "monetary-policy" && provenancePolicy != "") {
			item.Tags = append(item.Tags, rule.tag)
			if rule.score > maxTopicScore {
				maxTopicScore = rule.score
				strongestTopic = rule.tag
			}
		}
	}
	item.ImportanceScore += maxTopicScore
	if maxTopicScore > 0 {
		if strongestTopic == "monetary-policy" && provenancePolicy != "" && !terminalTopicMatches(text, terminalTopicRules[0].words...) {
			item.ImportanceReason = append(item.ImportanceReason, fmt.Sprintf("Официальный раздел первоисточника «%s»: +%d баллов. Другие совпадения не суммируются.", provenancePolicy, maxTopicScore))
		} else {
			item.ImportanceReason = append(item.ImportanceReason, fmt.Sprintf("Ключевые слова темы «%s»: +%d баллов. При нескольких совпадениях учитывается самая весомая тема.", terminalTopicLabel(strongestTopic), maxTopicScore))
		}
	} else {
		item.ImportanceReason = append(item.ImportanceReason, "Ключевые слова значимых макроэкономических тем и системных рисков не найдены.")
	}
	for _, rule := range []struct {
		asset string
		terms []string
	}{
		{"BTC", []string{"bitcoin", "биткоин", " btc "}}, {"ETH", []string{"ethereum", "эфириум", " eth "}},
		{"SOL", []string{"solana", " sol "}}, {"XRP", []string{" xrp ", "ripple"}}, {"BNB", []string{" bnb ", "binance coin"}},
		{"SPX", []string{"s&p 500", "s&p500", "s&p 500"}}, {"NDX", []string{"nasdaq"}}, {"DXY", []string{"dollar index", " dxy "}},
		{"XAU", []string{" gold ", "золот"}}, {"USD", []string{"dollar", "federal reserve", "fomc", "доллар", "фрс"}}, {"EUR", []string{"ecb", "european central bank", " euro ", "ецб"}},
	} {
		if terminalContainsAny(text, rule.terms...) {
			item.Assets = append(item.Assets, rule.asset)
		}
	}
	if item.SourceID == "gdelt" {
		switch {
		case terminalMember("crypto", item.Tags...):
			item.Category = "crypto"
		case terminalMember("monetary-policy", item.Tags...) || terminalMember("inflation", item.Tags...) || terminalMember("employment", item.Tags...) || terminalMember("growth", item.Tags...):
			item.Category = "macro"
		case terminalMember("markets", item.Tags...):
			item.Category = "markets"
		default:
			item.Category = "world"
		}
	}
	if item.SourceID == "federal_reserve" && !terminalMember("USD", item.Assets...) {
		item.Assets = append(item.Assets, "USD")
	}
	if item.SourceID == "ecb" && !terminalMember("EUR", item.Assets...) {
		item.Assets = append(item.Assets, "EUR")
	}
	if item.SourceID == "ethereum_foundation" && !terminalMember("ETH", item.Assets...) {
		item.Assets = append(item.Assets, "ETH")
	}
	if item.SourceID == "bitcoin_core" && !terminalMember("BTC", item.Assets...) {
		item.Assets = append(item.Assets, "BTC")
	}
	if item.SourceID == "sec" && terminalMember("crypto", item.Tags...) {
		item.Category = "crypto"
	}
	item.Importance = "low"
	if item.ImportanceScore >= 60 {
		item.Importance = "high"
	} else if item.ImportanceScore >= 35 {
		item.Importance = "medium"
	}
	date, _ := time.Parse(time.RFC3339, item.PublishedAt)
	title := strings.TrimSpace(strings.ToLower(item.Title))
	explicitBreaking := strings.HasPrefix(title, "breaking:") || strings.HasPrefix(title, "breaking news:") || strings.HasPrefix(title, "срочно:")
	item.Breaking = explicitBreaking && item.TimestampBasis == "published" && !date.IsZero() && now.Sub(date) <= terminalBreakingWindow && !date.After(now)
}

func terminalTopicMatches(text string, words ...string) bool {
	for _, word := range words {
		if terminalMember(word, "fomc", "cpi", "pce", "gdp", "nfp", "ипц", "ввп", "etf") {
			if strings.Contains(" "+normalizeTerminalHeadline(text)+" ", " "+word+" ") {
				return true
			}
		} else if strings.Contains(text, word) {
			return true
		}
	}
	return false
}

func terminalTestnetAnnouncement(title string) bool {
	return terminalContainsAny(strings.ToLower(title), "testnet", "sepolia", "hoodi", "holesky", "goerli", "rinkeby", "тестов")
}

// Generic meeting titles acquire policy meaning only from the audited official
// site's dedicated policy archive, never merely from an ECB/Fed source label.
func terminalPolicyProvenance(item TerminalNewsItem) string {
	u, err := url.Parse(item.URL)
	if err != nil {
		return ""
	}
	host := strings.TrimPrefix(strings.ToLower(u.Hostname()), "www.")
	if item.SourceID == "ecb" && host == "ecb.europa.eu" && strings.HasPrefix(u.Path, "/press/accounts/") {
		return "отчёты заседаний ЕЦБ по денежно-кредитной политике"
	}
	if item.SourceID == "federal_reserve" && host == "federalreserve.gov" && strings.HasPrefix(u.Path, "/newsevents/pressreleases/monetary") {
		return "сообщения ФРС по денежно-кредитной политике"
	}
	return ""
}

func terminalTopicLabel(tag string) string {
	if label, ok := map[string]string{"monetary-policy": "денежно-кредитная политика", "inflation": "инфляция", "employment": "рынок труда", "growth": "экономический рост", "regulation": "регулирование", "systemic-risk": "системные риски", "geopolitics": "геополитика", "etf": "ETF", "liquidity": "ликвидность", "protocol-upgrade": "обновление основной сети", "protocol-security": "безопасность протокола"}[tag]; ok {
		return label
	}
	return tag
}

func terminalContainsAny(text string, words ...string) bool {
	for _, word := range words {
		if strings.Contains(text, word) {
			return true
		}
	}
	return false
}
