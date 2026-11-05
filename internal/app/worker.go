package app

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"github.com/biairmal/go-sdk/lib/logger"
	"github.com/biairmal/go-sdk/lib/sqlkit"
)

// RunWorker runs the background jobs until SIGINT, SIGTERM or ctx is done.
// No job is registered yet, so it only holds the database connection open.
func RunWorker(ctx context.Context, cfg *Config, log logger.Logger) error {
	ctx, stop := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer stop()

	db, err := sqlkit.New(ctx, &cfg.Database)
	if err != nil {
		return err
	}
	defer func() {
		if err := db.Close(); err != nil {
			log.Error("database close", logger.F("error", err.Error()))
		}
	}()

	log.Info("worker started")
	<-ctx.Done()
	log.Info("worker stopped")
	return nil
}
