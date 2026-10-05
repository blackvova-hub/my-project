package aiapi

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"

	"backend/internal/auth"
)

type Config struct {
	OpenAIAPIKey    string
	OpenAIBaseURL   string
	OpenAIModel     string
	Timeout         time.Duration
	MaxContext      int
	MaxMessages     int
	DailyTokenLimit int
}

type API struct {
	authStore *auth.Store
	cfg       Config
	client    *http.Client
	mu        sync.Mutex
	inflight  map[int64]int
}

func New(authStore *auth.Store, cfg Config) *API {
	if cfg.OpenAIBaseURL == "" {
		cfg.OpenAIBaseURL = "https://api.openai.com/v1"
	}
	if cfg.OpenAIModel == "" {
		cfg.OpenAIModel = "gpt-5.4-mini"
	}
	if cfg.Timeout == 0 {
		cfg.Timeout = 20 * time.Second
	}
	if cfg.MaxContext == 0 {
		cfg.MaxContext = 6000
	}
	if cfg.MaxMessages == 0 {
		cfg.MaxMessages = 20
	}
	if cfg.DailyTokenLimit == 0 {
		cfg.DailyTokenLimit = 50000
	}

	return &API{
		authStore: authStore,
		cfg:       cfg,
		client: &http.Client{
			Timeout: cfg.Timeout,
		},
		inflight: make(map[int64]int),
	}
}

func (a *API) Routes(r chi.Router) {
	r.Group(func(r chi.Router) {
		r.Use(a.requireAuth)
		r.Post("/ai/chat", a.handleChat)
	})
}

type ctxKey string

const (
	ctxUserKey                 ctxKey = "auth_user"
	maxUpstreamAIResponseBytes        = int64(2 << 20)
)

var errUpstreamResponseTooLarge = errors.New("upstream AI response exceeds limit")

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
	err := a.authStore.DB.QueryRow(ctx, "SELECT num_id FROM users WHERE id = $1", u.ID).Scan(&numID)
	return numID, err
}

type ChatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type ChatRequest struct {
	Messages []ChatMessage `json:"messages"`
	Context  string        `json:"context"`
}

type ChatResponse struct {
	Reply string `json:"reply"`
	Model string `json:"model"`
}

type openAIInputMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type openAIReasoningConfig struct {
	Effort string `json:"effort"`
}

type openAITextConfig struct {
	Verbosity string `json:"verbosity"`
}

type openAIResponseRequest struct {
	Model            string                `json:"model"`
	Instructions     string                `json:"instructions"`
	Input            []openAIInputMessage  `json:"input"`
	MaxOutputTokens  int                   `json:"max_output_tokens"`
	Reasoning        openAIReasoningConfig `json:"reasoning"`
	Text             openAITextConfig      `json:"text"`
	SafetyIdentifier string                `json:"safety_identifier"`
	Store            bool                  `json:"store"`
}

type openAIResponse struct {
	Model      string `json:"model"`
	OutputText string `json:"output_text"`
	Output     []struct {
		Type    string `json:"type"`
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
	} `json:"output"`
}

func (a *API) handleChat(w http.ResponseWriter, r *http.Request) {
	if strings.TrimSpace(a.cfg.OpenAIAPIKey) == "" {
		writeErr(w, http.StatusServiceUnavailable, "ai_not_configured")
		return
	}

	user, ok := userFromCtx(r)
	if !ok {
		writeErr(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	if !auth.IsStandard(user.Plan) {
		writeErr(w, http.StatusForbidden, "plan_required")
		return
	}

	numID, err := a.userNumID(r.Context(), user)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "user_lookup_failed")
		return
	}
	a.mu.Lock()
	if a.inflight[numID] >= 2 {
		a.mu.Unlock()
		writeErr(w, http.StatusTooManyRequests, "ai_concurrency_exceeded")
		return
	}
	a.inflight[numID]++
	a.mu.Unlock()
	defer a.releaseInflight(numID)

	r.Body = http.MaxBytesReader(w, r.Body, 256<<10)
	var in ChatRequest
	dec := json.NewDecoder(r.Body)
	if err := dec.Decode(&in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid_json")
		return
	}
	if err := dec.Decode(&struct{}{}); err != io.EOF {
		writeErr(w, http.StatusBadRequest, "invalid_json")
		return
	}

	if len(in.Messages) == 0 {
		writeErr(w, http.StatusBadRequest, "messages_required")
		return
	}
	if len(in.Messages) > a.cfg.MaxMessages {
		writeErr(w, http.StatusBadRequest, "too_many_messages")
		return
	}
	if len(in.Context) > a.cfg.MaxContext {
		in.Context = truncateRunes(in.Context, a.cfg.MaxContext)
	}

	question := latestUserMessage(in.Messages)
	if question == "" {
		writeErr(w, http.StatusBadRequest, "messages_required")
		return
	}
	userContext := assistantUserContext{
		Plan:            user.Plan,
		PrimaryExchange: user.PrimaryExchange,
		EmailVerified:   user.EmailVerified,
		TwoFAEnabled:    user.TwoFAEnabled,
		IsAdmin:         user.IsAdmin,
	}
	if reply, ok := protectedLocalReply(question); ok {
		writeJSON(w, http.StatusOK, ChatResponse{Reply: reply, Model: "short-long-security"})
		return
	}
	inputMessages := buildOpenAIInput(in.Messages)
	if len(inputMessages) == 0 || inputMessages[len(inputMessages)-1].Role != "user" {
		writeErr(w, http.StatusBadRequest, "messages_required")
		return
	}
	knowledge := selectKnowledge(in.Context, in.Messages, userContext)

	if a.cfg.DailyTokenLimit > 0 {
		// Reserve quota atomically before invoking the paid upstream.
		reserve := estimateRequestTokens(knowledge.Instructions, inputMessages) + maxAIOutputTokens
		if reserve < 1 {
			reserve = 1
		}
		if reserve > a.cfg.DailyTokenLimit {
			writeErr(w, http.StatusTooManyRequests, "ai_quota_exceeded")
			return
		}
		var reserved int
		err = a.authStore.DB.QueryRow(r.Context(), `
			INSERT INTO ai_usage (user_id, day, tokens_used) VALUES ($1, CURRENT_DATE, $2)
			ON CONFLICT (user_id, day) DO UPDATE SET tokens_used = ai_usage.tokens_used + EXCLUDED.tokens_used, updated_at = now()
			WHERE ai_usage.tokens_used + EXCLUDED.tokens_used <= $3
			RETURNING tokens_used
		`, numID, reserve, a.cfg.DailyTokenLimit).Scan(&reserved)
		if errors.Is(err, pgx.ErrNoRows) {
			writeErr(w, http.StatusTooManyRequests, "ai_quota_exceeded")
			return
		}
		if err != nil {
			writeErr(w, http.StatusInternalServerError, "usage_reservation_failed")
			return
		}
	}

	payload := openAIResponseRequest{
		Model:            a.cfg.OpenAIModel,
		Instructions:     knowledge.Instructions,
		Input:            inputMessages,
		MaxOutputTokens:  maxAIOutputTokens,
		Reasoning:        openAIReasoningConfig{Effort: "low"},
		Text:             openAITextConfig{Verbosity: "low"},
		SafetyIdentifier: safetyIdentifier(user.ID),
		Store:            false,
	}

	ctx, cancel := context.WithTimeout(r.Context(), a.cfg.Timeout)
	defer cancel()

	baseURL := strings.TrimRight(a.cfg.OpenAIBaseURL, "/")
	respBody, status, err := a.callOpenAI(ctx, baseURL, payload)
	if err != nil {
		writeErr(w, http.StatusBadGateway, "ai_unavailable")
		return
	}
	if status < 200 || status >= 300 {
		writeErr(w, http.StatusBadGateway, "ai_error")
		return
	}

	var out openAIResponse
	if err := json.Unmarshal(respBody, &out); err != nil {
		writeErr(w, http.StatusBadGateway, "ai_error")
		return
	}
	reply := sanitizeAssistantReply(extractOpenAIText(out), question)
	if reply == "" {
		writeErr(w, http.StatusBadGateway, "ai_empty")
		return
	}
	usedModel := strings.TrimSpace(out.Model)
	if usedModel == "" {
		usedModel = a.cfg.OpenAIModel
	}

	writeJSON(w, http.StatusOK, ChatResponse{Reply: reply, Model: usedModel})
}

func safetyIdentifier(userID string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(userID)))
	return hex.EncodeToString(sum[:16])
}

func (a *API) callOpenAI(ctx context.Context, baseURL string, payload openAIResponseRequest) ([]byte, int, error) {
	body, _ := json.Marshal(payload)
	endpoint := baseURL + "/responses"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+a.cfg.OpenAIAPIKey)
	req.Header.Set("x-proxy-source", "shortlong-backend")

	resp, err := a.client.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()

	respBody, err := readLimitedResponse(resp.Body, maxUpstreamAIResponseBytes)
	if err != nil {
		return nil, resp.StatusCode, err
	}

	return respBody, resp.StatusCode, nil
}

func extractOpenAIText(response openAIResponse) string {
	if text := strings.TrimSpace(response.OutputText); text != "" {
		return text
	}

	var parts []string
	for _, item := range response.Output {
		for _, content := range item.Content {
			if content.Type != "output_text" {
				continue
			}
			if text := strings.TrimSpace(content.Text); text != "" {
				parts = append(parts, text)
			}
		}
	}
	return strings.TrimSpace(strings.Join(parts, "\n"))
}

func (a *API) releaseInflight(userID int64) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.inflight[userID] <= 1 {
		delete(a.inflight, userID)
		return
	}
	a.inflight[userID]--
}

func readLimitedResponse(r io.Reader, limit int64) ([]byte, error) {
	if limit < 0 {
		return nil, errUpstreamResponseTooLarge
	}
	body, err := io.ReadAll(io.LimitReader(r, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(body)) > limit {
		return nil, errUpstreamResponseTooLarge
	}
	return body, nil
}

func estimateTokens(text string) int {
	text = strings.TrimSpace(text)
	if text == "" {
		return 0
	}
	// UTF-8 bytes / 4 is a conservative lightweight approximation for both
	// English and Cyrillic without adding a tokenizer dependency.
	return (len(text) + 3) / 4
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]any{"error": msg})
}
