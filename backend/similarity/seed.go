package similarity

import (
	"context"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Editable initial taxonomy, deliberately separate from ranking. Unclassified
// instruments are still searchable; no sector is guessed from the ticker.
func SeedAssets(ctx context.Context, db *pgxpool.Pool) error {
	_, e := db.Exec(ctx, `INSERT INTO similarity_assets(market,symbol,sectors,is_alt) SELECT m,s,ARRAY[sector],s<>'BTCUSDT' FROM unnest(ARRAY['spot','linear']) m CROSS JOIN (VALUES ('BTCUSDT','l1'),('ETHUSDT','l1'),('SOLUSDT','l1'),('AVAXUSDT','l1'),('AAVEUSDT','defi'),('UNIUSDT','defi'),('ARBUSDT','l2'),('OPUSDT','l2'),('DOGEUSDT','meme'),('SHIBUSDT','meme'),('FETUSDT','ai'),('RENDERUSDT','ai'),('ONDOUSDT','rwa'),('AXSUSDT','gaming')) AS seed(s,sector) ON CONFLICT DO NOTHING`)
	return e
}
