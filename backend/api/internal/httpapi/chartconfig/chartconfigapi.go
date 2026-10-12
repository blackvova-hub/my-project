package chartconfigapi

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"math"
	"net/http"
	"regexp"
	"time"
	"unicode/utf8"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"backend/internal/auth"
)

const maxConfigBodyBytes = int64(256 << 10)

var allowedIntervals = map[string]struct{}{
	"1m": {}, "5m": {}, "15m": {}, "30m": {}, "1h": {}, "4h": {}, "1d": {}, "1w": {},
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
	"trend":              2,
	"arrow":              2,
	"ray":                2,
	"extended":           2,
	"angle":              2,
	"horizontal":         1,
	"horizontal-ray":     1,
	"vertical":           1,
	"cross":              1,
	"channel":            3,
	"regression":         2,
	"flat-channel":       2,
	"disjoint-channel":   4,
	"fibonacci":          2,
	"fib-extension":      3,
	"pitchfork":          3,
	"schiff":             3,
	"modified-schiff":    3,
	"inside-pitchfork":   3,
	"fib-channel":        3,
	"fib-time":           2,
	"fib-trend-time":     3,
	"gann-box":           2,
	"gann-square":        2,
	"gann-fan":           2,
	"fib-fan":            2,
	"brush":              0,
	"highlighter":        0,
	"rectangle":          2,
	"circle":             2,
	"ellipse":            2,
	"path":               -1,
	"curve":              3,
	"polyline":           -1,
	"triangle":           3,
	"rotated-rectangle":  3,
	"arc":                3,
	"double-curve":       4,
	"text":               1,
	"screen-text":        1,
	"note":               1,
	"screen-note":        1,
	"callout":            2,
	"comment":            1,
	"price-label":        1,
	"price-note":         1,
	"arrow-marker":       1,
	"arrow-left":         1,
	"arrow-right":        1,
	"arrow-up":           1,
	"arrow-down":         1,
	"emoji":              1,
	"xabcd":              5,
	"cypher":             5,
	"abcd":               4,
	"triangle-pattern":   4,
	"three-drives":       7,
	"head-shoulders":     7,
	"elliott-impulse":    6,
	"elliott-triangle":   6,
	"elliott-triple":     6,
	"elliott-correction": 4,
	"elliott-double":     4,
	"cycles":             2,
	"time-cycles":        2,
	"sine":               2,
	"long":               3,
	"short":              3,
	"forecast":           2,
	"date-range":         2,
	"price-range":        2,
	"ruler":              2,
	"bars-pattern":       3,
	"price-projection":   3,
	"projection":         3,
	"volume-profile":     2,
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
	ID       int64           `json:"id"`
	Type     string          `json:"type"`
	Points   []DrawingPoint  `json:"points"`
	Text     string          `json:"text,omitempty"`
	Locked   bool            `json:"locked,omitempty"`
	Hidden   bool            `json:"hidden,omitempty"`
	Color    string          `json:"color,omitempty"`
	Width    int             `json:"width,omitempty"`
	Dash     string          `json:"dash,omitempty"`
	Fill     *bool           `json:"fill,omitempty"`
	Screen   []float64       `json:"screen,omitempty"`
	Levels   []float64       `json:"levels,omitempty"`
	Quantity float64         `json:"quantity,omitempty"`
	Sample   []DrawingSample `json:"sample,omitempty"`
}
type DrawingSample struct {
	Time   int64   `json:"time"`
	Open   float64 `json:"open"`
	High   float64 `json:"high"`
	Low    float64 `json:"low"`
	Close  float64 `json:"close"`
	Volume float64 `json:"volume"`
}

var drawingColor = regexp.MustCompile(`^#[0-9a-fA-F]{6}$`)

func validateDrawingStyle(d ChartDrawing) error {
	if d.Color != "" && !drawingColor.MatchString(d.Color) {
		return errors.New("invalid_drawing_color")
	}
	if d.Width < 0 || d.Width > 8 {
		return errors.New("invalid_drawing_width")
	}
	if d.Dash != "" && d.Dash != "solid" && d.Dash != "dashed" && d.Dash != "dotted" {
		return errors.New("invalid_drawing_dash")
	}
	if len(d.Screen) != 0 && len(d.Screen) != 2 {
		return errors.New("invalid_screen_anchor")
	}
	for _, v := range d.Screen {
		if math.IsNaN(v) || math.IsInf(v, 0) || v < 0 || v > 1 {
			return errors.New("invalid_screen_anchor")
		}
	}
	if len(d.Levels) > 20 {
		return errors.New("too_many_levels")
	}
	for _, v := range d.Levels {
		if math.IsNaN(v) || math.IsInf(v, 0) || math.Abs(v) > 100 {
			return errors.New("invalid_level")
		}
	}
	if math.IsNaN(d.Quantity) || math.IsInf(d.Quantity, 0) || d.Quantity < 0 || d.Quantity > 1e15 {
		return errors.New("invalid_quantity")
	}
	if len(d.Sample) > 500 {
		return errors.New("too_many_sample_bars")
	}
	var previous int64
	for _, v := range d.Sample {
		if v.Time <= previous || v.Time <= 0 || v.Volume < 0 || v.Low <= 0 || v.High < math.Max(v.Open, v.Close) || v.Low > math.Min(v.Open, v.Close) {
			return errors.New("invalid_sample_bar")
		}
		for _, n := range []float64{v.Open, v.High, v.Low, v.Close, v.Volume} {
			if math.IsNaN(n) || math.IsInf(n, 0) {
				return errors.New("invalid_sample_bar")
			}
		}
		previous = v.Time
	}
	return nil
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

			if drawing.Type == "brush" || drawing.Type == "highlighter" || drawing.Type == "path" || drawing.Type == "polyline" {
				if len(drawing.Points) == 0 || len(drawing.Points) > 800 {
					return ChartConfig{}, errors.New("invalid_drawing_points")
				}
			} else if expected, ok := allowedDrawingPointCounts[drawing.Type]; !ok || len(drawing.Points) != expected {
				return ChartConfig{}, errors.New("invalid_drawing_points")
			}
			if drawing.Type == "text" || drawing.Type == "screen-text" || drawing.Type == "note" || drawing.Type == "screen-note" || drawing.Type == "callout" || drawing.Type == "comment" || drawing.Type == "price-note" || drawing.Type == "emoji" {
				if utf8.RuneCountInString(drawing.Text) == 0 || utf8.RuneCountInString(drawing.Text) > 200 {
					return ChartConfig{}, errors.New("invalid_drawing_text")
				}
			} else if drawing.Text != "" {
				return ChartConfig{}, errors.New("invalid_drawing_text")
			}
			if err := validateDrawingStyle(drawing); err != nil {
				return ChartConfig{}, err
			}
			for _, point := range drawing.Points {
				if point.Time <= 0 || point.Time > 8640000000000 || math.IsNaN(point.Price) || math.IsInf(point.Price, 0) {
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
