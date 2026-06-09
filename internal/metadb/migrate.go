package metadb

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type Migration struct {
	Version string
	SQL     string
	Name    string
}

func LoadMigrations(dir string) ([]Migration, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	migrations := make([]Migration, 0)
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".sql") {
			continue
		}
		payload, err := os.ReadFile(filepath.Join(dir, entry.Name()))
		if err != nil {
			return nil, err
		}
		version := strings.TrimSuffix(entry.Name(), ".sql")
		if strings.TrimSpace(version) == "" {
			return nil, fmt.Errorf("empty migration version for %s", entry.Name())
		}
		migrations = append(migrations, Migration{Version: version, SQL: string(payload), Name: migrationName(version)})
	}
	sort.Slice(migrations, func(i, j int) bool {
		return migrations[i].Version < migrations[j].Version
	})
	return migrations, nil
}

func Apply(ctx context.Context, db *sql.DB, migrations []Migration) error {
	if _, err := db.ExecContext(ctx, `
	CREATE TABLE IF NOT EXISTS schema_migrations (
		    version TEXT PRIMARY KEY,
		    checksum TEXT NOT NULL DEFAULT '',
		    name TEXT NOT NULL DEFAULT '',
		    applied_at TIMESTAMPTZ NOT NULL DEFAULT now()
		)`); err != nil {
		return fmt.Errorf("ensure schema_migrations: %w", err)
	}
	if _, err := db.ExecContext(ctx, `ALTER TABLE schema_migrations ADD COLUMN IF NOT EXISTS checksum TEXT NOT NULL DEFAULT ''`); err != nil {
		return fmt.Errorf("ensure schema_migrations checksum: %w", err)
	}
	if _, err := db.ExecContext(ctx, `ALTER TABLE schema_migrations ADD COLUMN IF NOT EXISTS name TEXT NOT NULL DEFAULT ''`); err != nil {
		return fmt.Errorf("ensure schema_migrations name: %w", err)
	}

	for _, migration := range migrations {
		if strings.TrimSpace(migration.Version) == "" {
			return fmt.Errorf("migration version is required")
		}
		checksum := migrationChecksum(migration.SQL)
		name := strings.TrimSpace(migration.Name)
		if name == "" {
			name = migrationName(migration.Version)
		}
		tx, err := db.BeginTx(ctx, nil)
		if err != nil {
			return fmt.Errorf("begin migration %s: %w", migration.Version, err)
		}
		applied, storedChecksum, storedName, err := migrationRecord(ctx, tx, migration.Version)
		if err != nil {
			_ = tx.Rollback()
			return err
		}
		if applied {
			if storedChecksum == "" {
				if _, err := tx.ExecContext(ctx, `UPDATE schema_migrations SET checksum = $2, name = CASE WHEN name = '' THEN $3 ELSE name END WHERE version = $1 AND checksum = ''`, migration.Version, checksum, name); err != nil {
					_ = tx.Rollback()
					return fmt.Errorf("record checksum for migration %s: %w", migration.Version, err)
				}
			} else if storedChecksum != checksum {
				_ = tx.Rollback()
				return fmt.Errorf("migration %s checksum mismatch", migration.Version)
			} else if storedName == "" {
				if _, err := tx.ExecContext(ctx, `UPDATE schema_migrations SET name = $2 WHERE version = $1 AND name = ''`, migration.Version, name); err != nil {
					_ = tx.Rollback()
					return fmt.Errorf("record name for migration %s: %w", migration.Version, err)
				}
			}
			if err := tx.Commit(); err != nil {
				return fmt.Errorf("commit skipped migration %s: %w", migration.Version, err)
			}
			continue
		}
		if _, err := tx.ExecContext(ctx, migration.SQL); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("apply migration %s: %w", migration.Version, err)
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO schema_migrations (version, checksum, name) VALUES ($1, $2, $3)`, migration.Version, checksum, name); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("record migration %s: %w", migration.Version, err)
		}
		if err := tx.Commit(); err != nil {
			return fmt.Errorf("commit migration %s: %w", migration.Version, err)
		}
	}
	return nil
}

func migrationRecord(ctx context.Context, tx *sql.Tx, version string) (bool, string, string, error) {
	var checksum string
	var name string
	if err := tx.QueryRowContext(ctx, `SELECT checksum, name FROM schema_migrations WHERE version = $1`, version).Scan(&checksum, &name); err != nil {
		if err == sql.ErrNoRows {
			return false, "", "", nil
		}
		return false, "", "", fmt.Errorf("check migration %s: %w", version, err)
	}
	return true, checksum, name, nil
}

func migrationChecksum(sql string) string {
	sum := sha256.Sum256([]byte(sql))
	return hex.EncodeToString(sum[:])
}

func migrationName(version string) string {
	version = strings.TrimSpace(version)
	prefix, suffix, ok := strings.Cut(version, "_")
	if ok && strings.Trim(prefix, "0123456789") == "" && strings.TrimSpace(suffix) != "" {
		return suffix
	}
	return version
}
