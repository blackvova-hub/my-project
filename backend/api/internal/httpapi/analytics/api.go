package analytics

import (
	"backend/internal/auth"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"mime"
	"net/http"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type API struct {
	db           *pgxpool.Pool
	authenticate func(*http.Request) (auth.User, error)
	origin       string
	encryption   cipher.AEAD
}
type userKey struct{}

func New(store *auth.Store, pool *pgxpool.Pool, origin string) *API {
	a := &API{db: pool, authenticate: store.Authenticate, origin: strings.TrimRight(origin, "/")}
	key, err := base64.StdEncoding.DecodeString(os.Getenv("ANALYTICS_ENCRYPTION_KEY"))
	if err == nil && len(key) == 32 {
		block, _ := aes.NewCipher(key)
		a.encryption, _ = cipher.NewGCM(block)
	}
	return a
}
func (a *API) Routes(r chi.Router) {
	r.Group(func(r chi.Router) {
		r.Use(a.requireAuth)
		r.Get("/analytics", a.dataset)
		r.Post("/analytics/connections", a.connect)
		r.Post("/analytics/connections/{id}/sync", a.queue)
		r.Delete("/analytics/connections/{id}", a.disconnect)
		r.Put("/analytics/notes", a.notes)
		r.Get("/analytics/trade-chart", a.tradeChart)
		r.Post("/analytics/explain", a.explain)
	})
}
func respond(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
func fail(w http.ResponseWriter, status int, msg string) {
	respond(w, status, map[string]string{"error": msg})
}
func (a *API) requireAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		u, err := a.authenticate(r)
		if err != nil {
			fail(w, 401, "Sign in to access analytics.")
			return
		}
		if r.Method != http.MethodGet {
			if r.Header.Get("Sec-Fetch-Site") == "cross-site" || (r.Header.Get("Origin") != "" && r.Header.Get("Origin") != a.origin) {
				fail(w, 403, "Cross-site request rejected.")
				return
			}
			if r.Method != http.MethodDelete {
				media, _, _ := mime.ParseMediaType(r.Header.Get("Content-Type"))
				if media != "application/json" {
					fail(w, 415, "JSON required.")
					return
				}
			}
		}
		ctx, cancel := context.WithTimeout(context.WithValue(r.Context(), userKey{}, u.ID), 25*time.Second)
		defer cancel()
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}
func owner(r *http.Request) string { return r.Context().Value(userKey{}).(string) }
func decode(w http.ResponseWriter, r *http.Request, v any) bool {
	d := json.NewDecoder(http.MaxBytesReader(w, r.Body, 256<<10))
	d.DisallowUnknownFields()
	if d.Decode(v) != nil {
		fail(w, 400, "Invalid request.")
		return false
	}
	if d.Decode(&struct{}{}) != io.EOF {
		fail(w, 400, "Invalid request.")
		return false
	}
	return true
}
func (a *API) seal(user string, c Credentials) ([]byte, error) {
	if a.encryption == nil {
		return nil, errors.New("Encryption is not configured on the server.")
	}
	b, _ := json.Marshal(c)
	nonce := make([]byte, a.encryption.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}
	return a.encryption.Seal(nonce, nonce, b, []byte(user)), nil
}
func (a *API) open(user string, b []byte) (Credentials, error) {
	var c Credentials
	if a.encryption == nil || len(b) < a.encryption.NonceSize() {
		return c, errors.New("Credential encryption unavailable.")
	}
	n := a.encryption.NonceSize()
	plain, err := a.encryption.Open(nil, b[:n], b[n:], []byte(user))
	if err != nil {
		return c, errors.New("Unable to decrypt this connection. Reconnect it.")
	}
	err = json.Unmarshal(plain, &c)
	return c, err
}
func (a *API) connect(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Exchange string `json:"exchange"`
		Name     string `json:"name"`
		Key      string `json:"key"`
		Secret   string `json:"secret"`
		ReadOnly bool   `json:"readOnly"`
	}
	if !decode(w, r, &req) {
		return
	}
	req.Name = strings.TrimSpace(req.Name)
	if (req.Exchange != "bybit" && req.Exchange != "binance") || req.Name == "" || len(req.Name) > 80 || len(req.Key) < 8 || len(req.Key) > 256 || len(req.Secret) < 8 || len(req.Secret) > 512 || !req.ReadOnly {
		fail(w, 400, "Choose an exchange, name and read-only API credentials.")
		return
	}
	if a.encryption == nil {
		fail(w, 503, "Credential encryption is not configured on the server.")
		return
	}
	var count int
	if err := a.db.QueryRow(r.Context(), "SELECT count(*) FROM analytics_connections WHERE user_id=$1", owner(r)).Scan(&count); err != nil {
		fail(w, 500, "Unable to read connections.")
		return
	}
	if count >= 10 {
		fail(w, 409, "Maximum of 10 connections per account.")
		return
	}
	c := Credentials{req.Key, req.Secret}
	if err := newExchange(req.Exchange, c).validate(r.Context()); err != nil {
		fail(w, 422, err.Error())
		return
	}
	encrypted, err := a.seal(owner(r), c)
	if err != nil {
		fail(w, 500, "Unable to encrypt credentials.")
		return
	}
	fingerprint := sha256.Sum256([]byte(req.Key))
	accountType := "unified"
	if req.Exchange == "binance" {
		accountType = "futures"
	}
	var id string
	coverageFrom := time.Now().UTC().AddDate(0, 0, -90)
	if req.Exchange == "bybit" {
		coverageFrom = time.Now().UTC().AddDate(-2, 0, 1)
	}
	err = a.db.QueryRow(r.Context(), `INSERT INTO analytics_connections(user_id,exchange,account_type,name,credentials,fingerprint,coverage_from) VALUES($1,$2,$3,$4,$5,$6,$7) ON CONFLICT(user_id,exchange,account_type,fingerprint) DO NOTHING RETURNING id::text`, owner(r), req.Exchange, accountType, req.Name, encrypted, hex.EncodeToString(fingerprint[:]), coverageFrom).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		fail(w, 409, "This API key is already connected.")
		return
	}
	if err != nil {
		fail(w, 500, "Unable to save connection.")
		return
	}
	respond(w, 201, map[string]string{"id": id, "status": "queued"})
}
func (a *API) queue(w http.ResponseWriter, r *http.Request) {
	var req struct {
		FullHistory bool `json:"fullHistory"`
	}
	if !decode(w, r, &req) {
		return
	}
	// Backfill reuses encrypted credentials and deduplicated events. Never erase history.
	tag, err := a.db.Exec(r.Context(), `UPDATE analytics_connections SET status='queued',error='',coverage_from=CASE WHEN $3 THEN now()-interval '2 years'+interval '1 day' ELSE coverage_from END,synced_through=CASE WHEN $3 THEN NULL ELSE synced_through END WHERE id::text=$1 AND user_id=$2 AND (NOT $3 OR exchange='bybit') AND status NOT IN ('syncing','queued') AND (last_sync IS NULL OR last_sync<now()-interval '30 seconds')`, chi.URLParam(r, "id"), owner(r), req.FullHistory)
	if err != nil {
		fail(w, 500, "Unable to queue sync.")
		return
	}
	if tag.RowsAffected() == 0 {
		fail(w, 409, "Connection is syncing, was just synced, or no longer exists.")
		return
	}
	respond(w, 202, map[string]string{"status": "queued"})
}
func (a *API) disconnect(w http.ResponseWriter, r *http.Request) {
	var removed int
	err := a.db.QueryRow(r.Context(), `WITH owned AS (
 DELETE FROM analytics_connections WHERE id::text=$1 AND user_id=$2 RETURNING id
), notes AS (
 DELETE FROM analytics_trade_notes n USING owned o WHERE n.user_id=$2 AND n.trade_id LIKE o.id::text || ':%'
)
SELECT count(*) FROM owned`, chi.URLParam(r, "id"), owner(r)).Scan(&removed)
	if err != nil {
		fail(w, 500, "Unable to disconnect.")
		return
	}
	if removed == 0 {
		fail(w, 404, "Connection not found.")
		return
	}
	respond(w, 200, map[string]bool{"disconnected": true})
}
func (a *API) load(ctx context.Context, user string) (Dataset, error) {
	d := Dataset{Connections: []Connection{}, Trades: []Trade{}, Ledger: []Ledger{}, Snapshots: []Snapshot{}, Warnings: []string{}, ServerTime: time.Now().UnixMilli()}
	rows, err := a.db.Query(ctx, `SELECT id::text,exchange,account_type,name,status,error,warnings,coverage_from,synced_through,last_sync FROM analytics_connections WHERE user_id=$1 ORDER BY created_at`, user)
	if err != nil {
		return d, err
	}
	for rows.Next() {
		var c Connection
		var warnings []byte
		if err = rows.Scan(&c.ID, &c.Exchange, &c.AccountType, &c.Name, &c.Status, &c.Error, &warnings, &c.CoverageFrom, &c.SyncedThrough, &c.LastSync); err != nil {
			rows.Close()
			return d, err
		}
		_ = json.Unmarshal(warnings, &c.Warnings)
		if c.Warnings == nil {
			c.Warnings = []string{}
		}
		d.Connections = append(d.Connections, c)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return d, err
	}
	// Bound the response explicitly. No silent truncation or partial statistics.
	var count int
	err = a.db.QueryRow(ctx, `SELECT count(*) FROM analytics_events e JOIN analytics_connections c ON c.id=e.connection_id WHERE c.user_id=$1`, user).Scan(&count)
	if err != nil {
		return d, err
	}
	if count > 200000 {
		return d, errors.New("Account exceeds the interactive 200,000-event limit; narrow history in the server configuration before analytics can be displayed.")
	}
	rows, err = a.db.Query(ctx, `SELECT e.kind,e.normalized FROM analytics_events e JOIN analytics_connections c ON c.id=e.connection_id WHERE c.user_id=$1 ORDER BY e.at,e.source_id`, user)
	if err != nil {
		return d, err
	}
	fills := []Fill{}
	for rows.Next() {
		var kind string
		var data []byte
		if err = rows.Scan(&kind, &data); err != nil {
			rows.Close()
			return d, err
		}
		switch kind {
		case "fill":
			var f Fill
			if err = json.Unmarshal(data, &f); err != nil {
				rows.Close()
				return d, err
			}
			fills = append(fills, f)
		case "ledger":
			var l Ledger
			if err = json.Unmarshal(data, &l); err != nil {
				rows.Close()
				return d, err
			}
			d.Ledger = append(d.Ledger, l)
		}
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return d, err
	}
	rows, err = a.db.Query(ctx, `SELECT s.snapshot FROM analytics_snapshots s JOIN analytics_connections c ON c.id=s.connection_id WHERE c.user_id=$1 ORDER BY s.at`, user)
	if err != nil {
		return d, err
	}
	for rows.Next() {
		var b []byte
		if err = rows.Scan(&b); err != nil {
			rows.Close()
			return d, err
		}
		var s Snapshot
		if err = json.Unmarshal(b, &s); err != nil {
			rows.Close()
			return d, err
		}
		d.Snapshots = append(d.Snapshots, s)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return d, err
	}
	d.Trades = Reconstruct(fills, d.Ledger)
	metricRows, metricErr := a.db.Query(ctx, `SELECT m.trade_id,m.mfe::float8,m.mae::float8,m.captured::float8 FROM analytics_trade_metrics m JOIN analytics_connections c ON c.id=m.connection_id WHERE c.user_id=$1`, user)
	if metricErr != nil {
		return d, metricErr
	}
	metrics := map[string][3]*float64{}
	for metricRows.Next() {
		var id string
		var values [3]*float64
		if metricErr = metricRows.Scan(&id, &values[0], &values[1], &values[2]); metricErr != nil {
			metricRows.Close()
			return d, metricErr
		}
		metrics[id] = values
	}
	metricErr = metricRows.Err()
	metricRows.Close()
	if metricErr != nil {
		return d, metricErr
	}
	for i := range d.Trades {
		m := metrics[d.Trades[i].ID]
		d.Trades[i].MFE = m[0]
		d.Trades[i].MAE = m[1]
		d.Trades[i].Captured = m[2]
	}
	rows, err = a.db.Query(ctx, `SELECT trade_id,tag,strategy FROM analytics_trade_notes WHERE user_id=$1`, user)
	if err != nil {
		return d, err
	}
	notes := map[string][2]string{}
	for rows.Next() {
		var id, tag, strategy string
		if err = rows.Scan(&id, &tag, &strategy); err != nil {
			rows.Close()
			return d, err
		}
		notes[id] = [2]string{tag, strategy}
	}
	err = rows.Err()
	rows.Close()
	for i := range d.Trades {
		n := notes[d.Trades[i].ID]
		d.Trades[i].Tag = n[0]
		d.Trades[i].Strategy = n[1]
	}
	return d, err
}
func (a *API) dataset(w http.ResponseWriter, r *http.Request) {
	d, err := a.load(r.Context(), owner(r))
	if err != nil {
		log.Printf("analytics dataset failed: %v", err)
		fail(w, 500, "Analytics could not be loaded. Please retry.")
		return
	}
	respond(w, 200, d)
}
func (a *API) notes(w http.ResponseWriter, r *http.Request) {
	var req struct {
		TradeID  string `json:"tradeId"`
		Tag      string `json:"tag"`
		Strategy string `json:"strategy"`
	}
	if !decode(w, r, &req) {
		return
	}
	if len(req.Tag) > 80 || len(req.Strategy) > 80 {
		fail(w, 400, "Tag and strategy must be under 80 characters.")
		return
	}
	d, err := a.load(r.Context(), owner(r))
	if err != nil {
		fail(w, 500, "Unable to load trades.")
		return
	}
	found := false
	for _, t := range d.Trades {
		if t.ID == req.TradeID {
			found = true
			break
		}
	}
	if !found {
		fail(w, 404, "Trade not found.")
		return
	}
	_, err = a.db.Exec(r.Context(), `INSERT INTO analytics_trade_notes(user_id,trade_id,tag,strategy) VALUES($1,$2,$3,$4) ON CONFLICT(user_id,trade_id) DO UPDATE SET tag=excluded.tag,strategy=excluded.strategy`, owner(r), req.TradeID, strings.TrimSpace(req.Tag), strings.TrimSpace(req.Strategy))
	if err != nil {
		fail(w, 500, "Unable to save annotation.")
		return
	}
	respond(w, 200, map[string]bool{"saved": true})
}
func (a *API) tradeChart(w http.ResponseWriter, r *http.Request) {
	d, err := a.load(r.Context(), owner(r))
	if err != nil {
		fail(w, 500, "Unable to load trade.")
		return
	}
	var selected *Trade
	for i := range d.Trades {
		if d.Trades[i].ID == r.URL.Query().Get("trade") {
			selected = &d.Trades[i]
			break
		}
	}
	if selected == nil {
		fail(w, 404, "Trade not found.")
		return
	}
	end := time.Now().UnixMilli()
	if selected.ClosedAt != nil {
		end = *selected.ClosedAt
	}
	if end-selected.OpenedAt > 30_000*60000 {
		fail(w, 422, "This trade exceeds the 30,000-minute chart limit.")
		return
	}
	candles, err := newExchange(selected.Exchange, Credentials{}).candles(r.Context(), selected.Symbol, selected.OpenedAt, end)
	if err != nil {
		fail(w, 502, "Mark-price history is unavailable. MFE and MAE cannot be calculated.")
		return
	}
	sort.Slice(candles, func(i, j int) bool { return candles[i].Time < candles[j].Time })
	mfe, mae, captured := Excursions(*selected, candles)
	if selected.ClosedAt != nil && mfe != nil {
		_, err = a.db.Exec(r.Context(), `INSERT INTO analytics_trade_metrics(connection_id,trade_id,mfe,mae,captured) VALUES($1,$2,$3,$4,$5) ON CONFLICT(connection_id,trade_id) DO UPDATE SET mfe=excluded.mfe,mae=excluded.mae,captured=excluded.captured,calculated_at=now()`, selected.ConnectionID, selected.ID, mfe, mae, captured)
		if err != nil {
			fail(w, 500, "Unable to save excursion measurements.")
			return
		}
	}
	respond(w, 200, map[string]any{"candles": candles, "mfe": mfe, "mae": mae, "captured": captured, "method": "1-minute mark-price samples; execution candles excluded. Gross PnL basis."})
}

// Run owns durable sync leases. A process restart resumes expired leases; each
// successful import commits events, snapshot and watermark atomically.
func (a *API) Run(ctx context.Context) {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			a.runNext(ctx)
		}
	}
}
func (a *API) runNext(parent context.Context) {
	if a.encryption == nil {
		return
	}
	ctx, cancel := context.WithTimeout(parent, 10*time.Minute)
	defer cancel()
	tx, err := a.db.Begin(ctx)
	if err != nil {
		return
	}
	defer tx.Rollback(context.Background())
	var c Connection
	var user string
	var encrypted []byte
	err = tx.QueryRow(ctx, `SELECT id::text,user_id,exchange,account_type,coverage_from,synced_through,credentials FROM analytics_connections WHERE status='queued' OR (status='ready' AND last_sync<now()-interval '15 minutes') OR (status='syncing' AND lease_until<now()) OR (status='error' AND lease_until<now()-interval '30 minutes') ORDER BY last_sync NULLS FIRST FOR UPDATE SKIP LOCKED LIMIT 1`).Scan(&c.ID, &user, &c.Exchange, &c.AccountType, &c.CoverageFrom, &c.SyncedThrough, &encrypted)
	if err != nil {
		return
	}
	_, err = tx.Exec(ctx, `UPDATE analytics_connections SET status='syncing',lease_until=now()+interval '12 minutes',error='' WHERE id=$1`, c.ID)
	if err != nil {
		return
	}
	if tx.Commit(ctx) != nil {
		return
	}
	creds, err := a.open(user, encrypted)
	if err == nil {
		client := newExchange(c.Exchange, creds)
		err = client.validate(ctx)
		if err == nil {
			from := c.CoverageFrom
			if c.SyncedThrough != nil {
				from = c.SyncedThrough.Add(-24 * time.Hour)
				if from.Before(c.CoverageFrom) {
					from = c.CoverageFrom
				}
			}
			to := time.Now().UTC()
			var result syncResult
			result, err = client.sync(ctx, c, from, to)
			if err == nil {
				err = a.commitSync(ctx, c, to, result)
			}
		}
	}
	if err != nil {
		cleanup, cancelCleanup := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancelCleanup()
		msg := err.Error()
		if len(msg) > 350 {
			msg = msg[:350]
		}
		_, _ = a.db.Exec(cleanup, `UPDATE analytics_connections SET status='error',error=$2,lease_until=now() WHERE id=$1`, c.ID, msg)
		log.Printf("analytics sync failed for connection %s", c.ID)
	}
}
func (a *API) commitSync(ctx context.Context, c Connection, to time.Time, result syncResult) error {
	tx, err := a.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(context.Background())
	// Lock before inserting: a concurrently disconnected connection cannot reappear.
	var id string
	if err = tx.QueryRow(ctx, `SELECT id::text FROM analytics_connections WHERE id=$1 FOR UPDATE`, c.ID).Scan(&id); err != nil {
		return err
	}
	for start := 0; start < len(result.Events); start += 500 {
		end := start + 500
		if end > len(result.Events) {
			end = len(result.Events)
		}
		batch := &pgx.Batch{}
		for _, e := range result.Events[start:end] {
			normalized, _ := json.Marshal(e.Normalized)
			raw, _ := json.Marshal(e.Raw)
			// Keep the original exchange payload; reimport can correct derived
			// classifications without duplicating events or erasing annotations.
			batch.Queue(`INSERT INTO analytics_events(connection_id,source_id,kind,at,symbol,currency,amount,normalized,raw) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9) ON CONFLICT(connection_id,source_id,kind) DO UPDATE SET normalized=excluded.normalized WHERE analytics_events.normalized IS DISTINCT FROM excluded.normalized`, c.ID, e.ID, e.Kind, time.UnixMilli(e.At), e.Symbol, e.Currency, e.Amount, normalized, raw)
		}
		if err = tx.SendBatch(ctx, batch).Close(); err != nil {
			return err
		}
	}
	b, _ := json.Marshal(result.Snapshot)
	_, err = tx.Exec(ctx, `INSERT INTO analytics_snapshots(connection_id,at,equity,wallet,snapshot) VALUES($1,$2,$3,$4,$5) ON CONFLICT DO NOTHING`, c.ID, time.UnixMilli(result.Snapshot.At), decimal(result.Snapshot.Equity), decimal(result.Snapshot.Wallet), b)
	if err != nil {
		return err
	}
	warnings, _ := json.Marshal(result.Warnings)
	_, err = tx.Exec(ctx, `UPDATE analytics_connections SET status='ready',last_sync=now(),synced_through=$2,lease_until=NULL,warnings=$3,error='' WHERE id=$1`, c.ID, to, warnings)
	if err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (a *API) String() string {
	return fmt.Sprintf("analytics (encryption configured: %t)", a.encryption != nil)
}
