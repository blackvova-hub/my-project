package adminapi

import (
	"context"
	"encoding/json"
	"math"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"backend/internal/auth"
)

type API struct {
	authStore *auth.Store
	db        *pgxpool.Pool
	logs      *LogBuffer
}

func New(authStore *auth.Store, db *pgxpool.Pool, logs *LogBuffer) *API {
	return &API{authStore: authStore, db: db, logs: logs}
}

func (a *API) Routes(r chi.Router) {
	r.Group(func(r chi.Router) {
		r.Use(a.requireAdmin)
		r.Get("/admin/users", a.handleListUsers)
		r.Get("/admin/users/{id}/scanner-rules", a.handleListUserScannerRules)
		r.Get("/admin/logs", a.handleListLogs)
		r.Patch("/admin/users/{id}/plan", a.handleUpdatePlan)
		r.Patch("/admin/users/{id}/subscription", a.handleUpdateSubscription)
		r.Delete("/admin/users/{id}", a.handleDeleteUser)
		r.Get("/admin/wallet-registry", a.handleListWalletRegistry)
		r.Post("/admin/wallet-registry", a.handleCreateWallet)
		r.Get("/admin/wallet-registry/{chain}/{address}", a.handleGetWallet)
		r.Patch("/admin/wallet-registry/{chain}/{address}", a.handlePatchWallet)
	})
}

type ctxKey string

const ctxUserKey ctxKey = "admin_user"

func (a *API) requireAdmin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user, err := a.authStore.Authenticate(r)
		if err != nil {
			writeErr(w, http.StatusUnauthorized, "unauthorized")
			return
		}
		if !user.IsAdmin {
			writeErr(w, http.StatusForbidden, "forbidden")
			return
		}
		ctx := context.WithValue(r.Context(), ctxUserKey, user)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

type AdminUserDTO struct {
	ID                           string   `json:"id"`
	NumID                        int64    `json:"num_id"`
	PublicID                     int64    `json:"public_id"`
	Email                        string   `json:"email"`
	DisplayName                  string   `json:"displayName"`
	LastIP                       string   `json:"lastIp"`
	Plan                         string   `json:"plan"`
	SubscriptionExpiresAt        *string  `json:"subscriptionExpiresAt,omitempty"`
	SubscriptionFrozenAt         *string  `json:"subscriptionFrozenAt,omitempty"`
	SubscriptionFrozenDaysRemain int      `json:"subscriptionFrozenDaysRemaining"`
	SubscriptionDaysRemaining    int      `json:"subscriptionDaysRemaining"`
	SubscriptionActive           bool     `json:"subscriptionActive"`
	IsAdmin                      bool     `json:"isAdmin"`
	TwoFAEnabled                 bool     `json:"twoFAEnabled"`
	EmailVerified                bool     `json:"emailVerified"`
	LastSeenAt                   *string  `json:"lastSeenAt"`
	TelegramLinked               bool     `json:"telegramLinked"`
	ScannerSlots                 []string `json:"scannerSlots"`
	CreatedAt                    string   `json:"createdAt"`
	UpdatedAt                    string   `json:"updatedAt"`
}

type LogEntryDTO struct {
	TS      string `json:"ts"`
	Level   string `json:"level"`
	Message string `json:"message"`
}

type AdminScannerRuleDTO struct {
	ID              int64           `json:"id"`
	ScannerSlot     string          `json:"scanner_slot"`
	Symbol          string          `json:"symbol"`
	WindowMinutes   int             `json:"window_minutes"`
	Enabled         bool            `json:"enabled"`
	CooldownSeconds int             `json:"cooldown_seconds"`
	Conditions      json.RawMessage `json:"conditions"`
	CreatedAt       string          `json:"created_at"`
	UpdatedAt       string          `json:"updated_at"`
}

func (a *API) handleListUsers(w http.ResponseWriter, r *http.Request) {
	rows, err := a.db.Query(r.Context(), `
		SELECT id::text, num_id, public_id, email, COALESCE(display_name, ''),
				COALESCE(last_ip, ''), plan, subscription_expires_at, subscription_frozen_at, COALESCE(subscription_frozen_days_remaining, 0), COALESCE(is_admin, false), two_fa_enabled,
			email_verified, last_seen_at, created_at, updated_at,
			telegram_id
		FROM users
		ORDER BY created_at DESC
	`)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "db_error")
		return
	}
	defer rows.Close()

	out := []AdminUserDTO{}
	for rows.Next() {
		var u AdminUserDTO
		var lastSeen pgtype.Timestamptz
		var subExpires pgtype.Timestamptz
		var subFrozenAt pgtype.Timestamptz
		var subFrozenDays int32
		var createdAt, updatedAt time.Time
		var telegramID *int64
		if err := rows.Scan(
			&u.ID,
			&u.NumID,
			&u.PublicID,
			&u.Email,
			&u.DisplayName,
			&u.LastIP,
			&u.Plan,
			&subExpires,
			&subFrozenAt,
			&subFrozenDays,
			&u.IsAdmin,
			&u.TwoFAEnabled,
			&u.EmailVerified,
			&lastSeen,
			&createdAt,
			&updatedAt,
			&telegramID,
		); err != nil {
			writeErr(w, http.StatusInternalServerError, "db_error")
			return
		}
		if lastSeen.Valid {
			v := lastSeen.Time.Format(time.RFC3339)
			u.LastSeenAt = &v
		}
		if subExpires.Valid {
			v := subExpires.Time.Format(time.RFC3339)
			u.SubscriptionExpiresAt = &v
		}
		if subFrozenAt.Valid {
			v := subFrozenAt.Time.Format(time.RFC3339)
			u.SubscriptionFrozenAt = &v
		}
		u.SubscriptionFrozenDaysRemain = int(subFrozenDays)
		u.SubscriptionDaysRemaining, u.SubscriptionActive = subscriptionSnapshot(subExpires, subFrozenAt, subFrozenDays, u.Plan)
		u.TelegramLinked = telegramID != nil

		// Активные слоты пользователя: считаем слот активным, если есть хотя бы 1 enabled правило.
		slots, err := a.userActiveScannerSlots(r.Context(), u.NumID)
		if err != nil {
			writeErr(w, http.StatusInternalServerError, "db_error")
			return
		}
		u.ScannerSlots = slots

		u.CreatedAt = createdAt.Format(time.RFC3339)
		u.UpdatedAt = updatedAt.Format(time.RFC3339)
		out = append(out, u)
	}
	if rows.Err() != nil {
		writeErr(w, http.StatusInternalServerError, "db_error")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"users": out})
}

func (a *API) handleListLogs(w http.ResponseWriter, r *http.Request) {
	limit := 200
	if s := r.URL.Query().Get("limit"); s != "" {
		if v, err := strconv.Atoi(s); err == nil {
			if v < 1 {
				limit = 1
			} else if v > 500 {
				limit = 500
			} else {
				limit = v
			}
		}
	}

	entries := a.logs.List(limit)
	out := make([]LogEntryDTO, 0, len(entries))
	for _, e := range entries {
		out = append(out, LogEntryDTO{TS: e.TS.Format(time.RFC3339), Level: e.Level, Message: e.Message})
	}

	writeJSON(w, http.StatusOK, map[string]any{"logs": out})
}

type UpdatePlanRequest struct {
	Plan string `json:"plan"`
}

type UpdatePlanResponse struct {
	UserID string `json:"userId"`
	Plan   string `json:"plan"`
}

func (a *API) handleUpdatePlan(w http.ResponseWriter, r *http.Request) {
	userID := chi.URLParam(r, "id")
	if userID == "" {
		writeErr(w, http.StatusBadRequest, "invalid_user")
		return
	}

	var payload UpdatePlanRequest
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid_body")
		return
	}
	plan := normalizePlan(payload.Plan)
	if plan == "" {
		writeErr(w, http.StatusBadRequest, "invalid_plan")
		return
	}

	var updatedPlan string
	err := a.db.QueryRow(r.Context(), `
		UPDATE users
		SET plan = $1,
			subscription_expires_at = CASE
				WHEN $1 IN ('standard', 'pro') AND subscription_expires_at IS NULL AND COALESCE(subscription_frozen_days_remaining, 0) = 0 THEN now() + interval '30 days'
				WHEN $1 = 'free' THEN NULL
				ELSE subscription_expires_at
			END,
			subscription_frozen_at = CASE WHEN $1 = 'free' THEN NULL ELSE subscription_frozen_at END,
			subscription_frozen_days_remaining = CASE WHEN $1 = 'free' THEN 0 ELSE COALESCE(subscription_frozen_days_remaining, 0) END,
			updated_at = now()
		WHERE id = $2
		RETURNING plan
	`, plan, userID).Scan(&updatedPlan)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "db_error")
		return
	}

	writeJSON(w, http.StatusOK, UpdatePlanResponse{UserID: userID, Plan: updatedPlan})
}

type UpdateSubscriptionRequest struct {
	Action string `json:"action"`
	Days   int    `json:"days"`
}

type UpdateSubscriptionResponse struct {
	UserID                          string  `json:"userId"`
	Plan                            string  `json:"plan"`
	EffectivePlan                   string  `json:"effectivePlan"`
	SubscriptionExpiresAt           *string `json:"subscriptionExpiresAt,omitempty"`
	SubscriptionFrozenAt            *string `json:"subscriptionFrozenAt,omitempty"`
	SubscriptionFrozenDaysRemaining int     `json:"subscriptionFrozenDaysRemaining"`
	SubscriptionDaysRemaining       int     `json:"subscriptionDaysRemaining"`
	SubscriptionActive              bool    `json:"subscriptionActive"`
}

func (a *API) handleUpdateSubscription(w http.ResponseWriter, r *http.Request) {
	userID := chi.URLParam(r, "id")
	if userID == "" {
		writeErr(w, http.StatusBadRequest, "invalid_user")
		return
	}

	var payload UpdateSubscriptionRequest
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid_body")
		return
	}
	action := strings.ToLower(strings.TrimSpace(payload.Action))
	if action != "add_days" && action != "subtract_days" && action != "freeze" && action != "unfreeze" {
		writeErr(w, http.StatusBadRequest, "invalid_action")
		return
	}
	if (action == "add_days" || action == "subtract_days") && payload.Days <= 0 {
		writeErr(w, http.StatusBadRequest, "days_required")
		return
	}

	tx, err := a.db.Begin(r.Context())
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "db_error")
		return
	}
	defer tx.Rollback(r.Context())

	var plan string
	var subExpires pgtype.Timestamptz
	var subFrozenAt pgtype.Timestamptz
	var subFrozenDays int32
	if err := tx.QueryRow(r.Context(), `
		SELECT plan, subscription_expires_at, subscription_frozen_at, COALESCE(subscription_frozen_days_remaining, 0)
		FROM users
		WHERE id = $1
		FOR UPDATE
	`, userID).Scan(&plan, &subExpires, &subFrozenAt, &subFrozenDays); err != nil {
		if err == pgx.ErrNoRows {
			writeErr(w, http.StatusNotFound, "user_not_found")
			return
		}
		writeErr(w, http.StatusInternalServerError, "db_error")
		return
	}

	now := time.Now()

	switch action {
	case "add_days":
		if subFrozenAt.Valid {
			subFrozenDays += int32(payload.Days)
		} else {
			base := now
			if subExpires.Valid && subExpires.Time.After(now) {
				base = subExpires.Time
			}
			subExpires = pgtype.Timestamptz{Time: base.AddDate(0, 0, payload.Days), Valid: true}
		}
	case "subtract_days":
		if subFrozenAt.Valid {
			if int(subFrozenDays) <= payload.Days {
				subFrozenDays = 0
			} else {
				subFrozenDays -= int32(payload.Days)
			}
		} else if subExpires.Valid {
			next := subExpires.Time.AddDate(0, 0, -payload.Days)
			if !next.After(now) {
				next = now
			}
			subExpires = pgtype.Timestamptz{Time: next, Valid: true}
		}
	case "freeze":
		if subFrozenAt.Valid {
			break
		}
		if !subExpires.Valid || !subExpires.Time.After(now) {
			writeErr(w, http.StatusBadRequest, "subscription_not_active")
			return
		}
		rem := int(subExpires.Time.Sub(now).Hours() / 24)
		if subExpires.Time.Sub(now).Hours()-float64(rem*24) > 0 {
			rem++
		}
		if rem < 1 {
			writeErr(w, http.StatusBadRequest, "subscription_not_active")
			return
		}
		subFrozenDays = int32(rem)
		subFrozenAt = pgtype.Timestamptz{Time: now, Valid: true}
		subExpires = pgtype.Timestamptz{}
	case "unfreeze":
		if !subFrozenAt.Valid {
			writeErr(w, http.StatusBadRequest, "subscription_not_frozen")
			return
		}
		if subFrozenDays > 0 {
			subExpires = pgtype.Timestamptz{Time: now.AddDate(0, 0, int(subFrozenDays)), Valid: true}
		}
		subFrozenAt = pgtype.Timestamptz{}
		subFrozenDays = 0
	}

	var expiresArg any = nil
	if subExpires.Valid {
		expiresArg = subExpires.Time
	}
	var frozenArg any = nil
	if subFrozenAt.Valid {
		frozenArg = subFrozenAt.Time
	}

	if _, err := tx.Exec(r.Context(), `
		UPDATE users
		SET subscription_expires_at = $1,
			subscription_frozen_at = $2,
			subscription_frozen_days_remaining = $3,
			updated_at = now()
		WHERE id = $4
	`, expiresArg, frozenArg, subFrozenDays, userID); err != nil {
		writeErr(w, http.StatusInternalServerError, "db_error")
		return
	}

	if err := tx.Commit(r.Context()); err != nil {
		writeErr(w, http.StatusInternalServerError, "db_error")
		return
	}

	resp := UpdateSubscriptionResponse{
		UserID:                          userID,
		Plan:                            plan,
		EffectivePlan:                   effectivePlanWithState(plan, subExpires, subFrozenAt),
		SubscriptionFrozenDaysRemaining: int(subFrozenDays),
	}
	if subExpires.Valid {
		v := subExpires.Time.Format(time.RFC3339)
		resp.SubscriptionExpiresAt = &v
	}
	if subFrozenAt.Valid {
		v := subFrozenAt.Time.Format(time.RFC3339)
		resp.SubscriptionFrozenAt = &v
	}
	resp.SubscriptionDaysRemaining, resp.SubscriptionActive = subscriptionSnapshot(subExpires, subFrozenAt, subFrozenDays, plan)

	writeJSON(w, http.StatusOK, resp)
}

func (a *API) handleDeleteUser(w http.ResponseWriter, r *http.Request) {
	userID := chi.URLParam(r, "id")
	if userID == "" {
		writeErr(w, http.StatusBadRequest, "invalid_user")
		return
	}

	res, err := a.db.Exec(r.Context(), `DELETE FROM users WHERE id = $1`, userID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "db_error")
		return
	}
	if res.RowsAffected() == 0 {
		writeErr(w, http.StatusNotFound, "user_not_found")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (a *API) handleListUserScannerRules(w http.ResponseWriter, r *http.Request) {
	userID := strings.TrimSpace(chi.URLParam(r, "id"))
	if userID == "" {
		writeErr(w, http.StatusBadRequest, "invalid_user")
		return
	}

	// Ищем пользователя и его num_id
	var numID int64
	err := a.db.QueryRow(r.Context(), `SELECT num_id FROM users WHERE id::text = $1`, userID).Scan(&numID)
	if err != nil {
		if err == pgx.ErrNoRows {
			writeErr(w, http.StatusNotFound, "user_not_found")
			return
		}
		writeErr(w, http.StatusInternalServerError, "db_error")
		return
	}

	rows, err := a.db.Query(r.Context(), `
		SELECT id, scanner_slot, symbol, window_minutes, enabled, cooldown_seconds,
				COALESCE(conditions, '[]'::jsonb),
				created_at, updated_at
		FROM alerts
		WHERE user_id = $1
		  AND enabled = true
		ORDER BY scanner_slot, id DESC
	`, numID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "db_error")
		return
	}
	defer rows.Close()

	out := make([]AdminScannerRuleDTO, 0)
	for rows.Next() {
		var dto AdminScannerRuleDTO
		var conds []byte
		var createdAt, updatedAt time.Time
		if err := rows.Scan(
			&dto.ID,
			&dto.ScannerSlot,
			&dto.Symbol,
			&dto.WindowMinutes,
			&dto.Enabled,
			&dto.CooldownSeconds,
			&conds,
			&createdAt,
			&updatedAt,
		); err != nil {
			writeErr(w, http.StatusInternalServerError, "db_error")
			return
		}
		dto.Conditions = json.RawMessage(conds)
		dto.CreatedAt = createdAt.Format(time.RFC3339)
		dto.UpdatedAt = updatedAt.Format(time.RFC3339)
		out = append(out, dto)
	}
	if rows.Err() != nil {
		writeErr(w, http.StatusInternalServerError, "db_error")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"rules": out})
}

// ---- Logs ----

type LogEntry struct {
	TS      time.Time
	Level   string
	Message string
}

type LogBuffer struct {
	mu      sync.Mutex
	entries []LogEntry
	max     int
}

func NewLogBuffer(max int) *LogBuffer {
	if max <= 0 {
		max = 500
	}
	return &LogBuffer{max: max}
}

func (b *LogBuffer) Add(level, message string) {
	if b == nil {
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	b.entries = append(b.entries, LogEntry{TS: time.Now().UTC(), Level: level, Message: message})
	if len(b.entries) > b.max {
		b.entries = b.entries[len(b.entries)-b.max:]
	}
}

func (b *LogBuffer) List(limit int) []LogEntry {
	if b == nil {
		return nil
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if limit <= 0 || limit > len(b.entries) {
		limit = len(b.entries)
	}
	start := len(b.entries) - limit
	if start < 0 {
		start = 0
	}
	out := make([]LogEntry, 0, limit)
	out = append(out, b.entries[start:]...)
	return out
}

// ---- helpers ----

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, status int, code string) {
	writeJSON(w, status, map[string]any{"error": code})
}

func normalizePlan(plan string) string {
	plan = strings.ToLower(strings.TrimSpace(plan))
	switch plan {
	case "standart":
		return "standard"
	case "free", "standard", "pro":
		return plan
	default:
		return ""
	}
}

func effectivePlanWithState(plan string, subExpires pgtype.Timestamptz, subFrozenAt pgtype.Timestamptz) string {
	p := normalizePlan(plan)
	if p == "" {
		p = "free"
	}
	if p == "standard" || p == "pro" {
		if subFrozenAt.Valid {
			return "free"
		}
		if !subExpires.Valid || !subExpires.Time.After(time.Now()) {
			return "free"
		}
		return p
	}
	return "free"
}

func subscriptionSnapshot(subExpires pgtype.Timestamptz, subFrozenAt pgtype.Timestamptz, subFrozenDays int32, plan string) (int, bool) {
	if subFrozenDays < 0 {
		subFrozenDays = 0
	}
	if subFrozenAt.Valid {
		return int(subFrozenDays), false
	}
	if subExpires.Valid && subExpires.Time.After(time.Now()) {
		remaining := int(math.Ceil(time.Until(subExpires.Time).Hours() / 24))
		if remaining < 0 {
			remaining = 0
		}
		active := remaining > 0 && (normalizePlan(plan) == "standard" || normalizePlan(plan) == "pro")
		return remaining, active
	}
	return 0, false
}

var _ = pgx.ErrNoRows

func (a *API) userActiveScannerSlots(ctx context.Context, userNumID int64) ([]string, error) {
	rows, err := a.db.Query(ctx, `
		SELECT DISTINCT scanner_slot
		FROM alerts
		WHERE user_id = $1
		  AND enabled = true
		ORDER BY scanner_slot
	`, userNumID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	slots := make([]string, 0, 3)
	for rows.Next() {
		var slot string
		if err := rows.Scan(&slot); err != nil {
			return nil, err
		}
		slots = append(slots, slot)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return slots, nil
}
