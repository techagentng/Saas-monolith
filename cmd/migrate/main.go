package main

import (
	"context"
	"fmt"
	"log"

	"github.com/joho/godotenv"
	"github.com/techagentng/saas-monolith/internal/config"
	"github.com/techagentng/saas-monolith/internal/database"
)

func main() {
	// Best effort: a missing .env is normal outside development, and real
	// environment variables always take precedence over the file.
	_ = godotenv.Load()
	cfg, err := config.Load()
	if err != nil {
		log.Fatal(err)
	}
	db, err := database.Open(context.Background(), cfg)
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()
	if err := database.ApplyMigrations(context.Background(), db, cfg.MigrationsDir); err != nil {
		log.Fatal(err)
	}
	fmt.Println("migrations applied")
}
