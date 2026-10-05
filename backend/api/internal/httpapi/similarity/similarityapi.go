package similarityapi

import (
	"backend/internal/auth"
	"context"
	"encoding/json"
	"github.com/go-chi/chi/v5"
	"github.com/redis/go-redis/v9"
	"io"
	"mime"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type API struct {
	Authenticate func(*http.Request) (auth.User, error)
	Redis        *redis.Client
	URL, Origin  string
	Client       *http.Client
}

func New(a *auth.Store, r *redis.Client, endpoint, origin string) *API {
	return &API{a.Authenticate, r, strings.TrimRight(endpoint, "/"), origin, &http.Client{Timeout: 70 * time.Second}}
}
func (a *API) Routes(r chi.Router) {
	r.Get("/similarity/options", a.proxy)
	r.Get("/similarity/status", a.proxy)
	r.Post("/similarity/search", a.proxy)
	r.Get("/similarity/jobs/{id}", a.proxy)
	r.Get("/similarity/jobs/{id}/charts", a.proxy)
	r.Delete("/similarity/jobs/{id}", a.proxy)
}
func reply(w http.ResponseWriter, code int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": msg})
}
func (a *API) proxy(w http.ResponseWriter, r *http.Request) {
	user, e := a.Authenticate(r)
	if e != nil {
		reply(w, 401, "Войдите в аккаунт")
		return
	}
	if a.URL == "" {
		reply(w, 503, "Поиск похожих ситуаций не настроен")
		return
	}
	if r.Method != "GET" {
		ct, _, _ := mime.ParseMediaType(r.Header.Get("Content-Type"))
		if r.Method == "POST" && ct != "application/json" {
			reply(w, 415, "Ожидается application/json")
			return
		}
		if r.Header.Get("Sec-Fetch-Site") == "cross-site" || (r.Header.Get("Origin") != "" && r.Header.Get("Origin") != a.Origin) {
			reply(w, 403, "cross_site_request")
			return
		}
		if a.Redis != nil {
			key := "similarity:user-rate:" + user.ID + ":" + time.Now().UTC().Format("200601021504")
			n, e := a.Redis.Incr(r.Context(), key).Result()
			if e != nil {
				reply(w, 503, "Сервис временно недоступен")
				return
			}
			if n == 1 {
				_ = a.Redis.Expire(r.Context(), key, 2*time.Minute).Err()
			}
			if n > 20 {
				reply(w, 429, "Слишком много запросов; повторите через минуту")
				return
			}
		}
	}
	r.Body = http.MaxBytesReader(w, r.Body, 16384)
	raw, e := io.ReadAll(r.Body)
	if e != nil {
		reply(w, 413, "Слишком большой запрос")
		return
	}
	_, suffix, _ := strings.Cut(r.URL.Path, "/similarity")
	u, e := url.Parse(a.URL + suffix)
	if e != nil {
		reply(w, 503, "Сервис не настроен")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 70*time.Second)
	defer cancel()
	req, e := http.NewRequestWithContext(ctx, r.Method, u.String(), strings.NewReader(string(raw)))
	if e != nil {
		reply(w, 503, "Сервис не настроен")
		return
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Similarity-User", user.ID)
	res, e := a.Client.Do(req)
	if e != nil {
		reply(w, 503, "Сервис поиска временно недоступен")
		return
	}
	defer res.Body.Close()
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(res.StatusCode)
	_, _ = io.Copy(w, io.LimitReader(res.Body, 4<<20))
}
