package main

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"log"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/cors"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"

	"backend/internal/auth"
	"backend/internal/config"
	"backend/internal/database"
	"backend/internal/httpapi/admin"
	"backend/internal/httpapi/ai"
	"backend/internal/httpapi/analytics"
	"backend/internal/httpapi/backtest"
	"backend/internal/httpapi/chartconfig"
	"backend/internal/httpapi/community"
	"backend/internal/httpapi/market"
	"backend/internal/httpapi/news"
	"backend/internal/httpapi/scanner"
	"backend/internal/httpapi/similarity"
	"backend/internal/telegram/accountbot"
)

const maxRequestBodyBytes = int64(32 << 20)

func main() {
	cfg := config.Load()

	if strings.EqualFold(strings.TrimSpace(cfg.AppEnv), "production") {
		if cfg.TelegramPolling {
			log.Fatal("TELEGRAM_POLLING_ENABLED is local-only and must be false in production")
		}
		if strings.TrimSpace(cfg.TurnstileSecret) == "" {
			log.Fatal("TURNSTILE_SECRET is required in production")
		}
		if strings.TrimSpace(cfg.ResetTokenSecret) == "" {
			log.Fatal("RESET_TOKEN_SECRET is required in production")
		}
		if len(strings.TrimSpace(cfg.ResetTokenSecret)) < 32 {
			log.Fatal("RESET_TOKEN_SECRET must be at least 32 characters in production")
		}
		frontendURL := strings.TrimSpace(cfg.FrontendURL)
		if frontendURL == "" {
			log.Fatal("FRONTEND_URL is required in production")
		}
		u, err := url.Parse(frontendURL)
		if err != nil || u.Host == "" || strings.ToLower(u.Scheme) != "https" {
			log.Fatal("FRONTEND_URL must be a valid https URL in production")
		}
	}

	// --- DB ---
	pool, err := db.NewPool(cfg.DatabaseURL)
	if err != nil {
		log.Fatalf("db: %v", err)
	}
	defer pool.Close()

	// --- MIGRATIONS ---
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if os.Getenv("SKIP_MIGRATIONS") != "1" {
		if err := db.ApplyMigrations(ctx, pool); err != nil {
			log.Fatalf("migrate: %v", err)
		}
	}

	// --- ROUTER ---
	r := chi.NewRouter()

	r.Use(requestBodyLimit(maxRequestBodyBytes))
	r.Use(ipBanMiddleware(pool))

	// CORS (нужно для cookie-сессий, sid)
	r.Use(cors.Handler(cors.Options{
		AllowedOrigins:   []string{cfg.CORSOrigin},
		AllowedMethods:   []string{"GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"},
		AllowedHeaders:   []string{"Accept", "Authorization", "Content-Type", "X-CSRF-Token"},
		ExposedHeaders:   []string{"Set-Cookie"},
		AllowCredentials: true,
		MaxAge:           300,
	}))

	logBuffer := adminapi.NewLogBuffer(800)
	r.Use(securityHeaders(cfg))
	r.Use(requestLogger(logBuffer))

	// --- HEALTH ---
	r.Get("/health", func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 500*time.Millisecond)
		defer cancel()

		if err := pool.Ping(ctx); err != nil {
			writeJSON(w, http.StatusServiceUnavailable, map[string]any{
				"ok":    false,
				"error": "db_unavailable",
			})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
	})

	// --- AUTH ---
	authStore := auth.NewStore(pool)
	mailer, err := auth.NewMailer(auth.MailerConfig{
		Host: cfg.SMTPHost,
		Port: cfg.SMTPPort,
		User: cfg.SMTPUser,
		Pass: cfg.SMTPPass,
		From: cfg.SMTPFrom,
	})
	if err != nil {
		log.Printf("smtp: disabled (%v)", err)
		mailer = nil
	}
	authHandlers := auth.NewHandlers(authStore, cfg.CookieSecure, mailer, cfg.TelegramBotUser, cfg.TurnstileSecret, cfg.TurnstileEnabled)
	authHandlers.FrontendURL = strings.TrimRight(strings.TrimSpace(cfg.FrontendURL), "/")
	authHandlers.ResetTokenSecret = strings.TrimSpace(cfg.ResetTokenSecret)
	if err := authHandlers.SetTrustedProxyCIDRs(cfg.TrustedProxyCIDRs); err != nil {
		log.Fatalf("trusted proxies: %v", err)
	}

	r.Route("/auth", func(r chi.Router) {
		r.Post("/register", authHandlers.Register)
		r.Post("/login", authHandlers.Login)
		r.Post("/password-reset/request", authHandlers.PasswordResetRequest)
		r.Post("/password-reset/confirm", authHandlers.PasswordResetConfirm)
		r.Post("/reset-password", authHandlers.PasswordResetRequest)
		r.Post("/ping", authHandlers.Ping)
		r.Post("/twofa/verify-login", authHandlers.VerifyTwoFALogin)
		r.Post("/logout", authHandlers.Logout)
		r.Get("/me", authHandlers.Me)
		r.Get("/watchlist", authHandlers.Watchlist)
		r.Put("/watchlist", authHandlers.UpdateWatchlist)
		r.Get("/sessions", authHandlers.Sessions)
		r.Get("/telegram/status", authHandlers.TelegramStatus)
		r.Post("/telegram/link", authHandlers.TelegramLink)
		r.Post("/telegram/toggle", authHandlers.TelegramToggle)
		r.Post("/telegram/unlink", authHandlers.TelegramUnlink)
		r.Post("/verify-email", authHandlers.VerifyEmail)
		r.Post("/resend-verification", authHandlers.ResendVerification)
		r.Post("/twofa/request-enable", authHandlers.RequestTwoFAEnable)
		r.Post("/twofa/request-disable", authHandlers.RequestTwoFADisable)
		r.Post("/twofa/confirm-enable", authHandlers.ConfirmTwoFAEnable)
		r.Patch("/profile", authHandlers.UpdateProfile)
		r.Patch("/twofa", authHandlers.UpdateTwoFA)
	})

	var telegramPollingCancel context.CancelFunc
	if strings.TrimSpace(cfg.TelegramBotToken) != "" {
		tg := telegramapi.New(authStore, telegramapi.Config{
			BotToken:      cfg.TelegramBotToken,
			WebhookSecret: cfg.TelegramSecret,
			BotUsername:   cfg.TelegramBotUser,
		})
		r.Post("/telegram/webhook", tg.HandleWebhook)
		if cfg.TelegramPolling {
			pollingCtx, cancelPolling := context.WithCancel(context.Background())
			telegramPollingCancel = cancelPolling
			log.Printf("telegram account bot: LOCAL_ONLY polling enabled")
			go func() {
				if err := tg.RunPolling(pollingCtx); err != nil && pollingCtx.Err() == nil {
					log.Printf("telegram polling (LOCAL_ONLY) stopped: %v", err)
				}
			}()
		}
	}

	// --- API (scanner rules + signals + news) ---
	// Внутри scannerapi уже стоит requireAuth middleware, и есть:
	// GET/POST/PATCH/DELETE /api/scanner/rules
	// GET /api/signals
	api := scannerapi.New(authStore, pool)
	chartConfig := chartconfigapi.New(authStore, pool)
	community := communityapi.New(authStore, pool)
	admin := adminapi.New(authStore, pool, logBuffer)
	news := newsapi.New(pool)
	var redisClient *redis.Client
	if strings.TrimSpace(cfg.RedisAddr) != "" {
		candidate := redis.NewClient(&redis.Options{
			Addr: cfg.RedisAddr, Password: cfg.RedisPassword, DB: cfg.RedisDB,
		})
		pingCtx, pingCancel := context.WithTimeout(context.Background(), 2*time.Second)
		if pingErr := candidate.Ping(pingCtx).Err(); pingErr != nil {
			log.Printf("redis: market cache disabled (%v)", pingErr)
			_ = candidate.Close()
		} else {
			redisClient = candidate
			defer redisClient.Close()
		}
		pingCancel()
	}
	market := marketapi.New(redisClient)
	traderAnalytics := analytics.New(authStore, pool, cfg.CORSOrigin)
	analyticsContext, stopAnalytics := context.WithCancel(context.Background())
	defer stopAnalytics()
	go traderAnalytics.Run(analyticsContext)
	backtests := backtestapi.New(authStore, pool, redisClient, os.Getenv("BYBIT_BASE_URL"), cfg.CORSOrigin)
	similarities := similarityapi.New(authStore, redisClient, os.Getenv("SIMILARITY_URL"), cfg.CORSOrigin)
	ai := aiapi.New(authStore, aiapi.Config{
		OpenAIAPIKey:    cfg.OpenAIAPIKey,
		OpenAIBaseURL:   cfg.OpenAIBaseURL,
		OpenAIModel:     cfg.OpenAIModel,
		Timeout:         20 * time.Second,
		MaxContext:      6000,
		MaxMessages:     20,
		DailyTokenLimit: cfg.AIDailyTokens,
	})
	r.Route("/api", func(r chi.Router) {
		traderAnalytics.Routes(r)
		backtests.Routes(r)
		similarities.Routes(r)
		api.Routes(r)
		chartConfig.Routes(r)
		community.Routes(r)
		admin.Routes(r)
		news.Routes(r)
		market.Routes(r)
		ai.Routes(r)
	})

	// Compatibility for nginx /api rewrite: also expose API routes at root.
	r.Route("/", func(r chi.Router) {
		traderAnalytics.Routes(r)
		backtests.Routes(r)
		similarities.Routes(r)
		api.Routes(r)
		chartConfig.Routes(r)
		community.Routes(r)
		admin.Routes(r)
		news.Routes(r)
		market.Routes(r)
		ai.Routes(r)
	})

	// --- SERVER ---
	srv := &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           r,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
		MaxHeaderBytes:    16 << 10,
		TLSConfig: &tls.Config{
			MinVersion: tls.VersionTLS12,
		},
	}

	go func() {
		if cfg.TLSCertFile != "" && cfg.TLSKeyFile != "" {
			log.Printf("https: listening on %s", cfg.HTTPAddr)
			if err := srv.ListenAndServeTLS(cfg.TLSCertFile, cfg.TLSKeyFile); err != nil && err != http.ErrServerClosed {
				log.Fatalf("https: %v", err)
			}
			return
		}
		log.Printf("http: listening on %s", cfg.HTTPAddr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("http: %v", err)
		}
	}()

	// --- GRACEFUL SHUTDOWN ---
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	<-stop
	if telegramPollingCancel != nil {
		telegramPollingCancel()
	}

	ctxShutdown, cancelShutdown := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancelShutdown()

	_ = srv.Shutdown(ctxShutdown)
	log.Println("shutdown: ok")
}

func requestBodyLimit(limit int64) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if limit <= 0 {
				next.ServeHTTP(w, r)
				return
			}
			if r.ContentLength > limit {
				http.Error(w, "request body too large", http.StatusRequestEntityTooLarge)
				return
			}
			r.Body = http.MaxBytesReader(w, r.Body, limit)
			next.ServeHTTP(w, r)
		})
	}
}

func ipBanMiddleware(db *pgxpool.Pool) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ip := auth.ClientIP(r)
			if strings.TrimSpace(ip) == "" {
				next.ServeHTTP(w, r)
				return
			}
			var exists bool
			err := db.QueryRow(r.Context(), `SELECT EXISTS(SELECT 1 FROM banned_ips WHERE ip = $1)`, ip).Scan(&exists)
			if err == nil && exists {
				writeJSON(w, http.StatusForbidden, map[string]any{"error": "ip_banned"})
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

type statusWriter struct {
	http.ResponseWriter
	status int
}

func (w *statusWriter) WriteHeader(code int) {
	w.status = code
	w.ResponseWriter.WriteHeader(code)
}

func requestLogger(buf *adminapi.LogBuffer) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			sw := &statusWriter{ResponseWriter: w, status: http.StatusOK}
			start := time.Now()
			next.ServeHTTP(sw, r)
			dur := time.Since(start)
			buf.Add("info", r.Method+" "+r.URL.Path+" -> "+strconv.Itoa(sw.status)+" ("+dur.String()+")")
		})
	}
}

func securityHeaders(cfg config.Config) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("X-Content-Type-Options", "nosniff")
			w.Header().Set("X-Frame-Options", "DENY")
			w.Header().Set("Referrer-Policy", "strict-origin-when-cross-origin")
			w.Header().Set("Permissions-Policy", "camera=(), microphone=(), geolocation=()")
			w.Header().Set("Content-Security-Policy", "default-src 'none'; frame-ancestors 'none'; base-uri 'none'; form-action 'none'")
			if cfg.CookieSecure {
				maxAge := cfg.HSTSMaxAge
				if maxAge <= 0 {
					maxAge = 31536000
				}
				w.Header().Set("Strict-Transport-Security", "max-age="+strconv.Itoa(maxAge)+"; includeSubDomains")
			}
			next.ServeHTTP(w, r)
		})
	}
}
