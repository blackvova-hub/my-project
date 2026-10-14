package main

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestTerminalCollectorClaimRequiresExistingPublicationApproval(t *testing.T) {
	dsn := os.Getenv("NEWS_TERMINAL_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("set NEWS_TERMINAL_TEST_DATABASE_URL to a migrated isolated QA database")
	}
	ctx := context.Background()
	admin, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal("cannot initialize QA database")
	}
	schema := fmt.Sprintf("news_collector_terminal_test_%d", time.Now().UnixNano())
	name := pgx.Identifier{schema}.Sanitize()
	if _, err = admin.Exec(ctx, "CREATE SCHEMA "+name); err != nil {
		admin.Close()
		t.Fatal(err)
	}
	defer func() { _, _ = admin.Exec(ctx, "DROP SCHEMA "+name+" CASCADE"); admin.Close() }()
	if _, err = admin.Exec(ctx, "CREATE TABLE "+name+".news_items (LIKE public.news_items INCLUDING ALL)"); err != nil {
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
	defer pool.Close()
	const rawURL = "https://federalreserve.gov/test-release"
	const hash = "terminal-test-url-hash"
	if _, err = pool.Exec(ctx, `INSERT INTO news_items(category,title,url,source,source_id,published_at,url_hash,title_norm,terminal_collected,publication_eligible)
	 VALUES('macro','Terminal snapshot',$1,'Federal Reserve','federal_reserve',$2,$3,'terminal snapshot',TRUE,FALSE)`, rawURL, time.Now().UTC(), hash); err != nil {
		t.Fatal(err)
	}
	duplicate, err := existsURLHash(ctx, pool, hash)
	if err != nil || duplicate {
		t.Fatalf("terminal row wrongly prevents normal moderation: %v %v", duplicate, err)
	}
	item := NewsItem{Category: "macro", Title: "Collector reviewed release", OriginalTitle: "Official release", OriginalText: "Source excerpt", TitleNorm: "collector reviewed release", URL: rawURL, URLHash: hash, Source: "Federal Reserve", SourceID: "federal_reserve", SourceType: "official_government", Published: time.Now().UTC(), EventType: "monetary_policy", Assets: []string{"USD"}}
	inserted, err := insertNewsItemIfUnique(ctx, pool, item, time.Hour)
	if err != nil || inserted {
		t.Fatalf("unapproved item claimed the terminal row: %v %v", inserted, err)
	}
	var eligible bool
	if err = pool.QueryRow(ctx, `SELECT publication_eligible FROM news_items WHERE url=$1`, rawURL).Scan(&eligible); err != nil || eligible {
		t.Fatal("publication approval was fabricated")
	}
	item.PublicationEligible = true
	inserted, err = insertNewsItemIfUnique(ctx, pool, item, time.Hour)
	if err != nil || !inserted {
		t.Fatalf("independently reviewed collector item could not claim cached row: %v %v", inserted, err)
	}
	var count int
	var title string
	if err = pool.QueryRow(ctx, `SELECT count(*) FROM news_items`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, `SELECT publication_eligible,title FROM news_items WHERE url=$1`, rawURL).Scan(&eligible, &title); err != nil {
		t.Fatal(err)
	}
	if count != 1 || !eligible || title != item.Title {
		t.Fatal("claim created duplicate or failed to apply normal collector content")
	}
	duplicate, err = existsURLHash(ctx, pool, hash)
	if err != nil || !duplicate {
		t.Fatal("approved collector row no longer participates in URL dedup")
	}
	// Rolling deployment: the collector remains compatible with the old schema.
	if _, err = pool.Exec(ctx, `ALTER TABLE news_items DROP COLUMN terminal_collected`); err != nil {
		t.Fatal(err)
	}
	if _, err = existsURLHash(ctx, pool, hash); err != nil {
		t.Fatalf("old schema became incompatible: %v", err)
	}
}
