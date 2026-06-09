package metadb

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func TestLoadMigrationsSorted(t *testing.T) {
	dir := t.TempDir()
	for name, body := range map[string]string{
		"0002_second.sql": "CREATE TABLE second (id text);",
		"0001_first.sql":  "CREATE TABLE first (id text);",
		"README.md":       "ignored",
	} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	migrations, err := LoadMigrations(dir)
	if err != nil {
		t.Fatalf("load migrations: %v", err)
	}
	if len(migrations) != 2 {
		t.Fatalf("expected two migrations, got %d", len(migrations))
	}
	if migrations[0].Version != "0001_first" || migrations[1].Version != "0002_second" {
		t.Fatalf("unexpected migration order: %#v", migrations)
	}
	if migrations[0].Name != "first" || migrations[1].Name != "second" {
		t.Fatalf("unexpected migration names: %#v", migrations)
	}
}

func TestApplyMigrationsSkipsAlreadyAppliedVersions(t *testing.T) {
	db := openFakeDB(t)
	state := fakeMigrationStateFor(db)
	state.applied["0001_first"] = migrationChecksum("CREATE TABLE first (id text);")

	err := Apply(context.Background(), db, []Migration{
		{Version: "0001_first", SQL: "CREATE TABLE first (id text);"},
		{Version: "0002_second", SQL: "CREATE TABLE second (id text);"},
	})
	if err != nil {
		t.Fatalf("apply migrations: %v", err)
	}
	if _, ok := state.applied["0001_first"]; !ok {
		t.Fatalf("expected first migration recorded: %#v", state.applied)
	}
	if _, ok := state.applied["0002_second"]; !ok {
		t.Fatalf("expected both migrations recorded: %#v", state.applied)
	}
	if state.names["0002_second"] != "second" {
		t.Fatalf("expected second migration name recorded, got %#v", state.names)
	}
	execLog := strings.Join(state.execs, "\n")
	if strings.Contains(execLog, "CREATE TABLE first") {
		t.Fatalf("expected first migration skipped, exec log:\n%s", execLog)
	}
	if !strings.Contains(execLog, "CREATE TABLE second") {
		t.Fatalf("expected second migration applied, exec log:\n%s", execLog)
	}
}

func TestApplyMigrationsFailsOnChecksumDrift(t *testing.T) {
	db := openFakeDB(t)
	state := fakeMigrationStateFor(db)
	state.applied["0001_first"] = migrationChecksum("CREATE TABLE first (id text);")

	err := Apply(context.Background(), db, []Migration{
		{Version: "0001_first", SQL: "CREATE TABLE first (id integer);"},
	})
	if err == nil || !strings.Contains(err.Error(), "checksum mismatch") {
		t.Fatalf("expected checksum mismatch, got %v", err)
	}
}

func TestApplyMigrationsRecordsChecksumForLegacyRows(t *testing.T) {
	db := openFakeDB(t)
	state := fakeMigrationStateFor(db)
	state.applied["0001_first"] = ""

	sql := "CREATE TABLE first (id text);"
	if err := Apply(context.Background(), db, []Migration{{Version: "0001_first", SQL: sql}}); err != nil {
		t.Fatalf("apply migrations: %v", err)
	}
	if state.applied["0001_first"] != migrationChecksum(sql) {
		t.Fatalf("expected legacy row checksum recorded, got %#v", state.applied)
	}
	if state.names["0001_first"] != "first" {
		t.Fatalf("expected legacy row name recorded, got %#v", state.names)
	}
}

func TestApplyMigrationsBackfillsMissingNameForAppliedRows(t *testing.T) {
	db := openFakeDB(t)
	state := fakeMigrationStateFor(db)
	sql := "CREATE TABLE first (id text);"
	state.applied["0001_first"] = migrationChecksum(sql)

	if err := Apply(context.Background(), db, []Migration{{Version: "0001_first", SQL: sql}}); err != nil {
		t.Fatalf("apply migrations: %v", err)
	}
	if state.names["0001_first"] != "first" {
		t.Fatalf("expected applied row name backfilled, got %#v", state.names)
	}
}

var (
	fakeDriverOnce sync.Once
	fakeDriversMu  sync.Mutex
	fakeDrivers    = map[string]*fakeMigrationState{}
)

type fakeMigrationState struct {
	mu      sync.Mutex
	applied map[string]string
	names   map[string]string
	execs   []string
}

func openFakeDB(t *testing.T) *sql.DB {
	t.Helper()
	fakeDriverOnce.Do(func() {
		sql.Register("metadb_fake", fakeMigrationDriver{})
	})
	dsn := t.Name()
	state := &fakeMigrationState{applied: map[string]string{}, names: map[string]string{}}
	fakeDriversMu.Lock()
	fakeDrivers[dsn] = state
	fakeDriversMu.Unlock()
	db, err := sql.Open("metadb_fake", dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = db.Close()
		fakeDriversMu.Lock()
		delete(fakeDrivers, dsn)
		fakeDriversMu.Unlock()
	})
	return db
}

func fakeMigrationStateFor(db *sql.DB) *fakeMigrationState {
	db.Ping()
	fakeDriversMu.Lock()
	defer fakeDriversMu.Unlock()
	for _, state := range fakeDrivers {
		return state
	}
	return nil
}

type fakeMigrationDriver struct{}

func (fakeMigrationDriver) Open(name string) (driver.Conn, error) {
	fakeDriversMu.Lock()
	state := fakeDrivers[name]
	fakeDriversMu.Unlock()
	return fakeMigrationConn{state: state}, nil
}

type fakeMigrationConn struct {
	state *fakeMigrationState
}

func (fakeMigrationConn) Prepare(query string) (driver.Stmt, error) {
	return nil, driver.ErrSkip
}

func (fakeMigrationConn) Close() error {
	return nil
}

func (fakeMigrationConn) Begin() (driver.Tx, error) {
	return fakeMigrationTx{}, nil
}

func (fakeMigrationConn) BeginTx(context.Context, driver.TxOptions) (driver.Tx, error) {
	return fakeMigrationTx{}, nil
}

func (conn fakeMigrationConn) ExecContext(_ context.Context, query string, args []driver.NamedValue) (driver.Result, error) {
	conn.state.mu.Lock()
	defer conn.state.mu.Unlock()
	trimmed := strings.TrimSpace(query)
	conn.state.execs = append(conn.state.execs, trimmed)
	if strings.HasPrefix(trimmed, "INSERT INTO schema_migrations") && len(args) > 0 {
		if version, ok := args[0].Value.(string); ok {
			checksum := ""
			if len(args) > 1 {
				checksum, _ = args[1].Value.(string)
			}
			conn.state.applied[version] = checksum
			if len(args) > 2 {
				name, _ := args[2].Value.(string)
				conn.state.names[version] = name
			}
		}
	}
	if strings.HasPrefix(trimmed, "UPDATE schema_migrations") && len(args) > 1 {
		if version, ok := args[0].Value.(string); ok {
			if strings.Contains(trimmed, "checksum") {
				checksum, _ := args[1].Value.(string)
				conn.state.applied[version] = checksum
			}
			nameArg := 1
			if strings.Contains(trimmed, "checksum") {
				nameArg = 2
			}
			if len(args) > nameArg && strings.Contains(trimmed, "name") {
				name, _ := args[nameArg].Value.(string)
				conn.state.names[version] = name
			}
		}
	}
	return driver.RowsAffected(1), nil
}

func (conn fakeMigrationConn) QueryContext(_ context.Context, query string, args []driver.NamedValue) (driver.Rows, error) {
	conn.state.mu.Lock()
	defer conn.state.mu.Unlock()
	version := ""
	if len(args) > 0 {
		version, _ = args[0].Value.(string)
	}
	checksum, ok := conn.state.applied[version]
	if !ok {
		return &fakeRows{}, nil
	}
	return &fakeRows{values: []driver.Value{checksum, conn.state.names[version]}}, nil
}

type fakeMigrationTx struct{}

func (fakeMigrationTx) Commit() error {
	return nil
}

func (fakeMigrationTx) Rollback() error {
	return nil
}

type fakeRows struct {
	values []driver.Value
	read   bool
}

func (fakeRows) Columns() []string {
	return []string{"checksum", "name"}
}

func (rows *fakeRows) Close() error {
	return nil
}

func (rows *fakeRows) Next(dest []driver.Value) error {
	if rows.read || len(rows.values) == 0 {
		return io.EOF
	}
	rows.read = true
	copy(dest, rows.values)
	return nil
}
