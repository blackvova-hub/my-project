package auth

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestSessionCookieUsesHostPrefixOnlyOverHTTPS(t *testing.T) {
	localRecorder := httptest.NewRecorder()
	setSessionCookie(localRecorder, "local-token", time.Now().Add(time.Hour), false)
	localCookies := localRecorder.Result().Cookies()
	if len(localCookies) != 2 || localCookies[0].Name != "__Host-sid" || localCookies[0].MaxAge != -1 ||
		localCookies[1].Name != "sid" || localCookies[1].Secure {
		t.Fatalf("unexpected local session cookie: %+v", localCookies)
	}

	secureRecorder := httptest.NewRecorder()
	setSessionCookie(secureRecorder, "secure-token", time.Now().Add(time.Hour), true)
	secureCookies := secureRecorder.Result().Cookies()
	if len(secureCookies) != 1 || secureCookies[0].Name != "__Host-sid" || !secureCookies[0].Secure {
		t.Fatalf("unexpected secure session cookie: %+v", secureCookies)
	}
}

func TestGetSIDPrefersCookieForRequestScheme(t *testing.T) {
	localRequest := httptest.NewRequest(http.MethodGet, "/auth/me", nil)
	localRequest.Header.Set("X-Forwarded-Proto", "http")
	localRequest.AddCookie(&http.Cookie{Name: "__Host-sid", Value: "old-secure-token"})
	localRequest.AddCookie(&http.Cookie{Name: "sid", Value: "current-local-token"})
	if got, ok := getSID(localRequest); !ok || got != "current-local-token" {
		t.Fatalf("local getSID() = %q, %v", got, ok)
	}

	secureRequest := httptest.NewRequest(http.MethodGet, "/auth/me", nil)
	secureRequest.Header.Set("X-Forwarded-Proto", "https")
	secureRequest.AddCookie(&http.Cookie{Name: "sid", Value: "old-local-token"})
	secureRequest.AddCookie(&http.Cookie{Name: "__Host-sid", Value: "current-secure-token"})
	if got, ok := getSID(secureRequest); !ok || got != "current-secure-token" {
		t.Fatalf("secure getSID() = %q, %v", got, ok)
	}
}

func TestGetSIDAcceptsSecureAndLocalCookieNames(t *testing.T) {
	for _, tc := range []struct {
		name  string
		value string
	}{
		{name: "__Host-sid", value: "secure-token"},
		{name: "sid", value: "local-token"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/auth/me", nil)
			req.AddCookie(&http.Cookie{Name: tc.name, Value: tc.value})
			got, ok := getSID(req)
			if !ok || got != tc.value {
				t.Fatalf("getSID() = %q, %v; want %q, true", got, ok, tc.value)
			}
		})
	}
}

func TestClientIPIgnoresForwardedHeadersFromUntrustedPeer(t *testing.T) {
	req := httptest.NewRequest("POST", "/auth/login", nil)
	req.RemoteAddr = "203.0.113.10:4321"
	req.Header.Set("X-Forwarded-For", "198.51.100.77")
	req.Header.Set("X-Real-IP", "198.51.100.88")

	if got := clientIP(req, nil); got != "203.0.113.10" {
		t.Fatalf("clientIP() = %q, want direct peer IP", got)
	}
}

func TestClientIPUsesRightmostUntrustedHopBehindTrustedProxy(t *testing.T) {
	h := &Handlers{}
	if err := h.SetTrustedProxyCIDRs("10.0.0.0/8"); err != nil {
		t.Fatalf("SetTrustedProxyCIDRs: %v", err)
	}
	req := httptest.NewRequest("POST", "/auth/login", nil)
	req.RemoteAddr = "10.0.0.2:4321"
	req.Header.Set("X-Forwarded-For", "198.51.100.77, 203.0.113.10, 10.0.0.3")

	if got := h.clientIP(req); got != "203.0.113.10" {
		t.Fatalf("clientIP() = %q, want rightmost untrusted hop", got)
	}
}

func TestRateLimiterRemovesExpiredBuckets(t *testing.T) {
	limiter := newRateLimiter(10*time.Minute, 2)
	limiter.buckets["expired"] = &rateBucket{count: 1, reset: time.Now().Add(-time.Minute)}
	limiter.buckets["active"] = &rateBucket{count: 1, reset: time.Now().Add(time.Minute)}

	if !limiter.Allow("new") {
		t.Fatal("new key should be allowed after expired bucket cleanup")
	}
	if _, ok := limiter.buckets["expired"]; ok {
		t.Fatal("expired rate-limit bucket was not removed")
	}
}

func TestVerifyEmailAccountLimiterStopsCodeBruteForce(t *testing.T) {
	h := NewHandlers(nil, false, nil, "", "", false)
	for i := 0; i < 8; i++ {
		if !h.VerifyEmailAccountLimiter.Allow("user@example.com") {
			t.Fatalf("attempt %d unexpectedly rejected", i+1)
		}
	}
	if h.VerifyEmailAccountLimiter.Allow("user@example.com") {
		t.Fatal("ninth verification-code attempt must be rate limited")
	}
}

func TestLoginFailureMapRemovesExpiredEntries(t *testing.T) {
	h := &Handlers{loginFails: map[failKey]*failBucket{
		{IP: "203.0.113.1", Email: "old@example.com"}: {Count: 3, Reset: time.Now().Add(-time.Minute)},
	}}
	h.recordLoginFail("203.0.113.2", "new@example.com")

	if len(h.loginFails) != 1 {
		t.Fatalf("login failure map has %d entries, want 1", len(h.loginFails))
	}
	if h.shouldRequireLoginCaptcha("203.0.113.1", "old@example.com") {
		t.Fatal("expired login failures must not require CAPTCHA")
	}
}
