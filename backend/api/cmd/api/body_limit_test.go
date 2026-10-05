package main

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRequestBodyLimitRejectsKnownOversizeBody(t *testing.T) {
	called := false
	handler := requestBodyLimit(4)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		_, _ = io.Copy(io.Discard, r.Body)
	}))
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader("12345"))
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if called {
		t.Fatal("oversize request reached handler")
	}
	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status=%d want=%d", rec.Code, http.StatusRequestEntityTooLarge)
	}
}
