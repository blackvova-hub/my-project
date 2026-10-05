package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"math"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"alert-engine/internal/onchain"
	"alert-engine/internal/onchain/providers/blockstream"
	"alert-engine/internal/onchain/providers/etherscan"
	"alert-engine/internal/onchain/providers/solana"
	"alert-engine/internal/onchain/providers/trongrid"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
)

type walletLabel struct {
	EntityID, EntityName, EntityType, Status string
	KnownBalanceUSD, DailyVolumeUSD          float64
}

func startOnchainMonitor(ctx context.Context, db *pgxpool.Pool, rdb *redis.Client, cfg Config) {
	// Ethereum включается наличием ETHERSCAN_API_KEY.
	// Bitcoin, Solana и Tron включаются отдельными ONCHAIN_*_ENABLED флагами.
	providers := make([]onchain.Provider, 0, 4)
	if strings.TrimSpace(cfg.EtherscanAPIKey) != "" {
		providers = append(providers, etherscan.New(cfg.EtherscanAPIKey, cfg.EtherscanChainID))
	}
	if cfg.OnchainBTCEnabled {
		providers = append(providers, blockstream.New(cfg.BitcoinAPIURL))
	}
	if cfg.OnchainSolanaEnabled {
		providers = append(providers, solana.New(cfg.SolanaRPCURL))
	}
	if cfg.OnchainTronEnabled {
		providers = append(providers, trongrid.New(cfg.TronGridBaseURL, cfg.TronGridAPIKey))
	}
	if len(providers) == 0 {
		log.Printf("onchain monitor enabled but no providers configured")
		return
	}
	interval := time.Duration(cfg.OnchainIntervalSeconds) * time.Second
	if interval < 30*time.Second {
		interval = 60 * time.Second
	}
	for _, provider := range providers {
		go (&onchainMonitor{db: db, rdb: rdb, cfg: cfg, provider: provider, interval: interval, maxAddresses: cfg.OnchainMaxAddresses, minUSD: cfg.OnchainMinUSD, binanceBaseURL: cfg.BinanceSpotBaseURL, httpClient: &http.Client{Timeout: 10 * time.Second}}).run(ctx)
	}
}

type onchainMonitor struct {
	db             *pgxpool.Pool
	rdb            *redis.Client
	cfg            Config
	provider       onchain.Provider
	interval       time.Duration
	maxAddresses   int
	minUSD         float64
	binanceBaseURL string
	httpClient     *http.Client
}

func (m *onchainMonitor) run(ctx context.Context) {
	ticker := time.NewTicker(m.interval)
	defer ticker.Stop()
	_ = m.poll(ctx)
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			_ = m.poll(ctx)
		}
	}
}
func (m *onchainMonitor) poll(ctx context.Context) error {
	rows, err := m.db.Query(ctx, `SELECT chain,address FROM wallet_registry WHERE status = ANY($3::text[]) AND chain=$1 ORDER BY last_seen_at DESC NULLS LAST LIMIT $2`, m.provider.Chain(), maxInt(m.maxAddresses, 100), monitoredWalletStatuses())
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var chain, address string
		if err := rows.Scan(&chain, &address); err != nil {
			continue
		}
		cursor, err := m.loadCursor(ctx, address)
		if err != nil {
			log.Printf("onchain cursor load provider=%s address=%s err=%v", m.provider.ID(), address, err)
			continue
		}
		batch, fetchErr := m.provider.Transfers(ctx, address, cursor)
		if fetchErr != nil {
			log.Printf("onchain partial fetch provider=%s chain=%s address=%s transfers=%d err=%v", m.provider.ID(), chain, address, len(batch.Transfers), fetchErr)
		}
		for _, tr := range batch.Transfers {
			if err := m.handleTransfer(ctx, tr); err != nil {
				log.Printf("onchain transfer %s err=%v", tr.TxHash, err)
			}
		}
		if batch.Cursor.BlockNumber > cursor.BlockNumber || batch.Cursor.Token != cursor.Token || batch.Cursor.UpdatedAt.After(cursor.UpdatedAt) {
			if err := m.saveCursor(ctx, address, batch.Cursor); err != nil {
				log.Printf("onchain cursor save provider=%s address=%s err=%v", m.provider.ID(), address, err)
			}
		}
	}
	return rows.Err()
}

func monitoredWalletStatuses() []string {
	return []string{"verified", "probable"}
}

func (m *onchainMonitor) handleTransfer(ctx context.Context, tr onchain.Transfer) error {
	if tr.USDValue <= 0 {
		tr.USDValue = m.estimateUSD(ctx, tr.Symbol, tr.TokenAmount)
	}
	ingestionFloor := m.minUSD
	if ingestionFloor > 5_000_000 {
		ingestionFloor = 5_000_000
	}
	if ingestionFloor > 0 && tr.USDValue > 0 && tr.USDValue < ingestionFloor {
		return nil
	}
	from, err := m.lookupLabel(ctx, tr.Chain, tr.FromAddress)
	if err != nil {
		return err
	}
	to, err := m.lookupLabel(ctx, tr.Chain, tr.ToAddress)
	if err != nil {
		return err
	}
	if from.Status == "ignore" || to.Status == "ignore" || sameKnownEntity(from, to) {
		return m.recordTransfer(ctx, tr, from, to, true)
	}
	if err := m.recordTransfer(ctx, tr, from, to, false); err != nil {
		return err
	}
	significance := transferSignificance(tr, from, to)
	if significance >= 40 {
		_ = m.noteUnknown(ctx, tr, from, to)
	}
	decision := evaluateOnchainTransfer(tr, from, to)
	if !decision.Publish {
		return nil
	}
	ce := CandleEvent{Exchange: firstNonEmpty(to.EntityName, from.EntityName, "onchain"), MarketType: "spot", Symbol: strings.ToUpper(tr.Symbol), TS: tr.BlockTime.UnixMilli()}
	title := fmt.Sprintf("%s: перевод %s → %s на %s", ce.Symbol, displayEntity(from), displayEntity(to), formatUSD(tr.USDValue))
	event := newImportantEvent(ce, "onchain", "onchain_transfer", decision.Direction, 10)
	event.EventAt = tr.BlockTime
	event.SourceKind = "onchain"
	event.SourceRef = tr.TxHash
	event.Confidence = decision.Confidence
	event.AmountUSD = floatPtr(tr.USDValue)
	event.Title = title
	event.Details = fmt.Sprintf("%s → %s; сумма %s; транзакция %s", displayEntity(from), displayEntity(to), formatUSD(tr.USDValue), tr.TxHash)
	event.Metadata = map[string]any{"fromAddress": tr.FromAddress, "toAddress": tr.ToAddress, "fromEntity": displayEntity(from), "toEntity": displayEntity(to), "fromEntityType": from.EntityType, "toEntityType": to.EntityType, "usdValue": tr.USDValue, "relevantKnownBalanceUsd": decision.RelevantBalanceUSD, "publicationThresholdUsd": decision.ThresholdUSD, "balanceShare": balanceShare(tr, from, to), "provider": m.provider.ID(), "requiredObservations": 1, "scenario": decision.Scenario, "direction": decision.Direction, "sourceRef": tr.TxHash}
	_, err = syncQualityImportantEvents(ctx, m.db, importantEvaluation{Events: []importantEvent{event}, EvaluatedFamilies: map[string]bool{"onchain": true}})
	if err != nil {
		return err
	}
	return deliverImportantEventOutbox(ctx, m.db, m.rdb, m.cfg)
}

type onchainTransferDecision struct {
	Publish            bool
	Scenario           string
	Direction          string
	RelevantBalanceUSD float64
	ThresholdUSD       float64
	Confidence         int
}

// evaluateOnchainTransfer is the public-event policy. Transfer storage and
// unknown-wallet discovery deliberately remain independent from publication.
func evaluateOnchainTransfer(tr onchain.Transfer, from, to walletLabel) onchainTransferDecision {
	decision := onchainTransferDecision{Confidence: 2}
	if tr.USDValue <= 0 || from.Status == "ignore" || to.Status == "ignore" || sameKnownEntity(from, to) {
		return decision
	}
	fromKnown, toKnown := isVerifiedOrProbable(from), isVerifiedOrProbable(to)
	if !fromKnown && !toKnown {
		return decision
	}
	if !fromKnown || !toKnown {
		known := to
		decision.Direction = "from_unknown"
		if fromKnown {
			known = from
			decision.Direction = "to_unknown"
		}
		if !isRelevantKnownWallet(known) {
			return onchainTransferDecision{}
		}
		decision.Scenario = "extreme_unknown_counterparty"
		decision.RelevantBalanceUSD = known.KnownBalanceUSD
		decision.ThresholdUSD = math.Max(100_000_000, known.KnownBalanceUSD*.01)
		decision.Publish = tr.USDValue >= decision.ThresholdUSD
		decision.Confidence = 1
		return decision
	}
	switch {
	case isHacker(from) && (isMixer(to) || isExchange(to)):
		decision.Scenario = "hacker_to_" + destinationKind(to)
		decision.Direction = "to_" + destinationKind(to)
		decision.RelevantBalanceUSD = from.KnownBalanceUSD
	case isGovernment(from) && isExchange(to):
		decision.Scenario = "government_to_exchange"
		decision.Direction = "to_exchange"
		decision.RelevantBalanceUSD = from.KnownBalanceUSD
	case isStrategic(from) && isExchange(to):
		decision.Scenario = "strategic_to_exchange"
		decision.Direction = "to_exchange"
		decision.RelevantBalanceUSD = from.KnownBalanceUSD
	case isExchange(from) && isStrategic(to):
		decision.Scenario = "exchange_to_strategic"
		decision.Direction = "from_exchange"
		decision.RelevantBalanceUSD = to.KnownBalanceUSD
	default:
		return onchainTransferDecision{}
	}
	decision.ThresholdUSD = math.Max(5_000_000, decision.RelevantBalanceUSD*.01)
	decision.Publish = tr.USDValue >= decision.ThresholdUSD
	decision.Confidence = 3
	return decision
}

func isVerifiedOrProbable(label walletLabel) bool {
	return label.Status == "verified" || label.Status == "probable"
}
func isRelevantKnownWallet(label walletLabel) bool {
	return isVerifiedOrProbable(label) && (isExchange(label) || isStrategic(label) || isMixer(label))
}
func entityTypeContains(label walletLabel, values ...string) bool {
	typeName := strings.ToLower(strings.NewReplacer("-", "_", " ", "_").Replace(strings.TrimSpace(label.EntityType)))
	for _, value := range values {
		if strings.Contains(typeName, value) {
			return true
		}
	}
	return false
}
func isGovernment(label walletLabel) bool { return entityTypeContains(label, "government") }
func isHacker(label walletLabel) bool     { return entityTypeContains(label, "hacker", "exploiter") }
func isMixer(label walletLabel) bool      { return entityTypeContains(label, "mixer", "tumbler") }
func destinationKind(label walletLabel) string {
	if isMixer(label) {
		return "mixer"
	}
	return "exchange"
}
func (m *onchainMonitor) noteUnknown(ctx context.Context, tr onchain.Transfer, from, to walletLabel) error {
	pairs := []struct {
		address      string
		counterparty string
		label        walletLabel
	}{
		{tr.FromAddress, tr.ToAddress, from},
		{tr.ToAddress, tr.FromAddress, to},
	}

	for _, pair := range pairs {
		if !isUnknown(pair.label) {
			continue
		}

		if _, err := m.db.Exec(
			ctx,
			`UPDATE wallet_registry
			 SET last_seen_at=GREATEST(last_seen_at,$3),
			     significant_tx_count=significant_tx_count+1,
			     largest_tx_usd=GREATEST(largest_tx_usd,$4),
			     sample_tx_hashes=CASE WHEN sample_tx_hashes ? $5 THEN sample_tx_hashes ELSE sample_tx_hashes || jsonb_build_array($5) END,
			     counterparties=CASE WHEN counterparties ? $6 THEN counterparties ELSE counterparties || jsonb_build_array($6) END,
			     discovery_source=$7,
			     updated_at=now()
			 WHERE chain=$1 AND address=$2 AND status='unknown'`,
			tr.Chain,
			pair.address,
			tr.BlockTime,
			tr.USDValue,
			tr.TxHash,
			pair.counterparty,
			m.provider.ID(),
		); err != nil {
			return err
		}
	}

	return nil
}
func (m *onchainMonitor) recordTransfer(ctx context.Context, tr onchain.Transfer, from, to walletLabel, internal bool) error {
	meta, _ := json.Marshal(map[string]any{"provider": m.provider.ID(), "internal": internal})
	_, err := m.db.Exec(ctx, `INSERT INTO onchain_transfers(chain,tx_hash,token_address,symbol,block_number,block_time,from_address,to_address,from_entity_id,to_entity_id,usd_value,token_amount,transfer_kind,is_internal,significance,metadata) VALUES($1,$2,$3,$4,$5,$6,$7,$8,NULLIF($9,'')::uuid,NULLIF($10,'')::uuid,$11,$12,$13,$14,$15,$16::jsonb) ON CONFLICT(chain,tx_hash,from_address,to_address,token_address) DO UPDATE SET usd_value=EXCLUDED.usd_value,transfer_kind=EXCLUDED.transfer_kind,is_internal=EXCLUDED.is_internal,significance=EXCLUDED.significance,metadata=EXCLUDED.metadata`, tr.Chain, tr.TxHash, tr.TokenAddress, tr.Symbol, tr.BlockNumber, tr.BlockTime, tr.FromAddress, tr.ToAddress, from.EntityID, to.EntityID, tr.USDValue, tr.TokenAmount, transferType(from, to, internal), internal, transferSignificance(tr, from, to), string(meta))
	return err
}
func (m *onchainMonitor) lookupLabel(ctx context.Context, chain, address string) (walletLabel, error) {
	var l walletLabel
	err := m.db.QueryRow(ctx, `SELECT COALESCE(entity_id::text,''),COALESCE(entity_name,''),COALESCE(entity_type,''),status,COALESCE(known_balance_usd,0),COALESCE(daily_volume_usd,0) FROM wallet_registry WHERE chain=$1 AND address=$2`, chain, strings.ToLower(address)).Scan(&l.EntityID, &l.EntityName, &l.EntityType, &l.Status, &l.KnownBalanceUSD, &l.DailyVolumeUSD)
	if err == nil {
		return l, nil
	}
	if err := m.db.QueryRow(ctx, `INSERT INTO wallet_registry(chain,address,status,label_source,first_seen_at,last_seen_at,significant_tx_count) VALUES($1,$2,'unknown','onchain_monitor',now(),now(),0) ON CONFLICT(chain,address) DO UPDATE SET last_seen_at=now() RETURNING COALESCE(entity_id::text,''),COALESCE(entity_name,''),COALESCE(entity_type,''),status,COALESCE(known_balance_usd,0),COALESCE(daily_volume_usd,0)`, chain, strings.ToLower(address)).Scan(&l.EntityID, &l.EntityName, &l.EntityType, &l.Status, &l.KnownBalanceUSD, &l.DailyVolumeUSD); err != nil {
		return l, err
	}
	return l, nil
}
func (m *onchainMonitor) estimateUSD(ctx context.Context, symbol string, amount float64) float64 {
	if amount <= 0 {
		return 0
	}
	symbol = strings.ToUpper(strings.TrimSpace(symbol))
	if symbol == "USDT" || symbol == "USDC" || symbol == "DAI" || symbol == "FDUSD" {
		return amount
	}
	if symbol == "WBTC" {
		symbol = "BTC"
	}
	if symbol == "WETH" {
		symbol = "ETH"
	}
	u, err := url.Parse(strings.TrimRight(m.binanceBaseURL, "/") + "/api/v3/ticker/price")
	if err != nil {
		return 0
	}
	q := u.Query()
	q.Set("symbol", symbol+"USDT")
	u.RawQuery = q.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return 0
	}
	resp, err := m.httpClient.Do(req)
	if err != nil {
		return 0
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		return 0
	}
	var body struct {
		Price string `json:"price"`
	}
	if json.NewDecoder(resp.Body).Decode(&body) != nil {
		return 0
	}
	price, _ := strconv.ParseFloat(body.Price, 64)
	return amount * price
}
func transferSignificance(tr onchain.Transfer, from, to walletLabel) float64 {
	score := 0.0
	if tr.USDValue > 0 {
		score += math.Min(30, math.Log10(tr.USDValue+1)*5)
	}
	score += math.Min(20, balanceShare(tr, from, to)*100)
	score += math.Min(20, liquidityShare(tr)*100)
	if isStrategic(from) && isExchange(to) {
		score += 45
	}
	if isStrategic(to) && isExchange(from) {
		score += 25
	}
	if isUnknown(from) || isUnknown(to) {
		score -= 15
		if tr.USDValue >= 100000000 {
			score = math.Max(score, 75)
		}
	}
	return score
}
func balanceShare(tr onchain.Transfer, from, to walletLabel) float64 {
	b := from.KnownBalanceUSD
	if b <= 0 {
		b = to.KnownBalanceUSD
	}
	if b <= 0 {
		return 0
	}
	return tr.USDValue / b
}
func liquidityShare(tr onchain.Transfer) float64 {
	if tr.USDValue <= 0 {
		return 0
	}
	return math.Min(1, tr.USDValue/10000000)
}
func sameKnownEntity(a, b walletLabel) bool { return a.EntityID != "" && a.EntityID == b.EntityID }
func isExchange(l walletLabel) bool {
	return strings.Contains(strings.ToLower(l.EntityType), "exchange")
}
func isStrategic(l walletLabel) bool {
	return isVerifiedOrProbable(l) && entityTypeContains(l, "government", "fund", "institution", "treasury", "foundation", "etf", "market_maker", "custodian", "whale", "hacker", "exploiter")
}
func isUnknown(l walletLabel) bool { return l.Status == "unknown" || l.Status == "" }
func displayEntity(l walletLabel) string {
	return firstNonEmpty(l.EntityName, "неизвестный кошелёк")
}
func transferType(from, to walletLabel, internal bool) string {
	if internal {
		return "internal_custody"
	}
	if isExchange(to) {
		return "exchange_inflow"
	}
	if isExchange(from) {
		return "exchange_outflow"
	}
	return "external"
}
func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return "unknown"
}
func (m *onchainMonitor) loadCursor(ctx context.Context, address string) (onchain.Cursor, error) {
	var c onchain.Cursor
	var at *time.Time
	err := m.db.QueryRow(ctx, `SELECT last_scanned_block,cursor_token,last_scanned_at FROM onchain_scan_cursors WHERE provider=$1 AND chain=$2 AND address=$3`, m.provider.ID(), m.provider.Chain(), address).Scan(&c.BlockNumber, &c.Token, &at)
	if err == pgx.ErrNoRows {
		return c, nil
	}
	if err != nil {
		return c, err
	}
	if at != nil {
		c.UpdatedAt = at.UTC()
	}
	return c, nil
}
func (m *onchainMonitor) saveCursor(ctx context.Context, address string, c onchain.Cursor) error {
	_, err := m.db.Exec(ctx, `INSERT INTO onchain_scan_cursors(provider,chain,address,last_scanned_block,cursor_token,last_scanned_at,updated_at) VALUES($1,$2,$3,$4,$5,$6,now()) ON CONFLICT(provider,chain,address) DO UPDATE SET last_scanned_block=GREATEST(onchain_scan_cursors.last_scanned_block,EXCLUDED.last_scanned_block),cursor_token=EXCLUDED.cursor_token,last_scanned_at=EXCLUDED.last_scanned_at,updated_at=now()`, m.provider.ID(), m.provider.Chain(), address, c.BlockNumber, c.Token, c.UpdatedAt)
	return err
}
