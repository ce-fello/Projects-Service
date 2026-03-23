package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"Projects_Service/internal/application"
	"Projects_Service/internal/config"
	"Projects_Service/internal/platform/auth"
	"Projects_Service/internal/platform/postgres"
	transporthttp "Projects_Service/internal/transport/http"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))

	cfg, err := config.Load()
	if err != nil {
		logger.Error("load config", slog.Any("error", err))
		os.Exit(1)
	}

	ctx := context.Background()
	db, err := postgres.Open(ctx, cfg.DatabaseURL)
	if err != nil {
		logger.Error("open database", slog.Any("error", err))
		os.Exit(1)
	}
	defer db.Close()

	if err := postgres.RunMigrations(ctx, db); err != nil {
		logger.Error("run migrations", slog.Any("error", err))
		os.Exit(1)
	}

	if cfg.EnableDemoSeed {
		if err := postgres.Seed(ctx, db); err != nil {
			logger.Error("seed database", slog.Any("error", err))
			os.Exit(1)
		}
	}

	userRepository := postgres.NewUserRepository(db)
	tokenManager := auth.NewTokenManager(cfg.JWTSecret)
	service := application.NewService(
		userRepository,
		postgres.NewProjectTypeRepository(db),
		postgres.NewExternalApplicationRepository(db),
		postgres.NewTransactor(db),
		tokenManager,
	)

	server := newHTTPServer(
		logger,
		cfg.HTTPPort,
		transporthttp.NewHandler(logger, service, userRepository, tokenManager),
	)

	go func() {
		logger.Info("server starting", slog.String("addr", server.Addr))
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Error("server stopped", slog.Any("error", err))
			os.Exit(1)
		}
	}()

	shutdownCtx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	<-shutdownCtx.Done()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := server.Shutdown(ctx); err != nil {
		logger.Error("shutdown server", slog.Any("error", err))
		os.Exit(1)
	}
}

func newHTTPServer(_ *slog.Logger, port string, handler http.Handler) *http.Server {
	return &http.Server{
		Addr:              ":" + port,
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      10 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
}
