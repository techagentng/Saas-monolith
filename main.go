package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	// Embeds the IANA timezone database in the binary as a fallback for when
	// the host has no system zoneinfo — common in minimal containers and on
	// Windows. Tenant profile updates and onboarding completion both reject a
	// timezone that time.LoadLocation cannot resolve, so without this a
	// deployment could refuse every valid timezone and make onboarding
	// impossible to finish. Costs ~450KB and is only consulted when the
	// system database is missing.
	_ "time/tzdata"

	"github.com/joho/godotenv"
	"github.com/techagentng/saas-monolith/internal/app"
	"github.com/techagentng/saas-monolith/internal/config"
)

func main() {
	if err := run(); err != nil {
		log.Printf("server stopped: %v", err)
		os.Exit(1)
	}
}

func run() error {
	// Best effort: a missing .env is normal outside development, and real
	// environment variables always take precedence over the file.
	_ = godotenv.Load()
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("loading configuration: %w", err)
	}
	log.Printf("starting API environment=%s host=%s port=%d postgres_host=%s postgres_port=%d database=%s", cfg.Env, cfg.Host, cfg.Port, cfg.PostgresHost, cfg.PostgresPort, cfg.PostgresDB)
	application, err := app.New(context.Background(), cfg)
	if err != nil {
		return fmt.Errorf("initializing application: %w", err)
	}
	defer application.Close()
	serverErrors := make(chan error, 1)
	go func() { serverErrors <- application.Server.ListenAndServe() }()
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, syscall.SIGINT, syscall.SIGTERM)
	select {
	case err := <-serverErrors:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	case <-signals:
		shutdownContext, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		return application.Server.Shutdown(shutdownContext)
	}
}
