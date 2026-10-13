package voiceapi

import (
	"backend/internal/auth"
	"context"
	"encoding/json"
	"errors"
	"github.com/go-chi/chi/v5"
	lkauth "github.com/livekit/protocol/auth"
	"github.com/livekit/protocol/livekit"
	lksdk "github.com/livekit/server-sdk-go/v2"
	"google.golang.org/protobuf/proto"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type catalogueTransport func(*http.Request) (*http.Response, error)

func TestCatalogueCacheValidation(t *testing.T) {
	file := filepath.Join(t.TempDir(), "pairs.json")
	saved := map[string]any{"at": time.Now(), "sources": []Source{{Name: "Bybit", UpdatedAt: time.Now()}, {Name: "Binance", UpdatedAt: time.Now().Add(-48 * time.Hour)}}, "bySource": map[string][]Pair{"Bybit": {{ID: "../../admin", Base: "BTC", Quote: "USDT"}, {Base: "BTC", Quote: "USDT"}, {Base: "../BAD", Quote: "USDT"}}, "Binance": {{Base: "STALE", Quote: "USDT"}}}}
	data, _ := json.Marshal(saved)
	if err := os.WriteFile(file, data, 0o600); err != nil {
		t.Fatal(err)
	}
	c := &catalogue{cacheFile: file, bySource: map[string][]Pair{}}
	c.loadCache()
	pairs, sources := c.snapshot()
	if len(pairs) != 1 || pairs[0].ID != "BTC-USDT" || len(sources) != 1 || sources[0].Available || sources[0].Complete {
		t.Fatalf("untrusted/stale cache accepted: %+v %+v", pairs, sources)
	}
}
func (f catalogueTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func TestBinanceKeepsPairsWhenOneMarketFails(t *testing.T) {
	c := &catalogue{client: &http.Client{Transport: catalogueTransport(func(r *http.Request) (*http.Response, error) {
		if strings.Contains(r.URL.Host, "dapi") {
			return nil, errors.New("unreachable coin-margined endpoint")
		}
		base := "BTC"
		if strings.Contains(r.URL.Path, "fapi") {
			base = "ETH"
		}
		body := `{"symbols":[{"baseAsset":"` + base + `","quoteAsset":"USDT","status":"TRADING"}]}`
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}, nil
	})}}
	pairs, err := c.binance(context.Background())
	if err == nil || len(pairs) != 2 {
		t.Fatalf("partial result lost: %+v %v", pairs, err)
	}
}

func TestPairIdentity(t *testing.T) {
	pairs := uniquePairs([]Pair{{Base: "BTC", Quote: "USDT"}, {Base: "btc", Quote: "usdt"}, {Base: "1000PEPE", Quote: "USDT"}, {Base: "PEPE", Quote: "USDT"}, {Base: "BTC", Quote: "USDC"}, {Base: "BTC/../", Quote: "USDT"}})
	if len(pairs) != 3 || pairs[0].ID != "BTC-USDT" {
		t.Fatalf("deduplication: %+v", pairs)
	}
	for _, p := range pairs {
		if p.Base == "1000PEPE" {
			t.Fatal("contract multiplier got a separate room")
		}
	}
}
func TestBybitPaginationAndMarketDedup(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		cursor := ""
		base := "BTC"
		if r.URL.Query().Get("category") == "linear" && r.URL.Query().Get("cursor") == "" {
			cursor = "next"
		}
		if r.URL.Query().Get("cursor") == "next" {
			base = "SOL"
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"retCode": 0, "result": map[string]any{"nextPageCursor": cursor, "list": []map[string]string{{"baseCoin": base, "quoteCoin": "USDT", "status": "Trading"}}}})
	}))
	defer server.Close()
	t.Setenv("BYBIT_BASE_URL", server.URL)
	c := &catalogue{client: server.Client()}
	pairs, err := c.bybit(context.Background())
	if err != nil || len(pairs) != 2 || calls != 4 {
		t.Fatalf("pagination: %v, %+v, %d calls", err, pairs, calls)
	}
}
func testAPI(t *testing.T) (*API, http.Handler, *int) {
	t.Helper()
	calls := new(int)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		*calls++
		if !strings.HasSuffix(r.URL.Path, "/ListRooms") {
			t.Error("unexpected room provisioning: " + r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/protobuf")
		payload, _ := proto.Marshal(&livekit.ListRoomsResponse{Rooms: []*livekit.Room{{Name: "pair-BTC-USDT", NumParticipants: 2}, {Name: "pair-ETH-USDT", NumParticipants: 0}, {Name: "other", NumParticipants: 7}}})
		_, _ = w.Write(payload)
	}))
	t.Cleanup(server.Close)
	a := &API{authenticate: func(*http.Request) (auth.User, error) {
		return auth.User{ID: "opaque-user-id", DisplayName: "Trader", Plan: "pro"}, nil
	}, origin: "https://voice.local", publicURL: "wss://voice.local/livekit", key: "testkey", secret: strings.Repeat("x", 32), rooms: lksdk.NewRoomServiceClient(server.URL, "testkey", strings.Repeat("x", 32)), catalog: &catalogue{pairs: []Pair{{ID: "BTC-USDT", Base: "BTC", Quote: "USDT"}}}}
	r := chi.NewRouter()
	a.Routes(r)
	return a, r, calls
}
func request(handler http.Handler, method, path, body, origin string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, path, strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	if origin != "" {
		r.Header.Set("Origin", origin)
	}
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	return w
}
func TestJoinScopedTokenAndNoProvisioning(t *testing.T) {
	a, h, calls := testAPI(t)
	w := request(h, "POST", "/community/voice/join", `{"pair":"BTC-USDT"}`, a.origin)
	if w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	var result struct {
		Token string `json:"token"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &result)
	verifier, err := lkauth.ParseAPIToken(result.Token)
	if err != nil {
		t.Fatal(err)
	}
	registered, grants, err := verifier.Verify(a.secret)
	if err != nil {
		t.Fatal(err)
	}
	if verifier.Identity() != "opaque-user-id" || grants.Video.Room != "pair-BTC-USDT" || !grants.Video.RoomJoin || grants.Video.RoomAdmin || grants.Video.RoomCreate || grants.Video.RoomList || *grants.Video.CanPublishData || len(grants.Video.CanPublishSources) != 1 || grants.Video.CanPublishSources[0] != "microphone" {
		t.Fatalf("unexpected grants: %+v", grants.Video)
	}
	if registered.ExpiresAt.Time.Sub(time.Now()) > 5*time.Minute || *calls != 1 {
		t.Fatal("expiry/provisioning")
	}
	if _, _, err = verifier.Verify("wrong-secret"); err == nil {
		t.Fatal("invalid signature accepted")
	}
}
func TestActiveRoomsHideEmptyAndCache(t *testing.T) {
	_, h, calls := testAPI(t)
	for range 2 {
		w := request(h, "GET", "/community/voice/rooms", "", "")
		if w.Code != 200 {
			t.Fatal(w.Body.String())
		}
		var p struct {
			Rooms map[string]int `json:"rooms"`
		}
		_ = json.Unmarshal(w.Body.Bytes(), &p)
		if len(p.Rooms) != 1 || p.Rooms["BTC-USDT"] != 2 {
			t.Fatalf("empty/non-pair rooms leaked: %+v", p.Rooms)
		}
	}
	if *calls != 1 {
		t.Fatal("shared cache failed")
	}
}
func TestAuthOriginAndRoomValidation(t *testing.T) {
	a, h, calls := testAPI(t)
	for _, tc := range []struct {
		body, origin string
		code         int
	}{{`{"pair":"NOT-A-ROOM"}`, a.origin, 404}, {`{"pair":"BTC-USDT","roomAdmin":true}`, a.origin, 400}, {`{"pair":"BTC-USDT"}`, "https://evil.local", 403}} {
		w := request(h, "POST", "/community/voice/join", tc.body, tc.origin)
		if w.Code != tc.code {
			t.Fatalf("got %d want %d", w.Code, tc.code)
		}
	}
	if *calls != 0 {
		t.Fatal("invalid join touched SFU")
	}
	a.authenticate = func(*http.Request) (auth.User, error) { return auth.User{}, errors.New("no session") }
	if request(h, "GET", "/community/voice/pairs", "", "").Code != 401 {
		t.Fatal("anonymous access")
	}
	a.authenticate = func(*http.Request) (auth.User, error) { return auth.User{Plan: "free"}, nil }
	if request(h, "GET", "/community/voice/pairs", "", "").Code != 403 {
		t.Fatal("plan guard")
	}
}
