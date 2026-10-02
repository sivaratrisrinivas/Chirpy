package main

import (
	"crypto/subtle"
	"database/sql"
	"errors"
	"net/http"

	"github.com/google/uuid"
	"github.com/sivaratrisrinivas/Chirpy/internal/auth"
)

// handlerWebhook receives payment events from Polka. Upgrading is idempotent:
// a retried "user.upgraded" event sets the same flag again and returns 204.
func (cfg *apiConfig) handlerWebhook(w http.ResponseWriter, r *http.Request) {
	apiKey, err := auth.GetAPIKey(r.Header)
	if err != nil {
		respondWithError(w, http.StatusUnauthorized, "Missing API key", err)
		return
	}
	if subtle.ConstantTimeCompare([]byte(apiKey), []byte(cfg.polkaKey)) != 1 {
		respondWithError(w, http.StatusUnauthorized, "Invalid API key", nil)
		return
	}

	var params struct {
		Event string `json:"event"`
		Data  struct {
			UserID uuid.UUID `json:"user_id"`
		} `json:"data"`
	}
	if !decodeJSON(w, r, &params) {
		return
	}
	if params.Event != "user.upgraded" {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if _, err := cfg.db.UpgradeToChirpyRed(r.Context(), params.Data.UserID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			respondWithError(w, http.StatusNotFound, "Couldn't find user", err)
			return
		}
		respondWithError(w, http.StatusInternalServerError, "Couldn't update user", err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
