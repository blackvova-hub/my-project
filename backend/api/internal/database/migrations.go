package db

import (
	"context"
	"embed"
	"fmt"
	"log"
	"path"
	"sort"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
)

//go:embed migrations/*.sql baseline/*.sql
var migrationsFS embed.FS

// Databases created before the embedded migration runner already contain the
// schema through this migration. Newer migrations must still be executed.
const legacySchemaCutoff = "037_fix_signals_scanner_slot3.sql"

// The current baseline is a schema snapshot through migration 058. A clean
// database must not replay historical, data-transforming migrations on top of
// that snapshot. Migration 059 holds the compatibility additions that remain.
const baselineMigrationCutoff = "058_important_event_ingestion_clock.sql"

func ApplyMigrations(ctx context.Context, pool *pgxpool.Pool) error {
	if err := ensureMigrationsTable(ctx, pool); err != nil {
		return err
	}

	entries, err := migrationsFS.ReadDir("migrations")
	if err != nil {
		return err
	}

	names := make([]string, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if strings.HasSuffix(name, ".sql") && isSafeMigrationName(name) {
			names = append(names, name)
		}
	}
	sort.Strings(names)

	applied, err := loadAppliedMigrations(ctx, pool)
	if err != nil {
		return err
	}

	if len(applied) == 0 {
		hasTables, err := hasUserTables(ctx, pool)
		if err != nil {
			return err
		}
		if hasTables {
			legacyNames := make([]string, 0, len(names))
			for _, name := range names {
				if name <= legacySchemaCutoff {
					legacyNames = append(legacyNames, name)
				}
			}
			if err := markAllApplied(ctx, pool, legacyNames); err != nil {
				return err
			}
			log.Printf("migrations: existing schema detected, marked %d legacy migrations as applied", len(legacyNames))
		}
		if !hasTables {
			baselineSQL, err := migrationsFS.ReadFile("baseline/001_base_current.sql")
			if err != nil {
				return err
			}
			if _, err := pool.Exec(ctx, string(baselineSQL)); err != nil {
				return fmt.Errorf("baseline 001_base_current.sql: %w", err)
			}
			if err := markApplied(ctx, pool, "baseline/001_base_current.sql"); err != nil {
				return err
			}
			baselineNames := make([]string, 0, len(names))
			for _, name := range names {
				if name <= baselineMigrationCutoff {
					baselineNames = append(baselineNames, name)
				}
			}
			if err := markAllApplied(ctx, pool, baselineNames); err != nil {
				return err
			}
			log.Printf("migrations: baseline schema applied; marked %d represented migrations as applied", len(baselineNames))
		}
	}

	applied, err = loadAppliedMigrations(ctx, pool)
	if err != nil {
		return err
	}

	for _, name := range names {
		if _, ok := applied[name]; ok {
			continue
		}
		filePath := path.Join("migrations", name)
		b, err := migrationsFS.ReadFile(filePath)
		if err != nil {
			return err
		}
		if _, err := pool.Exec(ctx, string(b)); err != nil {
			return fmt.Errorf("migration %s: %w", name, err)
		}
		if err := markApplied(ctx, pool, name); err != nil {
			return err
		}
	}

	return nil
}

func ensureMigrationsTable(ctx context.Context, pool *pgxpool.Pool) error {
	_, err := pool.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS schema_migrations (
			name TEXT PRIMARY KEY,
			applied_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		)
	`)
	return err
}

func loadAppliedMigrations(ctx context.Context, pool *pgxpool.Pool) (map[string]struct{}, error) {
	rows, err := pool.Query(ctx, `SELECT name FROM schema_migrations`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make(map[string]struct{})
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		out[name] = struct{}{}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

func hasUserTables(ctx context.Context, pool *pgxpool.Pool) (bool, error) {
	var count int
	err := pool.QueryRow(ctx, `
		SELECT COUNT(*)
		FROM information_schema.tables
		WHERE table_schema = 'public'
		  AND table_type = 'BASE TABLE'
		  AND table_name <> 'schema_migrations'
	`).Scan(&count)
	if err != nil {
		return false, err
	}
	return count > 0, nil
}

func markApplied(ctx context.Context, pool *pgxpool.Pool, name string) error {
	_, err := pool.Exec(ctx, `INSERT INTO schema_migrations (name) VALUES ($1) ON CONFLICT DO NOTHING`, name)
	return err
}

func markAllApplied(ctx context.Context, pool *pgxpool.Pool, names []string) error {
	for _, name := range names {
		if err := markApplied(ctx, pool, name); err != nil {
			return err
		}
	}
	return nil
}

func isSafeMigrationName(name string) bool {
	if name == "" {
		return false
	}
	if strings.Contains(name, "..") {
		return false
	}
	if strings.ContainsAny(name, "\\/") {
		return false
	}
	return true
}
