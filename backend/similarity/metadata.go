package similarity

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"io"
	"regexp"
	bt "shortlong/backtest"
)

var sectorPattern = regexp.MustCompile(`^[a-z0-9_-]{1,64}$`)

func ImportAssets(ctx context.Context, db *pgxpool.Pool, raw []byte) error {
	var assets []Asset
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if e := d.Decode(&assets); e != nil {
		return e
	}
	if d.Decode(&struct{}{}) != io.EOF {
		return fmt.Errorf("expected one JSON array")
	}
	if len(assets) > 10000 {
		return fmt.Errorf("too many assets")
	}
	batch := &pgx.Batch{}
	for _, a := range assets {
		if (a.Market != "spot" && a.Market != "linear") || !bt.ValidSymbol(a.Symbol) || len(a.Sectors) > 20 {
			return fmt.Errorf("invalid asset")
		}
		for _, s := range a.Sectors {
			if !sectorPattern.MatchString(s) {
				return fmt.Errorf("invalid sector")
			}
		}
		if a.Sectors == nil {
			a.Sectors = []string{}
		}
		batch.Queue(`INSERT INTO similarity_assets(market,symbol,sectors,is_alt,is_stablecoin,enabled) VALUES($1,$2,$3,$4,$5,$6) ON CONFLICT(market,symbol) DO UPDATE SET sectors=EXCLUDED.sectors,is_alt=EXCLUDED.is_alt,is_stablecoin=EXCLUDED.is_stablecoin,enabled=EXCLUDED.enabled,updated_at=now()`, a.Market, a.Symbol, a.Sectors, a.Alt, a.Stable, a.Enabled)
	}
	tx, e := db.Begin(ctx)
	if e != nil {
		return e
	}
	defer tx.Rollback(ctx)
	if e = tx.SendBatch(ctx, batch).Close(); e != nil {
		return e
	}
	return tx.Commit(ctx)
}
