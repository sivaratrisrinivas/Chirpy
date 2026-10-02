package main

import (
	"database/sql"
	"log/slog"
	"net/http"
	"sync/atomic"
	"time"

	"github.com/sivaratrisrinivas/Chirpy/internal/database"
)

type apiConfig struct {
	fileserverHits atomic.Int32
	sqlDB          *sql.DB
	db             *database.Queries
	platform       string
	jwtSecret      string
	polkaKey       string
	accessTTL      time.Duration
	refreshTTL     time.Duration
	authLimiter    *ipRateLimiter
}

func newAPI(db *sql.DB, cfg config) *apiConfig {
	return &apiConfig{
		sqlDB:       db,
		db:          database.New(db),
		platform:    cfg.Platform,
		jwtSecret:   cfg.JWTSecret,
		polkaKey:    cfg.PolkaKey,
		accessTTL:   time.Hour,
		refreshTTL:  60 * 24 * time.Hour,
		authLimiter: newIPRateLimiter(10, time.Minute),
	}
}

func (cfg *apiConfig) routes() http.Handler {
	mux := http.NewServeMux()

	// Only ./static is served. The old code served "." which exposed .env and source.
	fs := http.StripPrefix("/app", http.FileServer(http.Dir("static")))
	mux.Handle("/app/", cfg.middlewareMetricsInc(fs))
	mux.Handle("GET /{$}", http.RedirectHandler("/app/", http.StatusFound))

	mux.HandleFunc("GET /api/healthz", cfg.handlerReadiness)

	mux.HandleFunc("POST /api/polka/webhooks", cfg.handlerWebhook)

	mux.Handle("POST /api/login", cfg.authLimiter.middleware(http.HandlerFunc(cfg.handlerLogin)))
	mux.HandleFunc("POST /api/refresh", cfg.handlerRefresh)
	mux.HandleFunc("POST /api/revoke", cfg.handlerRevoke)

	mux.Handle("POST /api/users", cfg.authLimiter.middleware(http.HandlerFunc(cfg.handlerUsersCreate)))
	mux.HandleFunc("PUT /api/users", cfg.handlerUsersUpdate)

	mux.HandleFunc("POST /api/chirps", cfg.handlerChirpsCreate)
	mux.HandleFunc("GET /api/chirps", cfg.handlerChirpsList)
	mux.HandleFunc("GET /api/chirps/{chirpID}", cfg.handlerChirpsGet)
	mux.HandleFunc("DELETE /api/chirps/{chirpID}", cfg.handlerChirpsDelete)

	mux.HandleFunc("POST /admin/reset", cfg.handlerReset)
	mux.HandleFunc("GET /admin/metrics", cfg.handlerMetrics)

	return requestLogger(securityHeaders(mux))
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(code int) {
	r.status = code
	r.ResponseWriter.WriteHeader(code)
}

func requestLogger(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rec, r)
		slog.Info("request", "method", r.Method, "path", r.URL.Path, "status", rec.status, "duration_ms", time.Since(start).Milliseconds())
	})
}

func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Referrer-Policy", "no-referrer")
		next.ServeHTTP(w, r)
	})
}
