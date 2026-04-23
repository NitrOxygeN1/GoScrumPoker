// Command migrate runs golang-migrate against DATABASE_URL.
//
// Usage:
//
//	go run ./cmd/migrate -path migrations up
//	go run ./cmd/migrate -path migrations down
//	go run ./cmd/migrate -path migrations down -steps 2
//	go run ./cmd/migrate -path migrations version
//
// Environment:
//
//	DATABASE_URL — required (postgres:// or postgresql://)
package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/rs/zerolog"

	"GoScrumPoker/internal/dbmigrate"
)

func main() {
	log := zerolog.New(os.Stdout).Level(zerolog.InfoLevel).With().Timestamp().Str("component", "migrate").Logger()

	path := flag.String("path", envOr("MIGRATIONS_PATH", "migrations"), "path to migrations directory")
	database := flag.String("database", os.Getenv("DATABASE_URL"), "database URL (default: DATABASE_URL)")
	steps := flag.Int("steps", 1, "number of migrations for down")
	flag.Usage = func() {
		_, _ = fmt.Fprintf(os.Stderr, "Usage: %s [flags] <command>\n", os.Args[0])
		_, _ = fmt.Fprintf(os.Stderr, "Commands: up | down | version\n")
		flag.PrintDefaults()
	}
	flag.Parse()

	if *database == "" {
		log.Fatal().Msg("database URL required: set -database or DATABASE_URL")
	}

	args := flag.Args()
	if len(args) != 1 {
		flag.Usage()
		os.Exit(2)
	}

	cmd := args[0]
	switch cmd {
	case "up":
		if err := dbmigrate.Up(*database, *path); err != nil {
			log.Fatal().Err(err).Msg("migrate up failed")
		}
		log.Info().Msg("migrations applied (up OK)")
	case "down":
		if err := dbmigrate.Down(*database, *path, *steps); err != nil {
			log.Fatal().Err(err).Msg("migrate down failed")
		}
		log.Info().Msg("migrations reverted (down OK)")
	case "version":
		v, dirty, err := dbmigrate.Version(*database, *path)
		if err != nil {
			log.Fatal().Err(err).Msg("migrate version failed")
		}
		log.Info().Uint("version", v).Bool("dirty", dirty).Msg("migration version")
	default:
		flag.Usage()
		os.Exit(2)
	}
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
