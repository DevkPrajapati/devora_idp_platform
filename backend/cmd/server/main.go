package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/idp/platform/backend/internal/config"
	"github.com/idp/platform/backend/internal/database"
	"github.com/idp/platform/backend/internal/logging"
	"github.com/idp/platform/backend/internal/server"
	"go.uber.org/zap"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		panic("failed to load config: " + err.Error())
	}

	logger, err := logging.NewLogger(cfg.Log)
	if err != nil {
		panic("failed to create logger: " + err.Error())
	}
	defer logger.Sync() //nolint:errcheck

	logger.Info("loaded configuration",
		zap.Bool("auth.enabled", cfg.Auth.Enabled),
		zap.String("database.url", cfg.Database.URL),
		zap.String("kubernetes.kubeconfig", cfg.Kubernetes.Kubeconfig),
	)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	poolCtx, poolCancel := context.WithTimeout(ctx, 15*time.Second)
	defer poolCancel()
	pool, err := database.NewPool(poolCtx, cfg.Database)
	if err != nil {
		logger.Fatal("failed to connect to database", zap.Error(err))
	}

	srv := server.New(cfg, logger, pool)

	go func() {
		if err := srv.Start(); err != nil {
			logger.Fatal("server error", zap.Error(err))
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	shutdownCtx, shutdownCancel := context.WithTimeout(ctx, cfg.Server.ShutdownTimeout)
	defer shutdownCancel()

	if err := srv.Shutdown(shutdownCtx); err != nil {
		logger.Error("shutdown error", zap.Error(err))
		os.Exit(1)
	}

	logger.Info("server stopped gracefully")
}
