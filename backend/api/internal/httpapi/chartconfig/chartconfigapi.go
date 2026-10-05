package chartconfigapi

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"math"
	"net/http"
	"time"
	"unicode/utf8"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"backend/internal/auth"
)

const maxConfigBodyBytes = int64(256 << 10)

var allowedIntervals = map[string]struct{}{
	"1m": {}, "5m": {}, "15m": {}, "1h": {}, "4h": {},
}

var allowedIndicators = map[string]struct{}{
	"sma": {}, "ema": {}, "bollinger": {}, "vwap": {},
	"supertrend": {}, "ichimoku": {}, "rsi": {}, "macd": {},
	"atr": {}, "stochastic": {}, "adx": {}, "cci": {}, "roc": {},
	"swing_points": {}, "bos": {}, "choch": {}, "mss": {},
	"order_blocks": {}, "fvg": {}, "liquidity": {},
	"equal_high_low": {}, "liquidity_sweeps": {},
}

var allowedDrawingPointCounts = map[string]int{
	"horizontal": 1,
	"vertical":   1,
	"trend":      2,
	"ray":        2,
	"rectangle":  2,
	"fibonacci":  2,
	"text":       1,
	"ruler":      2,
}

type API struct {
	authStore *auth.Store
	db        *pgxpool.Pool
}

type ctxKey string

const ctxUserKey ctxKey = "auth_user"

type ChartConfig struct {
	Version            int                       `json:"version"`
	SelectedInterval   string                    `json:"selected_interval"`
	ActiveIndicatorIDs []string                  `json:"active_indicator_ids"`
	DrawingSets        map[string][]ChartDrawing `json:"drawing_sets"`
}

type DrawingPoint struct {
	Time  int64   `json:"time"`
	Price float64 `json:"price"`
}

type ChartDrawing struct {
	ID     int64          `json:"id"`
	Type   string         `json:"type"`
	Points []DrawingPoint `json:"points"`
	Text   string         `json:"text,omitempty"`
	Locked bool           `json:"locked,omitempty"`
}

type chartConfigResponse struct {
	Config    ChartConfig `json:"config"`
	Saved     bool        `json:"saved"`
	UpdatedAt *time.Time  `json:"updated_at,omitempty"`
}

func New(authStore *auth.Store, db *pgxpool.Pool) *API {
	return &API{authStore: authStore, db: db}
}

func (a *API) Routes(r chi.Router) {
	r.Group(func(r chi.Router) {
		r.Use(a.requireAuth)
		r.Get("/chart/config", a.handleGet)
		r.Put("/chart/config", a.handlePut)
	})
}

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

func userFromContext(r *http.Request) (auth.User, bool) {
	user, ok := r.Context().Value(ctxUserKey).(auth.User)
	return user, ok
}

func defaultConfig() ChartConfig {
	return ChartConfig{
		Version:            1,
		SelectedInterval:   "1m",
		ActiveIndicatorIDs: []string{},
		DrawingSets:        map[string][]ChartDrawing{},
	}
}

func normalizeConfig(config ChartConfig) (ChartConfig, error) {
	if config.Version != 1 {
		return ChartConfig{}, errors.New("unsupported_version")
	}
	if _, ok := allowedIntervals[config.SelectedInterval]; !ok {
		return ChartConfig{}, errors.New("invalid_interval")
	}
	if len(config.ActiveIndicatorIDs) > len(allowedIndicators) {
		return ChartConfig{}, errors.New("too_many_indicators")
	}
	seen := make(map[string]struct{}, len(config.ActiveIndicatorIDs))
	ids := make([]string, 0, len(config.ActiveIndicatorIDs))
	for _, id := range config.ActiveIndicatorIDs {
		if _, ok := allowedIndicators[id]; !ok {
			return ChartConfig{}, errors.New("invalid_indicator")
		}
		if _, duplicate := seen[id]; duplicate {
			continue
		}
		seen[id] = struct{}{}
		ids = append(ids, id)
	}
	config.ActiveIndicatorIDs = ids
	if config.DrawingSets == nil {
		config.DrawingSets = map[string][]ChartDrawing{}
	}
	if len(config.DrawingSets) > 50 {
		return ChartConfig{}, errors.New("too_many_drawing_sets")
	}
	for key, drawings := range config.DrawingSets {
		if key == "" || len(key) > 180 {
			return ChartConfig{}, errors.New("invalid_drawing_set")
		}
		if len(drawings) > 100 {
			return ChartConfig{}, errors.New("too_many_drawings")
		}
		seenDrawingIDs := make(map[int64]struct{}, len(drawings))
		for _, drawing := range drawings {
			if drawing.ID <= 0 {
				return ChartConfig{}, errors.New("invalid_drawing")
			}
			if _, duplicate := seenDrawingIDs[drawing.ID]; duplicate {
				return ChartConfig{}, errors.New("duplicate_drawing")
			}
			seenDrawingIDs[drawing.ID] = struct{}{}

			if drawing.Type == "brush" {
				if len(drawing.Points) == 0 || len(drawing.Points) > 800 {
					return ChartConfig{}, errors.New("invalid_drawing_points")
				}
			} else if expected, ok := allowedDrawingPointCounts[drawing.Type]; !ok || len(drawing.Points) != expected {
				return ChartConfig{}, errors.New("invalid_drawing_points")
			}
			if drawing.Type == "text" {
				if utf8.RuneCountInString(drawing.Text) == 0 || utf8.RuneCountInString(drawing.Text) > 200 {
					return ChartConfig{}, errors.New("invalid_drawing_text")
				}
			} else if drawing.Text != "" {
				return ChartConfig{}, errors.New("invalid_drawing_text")
			}
			for _, point := range drawing.Points {
				if point.Time <= 0 || math.IsNaN(point.Price) || math.IsInf(point.Price, 0) {
					return ChartConfig{}, errors.New("invalid_drawing_point")
				}
			}
		}
	}
	return config, nil
}

func (a *API) handleGet(w http.ResponseWriter, r *http.Request) {
	user, ok := userFromContext(r)
	if !ok {
		writeErr(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	var raw []byte
	var updatedAt time.Time
	err := a.db.QueryRow(r.Context(), `
		SELECT config, updated_at
		FROM user_chart_configurations
		WHERE user_id = $1
	`, user.ID).Scan(&raw, &updatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		writeJSON(w, http.StatusOK, chartConfigResponse{Config: defaultConfig(), Saved: false})
		return
	}
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "db_error")
		return
	}
	var config ChartConfig
	if err := json.Unmarshal(raw, &config); err != nil {
		writeErr(w, http.StatusInternalServerError, "invalid_stored_config")
		return
	}
	config, err = normalizeConfig(config)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "invalid_stored_config")
		return
	}
	writeJSON(w, http.StatusOK, chartConfigResponse{Config: config, Saved: true, UpdatedAt: &updatedAt})
}

func (a *API) handlePut(w http.ResponseWriter, r *http.Request) {
	user, ok := userFromContext(r)
	if !ok {
		writeErr(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxConfigBodyBytes)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	var config ChartConfig
	if err := decoder.Decode(&config); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid_json")
		return
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		writeErr(w, http.StatusBadRequest, "invalid_json")
		return
	}
	config, err := normalizeConfig(config)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	raw, err := json.Marshal(config)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "encode_error")
		return
	}
	var updatedAt time.Time
	err = a.db.QueryRow(r.Context(), `
		INSERT INTO user_chart_configurations (user_id, config)
		VALUES ($1, $2::jsonb)
		ON CONFLICT (user_id) DO UPDATE
		SET config = EXCLUDED.config, updated_at = now()
		RETURNING updated_at
	`, user.ID, raw).Scan(&updatedAt)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "db_error")
		return
	}
	writeJSON(w, http.StatusOK, chartConfigResponse{Config: config, Saved: true, UpdatedAt: &updatedAt})
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func writeErr(w http.ResponseWriter, status int, code string) {
	writeJSON(w, status, map[string]any{"error": code})
}
