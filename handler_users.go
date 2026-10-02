package main

import (
	"database/sql"
	"errors"
	"net/http"
	"net/mail"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/lib/pq"
	"github.com/sivaratrisrinivas/Chirpy/internal/auth"
	"github.com/sivaratrisrinivas/Chirpy/internal/database"
)

type User struct {
	ID          uuid.UUID `json:"id"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
	Email       string    `json:"email"`
	IsChirpyRed bool      `json:"is_chirpy_red"`
}

func userFromDB(u database.User) User {
	return User{ID: u.ID, CreatedAt: u.CreatedAt, UpdatedAt: u.UpdatedAt, Email: u.Email, IsChirpyRed: u.IsChirpyRed}
}

type credentials struct {
	Password string `json:"password"`
	Email    string `json:"email"`
}

func (c *credentials) validate() error {
	c.Email = strings.ToLower(strings.TrimSpace(c.Email))
	addr, err := mail.ParseAddress(c.Email)
	if err != nil || addr.Address != c.Email {
		return errors.New("Invalid email address")
	}
	if len(c.Password) < 8 {
		return errors.New("Password must be at least 8 characters")
	}
	if len(c.Password) > 72 {
		return errors.New("Password must be at most 72 bytes")
	}
	return nil
}

func isUniqueViolation(err error) bool {
	var pqErr *pq.Error
	return errors.As(err, &pqErr) && pqErr.Code == "23505"
}

func (cfg *apiConfig) handlerUsersCreate(w http.ResponseWriter, r *http.Request) {
	var params credentials
	if !decodeJSON(w, r, &params) {
		return
	}
	if err := params.validate(); err != nil {
		respondWithError(w, http.StatusBadRequest, err.Error(), err)
		return
	}
	hashed, err := auth.HashPassword(params.Password)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Couldn't hash password", err)
		return
	}
	user, err := cfg.db.CreateUser(r.Context(), database.CreateUserParams{Email: params.Email, HashedPassword: hashed})
	if err != nil {
		if isUniqueViolation(err) {
			respondWithError(w, http.StatusConflict, "Email already registered", err)
			return
		}
		respondWithError(w, http.StatusInternalServerError, "Couldn't create user", err)
		return
	}
	respondWithJSON(w, http.StatusCreated, userFromDB(user))
}

func (cfg *apiConfig) handlerUsersUpdate(w http.ResponseWriter, r *http.Request) {
	userID, ok := cfg.authenticate(w, r)
	if !ok {
		return
	}
	var params credentials
	if !decodeJSON(w, r, &params) {
		return
	}
	if err := params.validate(); err != nil {
		respondWithError(w, http.StatusBadRequest, err.Error(), err)
		return
	}
	hashed, err := auth.HashPassword(params.Password)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Couldn't hash password", err)
		return
	}
	user, err := cfg.db.UpdateUser(r.Context(), database.UpdateUserParams{ID: userID, Email: params.Email, HashedPassword: hashed})
	if err != nil {
		if isUniqueViolation(err) {
			respondWithError(w, http.StatusConflict, "Email already registered", err)
			return
		}
		if errors.Is(err, sql.ErrNoRows) {
			respondWithError(w, http.StatusNotFound, "User not found", err)
			return
		}
		respondWithError(w, http.StatusInternalServerError, "Couldn't update user", err)
		return
	}
	respondWithJSON(w, http.StatusOK, userFromDB(user))
}

// authenticate validates the bearer access token and returns the user ID.
func (cfg *apiConfig) authenticate(w http.ResponseWriter, r *http.Request) (uuid.UUID, bool) {
	token, err := auth.GetBearerToken(r.Header)
	if err != nil {
		respondWithError(w, http.StatusUnauthorized, "Missing bearer token", err)
		return uuid.Nil, false
	}
	userID, err := auth.ValidateJWT(token, cfg.jwtSecret)
	if err != nil {
		respondWithError(w, http.StatusUnauthorized, "Invalid or expired token", err)
		return uuid.Nil, false
	}
	return userID, true
}
