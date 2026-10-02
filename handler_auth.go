package main

import (
	"net/http"
	"time"

	"github.com/sivaratrisrinivas/Chirpy/internal/auth"
	"github.com/sivaratrisrinivas/Chirpy/internal/database"
)

// Bcrypt hash of a random string. Comparing against it when the email is unknown
// keeps login timing the same whether or not the account exists.
var dummyHash, _ = auth.HashPassword("chirpy-timing-equalizer")

func (cfg *apiConfig) handlerLogin(w http.ResponseWriter, r *http.Request) {
	var params credentials
	if !decodeJSON(w, r, &params) {
		return
	}
	user, err := cfg.db.GetUserByEmail(r.Context(), params.Email)
	if err != nil {
		_ = auth.CheckPasswordHash(params.Password, dummyHash)
		respondWithError(w, http.StatusUnauthorized, "Incorrect email or password", err)
		return
	}
	if err := auth.CheckPasswordHash(params.Password, user.HashedPassword); err != nil {
		respondWithError(w, http.StatusUnauthorized, "Incorrect email or password", err)
		return
	}

	accessToken, err := auth.MakeJWT(user.ID, cfg.jwtSecret, cfg.accessTTL)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Couldn't create access token", err)
		return
	}
	refreshToken, err := auth.MakeRefreshToken()
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Couldn't create refresh token", err)
		return
	}
	// Only a SHA-256 of the refresh token is stored, so a database leak does not leak sessions.
	_, err = cfg.db.CreateRefreshToken(r.Context(), database.CreateRefreshTokenParams{
		UserID:    user.ID,
		Token:     auth.HashToken(refreshToken),
		ExpiresAt: time.Now().UTC().Add(cfg.refreshTTL),
	})
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Couldn't save refresh token", err)
		return
	}

	respondWithJSON(w, http.StatusOK, struct {
		User
		Token        string `json:"token"`
		RefreshToken string `json:"refresh_token"`
	}{userFromDB(user), accessToken, refreshToken})
}

func (cfg *apiConfig) handlerRefresh(w http.ResponseWriter, r *http.Request) {
	refreshToken, err := auth.GetBearerToken(r.Header)
	if err != nil {
		respondWithError(w, http.StatusUnauthorized, "Missing refresh token", err)
		return
	}
	user, err := cfg.db.GetUserFromRefreshToken(r.Context(), auth.HashToken(refreshToken))
	if err != nil {
		respondWithError(w, http.StatusUnauthorized, "Invalid refresh token", err)
		return
	}
	accessToken, err := auth.MakeJWT(user.ID, cfg.jwtSecret, cfg.accessTTL)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Couldn't create access token", err)
		return
	}
	respondWithJSON(w, http.StatusOK, map[string]string{"token": accessToken})
}

func (cfg *apiConfig) handlerRevoke(w http.ResponseWriter, r *http.Request) {
	refreshToken, err := auth.GetBearerToken(r.Header)
	if err != nil {
		respondWithError(w, http.StatusUnauthorized, "Missing refresh token", err)
		return
	}
	if _, err := cfg.db.RevokeRefreshToken(r.Context(), auth.HashToken(refreshToken)); err != nil {
		respondWithError(w, http.StatusUnauthorized, "Invalid refresh token", err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
