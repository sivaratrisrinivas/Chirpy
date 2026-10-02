package main

import (
	"context"
	"database/sql"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/joho/godotenv"
	_ "github.com/lib/pq"
)

func main() {
	_ = godotenv.Load()
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	slog.SetDefault(logger)

	cfg, err := configFromEnv()
	if err != nil {
		slog.Error("invalid configuration", "err", err)
		os.Exit(1)
	}

	db, err := sql.Open("postgres", cfg.DBURL)
	if err != nil {
		slog.Error("open database", "err", err)
		os.Exit(1)
	}
	db.SetMaxOpenConns(20)
	db.SetMaxIdleConns(5)
	db.SetConnMaxLifetime(30 * time.Minute)

	if cfg.AutoMigrate {
		if err := migrate(db); err != nil {
			slog.Error("migrate", "err", err)
			os.Exit(1)
		}
	}

	api := newAPI(db, cfg)
	srv := &http.Server{
		Addr:              ":" + cfg.Port,
		Handler:           api.routes(),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      15 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	go func() {
		slog.Info("listening", "port", cfg.Port, "platform", cfg.Platform)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			slog.Error("server", "err", err)
			os.Exit(1)
		}
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	<-stop
	slog.Info("shutting down")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil {
		slog.Error("shutdown", "err", err)
	}
	_ = db.Close()
}
