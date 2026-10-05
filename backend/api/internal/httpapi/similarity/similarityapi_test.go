package similarityapi

import (
	"backend/internal/auth"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
)

func TestAuthAndCrossSite(t *testing.T) {
	a := &API{Authenticate: func(*http.Request) (auth.User, error) { return auth.User{}, errors.New("unauthorized") }, URL: "http://unused"}
	w := httptest.NewRecorder()
	a.proxy(w, httptest.NewRequest("POST", "/similarity/search", nil))
	if w.Code != 401 {
		t.Fatal(w.Code)
	}
	a.Authenticate = func(*http.Request) (auth.User, error) { return auth.User{}, nil }
	r := httptest.NewRequest("POST", "/similarity/search", strings.NewReader("{}"))
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Sec-Fetch-Site", "cross-site")
	w = httptest.NewRecorder()
	a.proxy(w, r)
	if w.Code != 403 {
		t.Fatal(w.Code)
	}
}

func TestChartRouteForwardsVerifiedOwner(t *testing.T) {
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/jobs/123/charts" || r.Method != "GET" || r.Header.Get("X-Similarity-User") != "verified-owner" {
			t.Errorf("invalid chart proxy request: %s %s", r.Method, r.URL.Path)
		}
		w.Write([]byte(`{"charts":[]}`))
	}))
	defer up.Close()
	a := &API{Authenticate: func(*http.Request) (auth.User, error) { return auth.User{ID: "verified-owner"}, nil }, URL: up.URL, Client: up.Client()}
	mux := chi.NewRouter()
	a.Routes(mux)
	r := httptest.NewRequest("GET", "/similarity/jobs/123/charts", nil)
	r.Header.Set("X-Similarity-User", "forged")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, r)
	if w.Code != 200 || w.Body.String() != `{"charts":[]}` {
		t.Fatal(w.Code, w.Body.String())
	}
}
func TestProxyBoundedRequest(t *testing.T) {
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/search" {
			t.Error(r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"matches":[]}`))
	}))
	defer up.Close()
	a := &API{Authenticate: func(*http.Request) (auth.User, error) { return auth.User{}, nil }, URL: up.URL, Client: up.Client()}
	for _, size := range []int{2, 20000} {
		r := httptest.NewRequest("POST", "/api/similarity/search", strings.NewReader(strings.Repeat("x", size)))
		r.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		a.proxy(w, r)
		want := 200
		if size > 16384 {
			want = 413
		}
		if w.Code != want {
			t.Fatal(w.Code, want)
		}
	}
}
func TestJobProxyKeepsPathAndVerifiedOwner(t *testing.T) {
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/jobs/123" {
			t.Error(r.URL.Path)
		}
		if r.Header.Get("X-Similarity-User") != "verified-owner" {
			t.Error("client forged owner")
		}
		w.Write([]byte(`{"ok":true}`))
	}))
	defer up.Close()
	a := &API{Authenticate: func(*http.Request) (auth.User, error) { return auth.User{ID: "verified-owner"}, nil }, URL: up.URL, Client: up.Client()}
	for _, method := range []string{"GET", "DELETE"} {
		r := httptest.NewRequest(method, "/api/similarity/jobs/123", nil)
		r.Header.Set("X-Similarity-User", "forged")
		w := httptest.NewRecorder()
		a.proxy(w, r)
		if w.Code != 200 {
			t.Fatal(w.Code)
		}
	}
	r := httptest.NewRequest("DELETE", "/api/similarity/jobs/123", nil)
	r.Header.Set("Sec-Fetch-Site", "cross-site")
	w := httptest.NewRecorder()
	a.proxy(w, r)
	if w.Code != 403 {
		t.Fatal(w.Code)
	}
}
