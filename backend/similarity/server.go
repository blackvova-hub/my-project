package similarity

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/jackc/pgx/v5"
	"io"
	"log"
	"net/http"
	"regexp"
	"sort"
	"strings"
	"time"
)

func Handler(search *Searcher, q *Qdrant, jobs *Jobs) http.Handler {
	mux := http.NewServeMux()
	respond := func(w http.ResponseWriter, code int, v any) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(code)
		_ = json.NewEncoder(w).Encode(v)
	}
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
		defer cancel()
		if search.Store.DB.Ping(ctx) != nil || search.Storage.Ping(ctx) != nil || search.Cache.Ping(ctx).Err() != nil || q.Ping(ctx) != nil {
			http.Error(w, "dependencies unavailable", 503)
			return
		}
		w.Write([]byte("ok"))
	})
	mux.HandleFunc("GET /status", func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
		defer cancel()
		status, e := search.Store.Status(ctx)
		if e != nil {
			respond(w, 503, map[string]string{"error": "Статус индекса недоступен"})
			return
		}
		respond(w, 200, map[string]any{"mode": "on_demand", "progress": status, "version": search.Store.Config.Version})
	})
	mux.HandleFunc("GET /options", func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
		defer cancel()
		if e := search.Store.SyncAssets(ctx); e != nil {
			respond(w, 503, map[string]string{"error": "Каталог недоступен"})
			return
		}
		assets, e := search.Store.Assets(ctx)
		if e != nil {
			respond(w, 503, map[string]string{"error": "Каталог недоступен"})
			return
		}
		sectors := map[string]bool{}
		for _, a := range assets {
			if a.Enabled {
				for _, s := range a.Sectors {
					sectors[s] = true
				}
			}
		}
		names := []string{}
		for s := range sectors {
			names = append(names, s)
		}
		sort.Strings(names)
		respond(w, 200, map[string]any{"windows": search.Store.Config.Windows, "sectors": names, "assets": assets, "horizons": Horizons})
	})
	mux.HandleFunc("POST /search", func(w http.ResponseWriter, r *http.Request) {
		owner := r.Header.Get("X-Similarity-User")
		if owner == "" {
			respond(w, 401, map[string]string{"error": "Войдите в аккаунт"})
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, 16384)
		var input Request
		d := json.NewDecoder(r.Body)
		d.DisallowUnknownFields()
		if d.Decode(&input) != nil || d.Decode(&struct{}{}) != io.EOF {
			respond(w, 400, map[string]string{"error": "Некорректный запрос поиска"})
			return
		}
		if e := input.Validate(search.Store.Config, time.Now()); e != nil {
			respond(w, 422, map[string]string{"error": e.Error()})
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
		defer cancel()
		id, e := jobs.Create(ctx, owner, input)
		if e != nil {
			log.Printf("similarity search: %v", e)
			if errors.Is(e, ErrQueueFull) {
				respond(w, 429, map[string]string{"error": e.Error()})
				return
			}
			respond(w, 503, map[string]string{"error": "Не удалось создать поиск. Проверьте доступность истории и повторите попытку."})
			return
		}
		respond(w, 202, map[string]string{"jobId": id})
	})
	validID := regexp.MustCompile(`^[a-f0-9]{8}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{12}$`)
	jobHandler := func(w http.ResponseWriter, r *http.Request) {
		chartsRequested := strings.HasSuffix(r.URL.Path, "/charts")
		owner, id := r.Header.Get("X-Similarity-User"), r.PathValue("id")
		if owner == "" {
			respond(w, 401, map[string]string{"error": "Войдите в аккаунт"})
			return
		}
		if !validID.MatchString(id) {
			respond(w, 404, map[string]string{"error": "Поиск не найден"})
			return
		}
		timeout := 5 * time.Second
		if chartsRequested {
			timeout = 20 * time.Second
		}
		ctx, cancel := context.WithTimeout(r.Context(), timeout)
		defer cancel()
		if r.Method == "DELETE" {
			e := jobs.Cancel(ctx, owner, id)
			if e != nil {
				if e == pgx.ErrNoRows {
					respond(w, 404, map[string]string{"error": "Поиск не найден"})
				} else {
					respond(w, 503, map[string]string{"error": "Не удалось отменить поиск"})
				}
				return
			}
			respond(w, 200, map[string]bool{"ok": true})
			return
		}
		v, e := jobs.Get(ctx, owner, id)
		if e != nil {
			if e == pgx.ErrNoRows {
				respond(w, 404, map[string]string{"error": "Поиск не найден"})
			} else {
				respond(w, 503, map[string]string{"error": "Не удалось получить состояние поиска"})
			}
			return
		}
		if chartsRequested {
			if v.Status != "completed" || v.Result == nil {
				respond(w, 409, map[string]string{"error": "Графики доступны после завершения поиска"})
				return
			}
			charts, err := search.Charts(ctx, *v.Result)
			if err != nil {
				log.Printf("similarity charts: %v", err)
				respond(w, 503, map[string]string{"error": "Не удалось загрузить свечи графиков. Повторите попытку."})
				return
			}
			respond(w, 200, map[string]any{"charts": charts})
			return
		}
		respond(w, 200, v)
	}
	mux.HandleFunc("GET /jobs/{id}", jobHandler)
	mux.HandleFunc("GET /jobs/{id}/charts", jobHandler)
	mux.HandleFunc("DELETE /jobs/{id}", jobHandler)
	return mux
}
