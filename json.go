package main

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
)

const maxBodyBytes = 1 << 20

func respondWithError(w http.ResponseWriter, code int, msg string, err error) {
	if code >= 500 {
		slog.Error("server error", "msg", msg, "err", err)
	} else if err != nil {
		slog.Debug("client error", "msg", msg, "err", err)
	}
	respondWithJSON(w, code, map[string]string{"error": msg})
}

func respondWithJSON(w http.ResponseWriter, code int, payload any) {
	dat, err := json.Marshal(payload)
	if err != nil {
		slog.Error("marshal json", "err", err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_, _ = w.Write(dat)
}

// decodeJSON reads a size-limited JSON body. Bad input is a 400, not a 500.
func decodeJSON(w http.ResponseWriter, r *http.Request, dst any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, maxBodyBytes)
	if err := json.NewDecoder(r.Body).Decode(dst); err != nil {
		var tooBig *http.MaxBytesError
		if errors.As(err, &tooBig) {
			respondWithError(w, http.StatusRequestEntityTooLarge, "Request body too large", err)
			return false
		}
		respondWithError(w, http.StatusBadRequest, "Invalid JSON body", err)
		return false
	}
	return true
}
