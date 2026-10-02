package main

import (
	"errors"
	"os"
)

type config struct {
	DBURL       string
	Platform    string
	JWTSecret   string
	PolkaKey    string
	Port        string
	AutoMigrate bool
}

func configFromEnv() (config, error) {
	c := config{
		DBURL:       os.Getenv("DB_URL"),
		Platform:    os.Getenv("PLATFORM"),
		JWTSecret:   os.Getenv("JWT_SECRET"),
		PolkaKey:    os.Getenv("POLKA_KEY"),
		Port:        os.Getenv("PORT"),
		AutoMigrate: os.Getenv("AUTO_MIGRATE") == "true",
	}
	if c.Port == "" {
		c.Port = "8080"
	}
	if c.Platform == "" {
		c.Platform = "prod"
	}
	switch {
	case c.DBURL == "":
		return c, errors.New("DB_URL must be set")
	case len(c.JWTSecret) < 32:
		return c, errors.New("JWT_SECRET must be set and at least 32 characters")
	case c.PolkaKey == "":
		return c, errors.New("POLKA_KEY must be set")
	}
	return c, nil
}
