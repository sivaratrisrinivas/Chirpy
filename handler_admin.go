package main

import (
	"context"
	"fmt"
	"net/http"
	"time"
)

func (cfg *apiConfig) handlerReadiness(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()
	if err := cfg.sqlDB.PingContext(ctx); err != nil {
		respondWithError(w, http.StatusServiceUnavailable, "database unavailable", err)
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("OK"))
}

func (cfg *apiConfig) handlerMetrics(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	fmt.Fprintf(w, "<html><body><h1>Welcome, Chirpy Admin</h1><p>Chirpy has been visited %d times!</p></body></html>", cfg.fileserverHits.Load())
}

func (cfg *apiConfig) middlewareMetricsInc(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cfg.fileserverHits.Add(1)
		next.ServeHTTP(w, r)
	})
}

func (cfg *apiConfig) handlerReset(w http.ResponseWriter, r *http.Request) {
	if cfg.platform != "dev" {
		respondWithError(w, http.StatusForbidden, "Reset is only allowed in dev", nil)
		return
	}
	cfg.fileserverHits.Store(0)
	if err := cfg.db.Reset(r.Context()); err != nil {
		respondWithError(w, http.StatusInternalServerError, "Couldn't reset database", err)
		return
	}
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("Hits reset to 0 and database reset to initial state."))
}
