package db

import (
	"database/sql"
	"embed"
	"fmt"
	"log"
	"sort"
	"strconv"
	"strings"

	_ "github.com/lib/pq"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

func Connect(databaseURL string) (*sql.DB, error) {
	db, err := sql.Open("postgres", databaseURL)
	if err != nil {
		return nil, fmt.Errorf("open db: %w", err)
	}
	if err := db.Ping(); err != nil {
		return nil, fmt.Errorf("ping db: %w", err)
	}
	return db, nil
}

func RunMigrations(db *sql.DB) error {
	_, err := db.Exec(`CREATE TABLE IF NOT EXISTS schema_migrations (version INT PRIMARY KEY)`)
	if err != nil {
		return fmt.Errorf("create migrations table: %w", err)
	}

	entries, err := migrationsFS.ReadDir("migrations")
	if err != nil {
		return fmt.Errorf("read migrations dir: %w", err)
	}

	type migration struct {
		version int
		path    string
	}
	var migrations []migration
	seenVersions := map[int]string{}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".sql") {
			continue
		}
		parts := strings.SplitN(e.Name(), "_", 2)
		ver, err := strconv.Atoi(parts[0])
		if err != nil {
			continue
		}
		path := "migrations/" + e.Name()
		if existing, ok := seenVersions[ver]; ok {
			return fmt.Errorf("duplicate migration version %d: %s and %s", ver, existing, path)
		}
		seenVersions[ver] = path
		migrations = append(migrations, migration{version: ver, path: path})
	}
	sort.Slice(migrations, func(i, j int) bool {
		return migrations[i].version < migrations[j].version
	})

	for _, m := range migrations {
		content, err := migrationsFS.ReadFile(m.path)
		if err != nil {
			return fmt.Errorf("read migration %s: %w", m.path, err)
		}

		tx, err := db.Begin()
		if err != nil {
			return fmt.Errorf("begin migration %s: %w", m.path, err)
		}
		if _, err := tx.Exec("SELECT pg_advisory_xact_lock($1)", int64(417341771)); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("lock migration %d: %w", m.version, err)
		}

		var exists bool
		if err := tx.QueryRow("SELECT EXISTS(SELECT 1 FROM schema_migrations WHERE version=$1)", m.version).Scan(&exists); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("check migration %d: %w", m.version, err)
		}
		if exists {
			_ = tx.Rollback()
			continue
		}

		log.Printf("Running migration %s", m.path)
		if _, err := tx.Exec(string(content)); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("run migration %s: %w", m.path, err)
		}

		if _, err := tx.Exec("INSERT INTO schema_migrations (version) VALUES ($1)", m.version); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("record migration %d: %w", m.version, err)
		}
		if err := tx.Commit(); err != nil {
			return fmt.Errorf("commit migration %d: %w", m.version, err)
		}
	}

	return nil
}
