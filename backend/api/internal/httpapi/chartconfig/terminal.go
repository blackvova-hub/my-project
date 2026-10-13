package chartconfigapi

import (
	"encoding/json"
	"errors"
	"github.com/jackc/pgx/v5"
	"io"
	"net/http"
	"strings"
	"time"
)

// The versioned terminal lives in the existing JSONB document. Legacy clients
// continue using /chart/config and preserve this field when saving drawings.
type terminalRequest struct {
	Workspace   json.RawMessage           `json:"workspace"`
	Revision    string                    `json:"revision"`
	DrawingSets map[string][]ChartDrawing `json:"drawing_sets,omitempty"`
}

func validateTerminal(raw json.RawMessage) error {
	if len(raw) > 180<<10 {
		return errors.New("terminal_too_large")
	}
	var w struct {
		Version int `json:"version"`
		Layout  int `json:"layout"`
		Active  int `json:"active"`
		Windows []struct {
			ID          string            `json:"id"`
			Symbol      string            `json:"symbol"`
			Exchange    string            `json:"exchange"`
			Market      string            `json:"market"`
			Interval    string            `json:"interval"`
			Indicators  []string          `json:"indicators"`
			Instances   []json.RawMessage `json:"instances"`
			Comparisons []json.RawMessage `json:"comparisons"`
		} `json:"windows"`
	}
	if json.Unmarshal(raw, &w) != nil || w.Version != 3 || w.Layout < 1 || w.Layout > 9 || w.Active < 0 || w.Active >= w.Layout || len(w.Windows) != 9 {
		return errors.New("invalid_terminal")
	}
	seen := map[string]bool{}
	for _, v := range w.Windows {
		if len(v.ID) == 0 || len(v.ID) > 40 || seen[v.ID] || len(v.Symbol) < 2 || len(v.Symbol) > 40 || strings.ContainsAny(v.Symbol, "/\\\x00") || (v.Exchange != "bybit" && v.Exchange != "binance") || (v.Market != "spot" && v.Market != "linear") {
			return errors.New("invalid_terminal_window")
		}
		seen[v.ID] = true
		if _, ok := allowedIntervals[v.Interval]; !ok {
			return errors.New("invalid_interval")
		}
		if len(v.Indicators) > len(allowedIndicators) || len(v.Instances) > 16 || len(v.Comparisons) > 4 {
			return errors.New("terminal_too_many_studies")
		}
		for _, id := range v.Indicators {
			if _, ok := allowedIndicators[id]; !ok {
				return errors.New("invalid_indicator")
			}
		}
	}
	return nil
}
func (a *API) handleTerminalGet(w http.ResponseWriter, r *http.Request) {
	user, ok := userFromContext(r)
	if !ok {
		writeErr(w, 401, "unauthorized")
		return
	}
	var raw json.RawMessage
	var updated time.Time
	err := a.db.QueryRow(r.Context(), `SELECT config, updated_at FROM user_chart_configurations WHERE user_id=$1`, user.ID).Scan(&raw, &updated)
	if errors.Is(err, pgx.ErrNoRows) {
		writeJSON(w, 200, map[string]any{"workspace": nil, "revision": ""})
		return
	}
	if err != nil {
		writeErr(w, 500, "db_error")
		return
	}
	if len(raw) == 0 {
		raw = json.RawMessage("null")
	}
	var config ChartConfig
	if json.Unmarshal(raw, &config) != nil {
		writeErr(w, 500, "invalid_stored_config")
		return
	}
	if len(config.Terminal) == 0 {
		config.Terminal = json.RawMessage("null")
	}
	writeJSON(w, 200, map[string]any{"workspace": config.Terminal, "drawing_sets": config.DrawingSets, "revision": updated.UTC().Format(time.RFC3339Nano)})
}
func (a *API) handleTerminalPut(w http.ResponseWriter, r *http.Request) {
	user, ok := userFromContext(r)
	if !ok {
		writeErr(w, 401, "unauthorized")
		return
	}
	// Only same-origin JSON writes; credentials never leave the API.
	if r.Header.Get("Sec-Fetch-Site") == "cross-site" || !strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") {
		writeErr(w, 403, "cross_site_request")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxConfigBodyBytes)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	var input terminalRequest
	if decoder.Decode(&input) != nil || !errors.Is(decoder.Decode(&struct{}{}), io.EOF) {
		writeErr(w, 400, "invalid_json")
		return
	}
	if err := validateTerminal(input.Workspace); err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	if input.DrawingSets != nil {
		candidate := defaultConfig()
		candidate.DrawingSets = input.DrawingSets
		if _, err := normalizeConfig(candidate); err != nil {
			writeErr(w, 400, err.Error())
			return
		}
	}
	var expected *time.Time
	if input.Revision != "" {
		parsed, err := time.Parse(time.RFC3339Nano, input.Revision)
		if err != nil {
			writeErr(w, 400, "invalid_revision")
			return
		}
		expected = &parsed
	}
	config := defaultConfig()
	config.Terminal = input.Workspace
	if input.DrawingSets != nil {
		config.DrawingSets = input.DrawingSets
	}
	drawings, _ := json.Marshal(input.DrawingSets)
	raw, _ := json.Marshal(config)
	var updated time.Time
	// Compare-and-swap in the database, including the initial concurrent insert.
	err := a.db.QueryRow(r.Context(), `INSERT INTO user_chart_configurations(user_id,config) VALUES($1,$2::jsonb)
	 ON CONFLICT(user_id) DO UPDATE SET config=CASE WHEN $5::jsonb='null'::jsonb THEN jsonb_set(user_chart_configurations.config,'{terminal}',$3::jsonb,true)
	 ELSE jsonb_set(jsonb_set(user_chart_configurations.config,'{terminal}',$3::jsonb,true),'{drawing_sets}',$5::jsonb,true) END,updated_at=now()
	 WHERE $4::timestamptz IS NOT NULL AND user_chart_configurations.updated_at=$4::timestamptz RETURNING updated_at`, user.ID, raw, []byte(input.Workspace), expected, drawings).Scan(&updated)
	if errors.Is(err, pgx.ErrNoRows) {
		writeErr(w, 409, "terminal_revision_conflict")
		return
	}
	if err != nil {
		writeErr(w, 500, "db_error")
		return
	}
	writeJSON(w, 200, map[string]any{"workspace": input.Workspace, "drawing_sets": input.DrawingSets, "revision": updated.UTC().Format(time.RFC3339Nano)})
}
func (a *API) handleTerminalDelete(w http.ResponseWriter, r *http.Request) {
	user, ok := userFromContext(r)
	if !ok {
		writeErr(w, 401, "unauthorized")
		return
	}
	if r.Header.Get("Sec-Fetch-Site") == "cross-site" || !strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") {
		writeErr(w, 403, "cross_site_request")
		return
	}
	var input struct {
		Revision string `json:"revision"`
	}
	r.Body = http.MaxBytesReader(w, r.Body, 1024)
	if json.NewDecoder(r.Body).Decode(&input) != nil {
		writeErr(w, 400, "invalid_json")
		return
	}
	revision, err := time.Parse(time.RFC3339Nano, input.Revision)
	if err != nil {
		writeErr(w, 400, "invalid_revision")
		return
	}
	var updated time.Time
	err = a.db.QueryRow(r.Context(), `UPDATE user_chart_configurations SET config=config-'terminal',updated_at=now() WHERE user_id=$1 AND updated_at=$2 RETURNING updated_at`, user.ID, revision).Scan(&updated)
	if errors.Is(err, pgx.ErrNoRows) {
		writeErr(w, 409, "terminal_revision_conflict")
		return
	}
	if err != nil {
		writeErr(w, 500, "db_error")
		return
	}
	writeJSON(w, 200, map[string]any{"workspace": nil, "revision": updated.UTC().Format(time.RFC3339Nano)})
}
