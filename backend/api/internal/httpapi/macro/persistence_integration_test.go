package macroapi

import (
	"context"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// This opt-in test uses a private schema inside the explicitly authorized local
// QA database. It neither modifies the preview cache nor calls source APIs.
func TestPostgresPersistenceAndRevisions(t *testing.T) {
	dsn := os.Getenv("MACRO_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("set MACRO_TEST_DATABASE_URL for isolated PostgreSQL persistence verification")
	}
	config, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatal("invalid test database URL")
	}
	if config.ConnConfig.Database != "news_macro_terminal_test" || config.ConnConfig.Port != 25432 || (config.ConnConfig.Host != "127.0.0.1" && config.ConnConfig.Host != "localhost") {
		t.Fatal("persistence test is restricted to local news_macro_terminal_test on port25432")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	base, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Fatal(err)
	}
	defer base.Close()
	schema := "macro_it_" + strconv.FormatInt(time.Now().UnixNano(), 36)
	quoted := pgx.Identifier{schema}.Sanitize()
	if _, err = base.Exec(ctx, "CREATE SCHEMA "+quoted); err != nil {
		t.Fatal(err)
	}
	defer func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if _, e := base.Exec(cleanup, "DROP SCHEMA "+quoted+" CASCADE"); e != nil {
			t.Error("isolated schema cleanup failed")
		}
	}()
	config = config.Copy()
	config.ConnConfig.RuntimeParams["search_path"] = schema
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	migration, err := os.ReadFile("../../database/migrations/071_news_macro.sql")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, string(migration)); err != nil {
		t.Fatal(err)
	}
	a := New(pool)
	fixed := time.Now().UTC().Truncate(time.Second)
	firstDate := fixed.AddDate(0, -2, 0).Format("2006-01") + "-01"
	secondDate := fixed.AddDate(0, -1, 0).Format("2006-01") + "-01"
	a.now = func() time.Time { return fixed }
	a.applyPoints("CPIAUCSL", []Point{{firstDate, 332.8}, {secondDate, 334.1}}, "BLS via FRED", "https://fred.stlouisfed.org/series/CPIAUCSL")
	sentimentAt := fixed.Format(time.RFC3339)
	a.state.Sentiment = &MarketItem{Symbol: "CRYPTO_FEAR_GREED", Name: "Bitcoin Fear & Greed", AssetClass: "sentiment", Price: ptr(62.0), ObservedAt: &sentimentAt, Source: "Alternative.me", SourceURL: sentimentSourceURL, Points: []Point{{fixed.Format("2006-01-02"), 62}}, Status: "fresh", Classification: ptr("Greed")}
	a.health("alternative-sentiment", nil)
	a.health("fred", nil)
	a.persist(ctx)
	var observations, revisions int
	if err = pool.QueryRow(ctx, `SELECT count(*) FROM news_macro_observations`).Scan(&observations); err != nil {
		t.Fatal(err)
	}
	if observations != 2 {
		t.Fatalf("expected2 persisted observations, got%d", observations)
	}
	a.persist(ctx)
	if err = pool.QueryRow(ctx, `SELECT count(*) FROM news_macro_revisions`).Scan(&revisions); err != nil {
		t.Fatal(err)
	}
	if revisions != 0 {
		t.Fatal("unchanged refresh created revision history")
	}
	fixed = fixed.Add(time.Minute)
	a.applyPoints("CPIAUCSL", []Point{{firstDate, 332.9}, {secondDate, 334.1}}, "BLS via FRED", "https://fred.stlouisfed.org/series/CPIAUCSL")
	a.persist(ctx)
	if err = pool.QueryRow(ctx, `SELECT count(*) FROM news_macro_revisions`).Scan(&revisions); err != nil {
		t.Fatal(err)
	}
	if revisions != 1 {
		t.Fatalf("changed historical observation must create1 revision, got%d", revisions)
	}
	var before, after float64
	if err = pool.QueryRow(ctx, `SELECT previous_value,revised_value FROM news_macro_revisions`).Scan(&before, &after); err != nil {
		t.Fatal(err)
	}
	if before != 332.8 || after != 332.9 {
		t.Fatalf("incorrect revision: %v→%v", before, after)
	}
	restored := New(pool)
	s := restored.clone()
	series := s.Series["CPIAUCSL"]
	if len(series.Points) != 2 || series.Points[0].Value != 332.9 || series.Latest == nil || *series.Latest != 334.1 {
		t.Fatal("cache restore lost revised history")
	}
	if series.Status != "fresh" {
		t.Fatal("fresh persisted observations incorrectly restored as stale")
	}
	if s.Sources["fred"].LastSuccess == nil || !strings.HasPrefix(*s.Sources["fred"].LastSuccess, fixed.Format("2006-01-02")) {
		t.Fatal("source health was not restored")
	}
	if s.Sentiment == nil || s.Sentiment.Price == nil || *s.Sentiment.Price != 62 || s.Sentiment.Classification == nil || *s.Sentiment.Classification != "Greed" || s.Sentiment.Status != "fresh" {
		t.Fatal("independent sentiment snapshot was not restored")
	}
}
