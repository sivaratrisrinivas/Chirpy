package main

import (
	"database/sql"
	"encoding/base64"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/sivaratrisrinivas/Chirpy/internal/database"
)

const (
	maxChirpLength   = 140
	defaultPageLimit = 50
	maxPageLimit     = 100
)

type Chirp struct {
	ID        uuid.UUID `json:"id"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
	UserID    uuid.UUID `json:"user_id"`
	Body      string    `json:"body"`
}

func chirpFromDB(c database.Chirp) Chirp {
	return Chirp{ID: c.ID, CreatedAt: c.CreatedAt, UpdatedAt: c.UpdatedAt, UserID: c.UserID, Body: c.Body}
}

func (cfg *apiConfig) handlerChirpsCreate(w http.ResponseWriter, r *http.Request) {
	userID, ok := cfg.authenticate(w, r)
	if !ok {
		return
	}
	var params struct {
		Body string `json:"body"`
	}
	if !decodeJSON(w, r, &params) {
		return
	}
	cleaned, err := validateChirp(params.Body)
	if err != nil {
		respondWithError(w, http.StatusBadRequest, err.Error(), err)
		return
	}
	chirp, err := cfg.db.CreateChirp(r.Context(), database.CreateChirpParams{UserID: userID, Body: cleaned})
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Couldn't create chirp", err)
		return
	}
	respondWithJSON(w, http.StatusCreated, chirpFromDB(chirp))
}

func validateChirp(body string) (string, error) {
	body = strings.TrimSpace(body)
	if body == "" {
		return "", errors.New("Chirp is empty")
	}
	// Count characters, not bytes, so emoji and non-Latin text get the full 140.
	if utf8.RuneCountInString(body) > maxChirpLength {
		return "", errors.New("Chirp is too long")
	}
	badWords := map[string]struct{}{"kerfuffle": {}, "sharbert": {}, "fornax": {}}
	return getCleanedBody(body, badWords), nil
}

func getCleanedBody(body string, badWords map[string]struct{}) string {
	words := strings.Split(body, " ")
	for i, word := range words {
		if _, ok := badWords[strings.ToLower(word)]; ok {
			words[i] = "****"
		}
	}
	return strings.Join(words, " ")
}

func (cfg *apiConfig) handlerChirpsGet(w http.ResponseWriter, r *http.Request) {
	chirpID, err := uuid.Parse(r.PathValue("chirpID"))
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Invalid chirp ID", err)
		return
	}
	c, err := cfg.db.GetChirp(r.Context(), chirpID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			respondWithError(w, http.StatusNotFound, "Chirp not found", err)
			return
		}
		respondWithError(w, http.StatusInternalServerError, "Couldn't get chirp", err)
		return
	}
	respondWithJSON(w, http.StatusOK, chirpFromDB(c))
}

func encodeCursor(c database.Chirp) string {
	raw := c.CreatedAt.UTC().Format(time.RFC3339Nano) + "|" + c.ID.String()
	return base64.RawURLEncoding.EncodeToString([]byte(raw))
}

func decodeCursor(s string) (time.Time, uuid.UUID, error) {
	raw, err := base64.RawURLEncoding.DecodeString(s)
	if err != nil {
		return time.Time{}, uuid.Nil, err
	}
	ts, id, found := strings.Cut(string(raw), "|")
	if !found {
		return time.Time{}, uuid.Nil, errors.New("malformed cursor")
	}
	t, err := time.Parse(time.RFC3339Nano, ts)
	if err != nil {
		return time.Time{}, uuid.Nil, err
	}
	u, err := uuid.Parse(id)
	return t, u, err
}

// handlerChirpsList returns chirps filtered and paginated in SQL.
// Query params: author_id, sort=asc|desc, limit (1-100), cursor (from X-Next-Cursor).
func (cfg *apiConfig) handlerChirpsList(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()

	sortOrder := q.Get("sort")
	if sortOrder == "" {
		sortOrder = "asc"
	}
	if sortOrder != "asc" && sortOrder != "desc" {
		respondWithError(w, http.StatusBadRequest, "Invalid sort order. Use 'asc' or 'desc'", nil)
		return
	}

	limit := defaultPageLimit
	if s := q.Get("limit"); s != "" {
		n, err := strconv.Atoi(s)
		if err != nil || n < 1 || n > maxPageLimit {
			respondWithError(w, http.StatusBadRequest, "limit must be between 1 and 100", err)
			return
		}
		limit = n
	}

	var author uuid.NullUUID
	if s := q.Get("author_id"); s != "" {
		id, err := uuid.Parse(s)
		if err != nil {
			respondWithError(w, http.StatusBadRequest, "Invalid author ID format", err)
			return
		}
		author = uuid.NullUUID{UUID: id, Valid: true}
	}

	var curTime sql.NullTime
	var curID uuid.NullUUID
	if s := q.Get("cursor"); s != "" {
		t, id, err := decodeCursor(s)
		if err != nil {
			respondWithError(w, http.StatusBadRequest, "Invalid cursor", err)
			return
		}
		curTime = sql.NullTime{Time: t, Valid: true}
		curID = uuid.NullUUID{UUID: id, Valid: true}
	}

	// Fetch one extra row to know whether another page exists.
	var rows []database.Chirp
	var err error
	if sortOrder == "desc" {
		rows, err = cfg.db.ListChirpsDesc(r.Context(), database.ListChirpsDescParams{
			AuthorID: author, CursorCreatedAt: curTime, CursorID: curID, RowLimit: int32(limit + 1),
		})
	} else {
		rows, err = cfg.db.ListChirpsAsc(r.Context(), database.ListChirpsAscParams{
			AuthorID: author, CursorCreatedAt: curTime, CursorID: curID, RowLimit: int32(limit + 1),
		})
	}
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Couldn't retrieve chirps", err)
		return
	}

	if len(rows) > limit {
		rows = rows[:limit]
		w.Header().Set("X-Next-Cursor", encodeCursor(rows[len(rows)-1]))
	}
	out := make([]Chirp, 0, len(rows))
	for _, c := range rows {
		out = append(out, chirpFromDB(c))
	}
	respondWithJSON(w, http.StatusOK, out)
}

func (cfg *apiConfig) handlerChirpsDelete(w http.ResponseWriter, r *http.Request) {
	chirpID, err := uuid.Parse(r.PathValue("chirpID"))
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Invalid chirp ID", err)
		return
	}
	userID, ok := cfg.authenticate(w, r)
	if !ok {
		return
	}
	// Ownership is enforced in the DELETE itself, so there is no check-then-act race.
	n, err := cfg.db.DeleteChirpByOwner(r.Context(), database.DeleteChirpByOwnerParams{ID: chirpID, UserID: userID})
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Couldn't delete chirp", err)
		return
	}
	if n == 0 {
		if _, err := cfg.db.GetChirp(r.Context(), chirpID); errors.Is(err, sql.ErrNoRows) {
			respondWithError(w, http.StatusNotFound, "Chirp not found", err)
			return
		}
		respondWithError(w, http.StatusForbidden, "You can't delete this chirp", nil)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
