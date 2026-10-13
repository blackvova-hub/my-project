// Package voiceapi bridges authenticated community users to a self-hosted SFU.
package voiceapi

import (
	"backend/internal/auth"
	"context"
	"encoding/json"
	"mime"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	lkauth "github.com/livekit/protocol/auth"
	"github.com/livekit/protocol/livekit"
	lksdk "github.com/livekit/server-sdk-go/v2"
)

type API struct {
	authenticate                   func(*http.Request) (auth.User, error)
	origin, publicURL, key, secret string
	rooms                          *lksdk.RoomServiceClient
	catalog                        *catalogue
	mu                             sync.Mutex
	active                         map[string]int
	activeAt                       time.Time
}

func New(store *auth.Store, origin string) *API {
	if allowed := os.Getenv("VOICE_ALLOWED_ORIGIN"); allowed != "" {
		origin = allowed
	}
	a := &API{authenticate: store.Authenticate, origin: strings.TrimRight(origin, "/"), publicURL: os.Getenv("LIVEKIT_PUBLIC_URL"), key: os.Getenv("LIVEKIT_API_KEY"), secret: os.Getenv("LIVEKIT_API_SECRET"), active: map[string]int{}, catalog: &catalogue{client: &http.Client{Timeout: 12 * time.Second}, bySource: map[string][]Pair{}}}
	a.catalog.cacheFile = os.Getenv("VOICE_CATALOGUE_CACHE_FILE")
	a.catalog.loadCache()
	if a.configured() {
		a.rooms = lksdk.NewRoomServiceClient(envURL("LIVEKIT_URL", "http://livekit:7880"), a.key, a.secret)
	}
	return a
}
func (a *API) Run(ctx context.Context) {
	for {
		refreshCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
		a.catalog.refresh(refreshCtx)
		cancel()
		pairs, sources := a.catalog.snapshot()
		pause := 30 * time.Minute
		if len(pairs) == 0 {
			pause = 15 * time.Second
		} else {
			for _, s := range sources {
				if !s.Complete {
					pause = 2 * time.Minute
					break
				}
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(pause):
		}
	}
}
func (a *API) configured() bool { return a.publicURL != "" && a.key != "" && len(a.secret) >= 32 }
func respond(w http.ResponseWriter, code int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(value)
}
func fail(w http.ResponseWriter, code int, message string) {
	respond(w, code, map[string]string{"error": message})
}

type userKey struct{}

func (a *API) guard(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		u, err := a.authenticate(r)
		if err != nil {
			fail(w, 401, "Войдите в аккаунт")
			return
		}
		if !auth.IsPro(u.Plan) {
			fail(w, 403, "Нужна подписка PRO")
			return
		}
		if r.Method != "GET" {
			if r.Header.Get("Sec-Fetch-Site") == "cross-site" || (r.Header.Get("Origin") != "" && r.Header.Get("Origin") != a.origin) {
				fail(w, 403, "Cross-site request rejected")
				return
			}
			media, _, _ := mime.ParseMediaType(r.Header.Get("Content-Type"))
			if media != "application/json" {
				fail(w, 415, "JSON required")
				return
			}
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), userKey{}, u)))
	})
}
func (a *API) Routes(r chi.Router) {
	r.Group(func(r chi.Router) {
		r.Use(a.guard)
		r.Get("/community/voice/pairs", a.pairs)
		r.Get("/community/voice/rooms", a.list)
		r.Post("/community/voice/join", a.join)
	})
}
func (a *API) pairs(w http.ResponseWriter, r *http.Request) {
	items, sources := a.catalog.snapshot()
	if len(items) == 0 {
		fail(w, 503, "Каталог криптопар загружается. Повторите через несколько секунд.")
		return
	}
	respond(w, 200, map[string]any{"items": items, "sources": sources, "configured": a.configured()})
}
func (a *API) activeRooms(ctx context.Context) (map[string]int, error) {
	// Serialize a short, shared cache refresh; do not flood the SFU per browser.
	a.mu.Lock()
	defer a.mu.Unlock()
	if time.Since(a.activeAt) < time.Second {
		return a.active, nil
	}
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	result, err := a.rooms.ListRooms(ctx, &livekit.ListRoomsRequest{})
	if err != nil {
		return nil, err
	}
	active := map[string]int{}
	for _, r := range result.Rooms {
		if strings.HasPrefix(r.Name, "pair-") && r.NumParticipants > 0 {
			active[strings.TrimPrefix(r.Name, "pair-")] = int(r.NumParticipants)
		}
	}
	a.active, a.activeAt = active, time.Now()
	return active, nil
}
func (a *API) list(w http.ResponseWriter, r *http.Request) {
	if !a.configured() {
		fail(w, 503, "Голосовой сервер не настроен")
		return
	}
	rooms, err := a.activeRooms(r.Context())
	if err != nil {
		fail(w, 503, "Голосовой сервер недоступен. Повторите подключение позже.")
		return
	}
	respond(w, 200, map[string]any{"rooms": rooms})
}
func (a *API) join(w http.ResponseWriter, r *http.Request) {
	if !a.configured() {
		fail(w, 503, "Голосовой сервер не настроен")
		return
	}
	var input struct {
		Pair string `json:"pair"`
	}
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1024))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&input); err != nil {
		fail(w, 400, "Некорректная криптопара")
		return
	}
	if !a.catalog.contains(input.Pair) {
		fail(w, 404, "Криптопара не найдена в каталоге")
		return
	}
	if _, err := a.activeRooms(r.Context()); err != nil {
		fail(w, 503, "Голосовой сервер недоступен")
		return
	}
	u := r.Context().Value(userKey{}).(auth.User)
	publish, subscribe, data, metadata := true, true, false, false
	grant := &lkauth.VideoGrant{RoomJoin: true, Room: "pair-" + input.Pair, CanPublish: &publish, CanSubscribe: &subscribe, CanPublishData: &data, CanUpdateOwnMetadata: &metadata, CanPublishSources: []string{"microphone"}}
	token, err := lkauth.NewAccessToken(a.key, a.secret).SetIdentity(u.ID).SetName(u.DisplayName).SetVideoGrant(grant).SetValidFor(5 * time.Minute).ToJWT()
	if err != nil {
		fail(w, 500, "Не удалось подключиться к комнате")
		return
	}
	// No CreateRoom call: issuing a token or browsing pairs creates nothing.
	respond(w, 200, map[string]any{"token": token, "url": a.publicURL, "room": "pair-" + input.Pair, "expiresIn": 300})
}
