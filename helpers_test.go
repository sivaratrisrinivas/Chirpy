package main

import (
	"time"

	"github.com/google/uuid"
	"github.com/sivaratrisrinivas/Chirpy/internal/database"
)

func databaseChirp(id uuid.UUID, ts time.Time) database.Chirp {
	return database.Chirp{ID: id, CreatedAt: ts}
}
