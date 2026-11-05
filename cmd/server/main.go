// Command server runs the service: serve (default) | worker | migrate.
package main

import (
	"context"
	"errors"
	"fmt"
	"os"

	"github.com/biairmal/go-service-template/internal/app"
)

// Config locations, relative to the working directory.
const (
	envFile    = ".env"
	configFile = "config/config.yaml"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	cmd := "serve"
	if len(args) > 0 {
		cmd = args[0]
	}
	cfg, err := app.LoadConfig(envFile, configFile)
	if err != nil {
		return err
	}
	log := app.NewLogger(&cfg)
	ctx := context.Background()

	switch cmd {
	case "serve":
		a, err := app.New(ctx, &cfg, log)
		if err != nil {
			return err
		}
		return a.Serve(ctx)
	case "worker":
		return app.RunWorker(ctx, &cfg, log)
	case "migrate":
		return errors.New("migrate is not implemented yet")
	default:
		return fmt.Errorf("unknown command %q (want serve, worker or migrate)", cmd)
	}
}
