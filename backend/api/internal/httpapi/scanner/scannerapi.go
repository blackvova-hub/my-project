package scannerapi

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"backend/internal/auth"
)

type API struct {
	authStore            *auth.Store
	db                   *pgxpool.Pool
	httpClient           *http.Client
	symbolCache          map[string]symbolCacheEntry
	symbolCacheMu        sync.RWMutex
	symbolCacheLastSweep time.Time
}

type symbolCacheEntry struct {
	Exists       bool
	CheckedAt    time.Time
	LastAccessed time.Time
}

type symbolInput struct {
	Raw        string
	Normalized string
	Index      int
}

type symbolList struct {
	Symbols    []string
	Items      []symbolInput
	IsWildcard bool
}

const alertWindowMaxMinutes = 1440

func isValidAlertWindowMinutes(value int) bool {
	return value >= 1 && value <= alertWindowMaxMinutes
}

func New(authStore *auth.Store, db *pgxpool.Pool) *API {
	return &API{
		authStore: authStore,
		db:        db,
		httpClient: &http.Client{
			Timeout: 6 * time.Second,
		},
		symbolCache: make(map[string]symbolCacheEntry),
	}
}

func (a *API) Routes(r chi.Router) {
	r.Group(func(r chi.Router) {
		r.Use(a.requireAuth)

		r.Get("/scanner/rules", a.handleListAlerts)
		r.Post("/scanner/rules", a.handleCreateAlert)
		r.Patch("/scanner/rules/{id}", a.handleUpdateAlert)
		r.Delete("/scanner/rules/{id}", a.handleDeleteAlert)

		r.Get("/signals", a.handleListSignals)
		r.Get("/trades", a.handleListTrades)
		r.Get("/trades/stats", a.handleTradeStats)
		r.Get("/trades/strategies", a.handleListTradeStrategies)
		r.Get("/trades/meta", a.handleGetTradeMeta)
		r.Post("/trades", a.handleCreateTrade)
		r.Patch("/trades/{id}", a.handleCloseTrade)
		r.Delete("/trades/strategies/{id}", a.handleDeleteTradeStrategy)
		r.Delete("/trades/strategies/{id}/pairs/{symbol}", a.handleDeleteTradeStrategyPair)
	})
}

// --------------------
// Auth middleware
// --------------------

type ctxKey string

const ctxUserKey ctxKey = "auth_user"

func (a *API) requireAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user, err := a.authStore.Authenticate(r)
		if err != nil {
			writeErr(w, http.StatusUnauthorized, "unauthorized")
			return
		}
		ctx := context.WithValue(r.Context(), ctxUserKey, user)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func userFromCtx(r *http.Request) (auth.User, bool) {
	v := r.Context().Value(ctxUserKey)
	u, ok := v.(auth.User)
	return u, ok
}

func (a *API) userNumID(ctx context.Context, u auth.User) (int64, error) {
	if u.NumID > 0 {
		return u.NumID, nil
	}
	var numID int64
	err := a.db.QueryRow(ctx, "SELECT num_id FROM users WHERE id = $1", u.ID).Scan(&numID)
	if err != nil {
		return 0, err
	}
	return numID, nil
}

// --------------------
// DTOs (new schema)
// --------------------

type AlertDTO struct {
	ID               int64          `json:"id"`
	Exchange         string         `json:"exchange"`
	MarketType       string         `json:"market_type"`
	Indicator        string         `json:"indicator"`
	Symbol           string         `json:"symbol"`
	WindowMinutes    int            `json:"window_minutes"`
	ThresholdPercent *float64       `json:"threshold_percent"`
	ThresholdAmount  *float64       `json:"threshold_amount"`
	Direction        string         `json:"direction"`        // up | down | both
	CooldownSeconds  int            `json:"cooldown_seconds"` // seconds
	Enabled          bool           `json:"enabled"`
	ScannerSlot      string         `json:"scanner_slot"`
	Conditions       []ConditionDTO `json:"conditions,omitempty"`
	CreatedAt        string         `json:"created_at"`
	UpdatedAt        string         `json:"updated_at"`
}

type ConditionDTO struct {
	Indicator        string   `json:"indicator"`
	Direction        string   `json:"direction"`
	ThresholdPercent *float64 `json:"threshold_percent"`
	ThresholdAmount  *float64 `json:"threshold_amount"`
	Negate           bool     `json:"negate"`
}

type CreateAlertInput struct {
	Exchange         string         `json:"exchange"`
	MarketType       string         `json:"market_type"`
	Indicator        string         `json:"indicator"`
	Symbol           string         `json:"symbol"`
	WindowMinutes    int            `json:"window_minutes"`
	ThresholdPercent *float64       `json:"threshold_percent"`
	ThresholdAmount  *float64       `json:"threshold_amount"`
	Direction        string         `json:"direction"`
	CooldownSeconds  int            `json:"cooldown_seconds"`
	Enabled          *bool          `json:"enabled"`
	ScannerSlot      string         `json:"scanner_slot"`
	Conditions       []ConditionDTO `json:"conditions"`
}

type UpdateAlertInput struct {
	Exchange         *string         `json:"exchange"`
	MarketType       *string         `json:"market_type"`
	Indicator        *string         `json:"indicator"`
	Symbol           *string         `json:"symbol"`
	WindowMinutes    *int            `json:"window_minutes"`
	ThresholdPercent *float64        `json:"threshold_percent"`
	ThresholdAmount  *float64        `json:"threshold_amount"`
	Direction        *string         `json:"direction"`
	CooldownSeconds  *int            `json:"cooldown_seconds"`
	Enabled          *bool           `json:"enabled"`
	ScannerSlot      *string         `json:"scanner_slot"`
	Conditions       *[]ConditionDTO `json:"conditions"`
}

type SignalDTO struct {
	ID          string          `json:"id"`
	RuleID      int64           `json:"rule_id"`
	UserID      int64           `json:"user_id"`
	Exchange    string          `json:"exchange"`
	MarketType  string          `json:"market_type"`
	Symbol      string          `json:"symbol"`
	TF          string          `json:"tf"`
	TS          int64           `json:"ts"`
	Payload     json.RawMessage `json:"payload"`
	ScannerSlot string          `json:"scanner_slot"`
	CreatedAt   string          `json:"created_at"`
}

type TradeDTO struct {
	ID            string   `json:"id"`
	SignalID      string   `json:"signal_id"`
	UserID        int64    `json:"user_id"`
	Symbol        string   `json:"symbol"`
	Status        string   `json:"status"`
	BuyAt         string   `json:"buy_at"`
	SellAt        *string  `json:"sell_at"`
	DurationMin   *int     `json:"duration_minutes"`
	ProfitPercent *float64 `json:"profit_percent"`
	ProfitUSD     *float64 `json:"profit_usd"`
	Exchange      *string  `json:"exchange"`
	Timeframe     *string  `json:"timeframe"`
	Comment       *string  `json:"comment"`
	EntryBasis    *string  `json:"entry_basis"`
	EntryPhotos   []string `json:"entry_photos"`
	StrategyID    *string  `json:"strategy_id"`
	StrategyName  *string  `json:"strategy_name"`
	CreatedAt     string   `json:"created_at"`
	UpdatedAt     string   `json:"updated_at"`
}

type TradeStrategyPairDTO struct {
	Symbol           string   `json:"symbol"`
	TotalTrades      int      `json:"total_trades"`
	ClosedTrades     int      `json:"closed_trades"`
	WinRate          *float64 `json:"win_rate"`
	AvgProfitPercent *float64 `json:"avg_profit_percent"`
	SumProfitPercent *float64 `json:"sum_profit_percent"`
	AvgProfitUSD     *float64 `json:"avg_profit_usd"`
	SumProfitUSD     *float64 `json:"sum_profit_usd"`
	AvgDurationMin   *int     `json:"avg_duration_minutes"`
}

type TradeStrategyDTO struct {
	ID               string                 `json:"id"`
	Name             string                 `json:"name"`
	TotalTrades      int                    `json:"total_trades"`
	ClosedTrades     int                    `json:"closed_trades"`
	WinRate          *float64               `json:"win_rate"`
	AvgProfitPercent *float64               `json:"avg_profit_percent"`
	SumProfitPercent *float64               `json:"sum_profit_percent"`
	AvgProfitUSD     *float64               `json:"avg_profit_usd"`
	SumProfitUSD     *float64               `json:"sum_profit_usd"`
	AvgDurationMin   *int                   `json:"avg_duration_minutes"`
	Pairs            []TradeStrategyPairDTO `json:"pairs"`
}

type TradeMetaDTO struct {
	Timeframe string `json:"timeframe"`
	Exchange  string `json:"exchange"`
	Strategy  string `json:"strategy"`
}

type TradeStatsDTO struct {
	TotalTrades        int      `json:"total_trades"`
	OpenTrades         int      `json:"open_trades"`
	ClosedTrades       int      `json:"closed_trades"`
	WinRate            *float64 `json:"win_rate"`
	AvgProfitPercent   *float64 `json:"avg_profit_percent"`
	SumProfitPercent   *float64 `json:"sum_profit_percent"`
	AvgProfitUSD       *float64 `json:"avg_profit_usd"`
	SumProfitUSD       *float64 `json:"sum_profit_usd"`
	BestProfitPercent  *float64 `json:"best_profit_percent"`
	WorstProfitPercent *float64 `json:"worst_profit_percent"`
	BestProfitUSD      *float64 `json:"best_profit_usd"`
	WorstProfitUSD     *float64 `json:"worst_profit_usd"`
	AvgDurationMin     *int     `json:"avg_duration_minutes"`
	MaxDurationMin     *int     `json:"max_duration_minutes"`
	MinDurationMin     *int     `json:"min_duration_minutes"`
	FirstBuyAt         *string  `json:"first_buy_at"`
	LastSellAt         *string  `json:"last_sell_at"`
}

type CreateTradeInput struct {
	SignalID string  `json:"signal_id"`
	BuyAt    *string `json:"buy_at"`
}

type CloseTradeInput struct {
	SellAt        *string   `json:"sell_at"`
	DurationMin   *int      `json:"duration_minutes"`
	ProfitPercent *float64  `json:"profit_percent"`
	ProfitUSD     *float64  `json:"profit_usd"`
	Exchange      *string   `json:"exchange"`
	Timeframe     *string   `json:"timeframe"`
	Comment       *string   `json:"comment"`
	EntryBasis    *string   `json:"entry_basis"`
	EntryPhotos   *[]string `json:"entry_photos"`
	Strategy      *string   `json:"strategy"`
}

// --------------------
// Handlers: Alerts
// --------------------

func (a *API) handleListAlerts(w http.ResponseWriter, r *http.Request) {
	user, ok := userFromCtx(r)
	if !ok {
		writeErr(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	numID, err := a.userNumID(r.Context(), user)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "user lookup failed")
		return
	}

	slotParam := strings.TrimSpace(r.URL.Query().Get("slot"))
	slotFilter := ""
	if slotParam != "" {
		switch strings.ToUpper(slotParam) {
		case "1", "SLOT_1":
			slotFilter = "SLOT_1"
		case "2", "SLOT_2":
			slotFilter = "SLOT_2"
		case "3", "SLOT_3":
			slotFilter = "SLOT_3"
		default:
			writeErr(w, http.StatusBadRequest, "invalid_slot")
			return
		}
	}

	if slotFilter == "SLOT_2" && !auth.IsStandard(user.Plan) {
		writeErr(w, http.StatusForbidden, "plan_required")
		return
	}
	if slotFilter == "SLOT_3" && !auth.IsPro(user.Plan) {
		writeErr(w, http.StatusForbidden, "plan_required")
		return
	}

	if slotFilter == "SLOT_2" && !auth.IsStandard(user.Plan) {
		writeErr(w, http.StatusForbidden, "plan_required")
		return
	}
	if slotFilter == "SLOT_3" && !auth.IsPro(user.Plan) {
		writeErr(w, http.StatusForbidden, "plan_required")
		return
	}

	baseQuery := `
		SELECT id, exchange, market_type, indicator, symbol, window_minutes, threshold_percent, threshold_amount, direction, cooldown_seconds, enabled, created_at, updated_at, conditions, scanner_slot
		FROM alerts
		WHERE user_id = $1
	`
	args := []any{numID}
	if slotFilter != "" {
		args = append(args, slotFilter)
		baseQuery += fmt.Sprintf(" AND scanner_slot = $%d", len(args))
	} else if !auth.IsPro(user.Plan) {
		if auth.IsStandard(user.Plan) {
			baseQuery += " AND scanner_slot IN ('SLOT_1','SLOT_2')"
		} else {
			baseQuery += " AND scanner_slot = 'SLOT_1'"
		}
	}
	if exchangeFilter := normalizeExchange(r.URL.Query().Get("exchange")); strings.TrimSpace(r.URL.Query().Get("exchange")) != "" {
		if !isValidExchange(exchangeFilter) {
			writeErr(w, http.StatusBadRequest, "invalid_exchange")
			return
		}
		args = append(args, exchangeFilter)
		baseQuery += fmt.Sprintf(" AND exchange = $%d", len(args))
	}
	if marketTypeFilter := normalizeMarketType(r.URL.Query().Get("market_type")); strings.TrimSpace(r.URL.Query().Get("market_type")) != "" {
		if !isValidMarketType(marketTypeFilter) {
			writeErr(w, http.StatusBadRequest, "invalid_market_type")
			return
		}
		args = append(args, marketTypeFilter)
		baseQuery += fmt.Sprintf(" AND market_type = $%d", len(args))
	}
	baseQuery += " ORDER BY scanner_slot, id DESC"

	rows, err := a.db.Query(r.Context(), baseQuery, args...)
	if err != nil {
		if isMissingScannerColumn(err) {
			writeErr(w, http.StatusInternalServerError, "db_migration_required")
			return
		}
		writeErr(w, http.StatusInternalServerError, "db error")
		return
	}
	defer rows.Close()

	var out []AlertDTO
	for rows.Next() {
		var aRow AlertDTO
		var createdAt, updatedAt time.Time
		var thresholdPercent, thresholdAmount pgtype.Float8
		var rawConditions []byte
		var scannerSlot string
		if err := rows.Scan(
			&aRow.ID,
			&aRow.Exchange,
			&aRow.MarketType,
			&aRow.Indicator,
			&aRow.Symbol,
			&aRow.WindowMinutes,
			&thresholdPercent,
			&thresholdAmount,
			&aRow.Direction,
			&aRow.CooldownSeconds,
			&aRow.Enabled,
			&createdAt,
			&updatedAt,
			&rawConditions,
			&scannerSlot,
		); err != nil {
			if isMissingScannerColumn(err) {
				writeErr(w, http.StatusInternalServerError, "db_migration_required")
				return
			}
			writeErr(w, http.StatusInternalServerError, "db error")
			return
		}
		if thresholdPercent.Valid {
			v := thresholdPercent.Float64
			aRow.ThresholdPercent = &v
		}
		if thresholdAmount.Valid {
			v := thresholdAmount.Float64
			aRow.ThresholdAmount = &v
		}
		conds := parseConditions(rawConditions)
		if len(conds) == 0 {
			conds = append(conds, ConditionDTO{
				Indicator:        aRow.Indicator,
				Direction:        aRow.Direction,
				ThresholdPercent: aRow.ThresholdPercent,
				ThresholdAmount:  aRow.ThresholdAmount,
			})
		}
		aRow.Conditions = conds
		aRow.ScannerSlot = scannerSlot
		aRow.CreatedAt = createdAt.Format(time.RFC3339)
		aRow.UpdatedAt = updatedAt.Format(time.RFC3339)
		out = append(out, aRow)
	}

	writeJSON(w, http.StatusOK, map[string]any{"rules": out})
}

func (a *API) handleCreateAlert(w http.ResponseWriter, r *http.Request) {
	user, ok := userFromCtx(r)
	if !ok {
		writeErr(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	numID, err := a.userNumID(r.Context(), user)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "user lookup failed")
		return
	}

	var in CreateAlertInput
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	in.Exchange = normalizeExchange(in.Exchange)
	in.MarketType = normalizeMarketType(in.MarketType)
	if !isSupportedInstrument(in.Exchange, in.MarketType) {
		writeErr(w, http.StatusBadRequest, "unsupported_exchange_market_type")
		return
	}

	if in.ScannerSlot == "" {
		in.ScannerSlot = "SLOT_1"
	}
	if !isValidScannerSlot(in.ScannerSlot) {
		writeErr(w, http.StatusBadRequest, "invalid scanner_slot")
		return
	}
	if in.ScannerSlot == "SLOT_2" && !auth.IsStandard(user.Plan) {
		writeErr(w, http.StatusForbidden, "plan_required")
		return
	}
	if in.ScannerSlot == "SLOT_3" && !auth.IsPro(user.Plan) {
		writeErr(w, http.StatusForbidden, "plan_required")
		return
	}

	if in.Symbol == "" {
		in.Symbol = "*"
	}
	parsedSymbols, err := normalizeSymbolsInput(in.Symbol)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	if parsedSymbols.IsWildcard {
		in.Symbol = "*"
	} else {
		in.Symbol = strings.Join(parsedSymbols.Symbols, " ")
		if err := a.ensureSymbolsExist(r.Context(), parsedSymbols, in.Exchange, in.MarketType); err != nil {
			writeErr(w, http.StatusBadRequest, err.Error())
			return
		}
	}
	if !isValidAlertWindowMinutes(in.WindowMinutes) {
		writeErr(w, http.StatusBadRequest, "invalid window_minutes")
		return
	}

	conds, err := normalizeConditions(in.Conditions, in.Indicator, in.Direction, in.ThresholdPercent, in.ThresholdAmount)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	primary := conds[0]
	in.Indicator = primary.Indicator
	in.Direction = primary.Direction
	in.ThresholdPercent = primary.ThresholdPercent
	in.ThresholdAmount = primary.ThresholdAmount
	if in.CooldownSeconds < 0 {
		writeErr(w, http.StatusBadRequest, "invalid cooldown_seconds")
		return
	}
	conditionsJSON, _ := json.Marshal(conds)

	enabled := true
	if in.Enabled != nil {
		enabled = *in.Enabled
	}

	// Если правило уже есть — обновляем последнее по slot
	var existingID int64
	err = a.db.QueryRow(r.Context(), `
		SELECT id
		FROM alerts
		WHERE user_id = $1 AND scanner_slot = $2 AND exchange = $3 AND market_type = $4
		ORDER BY id DESC
		LIMIT 1
	`, numID, in.ScannerSlot, in.Exchange, in.MarketType).Scan(&existingID)
	if err == nil {
		var out AlertDTO
		var createdAt, updatedAt time.Time
		var thresholdPercent, thresholdAmount pgtype.Float8
		err = a.db.QueryRow(r.Context(), `
			UPDATE alerts
			SET exchange = $2,
				market_type = $3,
				indicator = $4,
				symbol = $5,
				window_minutes = $6,
				threshold_percent = $7,
				threshold_amount = $8,
				direction = $9,
				cooldown_seconds = $10,
				enabled = $11,
				conditions = $12,
				scanner_slot = $13,
				updated_at = now()
			WHERE user_id = $1 AND id = $14
			RETURNING id, exchange, market_type, indicator, symbol, window_minutes, threshold_percent, threshold_amount, direction, cooldown_seconds, enabled, created_at, updated_at, conditions, scanner_slot
		`, numID, in.Exchange, in.MarketType, in.Indicator, in.Symbol, in.WindowMinutes, in.ThresholdPercent, in.ThresholdAmount, in.Direction, in.CooldownSeconds, enabled, conditionsJSON, in.ScannerSlot, existingID).Scan(
			&out.ID,
			&out.Exchange,
			&out.MarketType,
			&out.Indicator,
			&out.Symbol,
			&out.WindowMinutes,
			&thresholdPercent,
			&thresholdAmount,
			&out.Direction,
			&out.CooldownSeconds,
			&out.Enabled,
			&createdAt,
			&updatedAt,
			&conditionsJSON,
			&out.ScannerSlot,
		)
		if err != nil {
			if isMissingScannerColumn(err) {
				writeErr(w, http.StatusInternalServerError, "db_migration_required")
				return
			}
			writeErr(w, http.StatusInternalServerError, "db error")
			return
		}

		if thresholdPercent.Valid {
			v := thresholdPercent.Float64
			out.ThresholdPercent = &v
		}
		if thresholdAmount.Valid {
			v := thresholdAmount.Float64
			out.ThresholdAmount = &v
		}
		out.Conditions = conds
		out.ScannerSlot = in.ScannerSlot
		out.CreatedAt = createdAt.Format(time.RFC3339)
		out.UpdatedAt = updatedAt.Format(time.RFC3339)
		_ = a.disableOtherRules(r.Context(), numID, out.ScannerSlot, out.Exchange, out.MarketType, out.ID)
		writeJSON(w, http.StatusOK, map[string]any{"rule": out})
		return
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		if isMissingScannerColumn(err) {
			writeErr(w, http.StatusInternalServerError, "db_migration_required")
			return
		}
		writeErr(w, http.StatusInternalServerError, "db error")
		return
	}

	var out AlertDTO
	var createdAt, updatedAt time.Time
	var thresholdPercent, thresholdAmount pgtype.Float8
	err = a.db.QueryRow(r.Context(), `
		INSERT INTO alerts (user_id, exchange, market_type, indicator, symbol, window_minutes, threshold_percent, threshold_amount, direction, cooldown_seconds, enabled, conditions, scanner_slot)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)
		RETURNING id, exchange, market_type, indicator, symbol, window_minutes, threshold_percent, threshold_amount, direction, cooldown_seconds, enabled, created_at, updated_at, conditions, scanner_slot
	`, numID, in.Exchange, in.MarketType, in.Indicator, in.Symbol, in.WindowMinutes, in.ThresholdPercent, in.ThresholdAmount, in.Direction, in.CooldownSeconds, enabled, conditionsJSON, in.ScannerSlot).Scan(
		&out.ID,
		&out.Exchange,
		&out.MarketType,
		&out.Indicator,
		&out.Symbol,
		&out.WindowMinutes,
		&thresholdPercent,
		&thresholdAmount,
		&out.Direction,
		&out.CooldownSeconds,
		&out.Enabled,
		&createdAt,
		&updatedAt,
		&conditionsJSON,
		&out.ScannerSlot,
	)
	if err != nil {
		if isMissingScannerColumn(err) {
			writeErr(w, http.StatusInternalServerError, "db_migration_required")
			return
		}
		writeErr(w, http.StatusInternalServerError, "db error")
		return
	}

	if thresholdPercent.Valid {
		v := thresholdPercent.Float64
		out.ThresholdPercent = &v
	}
	if thresholdAmount.Valid {
		v := thresholdAmount.Float64
		out.ThresholdAmount = &v
	}
	out.Conditions = conds
	out.ScannerSlot = in.ScannerSlot
	out.CreatedAt = createdAt.Format(time.RFC3339)
	out.UpdatedAt = updatedAt.Format(time.RFC3339)
	_ = a.disableOtherRules(r.Context(), numID, out.ScannerSlot, out.Exchange, out.MarketType, out.ID)
	writeJSON(w, http.StatusOK, map[string]any{"rule": out})
}

func (a *API) handleUpdateAlert(w http.ResponseWriter, r *http.Request) {
	user, ok := userFromCtx(r)
	if !ok {
		writeErr(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	numID, err := a.userNumID(r.Context(), user)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "user lookup failed")
		return
	}

	idStr := chi.URLParam(r, "id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "invalid id")
		return
	}

	var in UpdateAlertInput
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}

	var currentExchange, currentMarketType, currentSymbol string
	if err := a.db.QueryRow(r.Context(), `
		SELECT exchange, market_type, symbol
		FROM alerts
		WHERE user_id = $1 AND id = $2
	`, numID, id).Scan(&currentExchange, &currentMarketType, &currentSymbol); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeErr(w, http.StatusNotFound, "rule not found")
			return
		}
		writeErr(w, http.StatusInternalServerError, "db error")
		return
	}
	targetExchange := currentExchange
	targetMarketType := currentMarketType
	if in.Exchange != nil {
		targetExchange = normalizeExchange(*in.Exchange)
		*in.Exchange = targetExchange
	}
	if in.MarketType != nil {
		targetMarketType = normalizeMarketType(*in.MarketType)
		*in.MarketType = targetMarketType
	}
	if !isSupportedInstrument(targetExchange, targetMarketType) {
		writeErr(w, http.StatusBadRequest, "unsupported_exchange_market_type")
		return
	}

	hasConditions := in.Conditions != nil

	sets := []string{}
	args := []any{}
	argN := 1
	if in.Exchange != nil {
		sets = append(sets, fmt.Sprintf("exchange = $%d", argN))
		args = append(args, targetExchange)
		argN++
	}
	if in.MarketType != nil {
		sets = append(sets, fmt.Sprintf("market_type = $%d", argN))
		args = append(args, targetMarketType)
		argN++
	}

	if in.ScannerSlot != nil {
		if !isValidScannerSlot(*in.ScannerSlot) {
			writeErr(w, http.StatusBadRequest, "invalid scanner_slot")
			return
		}
		if *in.ScannerSlot == "SLOT_2" && !auth.IsStandard(user.Plan) {
			writeErr(w, http.StatusForbidden, "plan_required")
			return
		}
		if *in.ScannerSlot == "SLOT_3" && !auth.IsPro(user.Plan) {
			writeErr(w, http.StatusForbidden, "plan_required")
			return
		}
		sets = append(sets, fmt.Sprintf("scanner_slot = $%d", argN))
		args = append(args, *in.ScannerSlot)
		argN++
	}

	if hasConditions {
		conds, err := normalizeConditions(*in.Conditions, "", "", nil, nil)
		if err != nil {
			writeErr(w, http.StatusBadRequest, err.Error())
			return
		}
		primary := conds[0]
		conditionsJSON, _ := json.Marshal(conds)
		sets = append(sets, fmt.Sprintf("conditions = $%d", argN))
		args = append(args, conditionsJSON)
		argN++
		sets = append(sets, fmt.Sprintf("indicator = $%d", argN))
		args = append(args, primary.Indicator)
		argN++
		sets = append(sets, fmt.Sprintf("direction = $%d", argN))
		args = append(args, primary.Direction)
		argN++
		sets = append(sets, fmt.Sprintf("threshold_percent = $%d", argN))
		args = append(args, primary.ThresholdPercent)
		argN++
		sets = append(sets, fmt.Sprintf("threshold_amount = $%d", argN))
		args = append(args, primary.ThresholdAmount)
		argN++
	} else if in.Indicator != nil {
		if !isValidIndicator(*in.Indicator) {
			writeErr(w, http.StatusBadRequest, "invalid indicator")
			return
		}
		if strings.HasPrefix(*in.Indicator, "liquidations") && in.ThresholdAmount == nil {
			writeErr(w, http.StatusBadRequest, "threshold_amount required for liquidations")
			return
		}
		sets = append(sets, fmt.Sprintf("indicator = $%d", argN))
		args = append(args, *in.Indicator)
		argN++
	}

	if in.Symbol != nil {
		parsedSymbols, err := normalizeSymbolsInput(*in.Symbol)
		if err != nil {
			writeErr(w, http.StatusBadRequest, err.Error())
			return
		}
		symbolValue := "*"
		if !parsedSymbols.IsWildcard {
			symbolValue = strings.Join(parsedSymbols.Symbols, " ")
			if err := a.ensureSymbolsExist(r.Context(), parsedSymbols, targetExchange, targetMarketType); err != nil {
				writeErr(w, http.StatusBadRequest, err.Error())
				return
			}
		}
		sets = append(sets, fmt.Sprintf("symbol = $%d", argN))
		args = append(args, symbolValue)
		argN++
	} else if in.Exchange != nil || in.MarketType != nil {
		parsedSymbols, err := normalizeSymbolsInput(currentSymbol)
		if err != nil {
			writeErr(w, http.StatusBadRequest, err.Error())
			return
		}
		if err := a.ensureSymbolsExist(r.Context(), parsedSymbols, targetExchange, targetMarketType); err != nil {
			writeErr(w, http.StatusBadRequest, err.Error())
			return
		}
	}
	if in.WindowMinutes != nil {
		if !isValidAlertWindowMinutes(*in.WindowMinutes) {
			writeErr(w, http.StatusBadRequest, "invalid window_minutes")
			return
		}
		sets = append(sets, fmt.Sprintf("window_minutes = $%d", argN))
		args = append(args, *in.WindowMinutes)
		argN++
	}
	if !hasConditions && in.ThresholdPercent != nil {
		if *in.ThresholdPercent <= 0 {
			writeErr(w, http.StatusBadRequest, "invalid threshold_percent")
			return
		}
		sets = append(sets, fmt.Sprintf("threshold_percent = $%d", argN))
		args = append(args, *in.ThresholdPercent)
		argN++
	}
	if !hasConditions && in.ThresholdAmount != nil {
		if *in.ThresholdAmount <= 0 {
			writeErr(w, http.StatusBadRequest, "invalid threshold_amount")
			return
		}
		sets = append(sets, fmt.Sprintf("threshold_amount = $%d", argN))
		args = append(args, *in.ThresholdAmount)
		argN++
	}
	if !hasConditions && in.Direction != nil {
		if !isValidDirection(*in.Direction) {
			writeErr(w, http.StatusBadRequest, "invalid direction")
			return
		}
		sets = append(sets, fmt.Sprintf("direction = $%d", argN))
		args = append(args, *in.Direction)
		argN++
	}
	if in.CooldownSeconds != nil {
		if *in.CooldownSeconds < 0 {
			writeErr(w, http.StatusBadRequest, "invalid cooldown_seconds")
			return
		}
		sets = append(sets, fmt.Sprintf("cooldown_seconds = $%d", argN))
		args = append(args, *in.CooldownSeconds)
		argN++
	}
	if in.Enabled != nil {
		sets = append(sets, fmt.Sprintf("enabled = $%d", argN))
		args = append(args, *in.Enabled)
		argN++
	}

	if len(sets) == 0 {
		writeErr(w, http.StatusBadRequest, "no fields to update")
		return
	}

	args = append(args, numID, id)

	var out AlertDTO
	var createdAt, updatedAt time.Time
	var thresholdPercent, thresholdAmount pgtype.Float8
	var rawConditions []byte
	var scannerSlot string
	err = a.db.QueryRow(r.Context(), fmt.Sprintf(`
		UPDATE alerts
		SET %s, updated_at = now()
		WHERE user_id = $%d AND id = $%d
		RETURNING id, exchange, market_type, indicator, symbol, window_minutes, threshold_percent, threshold_amount, direction, cooldown_seconds, enabled, created_at, updated_at, conditions, scanner_slot
	`, strings.Join(sets, ", "), argN, argN+1), args...).Scan(
		&out.ID,
		&out.Exchange,
		&out.MarketType,
		&out.Indicator,
		&out.Symbol,
		&out.WindowMinutes,
		&thresholdPercent,
		&thresholdAmount,
		&out.Direction,
		&out.CooldownSeconds,
		&out.Enabled,
		&createdAt,
		&updatedAt,
		&rawConditions,
		&scannerSlot,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeErr(w, http.StatusNotFound, "rule not found")
			return
		}
		if isMissingScannerColumn(err) {
			writeErr(w, http.StatusInternalServerError, "db_migration_required")
			return
		}
		writeErr(w, http.StatusInternalServerError, "db error")
		return
	}

	if thresholdPercent.Valid {
		v := thresholdPercent.Float64
		out.ThresholdPercent = &v
	}
	if thresholdAmount.Valid {
		v := thresholdAmount.Float64
		out.ThresholdAmount = &v
	}
	out.Conditions = parseConditions(rawConditions)
	out.ScannerSlot = scannerSlot
	out.CreatedAt = createdAt.Format(time.RFC3339)
	out.UpdatedAt = updatedAt.Format(time.RFC3339)
	_ = a.disableOtherRules(r.Context(), numID, out.ScannerSlot, out.Exchange, out.MarketType, out.ID)

	writeJSON(w, http.StatusOK, map[string]any{"rule": out})
}

func (a *API) handleDeleteAlert(w http.ResponseWriter, r *http.Request) {
	user, ok := userFromCtx(r)
	if !ok {
		writeErr(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	numID, err := a.userNumID(r.Context(), user)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "user lookup failed")
		return
	}

	idStr := chi.URLParam(r, "id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "invalid id")
		return
	}

	cmd, err := a.db.Exec(r.Context(), `DELETE FROM alerts WHERE user_id = $1 AND id = $2`, numID, id)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "db error")
		return
	}
	if cmd.RowsAffected() == 0 {
		writeErr(w, http.StatusNotFound, "rule not found")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// --------------------
// Handlers: Signals
// --------------------

func (a *API) handleListSignals(w http.ResponseWriter, r *http.Request) {
	user, ok := userFromCtx(r)
	if !ok {
		writeErr(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	numID, err := a.userNumID(r.Context(), user)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "user lookup failed")
		return
	}

	limit := 50
	if s := r.URL.Query().Get("limit"); s != "" {
		if v, err := strconv.Atoi(s); err == nil {
			if v < 1 {
				limit = 1
			} else if v > 200 {
				limit = 200
			} else {
				limit = v
			}
		}
	}

	slotParam := strings.TrimSpace(r.URL.Query().Get("slot"))
	slotFilter := ""
	if slotParam != "" {
		switch strings.ToUpper(slotParam) {
		case "1", "SLOT_1":
			slotFilter = "SLOT_1"
		case "2", "SLOT_2":
			slotFilter = "SLOT_2"
		case "3", "SLOT_3":
			slotFilter = "SLOT_3"
		default:
			writeErr(w, http.StatusBadRequest, "invalid_slot")
			return
		}
	}

	baseQuery := `
		SELECT id, rule_id, user_id, exchange, market_type, symbol, tf, ts, payload, scanner_slot, created_at
		FROM signals
		WHERE user_id = $1
	`
	args := []any{numID}
	if slotFilter != "" {
		args = append(args, slotFilter)
		baseQuery += fmt.Sprintf(" AND scanner_slot = $%d", len(args))
	} else if !auth.IsPro(user.Plan) {
		if auth.IsStandard(user.Plan) {
			baseQuery += " AND scanner_slot IN ('SLOT_1','SLOT_2')"
		} else {
			baseQuery += " AND scanner_slot = 'SLOT_1'"
		}
	}
	if exchangeFilter := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("exchange"))); exchangeFilter != "" {
		if !isValidExchange(exchangeFilter) {
			writeErr(w, http.StatusBadRequest, "invalid_exchange")
			return
		}
		args = append(args, exchangeFilter)
		baseQuery += fmt.Sprintf(" AND exchange = $%d", len(args))
	}
	if marketTypeFilter := normalizeMarketType(strings.TrimSpace(r.URL.Query().Get("market_type"))); strings.TrimSpace(r.URL.Query().Get("market_type")) != "" {
		if !isValidMarketType(marketTypeFilter) {
			writeErr(w, http.StatusBadRequest, "invalid_market_type")
			return
		}
		args = append(args, marketTypeFilter)
		baseQuery += fmt.Sprintf(" AND market_type = $%d", len(args))
	}
	args = append(args, limit)
	baseQuery += fmt.Sprintf(" ORDER BY created_at DESC LIMIT $%d", len(args))

	rows, err := a.db.Query(r.Context(), baseQuery, args...)
	if err != nil {
		if isMissingScannerColumn(err) {
			writeErr(w, http.StatusInternalServerError, "db_migration_required")
			return
		}
		writeErr(w, http.StatusInternalServerError, "db error")
		return
	}
	defer rows.Close()

	var out []SignalDTO
	for rows.Next() {
		var s SignalDTO
		var createdAt time.Time
		if err := rows.Scan(
			&s.ID,
			&s.RuleID,
			&s.UserID,
			&s.Exchange,
			&s.MarketType,
			&s.Symbol,
			&s.TF,
			&s.TS,
			&s.Payload,
			&s.ScannerSlot,
			&createdAt,
		); err != nil {
			writeErr(w, http.StatusInternalServerError, "db error")
			return
		}
		s.CreatedAt = createdAt.Format(time.RFC3339)
		out = append(out, s)
	}

	writeJSON(w, http.StatusOK, map[string]any{"signals": out})
}

// --------------------
// Handlers: Trades
// --------------------

func (a *API) handleListTrades(w http.ResponseWriter, r *http.Request) {
	user, ok := userFromCtx(r)
	if !ok {
		writeErr(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	numID, err := a.userNumID(r.Context(), user)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "user lookup failed")
		return
	}

	limit := 100
	if s := r.URL.Query().Get("limit"); s != "" {
		if v, err := strconv.Atoi(s); err == nil {
			if v < 1 {
				limit = 1
			} else if v > 200 {
				limit = 200
			} else {
				limit = v
			}
		}
	}
	offset := 0
	if s := r.URL.Query().Get("offset"); s != "" {
		if v, err := strconv.Atoi(s); err == nil && v > 0 {
			offset = v
		}
	}

	statusParam := strings.TrimSpace(r.URL.Query().Get("status"))
	statusFilter := "OPEN"
	if statusParam != "" {
		switch strings.ToUpper(statusParam) {
		case "OPEN", "CLOSED":
			statusFilter = strings.ToUpper(statusParam)
		case "ALL":
			statusFilter = ""
		default:
			writeErr(w, http.StatusBadRequest, "invalid_status")
			return
		}
	}

	baseQuery := `
		SELECT t.id, t.signal_id, t.user_id, s.symbol, t.status, t.buy_at, t.sell_at, t.duration_minutes,
			t.profit_percent, t.profit_usd, t.exchange, t.timeframe, t.comment, t.entry_basis, t.entry_photos,
			t.strategy_id, ts.name, t.created_at, t.updated_at
		FROM signal_trades t
		JOIN signals s ON s.id = t.signal_id
		LEFT JOIN trade_strategies ts ON ts.id = t.strategy_id
		WHERE t.user_id = $1
	`
	args := []any{numID}
	if statusFilter != "" {
		args = append(args, statusFilter)
		baseQuery += fmt.Sprintf(" AND status = $%d", len(args))
	}
	args = append(args, limit)
	limitArg := len(args)
	args = append(args, offset)
	baseQuery += fmt.Sprintf(" ORDER BY created_at DESC, t.id DESC LIMIT $%d OFFSET $%d", limitArg, len(args))

	rows, err := a.db.Query(r.Context(), baseQuery, args...)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "db error")
		return
	}
	defer rows.Close()

	var out []TradeDTO
	for rows.Next() {
		var t TradeDTO
		var buyAt, createdAt, updatedAt time.Time
		var sellAt pgtype.Timestamptz
		var duration pgtype.Int4
		var profitPercent, profitUSD pgtype.Float8
		var exchange, timeframe, comment, entryBasis, strategyName pgtype.Text
		var entryPhotos []string
		var strategyID pgtype.UUID
		if err := rows.Scan(
			&t.ID,
			&t.SignalID,
			&t.UserID,
			&t.Symbol,
			&t.Status,
			&buyAt,
			&sellAt,
			&duration,
			&profitPercent,
			&profitUSD,
			&exchange,
			&timeframe,
			&comment,
			&entryBasis,
			&entryPhotos,
			&strategyID,
			&strategyName,
			&createdAt,
			&updatedAt,
		); err != nil {
			writeErr(w, http.StatusInternalServerError, "db error")
			return
		}
		if sellAt.Valid {
			v := sellAt.Time.Format(time.RFC3339)
			t.SellAt = &v
		}
		if duration.Valid {
			v := int(duration.Int32)
			t.DurationMin = &v
		}
		if profitPercent.Valid {
			v := profitPercent.Float64
			t.ProfitPercent = &v
		}
		if profitUSD.Valid {
			v := profitUSD.Float64
			t.ProfitUSD = &v
		}
		if exchange.Valid {
			v := exchange.String
			t.Exchange = &v
		}
		if timeframe.Valid {
			v := timeframe.String
			t.Timeframe = &v
		}
		if comment.Valid {
			v := comment.String
			t.Comment = &v
		}
		if entryBasis.Valid {
			v := entryBasis.String
			t.EntryBasis = &v
		}
		if entryPhotos != nil {
			t.EntryPhotos = entryPhotos
		}
		if strategyID.Valid {
			v := strategyID.String()
			t.StrategyID = &v
		}
		if strategyName.Valid {
			v := strategyName.String
			t.StrategyName = &v
		}
		t.BuyAt = buyAt.Format(time.RFC3339)
		t.CreatedAt = createdAt.Format(time.RFC3339)
		t.UpdatedAt = updatedAt.Format(time.RFC3339)
		out = append(out, t)
	}

	writeJSON(w, http.StatusOK, map[string]any{"trades": out})
}

func (a *API) handleTradeStats(w http.ResponseWriter, r *http.Request) {
	user, ok := userFromCtx(r)
	if !ok {
		writeErr(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	numID, err := a.userNumID(r.Context(), user)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "user lookup failed")
		return
	}

	symbolFilter := strings.TrimSpace(r.URL.Query().Get("symbol"))
	var closedCount, openCount, totalCount int
	var winCount int
	var avgProfitPercent, sumProfitPercent, bestProfitPercent, worstProfitPercent pgtype.Float8
	var avgProfitUSD, sumProfitUSD, bestProfitUSD, worstProfitUSD pgtype.Float8
	var avgDuration, maxDuration, minDuration pgtype.Int4
	var firstBuyAt, lastSellAt pgtype.Timestamptz

	query := `
		SELECT
			COUNT(*) FILTER (WHERE status = 'CLOSED') AS closed_count,
			COUNT(*) FILTER (WHERE status = 'OPEN') AS open_count,
			COUNT(*) AS total_count,
			AVG(profit_percent) FILTER (WHERE status = 'CLOSED') AS avg_profit_percent,
			SUM(profit_percent) FILTER (WHERE status = 'CLOSED') AS sum_profit_percent,
			AVG(profit_usd) FILTER (WHERE status = 'CLOSED') AS avg_profit_usd,
			SUM(profit_usd) FILTER (WHERE status = 'CLOSED') AS sum_profit_usd,
			MAX(profit_percent) FILTER (WHERE status = 'CLOSED') AS best_profit_percent,
			MIN(profit_percent) FILTER (WHERE status = 'CLOSED') AS worst_profit_percent,
			MAX(profit_usd) FILTER (WHERE status = 'CLOSED') AS best_profit_usd,
			MIN(profit_usd) FILTER (WHERE status = 'CLOSED') AS worst_profit_usd,
			AVG(duration_minutes) FILTER (WHERE status = 'CLOSED') AS avg_duration_minutes,
			MAX(duration_minutes) FILTER (WHERE status = 'CLOSED') AS max_duration_minutes,
			MIN(duration_minutes) FILTER (WHERE status = 'CLOSED') AS min_duration_minutes,
			MIN(buy_at) FILTER (WHERE status = 'CLOSED') AS first_buy_at,
			MAX(sell_at) FILTER (WHERE status = 'CLOSED') AS last_sell_at,
			COUNT(*) FILTER (WHERE status = 'CLOSED' AND profit_percent > 0) AS win_count
		FROM signal_trades
		WHERE user_id = $1
	`
	args := []any{numID}
	if symbolFilter != "" {
		query = `
			SELECT
				COUNT(*) FILTER (WHERE t.status = 'CLOSED') AS closed_count,
				COUNT(*) FILTER (WHERE t.status = 'OPEN') AS open_count,
				COUNT(*) AS total_count,
				AVG(t.profit_percent) FILTER (WHERE t.status = 'CLOSED') AS avg_profit_percent,
				SUM(t.profit_percent) FILTER (WHERE t.status = 'CLOSED') AS sum_profit_percent,
				AVG(t.profit_usd) FILTER (WHERE t.status = 'CLOSED') AS avg_profit_usd,
				SUM(t.profit_usd) FILTER (WHERE t.status = 'CLOSED') AS sum_profit_usd,
				MAX(t.profit_percent) FILTER (WHERE t.status = 'CLOSED') AS best_profit_percent,
				MIN(t.profit_percent) FILTER (WHERE t.status = 'CLOSED') AS worst_profit_percent,
				MAX(t.profit_usd) FILTER (WHERE t.status = 'CLOSED') AS best_profit_usd,
				MIN(t.profit_usd) FILTER (WHERE t.status = 'CLOSED') AS worst_profit_usd,
				AVG(t.duration_minutes) FILTER (WHERE t.status = 'CLOSED') AS avg_duration_minutes,
				MAX(t.duration_minutes) FILTER (WHERE t.status = 'CLOSED') AS max_duration_minutes,
				MIN(t.duration_minutes) FILTER (WHERE t.status = 'CLOSED') AS min_duration_minutes,
				MIN(t.buy_at) FILTER (WHERE t.status = 'CLOSED') AS first_buy_at,
				MAX(t.sell_at) FILTER (WHERE t.status = 'CLOSED') AS last_sell_at,
				COUNT(*) FILTER (WHERE t.status = 'CLOSED' AND t.profit_percent > 0) AS win_count
			FROM signal_trades t
			JOIN signals s ON s.id = t.signal_id
			WHERE t.user_id = $1 AND s.symbol = $2
		`
		args = append(args, symbolFilter)
	}

	err = a.db.QueryRow(r.Context(), query, args...).Scan(
		&closedCount,
		&openCount,
		&totalCount,
		&avgProfitPercent,
		&sumProfitPercent,
		&avgProfitUSD,
		&sumProfitUSD,
		&bestProfitPercent,
		&worstProfitPercent,
		&bestProfitUSD,
		&worstProfitUSD,
		&avgDuration,
		&maxDuration,
		&minDuration,
		&firstBuyAt,
		&lastSellAt,
		&winCount,
	)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "db error")
		return
	}

	out := TradeStatsDTO{
		TotalTrades:  totalCount,
		OpenTrades:   openCount,
		ClosedTrades: closedCount,
	}
	if closedCount > 0 {
		winRate := (float64(winCount) / float64(closedCount)) * 100
		out.WinRate = &winRate
	}
	if avgProfitPercent.Valid {
		v := avgProfitPercent.Float64
		out.AvgProfitPercent = &v
	}
	if sumProfitPercent.Valid {
		v := sumProfitPercent.Float64
		out.SumProfitPercent = &v
	}
	if avgProfitUSD.Valid {
		v := avgProfitUSD.Float64
		out.AvgProfitUSD = &v
	}
	if sumProfitUSD.Valid {
		v := sumProfitUSD.Float64
		out.SumProfitUSD = &v
	}
	if bestProfitPercent.Valid {
		v := bestProfitPercent.Float64
		out.BestProfitPercent = &v
	}
	if worstProfitPercent.Valid {
		v := worstProfitPercent.Float64
		out.WorstProfitPercent = &v
	}
	if bestProfitUSD.Valid {
		v := bestProfitUSD.Float64
		out.BestProfitUSD = &v
	}
	if worstProfitUSD.Valid {
		v := worstProfitUSD.Float64
		out.WorstProfitUSD = &v
	}
	if avgDuration.Valid {
		v := int(avgDuration.Int32)
		out.AvgDurationMin = &v
	}
	if maxDuration.Valid {
		v := int(maxDuration.Int32)
		out.MaxDurationMin = &v
	}
	if minDuration.Valid {
		v := int(minDuration.Int32)
		out.MinDurationMin = &v
	}
	if firstBuyAt.Valid {
		v := firstBuyAt.Time.Format(time.RFC3339)
		out.FirstBuyAt = &v
	}
	if lastSellAt.Valid {
		v := lastSellAt.Time.Format(time.RFC3339)
		out.LastSellAt = &v
	}

	writeJSON(w, http.StatusOK, map[string]any{"stats": out})
}

func (a *API) handleGetTradeMeta(w http.ResponseWriter, r *http.Request) {
	user, ok := userFromCtx(r)
	if !ok {
		writeErr(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	numID, err := a.userNumID(r.Context(), user)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "user lookup failed")
		return
	}

	var timeframe, exchange, strategy pgtype.Text
	err = a.db.QueryRow(r.Context(), `
		SELECT timeframe, exchange, strategy_name
		FROM trade_meta
		WHERE user_id = $1
	`, numID).Scan(&timeframe, &exchange, &strategy)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeJSON(w, http.StatusOK, map[string]any{"meta": TradeMetaDTO{}})
			return
		}
		writeErr(w, http.StatusInternalServerError, "db error")
		return
	}

	out := TradeMetaDTO{}
	if timeframe.Valid {
		out.Timeframe = timeframe.String
	}
	if exchange.Valid {
		out.Exchange = exchange.String
	}
	if strategy.Valid {
		out.Strategy = strategy.String
	}

	writeJSON(w, http.StatusOK, map[string]any{"meta": out})
}

func (a *API) handleListTradeStrategies(w http.ResponseWriter, r *http.Request) {
	user, ok := userFromCtx(r)
	if !ok {
		writeErr(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	numID, err := a.userNumID(r.Context(), user)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "user lookup failed")
		return
	}

	rows, err := a.db.Query(r.Context(), `
		SELECT s.id, s.name,
			COUNT(t.id) AS total_trades,
			COUNT(t.id) FILTER (WHERE t.status = 'CLOSED') AS closed_trades,
			CASE
				WHEN COUNT(t.id) FILTER (WHERE t.status = 'CLOSED') = 0 THEN NULL
				ELSE SUM(CASE WHEN t.profit_percent > 0 THEN 1 ELSE 0 END)::float
					/ COUNT(t.id) FILTER (WHERE t.status = 'CLOSED')
			END AS win_rate,
			AVG(t.profit_percent) FILTER (WHERE t.status = 'CLOSED') AS avg_profit_percent,
			SUM(t.profit_percent) FILTER (WHERE t.status = 'CLOSED') AS sum_profit_percent,
			AVG(t.profit_usd) FILTER (WHERE t.status = 'CLOSED') AS avg_profit_usd,
			SUM(t.profit_usd) FILTER (WHERE t.status = 'CLOSED') AS sum_profit_usd,
			AVG(t.duration_minutes) FILTER (WHERE t.status = 'CLOSED')::float8 AS avg_duration_minutes
		FROM trade_strategies s
		LEFT JOIN signal_trades t ON t.strategy_id = s.id AND t.user_id = $1
		WHERE s.user_id = $1
		GROUP BY s.id
		ORDER BY s.created_at DESC
	`, numID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "db error")
		return
	}
	defer rows.Close()

	strategies := make([]TradeStrategyDTO, 0)
	strategyIndex := make(map[string]int)
	for rows.Next() {
		var s TradeStrategyDTO
		var id pgtype.UUID
		var totalTrades, closedTrades int64
		var winRate, avgProfitPercent, sumProfitPercent, avgProfitUSD, sumProfitUSD pgtype.Float8
		var avgDuration pgtype.Float8
		if err := rows.Scan(
			&id,
			&s.Name,
			&totalTrades,
			&closedTrades,
			&winRate,
			&avgProfitPercent,
			&sumProfitPercent,
			&avgProfitUSD,
			&sumProfitUSD,
			&avgDuration,
		); err != nil {
			writeErr(w, http.StatusInternalServerError, "db error")
			return
		}
		s.ID = id.String()
		s.TotalTrades = int(totalTrades)
		s.ClosedTrades = int(closedTrades)
		if winRate.Valid {
			v := winRate.Float64
			s.WinRate = &v
		}
		if avgProfitPercent.Valid {
			v := avgProfitPercent.Float64
			s.AvgProfitPercent = &v
		}
		if sumProfitPercent.Valid {
			v := sumProfitPercent.Float64
			s.SumProfitPercent = &v
		}
		if avgProfitUSD.Valid {
			v := avgProfitUSD.Float64
			s.AvgProfitUSD = &v
		}
		if sumProfitUSD.Valid {
			v := sumProfitUSD.Float64
			s.SumProfitUSD = &v
		}
		if avgDuration.Valid {
			v := int(math.Round(avgDuration.Float64))
			s.AvgDurationMin = &v
		}
		strategyIndex[s.ID] = len(strategies)
		strategies = append(strategies, s)
	}

	pairRows, err := a.db.Query(r.Context(), `
		SELECT t.strategy_id, s.symbol,
			COUNT(*) AS total_trades,
			COUNT(*) FILTER (WHERE t.status = 'CLOSED') AS closed_trades,
			CASE
				WHEN COUNT(*) FILTER (WHERE t.status = 'CLOSED') = 0 THEN NULL
				ELSE SUM(CASE WHEN t.profit_percent > 0 THEN 1 ELSE 0 END)::float
					/ COUNT(*) FILTER (WHERE t.status = 'CLOSED')
			END AS win_rate,
			AVG(t.profit_percent) FILTER (WHERE t.status = 'CLOSED') AS avg_profit_percent,
			SUM(t.profit_percent) FILTER (WHERE t.status = 'CLOSED') AS sum_profit_percent,
			AVG(t.profit_usd) FILTER (WHERE t.status = 'CLOSED') AS avg_profit_usd,
			SUM(t.profit_usd) FILTER (WHERE t.status = 'CLOSED') AS sum_profit_usd,
			AVG(t.duration_minutes) FILTER (WHERE t.status = 'CLOSED')::float8 AS avg_duration_minutes
		FROM signal_trades t
		JOIN signals s ON s.id = t.signal_id
		WHERE t.user_id = $1 AND t.strategy_id IS NOT NULL
		GROUP BY t.strategy_id, s.symbol
		ORDER BY total_trades DESC, s.symbol ASC
	`, numID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "db error")
		return
	}
	defer pairRows.Close()

	for pairRows.Next() {
		var strategyID pgtype.UUID
		var pair TradeStrategyPairDTO
		var totalTrades, closedTrades int64
		var winRate, avgProfitPercent, sumProfitPercent, avgProfitUSD, sumProfitUSD pgtype.Float8
		var avgDuration pgtype.Float8
		if err := pairRows.Scan(
			&strategyID,
			&pair.Symbol,
			&totalTrades,
			&closedTrades,
			&winRate,
			&avgProfitPercent,
			&sumProfitPercent,
			&avgProfitUSD,
			&sumProfitUSD,
			&avgDuration,
		); err != nil {
			writeErr(w, http.StatusInternalServerError, "db error")
			return
		}
		pair.TotalTrades = int(totalTrades)
		pair.ClosedTrades = int(closedTrades)
		if winRate.Valid {
			v := winRate.Float64
			pair.WinRate = &v
		}
		if avgProfitPercent.Valid {
			v := avgProfitPercent.Float64
			pair.AvgProfitPercent = &v
		}
		if sumProfitPercent.Valid {
			v := sumProfitPercent.Float64
			pair.SumProfitPercent = &v
		}
		if avgProfitUSD.Valid {
			v := avgProfitUSD.Float64
			pair.AvgProfitUSD = &v
		}
		if sumProfitUSD.Valid {
			v := sumProfitUSD.Float64
			pair.SumProfitUSD = &v
		}
		if avgDuration.Valid {
			v := int(math.Round(avgDuration.Float64))
			pair.AvgDurationMin = &v
		}
		key := strategyID.String()
		idx, ok := strategyIndex[key]
		if !ok {
			continue
		}
		strategies[idx].Pairs = append(strategies[idx].Pairs, pair)
	}

	writeJSON(w, http.StatusOK, map[string]any{"strategies": strategies})
}

func (a *API) handleDeleteTradeStrategy(w http.ResponseWriter, r *http.Request) {
	user, ok := userFromCtx(r)
	if !ok {
		writeErr(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	numID, err := a.userNumID(r.Context(), user)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "user lookup failed")
		return
	}

	idStr := chi.URLParam(r, "id")
	var strategyID pgtype.UUID
	if err := strategyID.Scan(idStr); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid id")
		return
	}

	ct, err := a.db.Exec(r.Context(), `
		DELETE FROM trade_strategies
		WHERE id = $1 AND user_id = $2
	`, strategyID, numID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "db error")
		return
	}
	if ct.RowsAffected() == 0 {
		writeErr(w, http.StatusNotFound, "strategy not found")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (a *API) handleDeleteTradeStrategyPair(w http.ResponseWriter, r *http.Request) {
	user, ok := userFromCtx(r)
	if !ok {
		writeErr(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	numID, err := a.userNumID(r.Context(), user)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "user lookup failed")
		return
	}

	idStr := chi.URLParam(r, "id")
	var strategyID pgtype.UUID
	if err := strategyID.Scan(idStr); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid id")
		return
	}

	symbol := strings.TrimSpace(chi.URLParam(r, "symbol"))
	if symbol == "" {
		writeErr(w, http.StatusBadRequest, "invalid symbol")
		return
	}

	ct, err := a.db.Exec(r.Context(), `
		DELETE FROM signal_trades t
		USING signals s
		WHERE t.signal_id = s.id
			AND t.user_id = $1
			AND t.strategy_id = $2
			AND UPPER(s.symbol) = UPPER($3)
	`, numID, strategyID, symbol)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "db error")
		return
	}
	if ct.RowsAffected() == 0 {
		writeErr(w, http.StatusNotFound, "pair not found")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (a *API) handleCreateTrade(w http.ResponseWriter, r *http.Request) {
	user, ok := userFromCtx(r)
	if !ok {
		writeErr(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	numID, err := a.userNumID(r.Context(), user)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "user lookup failed")
		return
	}

	var in CreateTradeInput
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	if strings.TrimSpace(in.SignalID) == "" {
		writeErr(w, http.StatusBadRequest, "signal_id required")
		return
	}
	var signalUUID pgtype.UUID
	if err := signalUUID.Scan(in.SignalID); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid signal_id")
		return
	}

	buyAt := time.Now()
	if in.BuyAt != nil {
		parsed, err := time.Parse(time.RFC3339, strings.TrimSpace(*in.BuyAt))
		if err != nil {
			writeErr(w, http.StatusBadRequest, "invalid buy_at")
			return
		}
		buyAt = parsed
	}

	var exists bool
	err = a.db.QueryRow(r.Context(), `SELECT true FROM signals WHERE id = $1 AND user_id = $2`, signalUUID, numID).Scan(&exists)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeErr(w, http.StatusNotFound, "signal not found")
			return
		}
		writeErr(w, http.StatusInternalServerError, "db error")
		return
	}

	var out TradeDTO
	var buyAtOut, createdAt, updatedAt time.Time
	var sellAt pgtype.Timestamptz
	var duration pgtype.Int4
	var profitPercent, profitUSD pgtype.Float8
	var exchange, timeframe, comment, entryBasis, strategyName pgtype.Text
	var entryPhotos []string
	var strategyID pgtype.UUID
	err = a.db.QueryRow(r.Context(), `
		INSERT INTO signal_trades (user_id, signal_id, status, buy_at)
		VALUES ($1, $2, 'OPEN', $3)
		RETURNING id, signal_id, user_id, status, buy_at, sell_at, duration_minutes, profit_percent, profit_usd,
			exchange, timeframe, comment, entry_basis, entry_photos, strategy_id,
			(SELECT name FROM trade_strategies WHERE id = signal_trades.strategy_id),
			created_at, updated_at,
			(SELECT symbol FROM signals WHERE id = $2)
	`, numID, signalUUID, buyAt).Scan(
		&out.ID,
		&out.SignalID,
		&out.UserID,
		&out.Status,
		&buyAtOut,
		&sellAt,
		&duration,
		&profitPercent,
		&profitUSD,
		&exchange,
		&timeframe,
		&comment,
		&entryBasis,
		&entryPhotos,
		&strategyID,
		&strategyName,
		&createdAt,
		&updatedAt,
		&out.Symbol,
	)
	if err != nil {
		if pgErr, ok := err.(*pgconn.PgError); ok && pgErr.Code == "23505" {
			writeErr(w, http.StatusConflict, "trade_open")
			return
		}
		writeErr(w, http.StatusInternalServerError, "db error")
		return
	}

	if sellAt.Valid {
		v := sellAt.Time.Format(time.RFC3339)
		out.SellAt = &v
	}
	if duration.Valid {
		v := int(duration.Int32)
		out.DurationMin = &v
	}
	if profitPercent.Valid {
		v := profitPercent.Float64
		out.ProfitPercent = &v
	}
	if profitUSD.Valid {
		v := profitUSD.Float64
		out.ProfitUSD = &v
	}
	if exchange.Valid {
		v := exchange.String
		out.Exchange = &v
	}
	if timeframe.Valid {
		v := timeframe.String
		out.Timeframe = &v
	}
	if comment.Valid {
		v := comment.String
		out.Comment = &v
	}
	if entryBasis.Valid {
		v := entryBasis.String
		out.EntryBasis = &v
	}
	if entryPhotos != nil {
		out.EntryPhotos = entryPhotos
	}
	if strategyID.Valid {
		v := strategyID.String()
		out.StrategyID = &v
	}
	if strategyName.Valid {
		v := strategyName.String
		out.StrategyName = &v
	}
	out.BuyAt = buyAtOut.Format(time.RFC3339)
	out.CreatedAt = createdAt.Format(time.RFC3339)
	out.UpdatedAt = updatedAt.Format(time.RFC3339)

	writeJSON(w, http.StatusOK, map[string]any{"trade": out})
}

func (a *API) handleCloseTrade(w http.ResponseWriter, r *http.Request) {
	user, ok := userFromCtx(r)
	if !ok {
		writeErr(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	numID, err := a.userNumID(r.Context(), user)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "user lookup failed")
		return
	}

	idStr := chi.URLParam(r, "id")
	var tradeUUID pgtype.UUID
	if err := tradeUUID.Scan(idStr); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid id")
		return
	}

	const maxTradeBodyBytes = 25 << 20
	r.Body = http.MaxBytesReader(w, r.Body, maxTradeBodyBytes)

	var in CloseTradeInput
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	if in.DurationMin != nil && *in.DurationMin < 0 {
		writeErr(w, http.StatusBadRequest, "invalid duration_minutes")
		return
	}

	var status string
	var buyAt time.Time
	var existingSellAt pgtype.Timestamptz
	var existingDuration pgtype.Int4
	var existingProfitPercent, existingProfitUSD pgtype.Float8
	var existingExchange, existingTimeframe, existingComment, existingEntryBasis pgtype.Text
	var existingEntryPhotos []string
	var existingStrategyID pgtype.UUID
	err = a.db.QueryRow(r.Context(), `
		SELECT status, buy_at, sell_at, duration_minutes, profit_percent, profit_usd,
			exchange, timeframe, comment, entry_basis, entry_photos, strategy_id
		FROM signal_trades
		WHERE id = $1 AND user_id = $2
	`, tradeUUID, numID).Scan(
		&status,
		&buyAt,
		&existingSellAt,
		&existingDuration,
		&existingProfitPercent,
		&existingProfitUSD,
		&existingExchange,
		&existingTimeframe,
		&existingComment,
		&existingEntryBasis,
		&existingEntryPhotos,
		&existingStrategyID,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeErr(w, http.StatusNotFound, "trade not found")
			return
		}
		writeErr(w, http.StatusInternalServerError, "db error")
		return
	}

	newSellAt := existingSellAt
	if in.SellAt != nil {
		parsed, err := time.Parse(time.RFC3339, strings.TrimSpace(*in.SellAt))
		if err != nil {
			writeErr(w, http.StatusBadRequest, "invalid sell_at")
			return
		}
		newSellAt = pgtype.Timestamptz{Time: parsed, Valid: true}
	} else if strings.EqualFold(status, "OPEN") {
		newSellAt = pgtype.Timestamptz{Time: time.Now(), Valid: true}
	}
	if !newSellAt.Valid && strings.EqualFold(status, "OPEN") {
		newSellAt = pgtype.Timestamptz{Time: time.Now(), Valid: true}
	}

	newDuration := existingDuration
	if in.DurationMin != nil {
		newDuration = pgtype.Int4{Int32: int32(*in.DurationMin), Valid: true}
	} else if strings.EqualFold(status, "OPEN") {
		minutes := int(math.Round(newSellAt.Time.Sub(buyAt).Minutes()))
		if minutes < 1 {
			minutes = 1
		}
		newDuration = pgtype.Int4{Int32: int32(minutes), Valid: true}
	}

	newProfitPercent := existingProfitPercent
	if in.ProfitPercent != nil {
		newProfitPercent = pgtype.Float8{Float64: *in.ProfitPercent, Valid: true}
	}

	newProfitUSD := existingProfitUSD
	if in.ProfitUSD != nil {
		newProfitUSD = pgtype.Float8{Float64: *in.ProfitUSD, Valid: true}
	}

	newExchange := existingExchange
	exchangeRaw := ""
	if in.Exchange != nil {
		v := strings.TrimSpace(*in.Exchange)
		exchangeRaw = v
		if v == "" {
			newExchange = pgtype.Text{}
		} else {
			newExchange = pgtype.Text{String: v, Valid: true}
		}
	}

	newTimeframe := existingTimeframe
	timeframeRaw := ""
	if in.Timeframe != nil {
		v := strings.TrimSpace(*in.Timeframe)
		timeframeRaw = v
		if v == "" {
			newTimeframe = pgtype.Text{}
		} else {
			newTimeframe = pgtype.Text{String: v, Valid: true}
		}
	}

	newComment := existingComment
	if in.Comment != nil {
		v := strings.TrimSpace(*in.Comment)
		if v == "" {
			newComment = pgtype.Text{}
		} else {
			newComment = pgtype.Text{String: v, Valid: true}
		}
	}

	newEntryBasis := existingEntryBasis
	if in.EntryBasis != nil {
		v := strings.TrimSpace(*in.EntryBasis)
		if v == "" {
			newEntryBasis = pgtype.Text{}
		} else {
			newEntryBasis = pgtype.Text{String: v, Valid: true}
		}
	}

	newEntryPhotos := existingEntryPhotos
	if in.EntryPhotos != nil {
		cleaned, err := validateTradePhotos(*in.EntryPhotos)
		if err != nil {
			writeErr(w, http.StatusBadRequest, err.Error())
			return
		}
		newEntryPhotos = cleaned
	}

	newStrategyID := existingStrategyID
	strategyNameRaw := ""
	if in.Strategy != nil {
		v := strings.TrimSpace(*in.Strategy)
		strategyNameRaw = v
		if v == "" {
			newStrategyID = pgtype.UUID{}
		} else {
			strategyID, err := a.getOrCreateStrategy(r.Context(), numID, v)
			if err != nil {
				writeErr(w, http.StatusInternalServerError, "strategy lookup failed")
				return
			}
			newStrategyID = strategyID
		}
	}

	var out TradeDTO
	var buyAtOut, createdAt, updatedAt time.Time
	var sellAtOut pgtype.Timestamptz
	var durationOut pgtype.Int4
	var profitPercent, profitUSD pgtype.Float8
	var exchangeOut, timeframeOut, commentOut, entryBasisOut, strategyName pgtype.Text
	var entryPhotosOut []string
	var strategyIDOut pgtype.UUID
	err = a.db.QueryRow(r.Context(), `
		UPDATE signal_trades
		SET status = CASE WHEN status = 'OPEN' THEN 'CLOSED' ELSE status END,
			sell_at = $3,
			duration_minutes = $4,
			profit_percent = $5,
			profit_usd = $6,
			exchange = $7,
			timeframe = $8,
			comment = $9,
			entry_basis = $10,
			entry_photos = $11,
			strategy_id = $12,
			updated_at = now()
		FROM signals s
		WHERE signal_trades.id = $1 AND signal_trades.user_id = $2
			AND s.id = signal_trades.signal_id
		RETURNING signal_trades.id, signal_trades.signal_id, signal_trades.user_id, signal_trades.status,
			signal_trades.buy_at, signal_trades.sell_at, signal_trades.duration_minutes,
			signal_trades.profit_percent, signal_trades.profit_usd,
			signal_trades.exchange, signal_trades.timeframe, signal_trades.comment,
			signal_trades.entry_basis, signal_trades.entry_photos, signal_trades.strategy_id,
			(SELECT name FROM trade_strategies WHERE id = signal_trades.strategy_id),
			signal_trades.created_at, signal_trades.updated_at,
			s.symbol
	`, tradeUUID, numID, newSellAt, newDuration, newProfitPercent, newProfitUSD, newExchange, newTimeframe, newComment, newEntryBasis, newEntryPhotos, newStrategyID).Scan(
		&out.ID,
		&out.SignalID,
		&out.UserID,
		&out.Status,
		&buyAtOut,
		&sellAtOut,
		&durationOut,
		&profitPercent,
		&profitUSD,
		&exchangeOut,
		&timeframeOut,
		&commentOut,
		&entryBasisOut,
		&entryPhotosOut,
		&strategyIDOut,
		&strategyName,
		&createdAt,
		&updatedAt,
		&out.Symbol,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeErr(w, http.StatusNotFound, "trade not found")
			return
		}
		writeErr(w, http.StatusInternalServerError, "db error")
		return
	}

	if sellAtOut.Valid {
		v := sellAtOut.Time.Format(time.RFC3339)
		out.SellAt = &v
	}
	if durationOut.Valid {
		v := int(durationOut.Int32)
		out.DurationMin = &v
	}
	if profitPercent.Valid {
		v := profitPercent.Float64
		out.ProfitPercent = &v
	}
	if profitUSD.Valid {
		v := profitUSD.Float64
		out.ProfitUSD = &v
	}
	if exchangeOut.Valid {
		v := exchangeOut.String
		out.Exchange = &v
	}
	if timeframeOut.Valid {
		v := timeframeOut.String
		out.Timeframe = &v
	}
	if commentOut.Valid {
		v := commentOut.String
		out.Comment = &v
	}
	if entryBasisOut.Valid {
		v := entryBasisOut.String
		out.EntryBasis = &v
	}
	if entryPhotosOut != nil {
		out.EntryPhotos = entryPhotosOut
	}
	if strategyIDOut.Valid {
		v := strategyIDOut.String()
		out.StrategyID = &v
	}
	if strategyName.Valid {
		v := strategyName.String
		out.StrategyName = &v
	}
	out.BuyAt = buyAtOut.Format(time.RFC3339)
	out.CreatedAt = createdAt.Format(time.RFC3339)
	out.UpdatedAt = updatedAt.Format(time.RFC3339)

	if timeframeRaw != "" || exchangeRaw != "" || strategyNameRaw != "" {
		_ = a.upsertTradeMeta(r.Context(), numID, timeframeRaw, exchangeRaw, strategyNameRaw)
	}

	writeJSON(w, http.StatusOK, map[string]any{"trade": out})
}

func (a *API) upsertTradeMeta(ctx context.Context, userID int64, timeframe, exchange, strategy string) error {
	_, err := a.db.Exec(ctx, `
		INSERT INTO trade_meta (user_id, timeframe, exchange, strategy_name)
		VALUES ($1, NULLIF($2, ''), NULLIF($3, ''), NULLIF($4, ''))
		ON CONFLICT (user_id) DO UPDATE
		SET timeframe = EXCLUDED.timeframe,
			exchange = EXCLUDED.exchange,
			strategy_name = EXCLUDED.strategy_name,
			updated_at = now()
	`, userID, timeframe, exchange, strategy)
	return err
}

const maxTradePhotos = 3
const maxTradePhotoBytes = 5 << 20

func validateTradePhotos(photos []string) ([]string, error) {
	if len(photos) > maxTradePhotos {
		return nil, errors.New("entry_photos_limit")
	}
	cleaned := make([]string, 0, len(photos))
	for _, raw := range photos {
		v := strings.TrimSpace(raw)
		if v == "" {
			return nil, errors.New("entry_photos_invalid")
		}
		if !strings.HasPrefix(v, "data:image/") {
			return nil, errors.New("entry_photos_invalid")
		}
		comma := strings.Index(v, ",")
		if comma < 0 || comma+1 >= len(v) {
			return nil, errors.New("entry_photos_invalid")
		}
		payload := v[comma+1:]
		decodedLen := base64.StdEncoding.DecodedLen(len(payload))
		if decodedLen > maxTradePhotoBytes {
			return nil, errors.New("entry_photos_too_large")
		}
		if _, err := base64.StdEncoding.DecodeString(payload); err != nil {
			return nil, errors.New("entry_photos_invalid")
		}
		cleaned = append(cleaned, v)
	}
	return cleaned, nil
}

// --------------------
// helpers
// --------------------

func isValidDirection(v string) bool {
	return v == "up" || v == "down" || v == "both"
}

func normalizeExchange(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "" {
		return "bybit"
	}
	return value
}

func isValidExchange(value string) bool {
	return value == "bybit" || value == "binance"
}

func normalizeMarketType(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", "linear", "future", "futures", "perp", "perpetual", "usdt_perpetual":
		return "perpetual"
	case "spot":
		return "spot"
	default:
		return strings.ToLower(strings.TrimSpace(value))
	}
}

func isValidMarketType(value string) bool {
	return value == "spot" || value == "perpetual"
}

func isSupportedInstrument(exchange, marketType string) bool {
	if !isValidExchange(exchange) || !isValidMarketType(marketType) {
		return false
	}
	return (exchange == "bybit" || exchange == "binance") && marketType == "perpetual"
}

func isValidIndicator(v string) bool {
	if isPriceIndicator(v) {
		return true
	}
	switch v {
	case "price",
		"openInterest", "fundingRate", "markPrice", "indexPrice", "volume", "delta", "cvd",
		"orderbookBid", "orderbookAsk", "orderbookSpread", "orderbookBidDepth", "orderbookAskDepth",
		"tradeLastPrice", "tradeLastSize", "tradeLastSide",
		"tradeBuyVolume", "tradeSellVolume", "tradeBuyUsd", "tradeSellUsd", "tradeCount",
		"longRatio", "shortRatio", "longShortRatio",
		"liquidations", "liquidationsCombined", "liquidationsLong", "liquidationsShort":
		return true
	default:
		return false
	}
}

func isPriceIndicator(v string) bool {
	if v == "price" {
		return true
	}
	if !strings.HasPrefix(v, "price:") {
		return false
	}
	parts := strings.Split(v, ":")
	if len(parts) != 3 {
		return false
	}
	allowed := map[string]bool{
		"open":  true,
		"high":  true,
		"low":   true,
		"close": true,
	}
	return allowed[parts[1]] && allowed[parts[2]]
}

func isMissingColumn(err error, column string) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "column") && strings.Contains(msg, column) && strings.Contains(msg, "does not exist")
}

func isMissingScannerColumn(err error) bool {
	for _, column := range []string{"conditions", "scanner_slot", "exchange", "market_type"} {
		if isMissingColumn(err, column) {
			return true
		}
	}
	return false
}

func parseConditions(raw []byte) []ConditionDTO {
	if len(raw) == 0 {
		return nil
	}
	var out []ConditionDTO
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil
	}
	return out
}

func (a *API) getOrCreateStrategy(ctx context.Context, userID int64, name string) (pgtype.UUID, error) {
	trimmed := strings.TrimSpace(name)
	if trimmed == "" {
		return pgtype.UUID{}, errors.New("strategy name empty")
	}
	nameNorm := strings.ToLower(trimmed)

	var id pgtype.UUID
	err := a.db.QueryRow(ctx, `
		SELECT id FROM trade_strategies WHERE user_id = $1 AND name_norm = $2
	`, userID, nameNorm).Scan(&id)
	if err == nil {
		return id, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return pgtype.UUID{}, err
	}

	err = a.db.QueryRow(ctx, `
		INSERT INTO trade_strategies (user_id, name, name_norm)
		VALUES ($1, $2, $3)
		RETURNING id
	`, userID, trimmed, nameNorm).Scan(&id)
	if err == nil {
		return id, nil
	}

	// In case of a race, try to read again.
	err = a.db.QueryRow(ctx, `
		SELECT id FROM trade_strategies WHERE user_id = $1 AND name_norm = $2
	`, userID, nameNorm).Scan(&id)
	if err != nil {
		return pgtype.UUID{}, err
	}
	return id, nil
}

func normalizeConditions(in []ConditionDTO, indicator, direction string, thresholdPercent, thresholdAmount *float64) ([]ConditionDTO, error) {
	conds := in
	if len(conds) == 0 {
		if indicator == "" {
			indicator = "price"
		}
		if direction == "" {
			direction = "both"
		}
		conds = []ConditionDTO{
			{
				Indicator:        indicator,
				Direction:        direction,
				ThresholdPercent: thresholdPercent,
				ThresholdAmount:  thresholdAmount,
				Negate:           false,
			},
		}
	}
	if len(conds) == 0 || len(conds) > 3 {
		return nil, errors.New("invalid conditions")
	}
	for i := range conds {
		c := &conds[i]
		c.Indicator = strings.TrimSpace(c.Indicator)
		if c.Indicator == "" {
			c.Indicator = "price"
		}
		if !isValidIndicator(c.Indicator) {
			return nil, errors.New("invalid indicator")
		}
		// Negate просто переносим как есть, по умолчанию false.

		if c.Direction == "" {
			c.Direction = "both"
		}
		if !isValidDirection(c.Direction) {
			return nil, errors.New("invalid direction")
		}
		if strings.HasPrefix(c.Indicator, "liquidations") {
			if c.ThresholdAmount == nil || *c.ThresholdAmount <= 0 {
				return nil, errors.New("invalid threshold_amount")
			}
			c.ThresholdPercent = nil
		} else {
			if c.ThresholdPercent == nil || *c.ThresholdPercent <= 0 {
				return nil, errors.New("invalid threshold_percent")
			}
			c.ThresholdAmount = nil
		}
	}
	return conds, nil
}

const (
	symbolCacheTTL           = 30 * time.Minute
	symbolCacheSweepInterval = time.Minute
	symbolCacheMaxEntries    = 10_000
)

func normalizeSymbolsInput(raw string) (symbolList, error) {
	value := strings.TrimSpace(raw)
	if value == "" || value == "*" || strings.EqualFold(value, "ALL") {
		return symbolList{IsWildcard: true}, nil
	}

	parts := strings.FieldsFunc(value, func(r rune) bool {
		switch r {
		case ' ', '\t', '\n', '\r', ',', ';':
			return true
		default:
			return false
		}
	})
	if len(parts) == 0 {
		return symbolList{}, errors.New("Укажи хотя бы один символ.")
	}

	re := regexp.MustCompile(`^[A-Z0-9]{3,30}$`)
	seen := make(map[string]bool)
	symbols := make([]string, 0, len(parts))
	items := make([]symbolInput, 0, len(parts))

	for i, part := range parts {
		rawToken := strings.TrimSpace(part)
		cleaned := strings.ToUpper(rawToken)
		cleaned = strings.ReplaceAll(cleaned, "/", "")
		cleaned = strings.ReplaceAll(cleaned, "-", "")
		if cleaned == "" {
			return symbolList{}, fmt.Errorf("Некорректный символ: %s (позиция %d)", rawToken, i+1)
		}
		if !strings.HasSuffix(cleaned, "USDT") {
			cleaned += "USDT"
		}
		if !re.MatchString(cleaned) {
			return symbolList{}, fmt.Errorf("Некорректный символ: %s (позиция %d)", rawToken, i+1)
		}
		if seen[cleaned] {
			continue
		}
		seen[cleaned] = true
		symbols = append(symbols, cleaned)
		items = append(items, symbolInput{Raw: rawToken, Normalized: cleaned, Index: i + 1})
	}

	if len(symbols) == 0 {
		return symbolList{}, errors.New("Укажи хотя бы один символ.")
	}

	return symbolList{Symbols: symbols, Items: items}, nil
}

func (a *API) ensureSymbolsExist(ctx context.Context, list symbolList, exchange, marketType string) error {
	if list.IsWildcard || len(list.Symbols) == 0 {
		return nil
	}

	invalid := make([]symbolInput, 0)
	for _, item := range list.Items {
		exists, err := a.symbolExists(ctx, exchange, marketType, item.Normalized)
		if err != nil {
			return errors.New("Не удалось проверить пары на бирже. Попробуй позже.")
		}
		if !exists {
			invalid = append(invalid, item)
		}
	}
	if len(invalid) == 0 {
		return nil
	}

	parts := make([]string, 0, len(invalid))
	for _, item := range invalid {
		label := strings.TrimSpace(item.Raw)
		if label == "" {
			label = item.Normalized
		}
		parts = append(parts, fmt.Sprintf("%s (позиция %d)", label, item.Index))
	}
	if len(parts) == 1 {
		return fmt.Errorf("Пара не найдена: %s", parts[0])
	}
	return fmt.Errorf("Пары не найдены: %s", strings.Join(parts, ", "))
}

func (a *API) symbolExists(ctx context.Context, exchange, marketType, symbol string) (bool, error) {
	exchange = normalizeExchange(exchange)
	marketType = normalizeMarketType(marketType)
	sym := strings.ToUpper(strings.TrimSpace(symbol))
	if sym == "" {
		return false, nil
	}
	cacheKey := exchange + ":" + marketType + ":" + sym
	if entry, ok := a.getSymbolCache(cacheKey); ok {
		return entry.Exists, nil
	}
	var (
		exists bool
		err    error
	)
	switch exchange {
	case "bybit":
		exists, err = a.symbolExistsBybit(ctx, marketType, sym)
	case "binance":
		exists, err = a.symbolExistsBinance(ctx, marketType, sym)
	default:
		return false, errors.New("unsupported exchange")
	}
	if err == nil {
		a.setSymbolCache(cacheKey, exists)
	}
	return exists, err
}

func (a *API) symbolExistsBybit(ctx context.Context, marketType, symbol string) (bool, error) {

	baseURL := strings.TrimSpace(os.Getenv("BYBIT_BASE_URL"))
	if baseURL == "" {
		baseURL = "https://api.bybit.com"
	}
	category := "linear"
	if marketType == "spot" {
		category = "spot"
	}

	q := url.Values{}
	q.Set("category", category)
	q.Set("symbol", symbol)
	urlStr := strings.TrimRight(baseURL, "/") + "/v5/market/instruments-info?" + q.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, urlStr, nil)
	if err != nil {
		return false, err
	}
	resp, err := a.httpClient.Do(req)
	if err != nil {
		return false, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return false, fmt.Errorf("bybit status %d", resp.StatusCode)
	}

	var payload struct {
		RetCode int `json:"retCode"`
		Result  struct {
			List []struct {
				Symbol string `json:"symbol"`
			} `json:"list"`
		} `json:"result"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return false, err
	}
	if payload.RetCode != 0 {
		return false, fmt.Errorf("bybit error %d", payload.RetCode)
	}
	return len(payload.Result.List) > 0, nil
}

func (a *API) symbolExistsBinance(ctx context.Context, marketType, symbol string) (bool, error) {
	if marketType != "perpetual" {
		return false, errors.New("binance spot scanner is not enabled")
	}
	baseURL := strings.TrimSpace(os.Getenv("BINANCE_FUTURES_BASE_URL"))
	if baseURL == "" {
		baseURL = "https://fapi.binance.com"
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(baseURL, "/")+"/fapi/v1/exchangeInfo", nil)
	if err != nil {
		return false, err
	}
	resp, err := a.httpClient.Do(req)
	if err != nil {
		return false, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return false, fmt.Errorf("binance status %d", resp.StatusCode)
	}
	var payload struct {
		Symbols []struct {
			Symbol       string `json:"symbol"`
			Status       string `json:"status"`
			ContractType string `json:"contractType"`
			QuoteAsset   string `json:"quoteAsset"`
		} `json:"symbols"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return false, err
	}
	found := false
	for _, item := range payload.Symbols {
		if !strings.EqualFold(item.Status, "TRADING") || !strings.EqualFold(item.ContractType, "PERPETUAL") || !strings.EqualFold(item.QuoteAsset, "USDT") {
			continue
		}
		listedSymbol := strings.ToUpper(strings.TrimSpace(item.Symbol))
		if listedSymbol == "" {
			continue
		}
		a.setSymbolCache("binance:perpetual:"+listedSymbol, true)
		if listedSymbol == symbol {
			found = true
		}
	}
	return found, nil
}

func (a *API) getSymbolCache(symbol string) (symbolCacheEntry, bool) {
	now := time.Now()
	a.symbolCacheMu.Lock()
	defer a.symbolCacheMu.Unlock()
	entry, ok := a.symbolCache[symbol]
	if !ok {
		return symbolCacheEntry{}, false
	}
	if now.Sub(entry.CheckedAt) > symbolCacheTTL {
		delete(a.symbolCache, symbol)
		return symbolCacheEntry{}, false
	}
	entry.LastAccessed = now
	a.symbolCache[symbol] = entry
	return entry, true
}

func (a *API) setSymbolCache(symbol string, exists bool) {
	a.symbolCacheMu.Lock()
	defer a.symbolCacheMu.Unlock()
	if a.symbolCache == nil {
		a.symbolCache = make(map[string]symbolCacheEntry)
	}
	now := time.Now()
	if a.symbolCacheLastSweep.IsZero() || now.Sub(a.symbolCacheLastSweep) >= symbolCacheSweepInterval {
		a.sweepSymbolCacheLocked(now, symbolCacheMaxEntries)
		a.symbolCacheLastSweep = now
	}
	if _, updating := a.symbolCache[symbol]; !updating && len(a.symbolCache) >= symbolCacheMaxEntries {
		a.sweepSymbolCacheLocked(now, symbolCacheMaxEntries-1)
	}
	a.symbolCache[symbol] = symbolCacheEntry{Exists: exists, CheckedAt: now, LastAccessed: now}
}

// sweepSymbolCacheLocked removes expired entries first and then evicts the
// least-recently-used entries until the requested bound is met.
// The caller must hold symbolCacheMu for writing.
func (a *API) sweepSymbolCacheLocked(now time.Time, maxEntries int) {
	if maxEntries < 0 {
		maxEntries = 0
	}
	for key, entry := range a.symbolCache {
		if now.Sub(entry.CheckedAt) > symbolCacheTTL {
			delete(a.symbolCache, key)
		}
	}
	for len(a.symbolCache) > maxEntries {
		oldestKey := ""
		var oldest time.Time
		for key, entry := range a.symbolCache {
			accessed := entry.LastAccessed
			if accessed.IsZero() {
				accessed = entry.CheckedAt
			}
			if oldestKey == "" || accessed.Before(oldest) {
				oldestKey = key
				oldest = accessed
			}
		}
		if oldestKey == "" {
			break
		}
		delete(a.symbolCache, oldestKey)
	}
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]any{"error": msg})
}

func (a *API) disableOtherRules(ctx context.Context, userID int64, slot, exchange, marketType string, keepID int64) error {
	_, err := a.db.Exec(ctx, `
		UPDATE alerts
		SET enabled = false
		WHERE user_id = $1 AND scanner_slot = $2 AND exchange = $3 AND market_type = $4 AND id <> $5
	`, userID, slot, exchange, marketType, keepID)
	return err
}

func isValidScannerSlot(slot string) bool {
	return slot == "SLOT_1" || slot == "SLOT_2" || slot == "SLOT_3"
}
