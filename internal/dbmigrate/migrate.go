package dbmigrate

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	migrate "github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/pgx/v5"
	_ "github.com/golang-migrate/migrate/v4/source/file"
)

// ErrNoChange is returned when the database is already at the requested version.
var ErrNoChange = migrate.ErrNoChange

// toPgx5MigrateURL rewrites postgres:// URLs for golang-migrate's pgx/v5 driver.
func toPgx5MigrateURL(databaseURL string) string {
	if strings.HasPrefix(databaseURL, "postgres://") {
		return "pgx5://" + strings.TrimPrefix(databaseURL, "postgres://")
	}
	if strings.HasPrefix(databaseURL, "postgresql://") {
		return "pgx5://" + strings.TrimPrefix(databaseURL, "postgresql://")
	}
	return databaseURL
}

// fileSourceURL returns a file:// URL for the migrations directory.
func fileSourceURL(migrationsDir string) (string, error) {
	abs, err := filepath.Abs(migrationsDir)
	if err != nil {
		return "", fmt.Errorf("migrations path: %w", err)
	}
	// file:///absolute/path (forward slashes)
	return "file://" + filepath.ToSlash(abs), nil
}

// Open returns a migrate instance. Caller must Close().
func Open(databaseURL, migrationsDir string) (*migrate.Migrate, error) {
	src, err := fileSourceURL(migrationsDir)
	if err != nil {
		return nil, err
	}
	dbURL := toPgx5MigrateURL(databaseURL)
	m, err := migrate.New(src, dbURL)
	if err != nil {
		return nil, fmt.Errorf("migrate new: %w", err)
	}
	return m, nil
}

// Up applies all pending migrations.
func Up(databaseURL, migrationsDir string) error {
	m, err := Open(databaseURL, migrationsDir)
	if err != nil {
		return err
	}
	defer m.Close()

	if err := m.Up(); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		return err
	}
	return nil
}

// Down rolls back migrations (one step by default when steps is 0).
func Down(databaseURL, migrationsDir string, steps int) error {
	m, err := Open(databaseURL, migrationsDir)
	if err != nil {
		return err
	}
	defer m.Close()

	if steps <= 0 {
		steps = 1
	}
	if err := m.Steps(-steps); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		return err
	}
	return nil
}

// Version returns the current migration version and dirty flag.
func Version(databaseURL, migrationsDir string) (version uint, dirty bool, err error) {
	m, err := Open(databaseURL, migrationsDir)
	if err != nil {
		return 0, false, err
	}
	defer m.Close()
	return m.Version()
}
