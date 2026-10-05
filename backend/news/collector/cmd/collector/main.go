package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"html"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"

	newsources "news-bot/internal/sources"
	"news-bot/internal/sources/telegrampreview"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/mmcdole/gofeed"
)

const rssUserAgent = "Mozilla/5.0 (compatible; ShortLongNewsBot/1.0; +https://short-and-long.ru)"

const defaultWuBlockchainRSSURL = "https://www.wublockchain.xyz/rss"

const newsDedupAdvisoryLockID int64 = 0x4e45575344454455

const feedCooldownDuration = 3 * time.Minute
const summaryInterval = time.Hour
const summaryWindow = 12 * time.Hour

var feedCooldown = struct {
	mu    sync.Mutex
	until map[string]time.Time
}{until: map[string]time.Time{}}

var timewebCooldown = struct {
	mu    sync.Mutex
	until map[string]time.Time
}{until: map[string]time.Time{}}

var htmlTagRe = regexp.MustCompile(`<[^>]+>`)
var bybitDateCandidateRe = regexp.MustCompile(`(?i)(\b(?:jan(?:uary)?|feb(?:ruary)?|mar(?:ch)?|apr(?:il)?|may|jun(?:e)?|jul(?:y)?|aug(?:ust)?|sep(?:t(?:ember)?)?|oct(?:ober)?|nov(?:ember)?|dec(?:ember)?)\s+\d{1,2}(?:st|nd|rd|th)?,\s*20\d{2}(?:\s+at)?(?:\s+\d{1,2}(?::\d{2})?\s*(?:am|pm)?)?(?:\s*(?:utc|gmt))?\b|\b\d{1,2}(?:st|nd|rd|th)?\s+(?:jan(?:uary)?|feb(?:ruary)?|mar(?:ch)?|apr(?:il)?|may|jun(?:e)?|jul(?:y)?|aug(?:ust)?|sep(?:t(?:ember)?)?|oct(?:ober)?|nov(?:ember)?|dec(?:ember)?)\s+20\d{2}(?:\s+at)?(?:\s+\d{1,2}(?::\d{2})?\s*(?:am|pm)?)?(?:\s*(?:utc|gmt))?\b|\b20\d{2}-\d{2}-\d{2}(?:[ t]\d{1,2}:\d{2}(?::\d{2})?)?(?:\s*(?:utc|gmt|z))?\b)`)
var bybitOrdinalDayRe = regexp.MustCompile(`(?i)(\d{1,2})(st|nd|rd|th)\b`)

type Config struct {
	PGDSN                        string
	FetchIntervalMinutes         int
	SourceIntervalMinutes        int
	SourceStaggerMinutes         int
	DedupWindowHours             int
	CryptoPanicRSSURL            string
	CoinDeskRSSURL               string
	DecryptRSSURL                string
	GDELTRSSURL                  string
	BBCWorldRSSURL               string
	WuBlockchainRSSURL           string
	CryptoPanicAPIURL            string
	CryptoPanicAPIToken          string
	CryptoPanicLimit             int
	CoinDeskLimit                int
	DecryptLimit                 int
	GDELTLimit                   int
	BBCWorldLimit                int
	WuBlockchainLimit            int
	WuBlockchainMaxAgeHours      int
	ExtraRSSCategory             string
	ExtraRSSLimit                int
	ExtraRSSURLs                 []string
	BybitLimit                   int
	BinanceListingsURL           string
	BinanceDelistingsURL         string
	BinanceArticleDetailURL      string
	BinanceLimit                 int
	CointelegraphEnabled         bool
	CointelegraphRSSURL          string
	CointelegraphLimit           int
	BybitAPIEnabled              bool
	BybitAPIURL                  string
	SECEnabled                   bool
	SECRSSURLs                   []string
	SECEDGAREnabled              bool
	SECCompanyCIKs               []string
	FederalReserveEnabled        bool
	FederalReserveURLs           []string
	WhiteHouseEnabled            bool
	WhiteHouseRSSURL             string
	FederalRegisterEnabled       bool
	FederalRegisterURL           string
	UpbitEnabled                 bool
	UpbitNoticesURL              string
	BithumbEnabled               bool
	BithumbNoticesURL            string
	SoSoValueEnabled             bool
	SoSoValueAPIKey              string
	SoSoValueAPIURL              string
	SoSoValueCurrentAPIURL       string
	SoSoValueInterval            time.Duration
	FearGreedEnabled             bool
	FearGreedURL                 string
	FearGreedDailyHourUTC        int
	FearGreedInterval            time.Duration
	DefiLlamaEnabled             bool
	DefiLlamaProtocolsURL        string
	DefiLlamaMinChangePct        float64
	TelegramCryptoAttackEnabled  bool
	TelegramCryptoAttackURL      string
	TelegramCryptoAttackLimit    int
	TelegramCryptoAttackInterval time.Duration
	FilterInclude                []string
	FilterExclude                []string
	TimewebAPIKey                string
	TimewebAgentAccessID         string
	TimewebSummaryAgentAccessID  string
	TimewebBaseURL               string
}

type NewsItem struct {
	Category            string
	Title               string
	TitleNorm           string
	URL                 string
	URLHash             string
	Source              string
	Published           time.Time
	Summary             string
	Image               string
	CalendarOnly        bool
	OriginalTitle       string
	OriginalText        string
	OriginalLanguage    string
	TranslatedTitle     string
	TranslatedSummary   string
	SourceID            string
	SourceType          string
	EventAt             *time.Time
	EventType           string
	PublishedFallback   bool
	Entities            []string
	Assets              []string
	PublicationEligible bool
}

func main() {
	cfg := loadConfig()
	if strings.TrimSpace(cfg.PGDSN) == "" {
		log.Fatal("PG_DSN is required")
	}

	ctx, stop := signalContext()
	defer stop()

	pool, err := pgxpool.New(ctx, cfg.PGDSN)
	if err != nil {
		log.Fatalf("pg connect: %v", err)
	}
	defer pool.Close()

	if err := pool.Ping(ctx); err != nil {
		log.Fatalf("pg ping failed: %v", err)
	}

	sourceInterval := time.Duration(cfg.SourceIntervalMinutes) * time.Minute
	if sourceInterval <= 0 {
		sourceInterval = 5 * time.Minute
	}
	stagger := time.Duration(cfg.SourceStaggerMinutes) * time.Minute
	if stagger < 0 {
		stagger = 0
	}
	log.Printf("news-bot started sourceInterval=%s stagger=%s", sourceInterval, stagger)

	sources := buildSources(cfg)
	if len(sources) == 0 {
		log.Printf("news-bot: no sources configured")
	}

	cleanupTicker := time.NewTicker(6 * time.Hour)
	defer cleanupTicker.Stop()
	go func() {
		if err := cleanupOldNews(ctx, pool, 30); err != nil {
			log.Printf("news cleanup failed: %v", err)
		}
		for {
			select {
			case <-ctx.Done():
				return
			case <-cleanupTicker.C:
				if err := cleanupOldNews(ctx, pool, 30); err != nil {
					log.Printf("news cleanup failed: %v", err)
				}
			}
		}
	}()

	go func() {
		if err := maybeGenerateSummary(ctx, pool, cfg, true); err != nil {
			log.Printf("news summary failed: %v", err)
		}
		ticker := time.NewTicker(summaryInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if err := maybeGenerateSummary(ctx, pool, cfg, false); err != nil {
					log.Printf("news summary failed: %v", err)
				}
			}
		}
	}()

	if stagger > 0 && sourceInterval <= stagger {
		log.Printf("news-bot: sequential source mode enabled (stagger=%s)", stagger)
		go runSourcesSequentially(ctx, pool, cfg, sources, stagger)
	} else {
		for i, src := range sources {
			offset := time.Duration(i) * stagger
			go runSourceLoop(ctx, pool, cfg, src, sourceInterval, offset)
		}
	}

	<-ctx.Done()
}

type sourceSpec struct {
	name        string
	minInterval time.Duration
	fetch       func(context.Context) ([]NewsItem, error)
}

func buildSources(cfg Config) []sourceSpec {
	sources := make([]sourceSpec, 0, 20)
	appendConnector := func(connector newsources.Source, minInterval time.Duration) {
		sources = append(sources, sourceSpec{name: connector.ID(), minInterval: minInterval, fetch: func(ctx context.Context) ([]NewsItem, error) {
			rows, err := connector.Fetch(ctx)
			items := make([]NewsItem, 0, len(rows))
			for _, row := range rows {
				items = append(items, fromSourceItem(row))
			}
			return items, err
		}})
	}
	if cfg.CointelegraphEnabled {
		appendConnector(&newsources.Cointelegraph{URL: cfg.CointelegraphRSSURL, Limit: cfg.CointelegraphLimit}, 0)
	}
	if cfg.SECEnabled {
		appendConnector(&newsources.SEC{URLs: cfg.SECRSSURLs, Limit: 50}, 0)
	}
	if cfg.SECEDGAREnabled && len(cfg.SECCompanyCIKs) > 0 {
		appendConnector(&newsources.EDGAR{CIKs: cfg.SECCompanyCIKs}, 5*time.Minute)
	}
	if cfg.FederalReserveEnabled {
		appendConnector(&newsources.FederalReserve{URLs: cfg.FederalReserveURLs, Limit: 50}, 0)
	}
	if cfg.WhiteHouseEnabled {
		appendConnector(&newsources.WhiteHouse{URL: cfg.WhiteHouseRSSURL, Limit: 50}, 0)
	}
	if cfg.FederalRegisterEnabled {
		appendConnector(&newsources.FederalRegister{URL: cfg.FederalRegisterURL, Limit: 20}, 5*time.Minute)
	}
	if cfg.UpbitEnabled {
		appendConnector(&newsources.Upbit{URL: cfg.UpbitNoticesURL, Limit: 30}, 0)
	}
	if cfg.BithumbEnabled {
		appendConnector(&newsources.Bithumb{URL: cfg.BithumbNoticesURL, Limit: 20}, 0)
	}
	if cfg.SoSoValueEnabled {
		appendConnector(&newsources.SoSoValue{APIKey: cfg.SoSoValueAPIKey, URL: cfg.SoSoValueAPIURL, CurrentURL: cfg.SoSoValueCurrentAPIURL}, cfg.SoSoValueInterval)
	}
	if cfg.FearGreedEnabled {
		appendConnector(&newsources.FearGreed{URL: cfg.FearGreedURL, DailyHour: cfg.FearGreedDailyHourUTC}, cfg.FearGreedInterval)
	}
	if cfg.DefiLlamaEnabled {
		appendConnector(&newsources.DefiLlama{ProtocolsURL: cfg.DefiLlamaProtocolsURL, MinChangePct: cfg.DefiLlamaMinChangePct}, time.Hour)
	}
	if strings.TrimSpace(cfg.WuBlockchainRSSURL) != "" {
		maxAge := time.Duration(cfg.WuBlockchainMaxAgeHours) * time.Hour
		if maxAge <= 0 {
			maxAge = 72 * time.Hour
		}
		sources = append(sources, sourceSpec{
			name: "wublockchain_rss",
			fetch: func(ctx context.Context) ([]NewsItem, error) {
				return fetchFreshRSS(ctx, cfg.WuBlockchainRSSURL, "crypto", cfg.WuBlockchainLimit, maxAge)
			},
		})
	}
	if strings.TrimSpace(cfg.CryptoPanicAPIToken) != "" {
		sources = append(sources, sourceSpec{
			name: "cryptopanic_api",
			fetch: func(ctx context.Context) ([]NewsItem, error) {
				return fetchCryptoPanicAPI(ctx, cfg, cfg.CryptoPanicLimit)
			},
		})
	} else if strings.TrimSpace(cfg.CryptoPanicRSSURL) != "" {
		sources = append(sources, sourceSpec{
			name: "cryptopanic_rss",
			fetch: func(ctx context.Context) ([]NewsItem, error) {
				return fetchRSS(ctx, cfg.CryptoPanicRSSURL, "crypto", cfg.CryptoPanicLimit)
			},
		})
	}
	if strings.TrimSpace(cfg.CoinDeskRSSURL) != "" {
		sources = append(sources, sourceSpec{
			name: "coindesk_rss",
			fetch: func(ctx context.Context) ([]NewsItem, error) {
				return fetchRSS(ctx, cfg.CoinDeskRSSURL, "crypto", cfg.CoinDeskLimit)
			},
		})
	}
	if strings.TrimSpace(cfg.DecryptRSSURL) != "" {
		sources = append(sources, sourceSpec{
			name: "decrypt_rss",
			fetch: func(ctx context.Context) ([]NewsItem, error) {
				return fetchRSS(ctx, cfg.DecryptRSSURL, "crypto", cfg.DecryptLimit)
			},
		})
	}
	if strings.TrimSpace(cfg.GDELTRSSURL) != "" {
		sources = append(sources, sourceSpec{
			name: "gdelt_rss",
			fetch: func(ctx context.Context) ([]NewsItem, error) {
				return fetchRSS(ctx, cfg.GDELTRSSURL, "world", cfg.GDELTLimit)
			},
		})
	}
	if strings.TrimSpace(cfg.BBCWorldRSSURL) != "" {
		sources = append(sources, sourceSpec{
			name: "bbc_world_rss",
			fetch: func(ctx context.Context) ([]NewsItem, error) {
				return fetchRSS(ctx, cfg.BBCWorldRSSURL, "world", cfg.BBCWorldLimit)
			},
		})
	}
	if len(cfg.ExtraRSSURLs) > 0 {
		category := strings.TrimSpace(cfg.ExtraRSSCategory)
		if category == "" {
			category = "world"
		}
		limit := cfg.ExtraRSSLimit
		if limit <= 0 {
			limit = 50
		}
		idx := 1
		for _, raw := range cfg.ExtraRSSURLs {
			url := strings.TrimSpace(raw)
			if url == "" {
				continue
			}
			name := fmt.Sprintf("extra_rss_%d", idx)
			idx++
			sources = append(sources, sourceSpec{
				name: name,
				fetch: func(ctx context.Context) ([]NewsItem, error) {
					return fetchRSS(ctx, url, category, limit)
				},
			})
		}
	}
	if cfg.BybitAPIEnabled {
		appendConnector(&newsources.Bybit{URL: cfg.BybitAPIURL, Limit: cfg.BybitLimit}, 0)
	}
	if cfg.TelegramCryptoAttackEnabled {
		telegramParser := parsertelegram.New(cfg.TelegramCryptoAttackURL, cfg.TelegramCryptoAttackLimit)
		appendConnector(telegramParser, cfg.TelegramCryptoAttackInterval)
	}
	if strings.TrimSpace(cfg.BinanceListingsURL) != "" || strings.TrimSpace(cfg.BinanceDelistingsURL) != "" {
		sources = append(sources, sourceSpec{
			name: "binance_announcements",
			fetch: func(ctx context.Context) ([]NewsItem, error) {
				return fetchBinanceAnnouncements(ctx, cfg)
			},
		})
	}
	return sources
}

func fromSourceItem(row newsources.Item) NewsItem {
	normalURL := normalizeURL(row.URL)
	originalTitle := row.OriginalTitle
	if strings.TrimSpace(originalTitle) == "" {
		originalTitle = row.Title
	}
	originalText := row.OriginalText
	if strings.TrimSpace(originalText) == "" {
		originalText = row.Summary
	}
	return NewsItem{Category: row.Category, Title: row.Title, TitleNorm: normalizeTitle(row.Title), URL: normalURL, URLHash: hashURL(normalURL), Source: row.Source, Published: row.PublishedAt.UTC(), Summary: row.Summary, Image: row.Image, CalendarOnly: row.CalendarOnly, OriginalTitle: originalTitle, OriginalText: originalText, OriginalLanguage: row.OriginalLanguage, SourceID: row.SourceID, SourceType: row.SourceType, EventAt: row.EventAt, EventType: row.EventType, PublishedFallback: row.PublishedFallback, Entities: row.Entities, Assets: row.Assets}
}

func runSourceLoop(ctx context.Context, pool *pgxpool.Pool, cfg Config, src sourceSpec, interval, offset time.Duration) {
	if src.minInterval > interval {
		interval = src.minInterval
	}
	if offset > 0 {
		timer := time.NewTimer(offset)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
	}
	if err := fetchAndStoreSource(ctx, pool, cfg, src); err != nil {
		log.Printf("news-bot source %s failed: %v", src.name, err)
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := fetchAndStoreSource(ctx, pool, cfg, src); err != nil {
				log.Printf("news-bot source %s failed: %v", src.name, err)
			}
		}
	}
}

func runSourcesSequentially(ctx context.Context, pool *pgxpool.Pool, cfg Config, sources []sourceSpec, stagger time.Duration) {
	if stagger <= 0 {
		stagger = time.Minute
	}
	lastRun := make(map[string]time.Time, len(sources))
	for {
		for _, src := range sources {
			if ctx.Err() != nil {
				return
			}
			if src.minInterval > 0 && time.Since(lastRun[src.name]) < src.minInterval {
				select {
				case <-ctx.Done():
					return
				case <-time.After(stagger):
				}
				continue
			}
			if err := fetchAndStoreSource(ctx, pool, cfg, src); err != nil {
				log.Printf("news-bot source %s failed: %v", src.name, err)
			}
			lastRun[src.name] = time.Now()
			select {
			case <-ctx.Done():
				return
			case <-time.After(stagger):
			}
		}
	}
}

func fetchAndStoreSource(ctx context.Context, pool *pgxpool.Pool, cfg Config, src sourceSpec) error {
	fetchCtx, fetchCancel := context.WithTimeout(ctx, 25*time.Second)
	defer fetchCancel()

	items, err := src.fetch(fetchCtx)
	if err != nil {
		return err
	}
	if len(items) == 0 {
		return nil
	}

	inserted := 0
	deduplicated := 0
	for _, item := range items {
		if item.Published.IsZero() {
			log.Printf("news source=%s skipped: published_at missing url=%s", src.name, item.URL)
			continue
		}
		if item.PublishedFallback {
			log.Printf("news source=%s published_at uses documented fallback url=%s", src.name, item.URL)
		}
		if !validOfficialExchangeAnnouncement(item) && !matchesFilter(cfg, item.Title, item.Summary) {
			continue
		}
		item.PublicationEligible = isPublicationEligible(cfg, item, time.Now().UTC())
		if !item.PublicationEligible {
			continue
		}
		existsCtx, existsCancel := context.WithTimeout(ctx, 3*time.Second)
		exists, existsErr := existsURLHash(existsCtx, pool, item.URLHash)
		existsCancel()
		if existsErr != nil {
			log.Printf("news duplicate URL check failed url=%s err=%v", item.URL, existsErr)
			continue
		}
		if exists {
			continue
		}
		translateCtx, translateCancel := context.WithTimeout(ctx, 15*time.Second)
		if item.OriginalTitle == "" {
			item.OriginalTitle = item.Title
		}
		if item.OriginalText == "" {
			item.OriginalText = item.Summary
		}
		item = maybeTranslateItem(translateCtx, cfg, item)
		translateCancel()
		if item.URLHash == "" || item.TitleNorm == "" {
			continue
		}
		insertCtx, insertCancel := context.WithTimeout(ctx, 5*time.Second)
		insertedNow, err := insertNewsItemIfUnique(insertCtx, pool, item, time.Duration(cfg.DedupWindowHours)*time.Hour)
		if err != nil {
			insertCancel()
			log.Printf("news insert failed url=%s err=%v", item.URL, err)
			continue
		}
		insertCancel()
		if !insertedNow {
			deduplicated++
			continue
		}
		inserted++
	}

	if inserted > 0 || deduplicated > 0 {
		log.Printf("news-bot source %s inserted=%d deduplicated=%d", src.name, inserted, deduplicated)
	}

	summaryCtx, summaryCancel := context.WithTimeout(ctx, 45*time.Second)
	if err := maybeGenerateSummary(summaryCtx, pool, cfg, false); err != nil {
		log.Printf("news summary failed: %v", err)
	}
	summaryCancel()
	return nil
}

func validOfficialExchangeAnnouncement(item NewsItem) bool {
	return validOfficialExchangeAnnouncementAt(item, time.Now().UTC())
}

func validOfficialExchangeAnnouncementAt(item NewsItem, now time.Time) bool {
	eventAt := item.EventAt
	if item.SourceType != "official_exchange" || item.EventAt == nil || len(item.Assets) == 0 {
		return false
	}
	if eventAt.Before(now.Add(-5 * time.Minute)) {
		return false
	}
	if eventAt.After(now.Add(7 * 24 * time.Hour)) {
		return false
	}
	switch item.EventType {
	case "spot_listing", "futures_listing", "delisting", "trading_suspension":
		return true
	default:
		return false
	}
}

var cryptoPublicationTerms = []string{
	"bitcoin", "btc", "ethereum", "ether", "solana", "xrp", "dogecoin", "doge",
	"cryptocurrency", "cryptocurrencies", "crypto", "digital asset", "virtual asset", "token", "tokenized",
	"blockchain", "stablecoin", "defi", "decentralized finance", "web3", "on-chain", "onchain", "wallet",
	"mining", "staking", "airdrop", "layer 2", "layer-2", "smart contract", "spot etf", "crypto etf",
	"coinbase", "binance", "bybit", "upbit", "bithumb", "kraken", "okx", "bitget", "crypto exchange",
	"крипто", "биткоин", "эфириум", "стейблкоин", "блокчейн", "токен", "майнинг", "стейкинг", "криптобирж",
	"가상자산", "디지털 자산", "비트코인", "이더리움", "스테이블코인", "블록체인", "거래지원", "입출금",
}

var financePublicationTerms = []string{
	"federal reserve", "fomc", "interest rate", "policy rate", "rate cut", "rate hike", "monetary policy",
	"inflation", "consumer price index", "cpi", "producer price index", "ppi", "core pce", "nonfarm payroll",
	"unemployment", "gdp", "recession", "treasury", "bond market", "yield curve", "financial stability",
	"banking crisis", "bank failure", "liquidity", "securities market", "capital market", "stock market",
	"exchange-traded fund", "etf", "sec enforcement", "securities and exchange commission", "cftc",
	"sanction", "tariff", "trade war", "export control", "capital control", "debt ceiling", "government shutdown",
	"федеральная резервная", "фрс", "ключевая ставка", "процентная ставка", "инфляц", "денежно-кредитн",
	"фондовый рынок", "финансовый рынок", "санкц", "тариф", "госдолг", "безработиц", "рецесс",
}

func isPublicationEligible(cfg Config, item NewsItem, now time.Time) bool {
	if !matchesFilter(cfg, item.Title, item.Summary) {
		return false
	}
	if malformedNewsText(item.Title) || malformedNewsText(item.Summary) {
		return false
	}
	if validOfficialExchangeAnnouncementAt(item, now) {
		return true
	}
	if item.SourceType == "telegram" && strings.EqualFold(strings.TrimSpace(item.Category), "crypto") {
		return true
	}
	text := strings.ToLower(strings.TrimSpace(item.Title + " " + item.Summary))
	switch strings.ToLower(strings.TrimSpace(item.Category)) {
	case "crypto":
		return containsPublicationTerm(text, cryptoPublicationTerms)
	case "macro":
		return containsPublicationTerm(text, cryptoPublicationTerms) || containsPublicationTerm(text, financePublicationTerms)
	default:
		return false
	}
}

func containsPublicationTerm(text string, terms []string) bool {
	for _, term := range terms {
		if containsTopicalTerm(text, term) {
			return true
		}
	}
	return false
}

func containsTopicalTerm(text, term string) bool {
	if strings.ContainsAny(term, " -_") || !asciiWord(term) {
		return strings.Contains(text, term)
	}
	for start := 0; start < len(text); {
		index := strings.Index(text[start:], term)
		if index < 0 {
			return false
		}
		index += start
		beforeOK := index == 0 || !asciiWordByte(text[index-1])
		after := index + len(term)
		afterOK := after == len(text) || !asciiWordByte(text[after])
		if beforeOK && afterOK {
			return true
		}
		start = index + 1
	}
	return false
}

func asciiWord(value string) bool {
	if value == "" {
		return false
	}
	for index := range value {
		if !asciiWordByte(value[index]) {
			return false
		}
	}
	return true
}

func asciiWordByte(value byte) bool {
	return value >= 'a' && value <= 'z' || value >= '0' && value <= '9'
}

func malformedNewsText(value string) bool {
	value = strings.TrimSpace(value)
	if value == "" {
		return false
	}
	lower := strings.ToLower(value)
	return strings.HasPrefix(lower, `{"node":`) || strings.HasPrefix(lower, `{"type":`) ||
		strings.Contains(lower, `"data-block-id"`) || strings.Contains(lower, `"children":[{"node":`)
}

func matchesFilter(cfg Config, title, summary string) bool {
	text := strings.ToLower(strings.TrimSpace(title + " " + summary))
	if text == "" {
		return false
	}
	if len(cfg.FilterInclude) > 0 {
		matched := false
		for _, kw := range cfg.FilterInclude {
			if kw != "" && strings.Contains(text, kw) {
				matched = true
				break
			}
		}
		if !matched {
			return false
		}
	}
	if len(cfg.FilterExclude) > 0 {
		for _, kw := range cfg.FilterExclude {
			if kw != "" && strings.Contains(text, kw) {
				return false
			}
		}
	}
	return true
}

func cleanupOldNews(ctx context.Context, pool *pgxpool.Pool, days int) error {
	if days <= 0 {
		days = 30
	}
	_, err := pool.Exec(ctx, `
		DELETE FROM news_items
		WHERE published_at < now() - ($1 * interval '1 day')
	`, days)
	return err
}

func maybeGenerateSummary(ctx context.Context, pool *pgxpool.Pool, cfg Config, force bool) error {
	if strings.TrimSpace(cfg.TimewebAPIKey) == "" || strings.TrimSpace(cfg.TimewebSummaryAgentAccessID) == "" {
		if force {
			log.Printf("news summary skipped: timeweb config missing")
		}
		return nil
	}

	lastEnd, err := loadLastSummaryEnd(ctx, pool)
	if err != nil {
		return err
	}
	if !force && lastEnd != nil && time.Since(*lastEnd) < summaryInterval {
		return nil
	}

	periodEnd := time.Now().UTC()
	periodStart := periodEnd.Add(-summaryWindow)
	items, err := loadNewsForSummary(ctx, pool, periodStart)
	if err != nil {
		return err
	}
	if len(items) == 0 {
		if force {
			log.Printf("news summary skipped: no crypto items in window")
		}
		return nil
	}

	summary, ok := generateNewsSummary(ctx, cfg, items)
	if !ok {
		log.Printf("news summary skipped: timeweb response empty")
		return nil
	}

	_, err = pool.Exec(ctx, `
		INSERT INTO news_summaries (period_start, period_end, summary)
		VALUES ($1, $2, $3)
	`, periodStart, periodEnd, stripPostgresNUL(summary))
	return err
}

func loadLastSummaryEnd(ctx context.Context, pool *pgxpool.Pool) (*time.Time, error) {
	var last time.Time
	err := pool.QueryRow(ctx, `
		SELECT period_end FROM news_summaries
		ORDER BY period_end DESC
		LIMIT 1
	`).Scan(&last)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	return &last, nil
}

type summaryItem struct {
	Title   string
	Summary string
}

func loadNewsForSummary(ctx context.Context, pool *pgxpool.Pool, from time.Time) ([]summaryItem, error) {
	rows, err := pool.Query(ctx, `
		SELECT title, COALESCE(summary, '')
		FROM news_items
		WHERE category = 'crypto' AND publication_eligible = TRUE AND published_at >= $1 AND calendar_only = FALSE
		ORDER BY published_at DESC
		LIMIT 400
	`, from)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]summaryItem, 0, 80)
	for rows.Next() {
		var title, summary string
		if err := rows.Scan(&title, &summary); err != nil {
			return nil, err
		}
		title = strings.TrimSpace(title)
		summary = strings.TrimSpace(summary)
		if title == "" {
			continue
		}
		items = append(items, summaryItem{Title: title, Summary: summary})
	}
	return items, nil
}

func generateNewsSummary(ctx context.Context, cfg Config, items []summaryItem) (string, bool) {
	lines := make([]string, 0, len(items))
	for _, item := range items {
		if item.Summary != "" {
			lines = append(lines, fmt.Sprintf("- %s — %s", item.Title, item.Summary))
		} else {
			lines = append(lines, fmt.Sprintf("- %s", item.Title))
		}
	}
	text, ok := callTimewebText(ctx, cfg, cfg.TimewebSummaryAgentAccessID, buildSummaryPayload(lines))
	if !ok {
		return "", false
	}
	return strings.TrimSpace(text), true
}

func buildSummaryPayload(lines []string) string {
	return strings.Join(lines, "\n")
}

func callTimewebText(ctx context.Context, cfg Config, agentAccessID, message string) (string, bool) {
	if strings.TrimSpace(cfg.TimewebAPIKey) == "" || strings.TrimSpace(agentAccessID) == "" {
		return "", false
	}
	agentAccessID = strings.TrimSpace(agentAccessID)
	if timewebAgentCoolingDown(agentAccessID) {
		return "", false
	}
	baseURL := strings.TrimSpace(cfg.TimewebBaseURL)
	if baseURL == "" {
		baseURL = "https://agent.timeweb.cloud"
	}
	endpoint := fmt.Sprintf("%s/api/v1/cloud-ai/agents/%s/call", strings.TrimRight(baseURL, "/"), url.PathEscape(agentAccessID))

	payload := map[string]any{
		"message": message,
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return "", false
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(string(body)))
	if err != nil {
		return "", false
	}
	req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(cfg.TimewebAPIKey))
	req.Header.Set("x-proxy-source", "news-bot")
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 40 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		log.Printf("timeweb api error: %v", err)
		return "", false
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		errorBody := readErrorBody(resp.Body)
		if strings.Contains(strings.ToLower(errorBody), "agent_suspended") {
			setTimewebAgentCooldown(agentAccessID, 6*time.Hour)
			log.Printf("timeweb agent suspended; calls paused for 6h agent=%s", agentAccessID)
			return "", false
		}
		log.Printf("timeweb api bad status: %s body=%s", resp.Status, errorBody)
		return "", false
	}

	var raw struct {
		Message string `json:"message"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		log.Printf("timeweb api decode error: %v", err)
		return "", false
	}
	text := strings.TrimSpace(raw.Message)
	if text == "" {
		return "", false
	}
	return text, true
}

func timewebAgentCoolingDown(agentAccessID string) bool {
	timewebCooldown.mu.Lock()
	defer timewebCooldown.mu.Unlock()
	until := timewebCooldown.until[agentAccessID]
	if until.IsZero() || time.Now().Before(until) {
		return !until.IsZero()
	}
	delete(timewebCooldown.until, agentAccessID)
	return false
}

func setTimewebAgentCooldown(agentAccessID string, duration time.Duration) {
	timewebCooldown.mu.Lock()
	timewebCooldown.until[agentAccessID] = time.Now().Add(duration)
	timewebCooldown.mu.Unlock()
}

func fetchCryptoPanicAPI(ctx context.Context, cfg Config, limit int) ([]NewsItem, error) {
	if limit <= 0 {
		limit = 50
	}
	apiToken := strings.TrimSpace(cfg.CryptoPanicAPIToken)
	if apiToken == "" {
		return nil, fmt.Errorf("cryptopanic api token is empty")
	}
	baseURL := strings.TrimSpace(cfg.CryptoPanicAPIURL)
	if baseURL == "" {
		baseURL = "https://cryptopanic.com/api/v1/posts/"
	}
	u, err := url.Parse(baseURL)
	if err != nil {
		return nil, err
	}
	q := u.Query()
	q.Set("auth_token", apiToken)
	q.Set("public", "true")
	q.Set("kind", "news")
	q.Set("limit", strconv.Itoa(limit))
	u.RawQuery = q.Encode()

	client := &http.Client{Timeout: 12 * time.Second}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", rssUserAgent)
	req.Header.Set("Accept", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusTooManyRequests {
		setFeedCooldown("cryptopanic_api")
		return nil, fmt.Errorf("http error: %d Too Many Requests", resp.StatusCode)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("http error: %s", resp.Status)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 2*1024*1024))
	if err != nil {
		return nil, err
	}

	var raw struct {
		Results []struct {
			Title       string `json:"title"`
			URL         string `json:"url"`
			PublishedAt string `json:"published_at"`
			Domain      string `json:"domain"`
			Source      struct {
				Title string `json:"title"`
			} `json:"source"`
			Metadata struct {
				Image string `json:"image"`
			} `json:"metadata"`
		} `json:"results"`
	}
	if err := json.Unmarshal(body, &raw); err != nil {
		preview := strings.TrimSpace(string(body[:min(len(body), 512)]))
		return nil, fmt.Errorf("decode error: %v; preview=%q", err, preview)
	}

	items := make([]NewsItem, 0, len(raw.Results))
	for _, it := range raw.Results {
		title := strings.TrimSpace(it.Title)
		link := strings.TrimSpace(it.URL)
		if title == "" || link == "" {
			continue
		}
		normalURL := normalizeURL(link)
		published := time.Now().UTC()
		if it.PublishedAt != "" {
			if parsed, err := time.Parse(time.RFC3339, it.PublishedAt); err == nil {
				published = parsed.UTC()
			}
		}
		source := strings.TrimSpace(it.Source.Title)
		if source == "" {
			source = strings.TrimSpace(it.Domain)
		}
		if source == "" {
			source = hostnameFromURL(normalURL)
		}
		image := strings.TrimSpace(it.Metadata.Image)
		items = append(items, NewsItem{
			Category:  "crypto",
			Title:     title,
			TitleNorm: normalizeTitle(title),
			URL:       normalURL,
			URLHash:   hashURL(normalURL),
			Source:    source,
			Published: published,
			Image:     image,
		})
	}
	return items, nil
}

func fetchRSS(ctx context.Context, feedURL, category string, limit int) ([]NewsItem, error) {
	if limit <= 0 {
		limit = 50
	}
	if skip, until := shouldSkipFeed(feedURL); skip {
		return nil, fmt.Errorf("feed cooldown until %s", until.Format(time.RFC3339))
	}
	parser := gofeed.NewParser()
	parser.UserAgent = rssUserAgent
	client := &http.Client{Timeout: 12 * time.Second}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, feedURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", rssUserAgent)
	req.Header.Set("Accept", "application/rss+xml, application/xml;q=0.9, text/xml;q=0.8, */*;q=0.7")
	req.Header.Set("Accept-Language", "ru,en;q=0.8")

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusTooManyRequests {
		setFeedCooldown(feedURL)
		return nil, fmt.Errorf("http error: %d Too Many Requests", resp.StatusCode)
	}
	if resp.StatusCode == http.StatusForbidden {
		setFeedCooldownFor(feedURL, 6*time.Hour)
		return nil, fmt.Errorf("http error: %s; feed paused for 6h", resp.Status)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("http error: %s", resp.Status)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 2*1024*1024))
	if err != nil {
		return nil, err
	}
	feed, err := parser.Parse(strings.NewReader(string(body)))
	if err != nil {
		preview := strings.TrimSpace(string(body[:min(len(body), 512)]))
		return nil, fmt.Errorf("parse error: %v; preview=%q", err, preview)
	}

	items := make([]NewsItem, 0, len(feed.Items))
	for _, it := range feed.Items {
		if len(items) >= limit {
			break
		}
		link := strings.TrimSpace(it.Link)
		if link == "" {
			link = strings.TrimSpace(it.GUID)
		}
		normalURL := normalizeURL(link)
		if normalURL == "" {
			continue
		}
		title := strings.TrimSpace(it.Title)
		if title == "" {
			continue
		}
		published := time.Now().UTC()
		if it.PublishedParsed != nil {
			published = it.PublishedParsed.UTC()
		}
		source := ""
		if feed.Title != "" {
			source = feed.Title
		}
		if it.Author != nil && it.Author.Name != "" {
			source = it.Author.Name
		}
		if source == "" {
			source = hostnameFromURL(normalURL)
		}
		summary := cleanFeedText(it.Description)
		if summary == "" {
			summary = cleanFeedText(it.Content)
		}
		image := ""
		if it.Image != nil {
			image = strings.TrimSpace(it.Image.URL)
		}
		items = append(items, NewsItem{
			Category:  category,
			Title:     title,
			TitleNorm: normalizeTitle(title),
			URL:       normalURL,
			URLHash:   hashURL(normalURL),
			Source:    source,
			Published: published,
			Summary:   summary,
			Image:     image,
		})
	}
	return items, nil
}

func fetchFreshRSS(ctx context.Context, feedURL, category string, limit int, maxAge time.Duration) ([]NewsItem, error) {
	items, err := fetchRSS(ctx, feedURL, category, limit)
	if err != nil {
		return nil, err
	}
	return filterFreshNewsItems(items, time.Now().UTC(), maxAge), nil
}

func filterFreshNewsItems(items []NewsItem, now time.Time, maxAge time.Duration) []NewsItem {
	if maxAge <= 0 {
		return items
	}
	cutoff := now.Add(-maxAge)
	latestAllowed := now.Add(15 * time.Minute)
	fresh := make([]NewsItem, 0, len(items))
	for _, item := range items {
		published := item.Published.UTC()
		if published.Before(cutoff) || published.After(latestAllowed) {
			continue
		}
		fresh = append(fresh, item)
	}
	return fresh
}

type binanceAnnouncementPage struct {
	name     string
	url      string
	fallback string
}

type binanceAnnouncementList struct {
	Code    string `json:"code"`
	Success bool   `json:"success"`
	Data    struct {
		Articles []struct {
			Code        string `json:"code"`
			Title       string `json:"title"`
			ReleaseDate int64  `json:"releaseDate"`
			PublishDate int64  `json:"publishDate"`
			ImageLink   string `json:"imageLink"`
		} `json:"articles"`
	} `json:"data"`
}

type binanceAnnouncementDetail struct {
	Code string `json:"code"`
	Data struct {
		Title string `json:"title"`
		Body  string `json:"body"`
	} `json:"data"`
}

func fetchBinanceAnnouncements(ctx context.Context, cfg Config) ([]NewsItem, error) {
	pages := make([]binanceAnnouncementPage, 0, 2)
	if strings.TrimSpace(cfg.BinanceListingsURL) != "" {
		pages = append(pages, binanceAnnouncementPage{
			name:     "binance_listings",
			url:      cfg.BinanceListingsURL,
			fallback: "binance_listing",
		})
	}
	if strings.TrimSpace(cfg.BinanceDelistingsURL) != "" {
		pages = append(pages, binanceAnnouncementPage{
			name:     "binance_delistings",
			url:      cfg.BinanceDelistingsURL,
			fallback: "binance_delisting",
		})
	}
	limit := cfg.BinanceLimit
	if limit <= 0 {
		limit = 25
	}

	seen := make(map[string]bool)
	items := make([]NewsItem, 0, len(pages)*limit)
	var pageErrors []string
	for _, page := range pages {
		var payload binanceAnnouncementList
		if err := fetchJSON(ctx, page.url, &payload); err != nil {
			pageErrors = append(pageErrors, page.name+": "+err.Error())
			continue
		}
		if payload.Code != "" && payload.Code != "000000" {
			pageErrors = append(pageErrors, page.name+": binance code "+payload.Code)
			continue
		}
		pageCount := 0
		for _, article := range payload.Data.Articles {
			if pageCount >= limit {
				break
			}
			code := strings.TrimSpace(article.Code)
			title := cleanFeedText(article.Title)
			if code == "" || title == "" {
				continue
			}
			articleURL := "https://www.binance.com/en/support/announcement/detail/" + url.PathEscape(code)
			if seen[articleURL] {
				continue
			}
			seen[articleURL] = true
			publishedAt := article.ReleaseDate
			if publishedAt <= 0 {
				publishedAt = article.PublishDate
			}
			if publishedAt <= 0 {
				log.Printf("news source=binance published_at missing article=%s", code)
				continue
			}
			published := time.UnixMilli(publishedAt).UTC()
			if publishedAt < 100000000000 {
				published = time.Unix(publishedAt, 0).UTC()
			}
			var eventAt *time.Time
			if extractedEventAt, ok := extractEventTime(title); ok {
				eventAt = newsources.TimePtr(extractedEventAt)
			} else if detailEventAt, ok := fetchBinanceAnnouncementEventTime(ctx, cfg.BinanceArticleDetailURL, code); ok {
				eventAt = newsources.TimePtr(detailEventAt)
			}
			eventType := newsources.ExchangeAnnouncementType(title, page.fallback)
			if strings.Contains(page.fallback, "delisting") {
				eventType = "delisting"
			}
			items = append(items, NewsItem{
				Category:     "crypto",
				Title:        title,
				TitleNorm:    normalizeTitle(title),
				URL:          articleURL,
				URLHash:      hashURL(articleURL),
				Source:       "Binance",
				Published:    published,
				Image:        strings.TrimSpace(article.ImageLink),
				CalendarOnly: true,
				SourceID:     "binance",
				SourceType:   "official_exchange",
				EventAt:      eventAt,
				EventType:    eventType,
				Assets:       newsources.ExchangeAnnouncementAssets(title),
			})
			pageCount++
		}
	}
	if len(items) == 0 && len(pageErrors) > 0 {
		return nil, fmt.Errorf("all Binance announcement feeds failed: %s", strings.Join(pageErrors, "; "))
	}
	for _, pageErr := range pageErrors {
		log.Printf("news-bot Binance announcement feed failed: %s", pageErr)
	}
	return items, nil
}

func fetchBinanceAnnouncementEventTime(ctx context.Context, detailURL, code string) (time.Time, bool) {
	detailURL = strings.TrimSpace(detailURL)
	code = strings.TrimSpace(code)
	if detailURL == "" || code == "" {
		return time.Time{}, false
	}
	endpoint := fmt.Sprintf(detailURL, url.QueryEscape(code))
	var payload binanceAnnouncementDetail
	if err := fetchJSON(ctx, endpoint, &payload); err != nil {
		log.Printf("news-bot Binance detail failed code=%s err=%v", code, err)
		return time.Time{}, false
	}
	if payload.Code != "" && payload.Code != "000000" {
		return time.Time{}, false
	}
	if eventAt, ok := extractEventTime(payload.Data.Title); ok {
		return eventAt, true
	}
	return extractEventTime(payload.Data.Body)
}

func fetchJSON(ctx context.Context, endpoint string, dst any) error {
	if skip, until := shouldSkipFeed(endpoint); skip {
		return fmt.Errorf("feed cooldown until %s", until.Format(time.RFC3339))
	}
	client := &http.Client{Timeout: 15 * time.Second}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", rssUserAgent)
	req.Header.Set("Accept", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusTooManyRequests {
		setFeedCooldown(endpoint)
		return fmt.Errorf("http error: %d Too Many Requests", resp.StatusCode)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("http error: %s", resp.Status)
	}
	dec := json.NewDecoder(io.LimitReader(resp.Body, 4*1024*1024))
	if err := dec.Decode(dst); err != nil {
		return fmt.Errorf("decode json: %w", err)
	}
	return nil
}

func parseFlexibleTime(raw string) (time.Time, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return time.Time{}, false
	}
	raw = strings.ReplaceAll(raw, " GMT", " UTC")
	raw = strings.ReplaceAll(raw, " at ", " ")

	layouts := []string{
		time.RFC3339,
		time.RFC3339Nano,
		"2006-01-02 15:04:05 MST",
		"2006-01-02 15:04 MST",
		"2006-01-02 15:04:05",
		"2006-01-02 15:04",
		"2006-01-02",
		"January 2, 2006 3:04 PM MST",
		"January 2, 2006 3 PM MST",
		"January 2, 2006 15:04 MST",
		"January 2, 2006",
		"2 January 2006 3:04 PM MST",
		"2 January 2006 3 PM MST",
		"2 January 2006 15:04 MST",
		"2 January 2006",
	}
	for _, layout := range layouts {
		t, err := time.Parse(layout, raw)
		if err != nil {
			continue
		}
		if strings.Contains(layout, "MST") || strings.Contains(layout, "Z07") {
			return t.UTC(), true
		}
		return time.Date(t.Year(), t.Month(), t.Day(), t.Hour(), t.Minute(), t.Second(), t.Nanosecond(), time.UTC), true
	}
	return time.Time{}, false
}

func extractEventTime(text string) (time.Time, bool) {
	clean := strings.TrimSpace(cleanFeedText(text))
	if clean == "" {
		return time.Time{}, false
	}

	best := time.Time{}
	for _, candidate := range bybitDateCandidateRe.FindAllString(clean, -1) {
		candidate = strings.TrimSpace(candidate)
		candidate = bybitOrdinalDayRe.ReplaceAllString(candidate, "$1")
		if t, ok := parseFlexibleTime(candidate); ok {
			if best.IsZero() || t.Before(best) {
				best = t
			}
		}
	}
	if best.IsZero() {
		return time.Time{}, false
	}
	return best, true
}

func maybeTranslateItem(ctx context.Context, cfg Config, item NewsItem) NewsItem {
	if strings.TrimSpace(cfg.TimewebAPIKey) == "" || strings.TrimSpace(cfg.TimewebAgentAccessID) == "" {
		return item
	}
	if looksRussian(item.Title) && looksRussian(item.Summary) {
		return item
	}
	translated, ok := translateToRussian(ctx, cfg, item.Title, item.Summary)
	if !ok {
		return item
	}
	item.Title = translated.Title
	item.Summary = translated.Summary
	item.TranslatedTitle = translated.Title
	item.TranslatedSummary = translated.Summary
	item.TitleNorm = normalizeTitle(item.Title)
	return item
}

type translatedText struct {
	Title   string `json:"title"`
	Summary string `json:"summary"`
}

func translateToRussian(ctx context.Context, cfg Config, title, summary string) (translatedText, bool) {
	text, ok := callTimewebText(ctx, cfg, cfg.TimewebAgentAccessID, buildTranslatePayload(title, summary))
	if !ok {
		return translatedText{}, false
	}
	out, ok := parseTranslatedText(text)
	if !ok {
		log.Printf("timeweb translate parse failed: %q", strings.TrimSpace(text))
		return translatedText{}, false
	}
	out.Title = strings.TrimSpace(out.Title)
	out.Summary = strings.TrimSpace(out.Summary)
	if out.Title == "" {
		return translatedText{}, false
	}
	return out, true
}

func parseTranslatedText(raw string) (translatedText, bool) {
	clean := stripCodeFences(strings.TrimSpace(raw))
	if clean == "" {
		return translatedText{}, false
	}
	var out translatedText
	if err := json.Unmarshal([]byte(clean), &out); err == nil {
		return out, true
	}

	if title, summary, ok := parseByMarkers(clean); ok {
		return translatedText{Title: title, Summary: summary}, true
	}

	lines := strings.Split(clean, "\n")
	if len(lines) >= 2 {
		title := strings.TrimSpace(lines[0])
		summary := strings.TrimSpace(strings.Join(lines[1:], "\n"))
		if title != "" {
			return translatedText{Title: title, Summary: summary}, true
		}
	}

	return translatedText{}, false
}

func stripCodeFences(raw string) string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return ""
	}
	lines := strings.Split(trimmed, "\n")
	if len(lines) < 2 {
		return trimmed
	}
	first := strings.TrimSpace(lines[0])
	last := strings.TrimSpace(lines[len(lines)-1])
	if strings.HasPrefix(first, "```") && last == "```" {
		inner := strings.Join(lines[1:len(lines)-1], "\n")
		return strings.TrimSpace(inner)
	}
	return trimmed
}

func parseByMarkers(raw string) (string, string, bool) {
	lower := strings.ToLower(raw)
	idxTitle := strings.Index(lower, "title:")
	idxSummary := strings.Index(lower, "summary:")
	if idxTitle == -1 || idxSummary == -1 {
		return "", "", false
	}
	if idxSummary < idxTitle {
		idxTitle, idxSummary = idxSummary, idxTitle
	}
	title := strings.TrimSpace(raw[idxTitle+len("title:") : idxSummary])
	summary := strings.TrimSpace(raw[idxSummary+len("summary:"):])
	if title == "" {
		return "", "", false
	}
	return title, summary, true
}

func buildTranslatePayload(title, summary string) string {
	cleanTitle := strings.TrimSpace(title)
	cleanSummary := strings.TrimSpace(summary)
	if cleanSummary == "" {
		cleanSummary = "-"
	}
	return fmt.Sprintf("TITLE: %s\nSUMMARY: %s", cleanTitle, cleanSummary)
}

func looksRussian(text string) bool {
	text = strings.TrimSpace(text)
	if text == "" {
		return true
	}
	letters := 0
	cy := 0
	for _, r := range text {
		if unicode.IsLetter(r) {
			letters++
			if unicode.In(r, unicode.Cyrillic) {
				cy++
			}
		}
	}
	if letters == 0 {
		return true
	}
	return float64(cy)/float64(letters) >= 0.55
}

func existsURLHash(ctx context.Context, pool *pgxpool.Pool, hash string) (bool, error) {
	if strings.TrimSpace(hash) == "" {
		return false, nil
	}
	var exists bool
	err := pool.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM news_items WHERE url_hash = $1)", hash).Scan(&exists)
	return exists, err
}

func insertNewsItemIfUnique(ctx context.Context, pool *pgxpool.Pool, item NewsItem, dedupWindow time.Duration) (bool, error) {
	item = sanitizeNewsItemForPostgres(item)
	if dedupWindow <= 0 {
		dedupWindow = 72 * time.Hour
	}
	tx, err := pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return false, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if _, err := tx.Exec(ctx, "SELECT pg_advisory_xact_lock($1)", newsDedupAdvisoryLockID); err != nil {
		return false, err
	}
	var exists bool
	if err := tx.QueryRow(ctx, `
		SELECT EXISTS(
			SELECT 1 FROM news_items
			WHERE url = $1 OR ($2 <> '' AND url_hash = $2)
		)
	`, item.URL, item.URLHash).Scan(&exists); err != nil {
		return false, err
	}
	if exists {
		return false, nil
	}

	rows, err := tx.Query(ctx, `
		SELECT title_norm
		FROM news_items
		WHERE title_norm IS NOT NULL
		  AND calendar_only = $1
		  AND published_at >= $2
	`, item.CalendarOnly, time.Now().UTC().Add(-dedupWindow))
	if err != nil {
		return false, err
	}
	for rows.Next() {
		var existingTitle string
		if err := rows.Scan(&existingTitle); err != nil {
			rows.Close()
			return false, err
		}
		if isSimilarTitle(item.TitleNorm, existingTitle) {
			rows.Close()
			return false, nil
		}
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return false, err
	}
	rows.Close()

	entitiesJSON, err := json.Marshal(item.Entities)
	if err != nil {
		return false, err
	}
	assetsJSON, err := json.Marshal(item.Assets)
	if err != nil {
		return false, err
	}
	tag, err := tx.Exec(ctx, `
		INSERT INTO news_items (
			category, title, url, url_hash, title_norm, source, published_at, summary, image, calendar_only,
			original_title, original_text, original_language, translated_title, translated_summary,
			source_id, source_type, event_at, event_type, entities, assets, publication_eligible
		)
		VALUES (
			$1, $2, $3, $4, $5, $6, $7, NULLIF($8, ''), NULLIF($9, ''), $10,
			$11, $12, $13, NULLIF($14, ''), NULLIF($15, ''), $16, $17, $18, $19, $20::jsonb, $21::jsonb, $22
		)
		ON CONFLICT DO NOTHING
	`, item.Category, item.Title, item.URL, item.URLHash, item.TitleNorm, item.Source, item.Published, item.Summary, item.Image, item.CalendarOnly,
		item.OriginalTitle, item.OriginalText, item.OriginalLanguage, item.TranslatedTitle, item.TranslatedSummary,
		item.SourceID, item.SourceType, item.EventAt, item.EventType, string(entitiesJSON), string(assetsJSON), item.PublicationEligible)
	if err != nil {
		return false, err
	}
	if err := tx.Commit(ctx); err != nil {
		return false, err
	}
	return tag.RowsAffected() == 1, nil
}

func stripPostgresNUL(value string) string {
	return strings.ReplaceAll(value, "\x00", "")
}

func sanitizeNewsItemForPostgres(item NewsItem) NewsItem {
	item.Category = stripPostgresNUL(item.Category)
	item.Title = stripPostgresNUL(item.Title)
	item.TitleNorm = stripPostgresNUL(item.TitleNorm)
	item.URL = stripPostgresNUL(item.URL)
	item.URLHash = stripPostgresNUL(item.URLHash)
	item.Source = stripPostgresNUL(item.Source)
	item.Summary = stripPostgresNUL(item.Summary)
	item.Image = stripPostgresNUL(item.Image)
	item.OriginalTitle = stripPostgresNUL(item.OriginalTitle)
	item.OriginalText = stripPostgresNUL(item.OriginalText)
	item.OriginalLanguage = stripPostgresNUL(item.OriginalLanguage)
	item.TranslatedTitle = stripPostgresNUL(item.TranslatedTitle)
	item.TranslatedSummary = stripPostgresNUL(item.TranslatedSummary)
	item.SourceID = stripPostgresNUL(item.SourceID)
	item.SourceType = stripPostgresNUL(item.SourceType)
	item.EventType = stripPostgresNUL(item.EventType)
	for index := range item.Entities {
		item.Entities[index] = stripPostgresNUL(item.Entities[index])
	}
	for index := range item.Assets {
		item.Assets[index] = stripPostgresNUL(item.Assets[index])
	}
	return item
}

func normalizeURL(raw string) string {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Host == "" {
		return strings.TrimSpace(raw)
	}
	u.Scheme = strings.ToLower(u.Scheme)
	u.Host = strings.ToLower(u.Host)
	u.Fragment = ""
	q := u.Query()
	for key := range q {
		k := strings.ToLower(key)
		if strings.HasPrefix(k, "utm_") || k == "ref" || k == "ref_src" || k == "ref_url" {
			q.Del(key)
		}
	}
	keys := make([]string, 0, len(q))
	for key := range q {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	clean := url.Values{}
	for _, key := range keys {
		clean[key] = q[key]
	}
	u.RawQuery = clean.Encode()
	u.Path = strings.TrimSuffix(u.Path, "/")
	return u.String()
}

func hashURL(normalURL string) string {
	if strings.TrimSpace(normalURL) == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(normalURL))
	return hex.EncodeToString(sum[:])
}

func normalizeTitle(title string) string {
	lower := strings.ToLower(strings.TrimSpace(title))
	lower = strings.NewReplacer("ё", "е").Replace(lower)
	builder := strings.Builder{}
	prevSpace := false
	for _, r := range lower {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || (r >= 'а' && r <= 'я') || r == ' ' {
			if r == ' ' {
				if prevSpace {
					continue
				}
				prevSpace = true
				builder.WriteRune(' ')
				continue
			}
			prevSpace = false
			builder.WriteRune(r)
		} else {
			if !prevSpace {
				builder.WriteRune(' ')
				prevSpace = true
			}
		}
	}
	return strings.TrimSpace(builder.String())
}

func cleanFeedText(raw string) string {
	text := strings.TrimSpace(raw)
	if text == "" {
		return ""
	}
	text = html.UnescapeString(text)
	text = htmlTagRe.ReplaceAllString(text, " ")
	text = strings.NewReplacer("\n", " ", "\r", " ", "\t", " ", "\u00a0", " ").Replace(text)
	return strings.Join(strings.Fields(text), " ")
}

func isSimilarTitle(a, b string) bool {
	a = strings.TrimSpace(a)
	b = strings.TrimSpace(b)
	if a == "" || b == "" {
		return false
	}
	if a == b {
		return true
	}
	if strings.Contains(a, b) || strings.Contains(b, a) {
		min := len(a)
		if len(b) < min {
			min = len(b)
		}
		max := len(a)
		if len(b) > max {
			max = len(b)
		}
		if float64(min)/float64(max) >= 0.85 {
			return true
		}
	}
	aj := tokenSet(a)
	bj := tokenSet(b)
	if len(aj) == 0 || len(bj) == 0 {
		return false
	}
	inter := 0
	for tok := range aj {
		if bj[tok] {
			inter++
		}
	}
	union := len(aj) + len(bj) - inter
	sim := float64(inter) / float64(union)
	if sim >= 0.85 {
		return true
	}
	minTokens := len(aj)
	if len(bj) < minTokens {
		minTokens = len(bj)
	}
	return minTokens >= 5 && inter == minTokens && sim >= 0.65
}

func tokenSet(s string) map[string]bool {
	out := make(map[string]bool)
	for _, part := range strings.Fields(s) {
		if len(part) < 3 {
			continue
		}
		out[part] = true
	}
	return out
}

func hostnameFromURL(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	return strings.TrimPrefix(u.Hostname(), "www.")
}

func readErrorBody(body io.ReadCloser) string {
	if body == nil {
		return ""
	}
	defer func() { _ = body.Close() }()
	data, err := io.ReadAll(io.LimitReader(body, 2048))
	if err != nil {
		return ""
	}
	text := strings.TrimSpace(string(data))
	if text == "" {
		return ""
	}
	return text
}

func shouldSkipFeed(url string) (bool, time.Time) {
	feedCooldown.mu.Lock()
	defer feedCooldown.mu.Unlock()
	until, ok := feedCooldown.until[url]
	if !ok {
		return false, time.Time{}
	}
	if time.Now().Before(until) {
		return true, until
	}
	delete(feedCooldown.until, url)
	return false, time.Time{}
}

func setFeedCooldown(url string) {
	setFeedCooldownFor(url, feedCooldownDuration)
}

func setFeedCooldownFor(url string, duration time.Duration) {
	feedCooldown.mu.Lock()
	defer feedCooldown.mu.Unlock()
	feedCooldown.until[url] = time.Now().Add(duration)
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func parseList(raw string) []string {
	parts := strings.FieldsFunc(raw, func(r rune) bool {
		switch r {
		case ',', ';', '\n', '\r', '\t', ' ':
			return true
		default:
			return false
		}
	})
	if len(parts) == 0 {
		return nil
	}
	result := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.ToLower(strings.TrimSpace(p))
		if p == "" {
			continue
		}
		result = append(result, p)
	}
	return result
}

func loadConfig() Config {
	legacyInterval := envInt("NEWS_FETCH_INTERVAL_MINUTES", 10)
	return Config{
		PGDSN:                        envStr("PG_DSN", ""),
		FetchIntervalMinutes:         legacyInterval,
		SourceIntervalMinutes:        envInt("NEWS_SOURCE_INTERVAL_MINUTES", legacyInterval),
		SourceStaggerMinutes:         envInt("NEWS_SOURCE_STAGGER_MINUTES", 1),
		DedupWindowHours:             envInt("NEWS_DEDUP_WINDOW_HOURS", 72),
		CryptoPanicLimit:             envInt("CRYPTOPANIC_LIMIT", 50),
		CoinDeskLimit:                envInt("COINDESK_LIMIT", 50),
		DecryptLimit:                 envInt("DECRYPT_LIMIT", 50),
		GDELTLimit:                   envInt("GDELT_LIMIT", 50),
		BBCWorldLimit:                envInt("BBC_WORLD_LIMIT", 50),
		WuBlockchainLimit:            envInt("WUBLOCKCHAIN_LIMIT", 50),
		WuBlockchainMaxAgeHours:      envInt("WUBLOCKCHAIN_MAX_AGE_HOURS", 72),
		CryptoPanicRSSURL:            envStr("CRYPTOPANIC_RSS_URL", ""),
		CoinDeskRSSURL:               envStr("COINDESK_RSS_URL", "https://www.coindesk.com/arc/outboundfeeds/rss/"),
		DecryptRSSURL:                envStr("DECRYPT_RSS_URL", "https://decrypt.co/feed"),
		GDELTRSSURL:                  envStr("GDELT_RSS_URL", defaultGdeltRSS(envInt("GDELT_LIMIT", 50))),
		BBCWorldRSSURL:               envStr("BBC_WORLD_RSS_URL", "https://feeds.bbci.co.uk/news/world/rss.xml"),
		WuBlockchainRSSURL:           envStr("WUBLOCKCHAIN_RSS_URL", defaultWuBlockchainRSSURL),
		CryptoPanicAPIURL:            envStr("CRYPTOPANIC_API_URL", "https://cryptopanic.com/api/v1/posts/"),
		CryptoPanicAPIToken:          envStr("CRYPTOPANIC_API_TOKEN", ""),
		ExtraRSSCategory:             envStr("EXTRA_RSS_CATEGORY", "world"),
		ExtraRSSLimit:                envInt("EXTRA_RSS_LIMIT", 50),
		ExtraRSSURLs:                 parseList(envStr("EXTRA_RSS_URLS", "")),
		BybitLimit:                   envInt("BYBIT_LIMIT", 25),
		BinanceListingsURL:           envStr("BINANCE_LISTINGS_URL", "https://www.binance.com/bapi/composite/v1/public/cms/article/catalog/list/query?catalogId=48&pageNo=1&pageSize=25"),
		BinanceDelistingsURL:         envStr("BINANCE_DELISTINGS_URL", "https://www.binance.com/bapi/composite/v1/public/cms/article/catalog/list/query?catalogId=161&pageNo=1&pageSize=25"),
		BinanceArticleDetailURL:      envStr("BINANCE_ARTICLE_DETAIL_URL", "https://www.binance.com/bapi/composite/v1/public/cms/article/detail/query?articleCode=%s"),
		BinanceLimit:                 envInt("BINANCE_ANNOUNCEMENTS_LIMIT", 25),
		CointelegraphEnabled:         envBool("COINTELEGRAPH_ENABLED", true),
		CointelegraphRSSURL:          envStr("COINTELEGRAPH_RSS_URL", "https://cointelegraph.com/rss"),
		CointelegraphLimit:           envInt("COINTELEGRAPH_LIMIT", 50),
		BybitAPIEnabled:              envBool("BYBIT_ANNOUNCEMENTS_ENABLED", true),
		BybitAPIURL:                  envStr("BYBIT_ANNOUNCEMENTS_API_URL", "https://api.bybit.com/v5/announcements/index?locale=en-US&limit=20"),
		SECEnabled:                   envBool("SEC_RSS_ENABLED", true),
		SECRSSURLs:                   parseList(envStr("SEC_RSS_URLS", "https://www.sec.gov/news/pressreleases.rss,https://www.sec.gov/enforcement-litigation/litigation-releases/rss")),
		SECEDGAREnabled:              envBool("SEC_EDGAR_ENABLED", true),
		SECCompanyCIKs:               parseList(envStr("SEC_COMPANY_CIKS", "1679788,1050446,1876042,1783879")),
		FederalReserveEnabled:        envBool("FEDERAL_RESERVE_ENABLED", true),
		FederalReserveURLs:           parseList(envStr("FEDERAL_RESERVE_RSS_URLS", "https://www.federalreserve.gov/feeds/press_monetary.xml,https://www.federalreserve.gov/feeds/press_enforcement.xml,https://www.federalreserve.gov/feeds/press_all.xml,https://www.federalreserve.gov/feeds/speeches.xml")),
		WhiteHouseEnabled:            envBool("WHITE_HOUSE_ENABLED", true),
		WhiteHouseRSSURL:             envStr("WHITE_HOUSE_RSS_URL", "https://www.whitehouse.gov/briefing-room/feed/"),
		FederalRegisterEnabled:       envBool("FEDERAL_REGISTER_ENABLED", true),
		FederalRegisterURL:           envStr("FEDERAL_REGISTER_API_URL", "https://www.federalregister.gov/api/v1/documents.json?per_page=20&order=newest&conditions%5Bpresidential_document_type%5D%5B%5D=executive_order"),
		UpbitEnabled:                 envBool("UPBIT_ENABLED", true),
		UpbitNoticesURL:              envStr("UPBIT_NOTICES_URL", "https://api-manager.upbit.com/api/v1/announcements"),
		BithumbEnabled:               envBool("BITHUMB_ENABLED", true),
		BithumbNoticesURL:            envStr("BITHUMB_NOTICES_URL", "https://api.bithumb.com/v1/notices"),
		SoSoValueEnabled:             envBool("SOSOVALUE_ETF_ENABLED", false),
		SoSoValueAPIKey:              envStr("SOSOVALUE_API_KEY", ""),
		SoSoValueAPIURL:              envStr("SOSOVALUE_API_URL", "https://api.sosovalue.xyz/openapi/v2/etf/historicalInflowChart"),
		SoSoValueCurrentAPIURL:       envStr("SOSOVALUE_CURRENT_API_URL", "https://api.sosovalue.xyz/openapi/v2/etf/currentEtfDataMetrics"),
		SoSoValueInterval:            envDuration("SOSOVALUE_ETF_INTERVAL", 30*time.Minute),
		FearGreedEnabled:             envBool("FEAR_GREED_ENABLED", true),
		FearGreedURL:                 envStr("FEAR_GREED_URL", "https://api.alternative.me/fng/?limit=2"),
		FearGreedDailyHourUTC:        envInt("FEAR_GREED_DAILY_HOUR_UTC", 12),
		FearGreedInterval:            envDuration("FEAR_GREED_INTERVAL", time.Hour),
		DefiLlamaEnabled:             envBool("DEFILLAMA_ENABLED", true),
		DefiLlamaProtocolsURL:        envStr("DEFILLAMA_PROTOCOLS_URL", "https://api.llama.fi/protocols"),
		DefiLlamaMinChangePct:        envFloat("DEFILLAMA_TVL_CHANGE_PCT", 10),
		TelegramCryptoAttackEnabled:  envBool("TELEGRAM_CRYPTOATTACK_ENABLED", true),
		TelegramCryptoAttackURL:      envStr("TELEGRAM_CRYPTOATTACK_URL", parsertelegram.DefaultChannelURL),
		TelegramCryptoAttackLimit:    envInt("TELEGRAM_CRYPTOATTACK_LIMIT", 25),
		TelegramCryptoAttackInterval: envDuration("TELEGRAM_CRYPTOATTACK_INTERVAL", 10*time.Minute),
		FilterInclude:                parseList(envStr("FILTER_INCLUDE_KEYWORDS", "")),
		FilterExclude:                parseList(envStr("FILTER_EXCLUDE_KEYWORDS", "")),
		TimewebAPIKey:                envStr("TIMEWEB_API_KEY", ""),
		TimewebAgentAccessID:         envStr("TIMEWEB_AGENT_ACCESS_ID", ""),
		TimewebSummaryAgentAccessID:  envStr("TIMEWEB_SUMMARY_AGENT_ACCESS_ID", ""),
		TimewebBaseURL:               envStr("TIMEWEB_BASE_URL", "https://agent.timeweb.cloud"),
	}
}

func defaultGdeltRSS(limit int) string {
	query := url.QueryEscape("(world OR global OR geopolitics OR economy OR markets OR inflation OR trade)")
	if limit <= 0 {
		limit = 50
	}
	return fmt.Sprintf("https://api.gdeltproject.org/api/v2/doc/doc?query=%s&mode=ArtList&format=rss&sort=DateDesc&maxrecords=%d", query, limit)
}

func envStr(key, def string) string {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		return def
	}
	return v
}

func envInt(key string, def int) int {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		return def
	}
	iv, err := strconv.Atoi(v)
	if err != nil {
		return def
	}
	return iv
}

func envBool(key string, def bool) bool {
	value := strings.ToLower(strings.TrimSpace(os.Getenv(key)))
	if value == "" {
		return def
	}
	switch value {
	case "1", "true", "yes", "on":
		return true
	case "0", "false", "no", "off":
		return false
	default:
		return def
	}
}

func envFloat(key string, def float64) float64 {
	value := strings.TrimSpace(os.Getenv(key))
	parsed, err := strconv.ParseFloat(value, 64)
	if value == "" || err != nil {
		return def
	}
	return parsed
}

func envDuration(key string, def time.Duration) time.Duration {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return def
	}
	parsed, err := time.ParseDuration(value)
	if err != nil || parsed <= 0 {
		return def
	}
	return parsed
}

func signalContext() (context.Context, func()) {
	ctx, cancel := context.WithCancel(context.Background())
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, os.Interrupt)
	go func() {
		<-ch
		cancel()
	}()
	return ctx, cancel
}
