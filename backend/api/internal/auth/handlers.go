package auth

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/mail"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"golang.org/x/crypto/bcrypt"
)

type Handlers struct {
	Store            *Store
	CookieSecure     bool
	SessionTTL       time.Duration
	VerifyTTL        time.Duration
	Mailer           *Mailer
	BotUsername      string
	TurnstileSecret  string
	TurnstileEnabled bool
	TrustedProxyNets []*net.IPNet

	LoginLimiter              *rateLimiter
	RegisterLimiter           *rateLimiter
	VerifyEmailLimiter        *rateLimiter
	VerifyEmailAccountLimiter *rateLimiter
	MaxBodyBytes              int64

	loginFails          map[failKey]*failBucket
	loginFailsMu        sync.Mutex
	loginFailsLastSweep time.Time

	FrontendURL      string
	ResetTokenSecret string
}

func NewHandlers(store *Store, cookieSecure bool, mailer *Mailer, botUsername string, turnstileSecret string, turnstileEnabled bool) *Handlers {
	return &Handlers{
		Store:                     store,
		CookieSecure:              cookieSecure,
		SessionTTL:                14 * 24 * time.Hour,
		VerifyTTL:                 10 * time.Minute,
		Mailer:                    mailer,
		BotUsername:               botUsername,
		TurnstileSecret:           strings.TrimSpace(turnstileSecret),
		TurnstileEnabled:          turnstileEnabled,
		LoginLimiter:              newRateLimiter(10*time.Minute, 40),
		RegisterLimiter:           newRateLimiter(10*time.Minute, 10),
		VerifyEmailLimiter:        newRateLimiter(10*time.Minute, 20),
		VerifyEmailAccountLimiter: newRateLimiter(10*time.Minute, 8),
		MaxBodyBytes:              32 << 10,
		FrontendURL:               "",
		ResetTokenSecret:          "",
	}
}

type registerReq struct {
	Email        string `json:"email"`
	Password     string `json:"password"`
	CaptchaToken string `json:"captchaToken"`
}

type loginReq struct {
	Email        string `json:"email"`
	Password     string `json:"password"`
	CaptchaToken string `json:"captchaToken"`
}

type verifyEmailReq struct {
	Email string `json:"email"`
	Code  string `json:"code"`
}

type twoFACodeReq struct {
	Code string `json:"code"`
}

type twoFALoginReq struct {
	Email     string `json:"email"`
	Code      string `json:"code"`
	Challenge string `json:"challenge"`
}

type resendReq struct {
	Email string `json:"email"`
}

type profileUpdateReq struct {
	DisplayName     *string `json:"displayName"`
	AvatarURL       *string `json:"avatarUrl"`
	PrimaryExchange *string `json:"primaryExchange"`
}

type twoFAReq struct {
	Enabled  bool   `json:"enabled"`
	Password string `json:"password"`
	Code     string `json:"code"`
}

type watchlistReq struct {
	Hot  []string `json:"hot"`
	Cold []string `json:"cold"`
}

type telegramLinkResp struct {
	Token     string    `json:"token"`
	BotUser   string    `json:"botUsername"`
	ExpiresAt time.Time `json:"expiresAt"`
	DeepLink  string    `json:"deepLink"`
}

type telegramStatusResp struct {
	Linked   bool   `json:"linked"`
	Enabled  bool   `json:"enabled"`
	Username string `json:"username"`
}

type sessionView struct {
	ID        string    `json:"id"`
	UserAgent string    `json:"userAgent"`
	IP        string    `json:"ip"`
	CreatedAt time.Time `json:"createdAt"`
	ExpiresAt time.Time `json:"expiresAt"`
	IsActive  bool      `json:"isActive"`
	IsCurrent bool      `json:"isCurrent"`
}

func (h *Handlers) Register(w http.ResponseWriter, r *http.Request) {
	if !h.allow(h.RegisterLimiter, r) {
		writeErr(w, http.StatusTooManyRequests, "rate_limited")
		return
	}
	if h.MaxBodyBytes > 0 {
		r.Body = http.MaxBytesReader(w, r.Body, h.MaxBodyBytes)
	}
	var req registerReq
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid_json")
		return
	}
	if err := dec.Decode(&struct{}{}); err != io.EOF {
		writeErr(w, http.StatusBadRequest, "invalid_json")
		return
	}

	if err := h.verifyTurnstile(r.Context(), req.CaptchaToken, h.clientIP(r)); err != nil {
		writeErr(w, http.StatusBadRequest, "captcha_failed")
		return
	}

	email := normalizeEmail(req.Email)
	pass := req.Password
	if email == "" || !isValidEmail(email) || !isValidPassword(pass) {
		writeErr(w, http.StatusBadRequest, "invalid_input")
		return
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(pass), bcrypt.DefaultCost)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "hash_failed")
		return
	}

	u, err := h.Store.CreateUser(r.Context(), email, string(hash))
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			u2, _, _, err := h.Store.GetEmailVerificationByEmail(r.Context(), email)
			if err == nil && !u2.EmailVerified {
				if err := h.sendVerification(r.Context(), u2.ID, email); err != nil {
					log.Printf("register: resend verification failed for %s: %v", email, err)
					writeErr(w, http.StatusInternalServerError, "email_send_failed")
					return
				}
				writeJSON(w, http.StatusOK, map[string]any{"requiresVerification": true})
				return
			}
			writeErr(w, http.StatusBadRequest, "email_taken")
			return
		}
		writeErr(w, http.StatusBadRequest, "user_create_failed")
		return
	}

	if err := h.sendVerification(r.Context(), u.ID, email); err != nil {
		log.Printf("register: send verification failed for %s: %v", email, err)
		writeErr(w, http.StatusInternalServerError, "email_send_failed")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"requiresVerification": true})
}

func (h *Handlers) Login(w http.ResponseWriter, r *http.Request) {
	if !h.allow(h.LoginLimiter, r) {
		writeErr(w, http.StatusTooManyRequests, "rate_limited")
		return
	}
	if h.MaxBodyBytes > 0 {
		r.Body = http.MaxBytesReader(w, r.Body, h.MaxBodyBytes)
	}
	var req loginReq
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid_json")
		return
	}
	if err := dec.Decode(&struct{}{}); err != io.EOF {
		writeErr(w, http.StatusBadRequest, "invalid_json")
		return
	}

	ip := h.clientIP(r)
	if h.shouldRequireLoginCaptcha(ip, req.Email) {
		if err := h.verifyTurnstile(r.Context(), req.CaptchaToken, ip); err != nil {
			writeErr(w, http.StatusBadRequest, "captcha_failed")
			return
		}
	}

	email := normalizeEmail(req.Email)
	pass := req.Password
	if email == "" || pass == "" {
		writeErr(w, http.StatusBadRequest, "invalid_input")
		return
	}

	u, hash, err := h.Store.GetUserByEmail(r.Context(), email)
	if err != nil {
		h.recordLoginFail(ip, email)
		writeErr(w, http.StatusUnauthorized, "invalid_credentials")
		return
	}

	if err := bcrypt.CompareHashAndPassword([]byte(hash), []byte(pass)); err != nil {
		h.recordLoginFail(ip, email)
		writeErr(w, http.StatusUnauthorized, "invalid_credentials")
		return
	}

	h.clearLoginFails(ip, email)
	if !u.EmailVerified {
		writeErr(w, http.StatusForbidden, "email_not_verified")
		return
	}
	if u.TwoFAEnabled {
		if err := h.sendTwoFACode(r.Context(), u.ID, u.Email, "login"); err != nil {
			log.Printf("twofa login send failed user=%s err=%v", u.ID, err)
			writeErr(w, http.StatusInternalServerError, "twofa_send_failed")
			return
		}
		challenge, challengeErr := h.Store.CreateLoginChallenge(r.Context(), u.ID)
		if challengeErr != nil {
			writeErr(w, http.StatusInternalServerError, "twofa_challenge_failed")
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"requiresTwoFA": true, "challenge": challenge})
		return
	}

	ua := normalizeUserAgent(r.UserAgent())
	ip = h.clientIP(r)
	sid, expiresAt, err := h.Store.CreateSession(r.Context(), u.ID, h.SessionTTL, ua, ip)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "session_failed")
		return
	}

	setSessionCookie(w, sid, expiresAt, h.CookieSecure)
	writeJSON(w, http.StatusOK, map[string]any{"user": u})
}

func (h *Handlers) PasswordResetRequest(w http.ResponseWriter, r *http.Request) {
	if !h.allow(h.LoginLimiter, r) {
		writeErr(w, http.StatusTooManyRequests, "rate_limited")
		return
	}
	if h.MaxBodyBytes > 0 {
		r.Body = http.MaxBytesReader(w, r.Body, h.MaxBodyBytes)
	}
	var req passwordResetRequestReq
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid_json")
		return
	}
	if err := dec.Decode(&struct{}{}); err != io.EOF {
		writeErr(w, http.StatusBadRequest, "invalid_json")
		return
	}
	if err := h.verifyTurnstile(r.Context(), req.CaptchaToken, h.clientIP(r)); err != nil {
		writeErr(w, http.StatusBadRequest, "captcha_failed")
		return
	}

	email := normalizeEmail(req.Email)
	if email != "" && isValidEmail(email) && h.Mailer != nil {
		if u, _, err := h.Store.GetUserByEmail(r.Context(), email); err == nil {
			rawToken, tokenHash, genErr := generatePasswordResetToken(h.ResetTokenSecret)
			if genErr == nil {
				expiresAt := time.Now().Add(30 * time.Minute)
				if saveErr := h.Store.CreatePasswordResetToken(r.Context(), u.NumID, tokenHash, expiresAt); saveErr == nil {
					if resetLink, ok := h.buildPasswordResetLink(r, rawToken); ok {
						if err := h.Mailer.SendPasswordResetLink(u.Email, resetLink); err != nil {
							log.Printf("password reset mail send failed for %s: %v", u.Email, err)
						}
					} else {
						log.Printf("password reset link skipped: frontend URL is not configured")
					}
				} else {
					log.Printf("password reset token save failed for %s: %v", u.Email, saveErr)
				}
			} else {
				log.Printf("password reset token generate failed for %s: %v", u.Email, genErr)
			}
		}
	}

	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "message": "Если такой email существует, ссылка для восстановления отправлена."})
}

func (h *Handlers) PasswordResetConfirm(w http.ResponseWriter, r *http.Request) {
	if !h.allow(h.LoginLimiter, r) {
		writeErr(w, http.StatusTooManyRequests, "rate_limited")
		return
	}
	if h.MaxBodyBytes > 0 {
		r.Body = http.MaxBytesReader(w, r.Body, h.MaxBodyBytes)
	}
	var req passwordResetConfirmReq
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid_json")
		return
	}
	if err := dec.Decode(&struct{}{}); err != io.EOF {
		writeErr(w, http.StatusBadRequest, "invalid_json")
		return
	}
	if !isValidPassword(req.NewPassword) || strings.TrimSpace(req.Token) == "" {
		writeErr(w, http.StatusBadRequest, "invalid_input")
		return
	}

	tokenHash := hashPasswordResetToken(h.ResetTokenSecret, req.Token)
	userNumID, err := h.Store.ConsumePasswordResetToken(r.Context(), tokenHash)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "reset_link_invalid")
		return
	}

	user, err := h.Store.GetUserByNumID(r.Context(), userNumID)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "reset_link_invalid")
		return
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(req.NewPassword), bcrypt.DefaultCost)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "hash_failed")
		return
	}
	if err := h.Store.UpdatePassword(r.Context(), user.ID, string(hash)); err != nil {
		writeErr(w, http.StatusInternalServerError, "password_update_failed")
		return
	}
	_ = h.Store.DeleteSessionsByUserNumID(r.Context(), userNumID)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (h *Handlers) RequestTwoFAEnable(w http.ResponseWriter, r *http.Request) {
	if !h.allow(h.LoginLimiter, r) {
		writeErr(w, http.StatusTooManyRequests, "rate_limited")
		return
	}
	sid, ok := getSID(r)
	if !ok {
		writeErr(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	u, err := h.Store.GetUserBySessionID(r.Context(), sid)
	if err != nil {
		writeErr(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	if !u.EmailVerified {
		writeErr(w, http.StatusForbidden, "email_not_verified")
		return
	}
	if err := h.sendTwoFACode(r.Context(), u.ID, u.Email, "enable"); err != nil {
		log.Printf("twofa enable send failed user=%s err=%v", u.ID, err)
		writeErr(w, http.StatusInternalServerError, "twofa_send_failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (h *Handlers) ConfirmTwoFAEnable(w http.ResponseWriter, r *http.Request) {
	if !h.allow(h.LoginLimiter, r) {
		writeErr(w, http.StatusTooManyRequests, "rate_limited")
		return
	}
	sid, ok := getSID(r)
	if !ok {
		writeErr(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	u, err := h.Store.GetUserBySessionID(r.Context(), sid)
	if err != nil {
		writeErr(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	if h.MaxBodyBytes > 0 {
		r.Body = http.MaxBytesReader(w, r.Body, h.MaxBodyBytes)
	}
	var req twoFACodeReq
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid_json")
		return
	}
	if err := dec.Decode(&struct{}{}); err != io.EOF {
		writeErr(w, http.StatusBadRequest, "invalid_json")
		return
	}
	code := strings.TrimSpace(req.Code)
	if code == "" {
		writeErr(w, http.StatusBadRequest, "invalid_input")
		return
	}

	hash, expiresAt, err := h.Store.GetTwoFACode(r.Context(), u.ID, "enable")
	if err != nil {
		writeErr(w, http.StatusUnauthorized, "invalid_code")
		return
	}
	if expiresAt == nil || time.Now().After(*expiresAt) {
		writeErr(w, http.StatusBadRequest, "code_expired")
		return
	}
	if !h.matchVerificationCode(u.Email, code, hash) {
		writeErr(w, http.StatusUnauthorized, "invalid_code")
		return
	}
	_ = h.Store.ClearTwoFACode(r.Context(), u.ID, "enable")
	updated, err := h.Store.UpdateTwoFA(r.Context(), u.ID, true)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "twofa_update_failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"user": updated})
}

func (h *Handlers) VerifyTwoFALogin(w http.ResponseWriter, r *http.Request) {
	if !h.allow(h.LoginLimiter, r) {
		writeErr(w, http.StatusTooManyRequests, "rate_limited")
		return
	}
	if h.MaxBodyBytes > 0 {
		r.Body = http.MaxBytesReader(w, r.Body, h.MaxBodyBytes)
	}
	var req twoFALoginReq
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid_json")
		return
	}
	if err := dec.Decode(&struct{}{}); err != io.EOF {
		writeErr(w, http.StatusBadRequest, "invalid_json")
		return
	}

	email := normalizeEmail(req.Email)
	code := strings.TrimSpace(req.Code)
	challenge := strings.TrimSpace(req.Challenge)
	if email == "" || code == "" || challenge == "" {
		writeErr(w, http.StatusBadRequest, "invalid_input")
		return
	}

	u, _, err := h.Store.GetUserByEmail(r.Context(), email)
	if err != nil {
		writeErr(w, http.StatusUnauthorized, "invalid_code")
		return
	}
	if !u.TwoFAEnabled {
		writeErr(w, http.StatusBadRequest, "twofa_disabled")
		return
	}

	hash, expiresAt, err := h.Store.GetTwoFACode(r.Context(), u.ID, "login")
	if err != nil {
		writeErr(w, http.StatusUnauthorized, "invalid_code")
		return
	}
	if expiresAt == nil || time.Now().After(*expiresAt) {
		writeErr(w, http.StatusBadRequest, "code_expired")
		return
	}
	if !h.matchVerificationCode(u.Email, code, hash) {
		writeErr(w, http.StatusUnauthorized, "invalid_code")
		return
	}
	if err := h.Store.ConsumeLoginChallenge(r.Context(), challenge, u.ID); err != nil {
		writeErr(w, http.StatusUnauthorized, "invalid_challenge")
		return
	}
	_ = h.Store.ClearTwoFACode(r.Context(), u.ID, "login")

	ua := normalizeUserAgent(r.UserAgent())
	ip := h.clientIP(r)
	sid, expiresAtSession, err := h.Store.CreateSession(r.Context(), u.ID, h.SessionTTL, ua, ip)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "session_failed")
		return
	}
	setSessionCookie(w, sid, expiresAtSession, h.CookieSecure)
	writeJSON(w, http.StatusOK, map[string]any{"user": u})
}

func (h *Handlers) Logout(w http.ResponseWriter, r *http.Request) {
	sid, ok := getSID(r)
	if ok {
		_ = h.Store.DeleteSession(r.Context(), sid)
	}

	clearSessionCookie(w, h.CookieSecure)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (h *Handlers) Me(w http.ResponseWriter, r *http.Request) {
	sid, ok := getSID(r)
	if !ok {
		writeErr(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	u, err := h.Store.GetUserBySessionID(r.Context(), sid)
	if err != nil {
		writeErr(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"user": u})
}

func (h *Handlers) Ping(w http.ResponseWriter, r *http.Request) {
	sid, ok := getSID(r)
	if !ok {
		writeErr(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	u, err := h.Store.GetUserBySessionID(r.Context(), sid)
	if err != nil {
		writeErr(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	_ = h.Store.TouchUser(r.Context(), u.ID)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (h *Handlers) UpdateProfile(w http.ResponseWriter, r *http.Request) {
	if !h.allow(h.LoginLimiter, r) {
		writeErr(w, http.StatusTooManyRequests, "rate_limited")
		return
	}
	sid, ok := getSID(r)
	if !ok {
		writeErr(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	u, err := h.Store.GetUserBySessionID(r.Context(), sid)
	if err != nil {
		writeErr(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	const maxProfileBytes = 2 << 20
	if h.MaxBodyBytes > 0 {
		limit := h.MaxBodyBytes
		if maxProfileBytes > limit {
			limit = maxProfileBytes
		}
		r.Body = http.MaxBytesReader(w, r.Body, limit)
	}
	var req profileUpdateReq
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid_json")
		return
	}
	if err := dec.Decode(&struct{}{}); err != io.EOF {
		writeErr(w, http.StatusBadRequest, "invalid_json")
		return
	}

	if req.DisplayName != nil {
		v := strings.TrimSpace(*req.DisplayName)
		if v != "" && (len(v) < 2 || len(v) > 40) {
			writeErr(w, http.StatusBadRequest, "invalid_display_name")
			return
		}
		if v != "" && containsHTML(v) {
			writeErr(w, http.StatusBadRequest, "invalid_display_name")
			return
		}
		req.DisplayName = &v
	}
	if req.AvatarURL != nil {
		v := strings.TrimSpace(*req.AvatarURL)
		if v != "" && !isValidAvatarURL(v) {
			writeErr(w, http.StatusBadRequest, "invalid_avatar")
			return
		}
		req.AvatarURL = &v
	}
	if req.PrimaryExchange != nil {
		v := strings.ToLower(strings.TrimSpace(*req.PrimaryExchange))
		if v != "bybit" && v != "binance" {
			writeErr(w, http.StatusBadRequest, "invalid_primary_exchange")
			return
		}
		req.PrimaryExchange = &v
	}

	updated, err := h.Store.UpdateProfile(r.Context(), u.ID, req.DisplayName, req.AvatarURL, req.PrimaryExchange)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "profile_update_failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"user": updated})
}

func (h *Handlers) RequestTwoFADisable(w http.ResponseWriter, r *http.Request) {
	if !h.allow(h.LoginLimiter, r) {
		writeErr(w, http.StatusTooManyRequests, "rate_limited")
		return
	}
	sid, ok := getSID(r)
	if !ok {
		writeErr(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	u, err := h.Store.GetUserBySessionID(r.Context(), sid)
	if err != nil {
		writeErr(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	if !u.TwoFAEnabled {
		writeErr(w, http.StatusBadRequest, "twofa_disabled")
		return
	}
	if err := h.sendTwoFACode(r.Context(), u.ID, u.Email, "disable"); err != nil {
		writeErr(w, http.StatusInternalServerError, "twofa_send_failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (h *Handlers) UpdateTwoFA(w http.ResponseWriter, r *http.Request) {
	if !h.allow(h.LoginLimiter, r) {
		writeErr(w, http.StatusTooManyRequests, "rate_limited")
		return
	}
	sid, ok := getSID(r)
	if !ok {
		writeErr(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	u, err := h.Store.GetUserBySessionID(r.Context(), sid)
	if err != nil {
		writeErr(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	if h.MaxBodyBytes > 0 {
		r.Body = http.MaxBytesReader(w, r.Body, h.MaxBodyBytes)
	}
	var req twoFAReq
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid_json")
		return
	}
	if err := dec.Decode(&struct{}{}); err != io.EOF {
		writeErr(w, http.StatusBadRequest, "invalid_json")
		return
	}

	if req.Enabled {
		writeErr(w, http.StatusBadRequest, "twofa_enable_requires_code")
		return
	}
	if strings.TrimSpace(req.Password) == "" || strings.TrimSpace(req.Code) == "" {
		writeErr(w, http.StatusBadRequest, "password_and_code_required")
		return
	}
	_, passwordHash, err := h.Store.GetUserByEmail(r.Context(), u.Email)
	if err != nil || bcrypt.CompareHashAndPassword([]byte(passwordHash), []byte(req.Password)) != nil {
		writeErr(w, http.StatusUnauthorized, "invalid_credentials")
		return
	}
	codeHash, expiresAt, err := h.Store.GetTwoFACode(r.Context(), u.ID, "disable")
	if err != nil || expiresAt == nil || time.Now().After(*expiresAt) || !h.matchVerificationCode(u.Email, req.Code, codeHash) {
		writeErr(w, http.StatusUnauthorized, "invalid_code")
		return
	}
	_ = h.Store.ClearTwoFACode(r.Context(), u.ID, "disable")
	updated, err := h.Store.UpdateTwoFA(r.Context(), u.ID, req.Enabled)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "twofa_update_failed")
		return
	}
	_ = h.Store.ClearTwoFACode(r.Context(), u.ID, "login")
	_ = h.Store.ClearTwoFACode(r.Context(), u.ID, "enable")
	writeJSON(w, http.StatusOK, map[string]any{"user": updated})
}

func (h *Handlers) VerifyEmail(w http.ResponseWriter, r *http.Request) {
	if h.MaxBodyBytes > 0 {
		r.Body = http.MaxBytesReader(w, r.Body, h.MaxBodyBytes)
	}
	var req verifyEmailReq
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid_json")
		return
	}
	if err := dec.Decode(&struct{}{}); err != io.EOF {
		writeErr(w, http.StatusBadRequest, "invalid_json")
		return
	}

	email := normalizeEmail(req.Email)
	code := strings.TrimSpace(req.Code)
	if email == "" || code == "" {
		writeErr(w, http.StatusBadRequest, "invalid_input")
		return
	}

	if !h.allow(h.VerifyEmailLimiter, r) || !h.VerifyEmailAccountLimiter.Allow(email) {
		writeErr(w, http.StatusTooManyRequests, "rate_limited")
		return
	}

	u, hash, expiresAt, err := h.Store.GetEmailVerificationByEmail(r.Context(), email)
	if err != nil {
		writeErr(w, http.StatusUnauthorized, "invalid_code")
		return
	}
	if u.EmailVerified {
		writeErr(w, http.StatusBadRequest, "already_verified")
		return
	}
	if expiresAt == nil || time.Now().After(*expiresAt) {
		writeErr(w, http.StatusBadRequest, "code_expired")
		return
	}
	if !h.matchVerificationCode(email, code, hash) {
		writeErr(w, http.StatusUnauthorized, "invalid_code")
		return
	}

	if err := h.Store.MarkEmailVerified(r.Context(), u.ID); err != nil {
		writeErr(w, http.StatusInternalServerError, "verify_failed")
		return
	}
	u.EmailVerified = true

	ua := normalizeUserAgent(r.UserAgent())
	ip := h.clientIP(r)
	sid, expires, err := h.Store.CreateSession(r.Context(), u.ID, h.SessionTTL, ua, ip)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "session_failed")
		return
	}
	setSessionCookie(w, sid, expires, h.CookieSecure)
	writeJSON(w, http.StatusOK, map[string]any{"user": u})
}

func (h *Handlers) ResendVerification(w http.ResponseWriter, r *http.Request) {
	if !h.allow(h.RegisterLimiter, r) {
		writeErr(w, http.StatusTooManyRequests, "rate_limited")
		return
	}
	if h.MaxBodyBytes > 0 {
		r.Body = http.MaxBytesReader(w, r.Body, h.MaxBodyBytes)
	}
	var req resendReq
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid_json")
		return
	}
	if err := dec.Decode(&struct{}{}); err != io.EOF {
		writeErr(w, http.StatusBadRequest, "invalid_json")
		return
	}

	email := normalizeEmail(req.Email)
	if email == "" {
		writeErr(w, http.StatusBadRequest, "invalid_input")
		return
	}
	u, _, _, err := h.Store.GetEmailVerificationByEmail(r.Context(), email)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "invalid_input")
		return
	}
	if u.EmailVerified {
		writeErr(w, http.StatusBadRequest, "already_verified")
		return
	}
	if err := h.sendVerification(r.Context(), u.ID, email); err != nil {
		log.Printf("resend: send verification failed for %s: %v", email, err)
		writeErr(w, http.StatusInternalServerError, "email_send_failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (h *Handlers) TelegramStatus(w http.ResponseWriter, r *http.Request) {
	sid, ok := getSID(r)
	if !ok {
		writeErr(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	user, err := h.Store.GetUserBySessionID(r.Context(), sid)
	if err != nil {
		writeErr(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	telegramID, username, enabled, err := h.Store.GetTelegramStatus(r.Context(), user.ID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "telegram_status_failed")
		return
	}
	writeJSON(w, http.StatusOK, telegramStatusResp{
		Linked:   telegramID != 0,
		Enabled:  enabled,
		Username: username,
	})
}

func (h *Handlers) TelegramLink(w http.ResponseWriter, r *http.Request) {
	sid, ok := getSID(r)
	if !ok {
		writeErr(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	user, err := h.Store.GetUserBySessionID(r.Context(), sid)
	if err != nil {
		writeErr(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	if strings.TrimSpace(h.BotUsername) == "" {
		writeErr(w, http.StatusBadRequest, "telegram_not_configured")
		return
	}
	code, err := generateLinkToken()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "token_failed")
		return
	}
	expires := time.Now().Add(10 * time.Minute)
	if err := h.Store.CreateTelegramLinkToken(r.Context(), user.ID, code, expires); err != nil {
		writeErr(w, http.StatusInternalServerError, "token_failed")
		return
	}
	deepLink := "https://t.me/" + strings.TrimPrefix(h.BotUsername, "@") + "?start=" + code
	writeJSON(w, http.StatusOK, telegramLinkResp{
		Token:     code,
		BotUser:   h.BotUsername,
		ExpiresAt: expires,
		DeepLink:  deepLink,
	})
}

func (h *Handlers) TelegramToggle(w http.ResponseWriter, r *http.Request) {
	sid, ok := getSID(r)
	if !ok {
		writeErr(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	user, err := h.Store.GetUserBySessionID(r.Context(), sid)
	if err != nil {
		writeErr(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	var req struct {
		Enabled bool `json:"enabled"`
	}
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid_json")
		return
	}
	telegramID, _, _, err := h.Store.GetTelegramStatus(r.Context(), user.ID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "telegram_status_failed")
		return
	}
	if telegramID == 0 {
		writeErr(w, http.StatusBadRequest, "telegram_not_linked")
		return
	}
	if err := h.Store.UpdateTelegramEnabled(r.Context(), user.ID, req.Enabled); err != nil {
		writeErr(w, http.StatusInternalServerError, "telegram_toggle_failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"enabled": req.Enabled})
}

func (h *Handlers) TelegramUnlink(w http.ResponseWriter, r *http.Request) {
	sid, ok := getSID(r)
	if !ok {
		writeErr(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	user, err := h.Store.GetUserBySessionID(r.Context(), sid)
	if err != nil {
		writeErr(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	if err := h.Store.ClearTelegramLink(r.Context(), user.ID); err != nil {
		writeErr(w, http.StatusInternalServerError, "telegram_unlink_failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (h *Handlers) Watchlist(w http.ResponseWriter, r *http.Request) {
	sid, ok := getSID(r)
	if !ok {
		writeErr(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	user, err := h.Store.GetUserBySessionID(r.Context(), sid)
	if err != nil {
		writeErr(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	hot, cold, err := h.Store.GetWatchlists(r.Context(), user.ID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "watchlist_failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"hot": hot, "cold": cold})
}

func (h *Handlers) UpdateWatchlist(w http.ResponseWriter, r *http.Request) {
	sid, ok := getSID(r)
	if !ok {
		writeErr(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	user, err := h.Store.GetUserBySessionID(r.Context(), sid)
	if err != nil {
		writeErr(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	if h.MaxBodyBytes > 0 {
		r.Body = http.MaxBytesReader(w, r.Body, h.MaxBodyBytes)
	}
	var req watchlistReq
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid_json")
		return
	}
	if err := dec.Decode(&struct{}{}); err != io.EOF {
		writeErr(w, http.StatusBadRequest, "invalid_json")
		return
	}
	hot, err := normalizeWatchlistSymbols(req.Hot)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	cold, err := normalizeWatchlistSymbols(req.Cold)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := h.Store.UpdateWatchlists(r.Context(), user.ID, hot, cold); err != nil {
		writeErr(w, http.StatusInternalServerError, "watchlist_failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"hot": hot, "cold": cold})
}

func (h *Handlers) Sessions(w http.ResponseWriter, r *http.Request) {
	sid, ok := getSID(r)
	if !ok {
		writeErr(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	user, err := h.Store.GetUserBySessionID(r.Context(), sid)
	if err != nil {
		writeErr(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	currentID, err := h.Store.GetSessionPublicID(r.Context(), sid)
	if err != nil {
		writeErr(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	sessions, err := h.Store.ListSessionsForUser(r.Context(), user.ID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "sessions_failed")
		return
	}
	views := make([]sessionView, 0, len(sessions))
	for _, s := range sessions {
		views = append(views, sessionView{
			ID:        s.ID,
			UserAgent: s.UserAgent,
			IP:        s.IP,
			CreatedAt: s.CreatedAt,
			ExpiresAt: s.ExpiresAt,
			IsActive:  s.IsActive,
			IsCurrent: s.ID == currentID,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"sessions": views})
}

func getSID(r *http.Request) (string, bool) {
	names := []string{"sid", "__Host-sid"}
	if requestIsSecure(r) {
		names[0], names[1] = names[1], names[0]
	}
	for _, name := range names {
		c, err := r.Cookie(name)
		if err == nil && c.Value != "" {
			return c.Value, true
		}
	}
	return "", false
}

func requestIsSecure(r *http.Request) bool {
	if r.TLS != nil {
		return true
	}
	forwardedProto := strings.TrimSpace(strings.Split(r.Header.Get("X-Forwarded-Proto"), ",")[0])
	return strings.EqualFold(forwardedProto, "https")
}

func sessionCookieName(secure bool) string {
	if secure {
		return "__Host-sid"
	}
	return "sid"
}

func normalizeUserAgent(ua string) string {
	ua = strings.TrimSpace(ua)
	if len(ua) > 512 {
		ua = ua[:512]
	}
	return ua
}

func setSessionCookie(w http.ResponseWriter, sid string, expiresAt time.Time, secure bool) {
	if !secure {
		expireSessionCookie(w, "__Host-sid", true)
	}
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName(secure),
		Value:    sid,
		Path:     "/",
		Expires:  expiresAt,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   secure,
	})
}

func clearSessionCookie(w http.ResponseWriter, secure bool) {
	expireSessionCookie(w, "sid", false)
	expireSessionCookie(w, "__Host-sid", true)
}

func expireSessionCookie(w http.ResponseWriter, name string, secure bool) {
	http.SetCookie(w, &http.Cookie{
		Name:     name,
		Value:    "",
		Path:     "/",
		Expires:  time.Unix(0, 0),
		MaxAge:   -1,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   secure,
	})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, status int, code string) {
	writeJSON(w, status, map[string]any{"error": code})
}

func normalizeEmail(email string) string {
	return strings.TrimSpace(strings.ToLower(email))
}

func normalizeWatchlistSymbols(input []string) ([]string, error) {
	if len(input) == 0 {
		return []string{}, nil
	}
	re := regexp.MustCompile(`^[A-Z0-9]{3,30}$`)
	seen := make(map[string]bool, len(input))
	list := make([]string, 0, len(input))
	for _, raw := range input {
		value := strings.TrimSpace(raw)
		if value == "" {
			continue
		}
		value = strings.ToUpper(value)
		value = strings.ReplaceAll(value, "/", "")
		value = strings.ReplaceAll(value, "-", "")
		if value == "" {
			continue
		}
		if !strings.HasSuffix(value, "USDT") {
			value += "USDT"
		}
		if !re.MatchString(value) {
			return nil, errors.New("invalid_symbol")
		}
		if seen[value] {
			continue
		}
		if len(list) >= 200 {
			return nil, errors.New("watchlist_too_large")
		}
		seen[value] = true
		list = append(list, value)
	}
	return list, nil
}

func generateLinkToken() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

func isValidEmail(email string) bool {
	if len(email) > 254 {
		return false
	}
	_, err := mail.ParseAddress(email)
	return err == nil
}

func isValidPassword(pass string) bool {
	if len(pass) < 8 || len(pass) > 128 {
		return false
	}
	var hasUpper, hasLower, hasDigit bool
	for _, r := range pass {
		switch {
		case r >= '0' && r <= '9':
			hasDigit = true
		case r >= 'A' && r <= 'Z':
			hasUpper = true
		case r >= 'a' && r <= 'z':
			hasLower = true
		}
	}
	return hasUpper && hasLower && hasDigit
}

func containsHTML(v string) bool {
	return strings.ContainsAny(v, "<>")
}

func isValidAvatarURL(v string) bool {
	if strings.HasPrefix(v, "data:image/") {
		return len(v) <= 2_000_000
	}
	if len(v) > 2048 {
		return false
	}
	u, err := url.Parse(v)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return false
	}
	return u.Scheme == "http" || u.Scheme == "https"
}

func (h *Handlers) sendVerification(ctx context.Context, userID, email string) error {
	if h.Mailer == nil {
		return errors.New("mailer_not_configured")
	}
	code, err := GenerateNumericCode()
	if err != nil {
		return err
	}
	hash := h.hashVerificationCode(email, code)
	expires := time.Now().Add(h.VerifyTTL)
	if err := h.Store.SetEmailVerification(ctx, userID, hash, expires); err != nil {
		return err
	}
	return h.Mailer.SendVerification(email, code)
}

func (h *Handlers) sendTwoFACode(ctx context.Context, userID, email, purpose string) error {
	if h.Mailer == nil {
		return errors.New("mailer_not_configured")
	}
	code, err := GenerateNumericCode()
	if err != nil {
		return err
	}
	hash := h.hashVerificationCode(email, code)
	expires := time.Now().Add(h.VerifyTTL)
	if err := h.Store.SetTwoFACode(ctx, userID, purpose, hash, expires); err != nil {
		return err
	}
	return h.Mailer.SendTwoFACode(email, code)
}

func (h *Handlers) hashVerificationCode(email, code string) string {
	msg := normalizeEmail(email) + ":" + strings.TrimSpace(code)
	sum := sha256.Sum256([]byte(msg))
	return hex.EncodeToString(sum[:])
}

func (h *Handlers) matchVerificationCode(email, code, expectedHash string) bool {
	if expectedHash == "" {
		return false
	}
	got := h.hashVerificationCode(email, code)
	return subtle.ConstantTimeCompare([]byte(got), []byte(expectedHash)) == 1
}

type rateLimiter struct {
	mu        sync.Mutex
	window    time.Duration
	max       int
	buckets   map[string]*rateBucket
	lastSweep time.Time
}

type rateBucket struct {
	count int
	reset time.Time
}

const (
	rateLimiterSweepInterval = time.Minute
	maxRateLimiterBuckets    = 10000
	maxLoginFailBuckets      = 10000
)

func newRateLimiter(window time.Duration, max int) *rateLimiter {
	return &rateLimiter{window: window, max: max, buckets: make(map[string]*rateBucket)}
}

func (l *rateLimiter) cleanupExpiredLocked(now time.Time) {
	if !l.lastSweep.IsZero() && now.Sub(l.lastSweep) < rateLimiterSweepInterval && len(l.buckets) < maxRateLimiterBuckets {
		return
	}
	for key, bucket := range l.buckets {
		if bucket == nil || !now.Before(bucket.reset) {
			delete(l.buckets, key)
		}
	}
	l.lastSweep = now
}

func (l *rateLimiter) Allow(key string) bool {
	if l == nil {
		return true
	}
	now := time.Now()
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.buckets == nil {
		l.buckets = make(map[string]*rateBucket)
	}
	l.cleanupExpiredLocked(now)
	if b, ok := l.buckets[key]; ok {
		if !now.Before(b.reset) {
			b.count = 1
			b.reset = now.Add(l.window)
			return true
		}
		if b.count >= l.max {
			return false
		}
		b.count++
		return true
	}
	if len(l.buckets) >= maxRateLimiterBuckets {
		return false
	}
	l.buckets[key] = &rateBucket{count: 1, reset: now.Add(l.window)}
	return true
}

func (h *Handlers) allow(l *rateLimiter, r *http.Request) bool {
	if l == nil {
		return true
	}
	return l.Allow(h.clientIP(r))
}

func (h *Handlers) SetTrustedProxyCIDRs(value string) error {
	networks := make([]*net.IPNet, 0)
	for _, raw := range strings.Split(value, ",") {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			continue
		}
		if ip := net.ParseIP(raw); ip != nil {
			bits := 128
			if ip.To4() != nil {
				bits = 32
			}
			networks = append(networks, &net.IPNet{IP: ip, Mask: net.CIDRMask(bits, bits)})
			continue
		}
		_, network, err := net.ParseCIDR(raw)
		if err != nil {
			return fmt.Errorf("invalid trusted proxy CIDR %q: %w", raw, err)
		}
		networks = append(networks, network)
	}
	h.TrustedProxyNets = networks
	return nil
}

func remoteRequestIP(r *http.Request) net.IP {
	host, _, err := net.SplitHostPort(strings.TrimSpace(r.RemoteAddr))
	if err == nil {
		return net.ParseIP(strings.Trim(host, "[]"))
	}
	return net.ParseIP(strings.Trim(strings.TrimSpace(r.RemoteAddr), "[]"))
}

func isTrustedProxy(ip net.IP, networks []*net.IPNet) bool {
	if ip == nil {
		return false
	}
	for _, network := range networks {
		if network != nil && network.Contains(ip) {
			return true
		}
	}
	return false
}

func clientIP(r *http.Request, trusted []*net.IPNet) string {
	remote := remoteRequestIP(r)
	if remote == nil {
		return strings.TrimSpace(r.RemoteAddr)
	}
	if !isTrustedProxy(remote, trusted) {
		return remote.String()
	}

	parts := strings.Split(r.Header.Get("X-Forwarded-For"), ",")
	for i := len(parts) - 1; i >= 0; i-- {
		ip := net.ParseIP(strings.TrimSpace(parts[i]))
		if ip != nil && !isTrustedProxy(ip, trusted) {
			return ip.String()
		}
	}
	if ip := net.ParseIP(strings.TrimSpace(r.Header.Get("X-Real-IP"))); ip != nil {
		return ip.String()
	}
	return remote.String()
}

func (h *Handlers) clientIP(r *http.Request) string {
	if h == nil {
		return clientIP(r, nil)
	}
	return clientIP(r, h.TrustedProxyNets)
}

// ClientIP is safe for callers without proxy configuration: forwarded headers
// are ignored. Handlers use their configured trusted proxy list instead.
func ClientIP(r *http.Request) string {
	return clientIP(r, nil)
}

var ErrUnauthorized = errors.New("unauthorized")

func (s *Store) Authenticate(r *http.Request) (User, error) {
	sid, ok := getSID(r)
	if !ok {
		return User{}, ErrUnauthorized
	}
	u, err := s.GetUserBySessionID(r.Context(), sid)
	if err != nil {
		return User{}, ErrUnauthorized
	}
	return u, nil
}

type turnstileVerifyResp struct {
	Success     bool     `json:"success"`
	ChallengeTS string   `json:"challenge_ts"`
	Hostname    string   `json:"hostname"`
	ErrorCodes  []string `json:"error-codes"`
}

func (h *Handlers) verifyTurnstile(ctx context.Context, token string, remoteIP string) error {
	if !h.TurnstileEnabled {
		return nil
	}
	secret := strings.TrimSpace(h.TurnstileSecret)
	if secret == "" {
		return errors.New("turnstile_secret_missing")
	}
	tok := strings.TrimSpace(token)
	if tok == "" {
		return errors.New("captcha_required")
	}

	form := url.Values{}
	form.Set("secret", secret)
	form.Set("response", tok)
	if strings.TrimSpace(remoteIP) != "" {
		form.Set("remoteip", remoteIP)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://challenges.cloudflare.com/turnstile/v0/siteverify", strings.NewReader(form.Encode()))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	client := &http.Client{Timeout: 6 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode >= 300 {
		return fmt.Errorf("turnstile_http_%d", resp.StatusCode)
	}

	var vr turnstileVerifyResp
	if err := json.Unmarshal(body, &vr); err != nil {
		return err
	}
	if !vr.Success {
		return errors.New("captcha_failed")
	}
	return nil
}

// --- login failed attempts (for requiring captcha) ---

type failKey struct {
	IP    string
	Email string
}

type failBucket struct {
	Count int
	Reset time.Time
}

func (h *Handlers) cleanupLoginFailsLocked(now time.Time) {
	if !h.loginFailsLastSweep.IsZero() && now.Sub(h.loginFailsLastSweep) < rateLimiterSweepInterval && len(h.loginFails) < maxLoginFailBuckets {
		return
	}
	for key, bucket := range h.loginFails {
		if bucket == nil || !now.Before(bucket.Reset) {
			delete(h.loginFails, key)
		}
	}
	h.loginFailsLastSweep = now
}

func (h *Handlers) shouldRequireLoginCaptcha(ip, email string) bool {
	// Require captcha after several failed attempts per (ip,email)
	const threshold = 3
	if h == nil {
		return false
	}
	if ip == "" {
		return false
	}
	email = normalizeEmail(email)
	key := failKey{IP: ip, Email: email}

	h.loginFailsMu.Lock()
	defer h.loginFailsMu.Unlock()

	now := time.Now()
	h.cleanupLoginFailsLocked(now)
	b := h.loginFails[key]
	if b == nil || !now.Before(b.Reset) {
		return false
	}
	return b.Count >= threshold
}

func (h *Handlers) recordLoginFail(ip, email string) {
	const window = 10 * time.Minute
	if ip == "" {
		return
	}
	email = normalizeEmail(email)
	key := failKey{IP: ip, Email: email}
	now := time.Now()

	h.loginFailsMu.Lock()
	defer h.loginFailsMu.Unlock()

	if h.loginFails == nil {
		h.loginFails = make(map[failKey]*failBucket)
	}
	h.cleanupLoginFailsLocked(now)
	b := h.loginFails[key]
	if b == nil || !now.Before(b.Reset) {
		if len(h.loginFails) >= maxLoginFailBuckets {
			return
		}
		h.loginFails[key] = &failBucket{Count: 1, Reset: now.Add(window)}
		return
	}
	b.Count++
}

func (h *Handlers) clearLoginFails(ip, email string) {
	if ip == "" {
		return
	}
	email = normalizeEmail(email)
	key := failKey{IP: ip, Email: email}
	h.loginFailsMu.Lock()
	defer h.loginFailsMu.Unlock()
	if h.loginFails == nil {
		return
	}
	delete(h.loginFails, key)
}

type passwordResetRequestReq struct {
	Email        string `json:"email"`
	CaptchaToken string `json:"captchaToken"`
}

type passwordResetConfirmReq struct {
	Token       string `json:"token"`
	NewPassword string `json:"newPassword"`
}

func (h *Handlers) ResetPassword(w http.ResponseWriter, r *http.Request) {
	h.PasswordResetRequest(w, r)
}

func generatePasswordResetToken(secret string) (string, string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", "", err
	}
	raw := strings.TrimRight(base64.URLEncoding.EncodeToString(b), "=")
	return raw, hashPasswordResetToken(secret, raw), nil
}

func hashPasswordResetToken(secret string, raw string) string {
	if strings.TrimSpace(secret) == "" {
		sum := sha256.Sum256([]byte(raw))
		return hex.EncodeToString(sum[:])
	}
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(raw))
	return hex.EncodeToString(mac.Sum(nil))
}

func (h *Handlers) buildPasswordResetLink(_ *http.Request, rawToken string) (string, bool) {
	base, ok := resolveFrontendBaseURL(h.FrontendURL)
	if !ok {
		return "", false
	}
	u, err := url.Parse(base)
	if err != nil {
		return "", false
	}
	u.Path = "/reset-password"
	u.RawQuery = "token=" + url.QueryEscape(rawToken)
	return u.String(), true
}

func resolveFrontendBaseURL(configured string) (string, bool) {
	configured = strings.TrimSpace(configured)
	if configured == "" {
		return "", false
	}
	u, err := url.Parse(configured)
	if err != nil || u.Host == "" {
		return "", false
	}
	scheme := strings.ToLower(strings.TrimSpace(u.Scheme))
	if scheme != "http" && scheme != "https" {
		return "", false
	}
	u.Scheme = scheme
	u.Path = strings.TrimRight(u.Path, "/")
	u.RawQuery = ""
	u.Fragment = ""
	return strings.TrimRight(u.String(), "/"), true
}
