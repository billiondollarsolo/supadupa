package main

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"

	"supadupa2026/internal/api"
	"supadupa2026/internal/control"
	"supadupa2026/internal/metadb"
	provisionerfactory "supadupa2026/internal/provisioner"
	"supadupa2026/internal/reconciler"
	"supadupa2026/internal/scheduler"
)

func main() {
	addr := os.Getenv("SUPADUPA_ADDR")
	if addr == "" {
		addr = ":8080"
	}

	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	metaDB, err := openMetaDB(context.Background(), logger)
	if err != nil {
		logger.Error("meta database setup failed", "error", err)
		os.Exit(1)
	}
	if metaDB != nil {
		defer metaDB.Close()
	}
	store := control.Store(control.NewMemoryStore())
	if metaDB != nil {
		persistentStore, err := control.NewPersistentStore(context.Background(), metaDB)
		if err != nil {
			logger.Error("persistent store setup failed", "error", err)
			os.Exit(1)
		}
		store = persistentStore
		logger.Info("persistent control-plane checkpoint store enabled")
	}
	if err := bootstrapInitialAdmin(context.Background(), store, logger); err != nil {
		logger.Error("initial admin bootstrap failed", "error", err)
		os.Exit(1)
	}
	provisioner, err := provisionerfactory.NewFromEnv(os.Getenv)
	if err != nil {
		logger.Error("invalid provisioner", "error", err)
		os.Exit(1)
	}
	server := api.NewServer(api.Config{
		Addr:         addr,
		Logger:       logger,
		Provisioner:  provisioner,
		Store:        store,
		AuthRequired: true,
	})

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	errCh := make(chan error, 1)
	go func() {
		logger.Info("starting supadupa control plane", "addr", addr)
		errCh <- server.ListenAndServe()
	}()
	go reconciler.New(store, provisioner, logger).Run(ctx)
	backupScheduler := scheduler.NewBackupScheduler(store, nil, logger)
	if tick, err := scheduler.BackupSchedulerTickFromEnv(os.Getenv); err != nil {
		logger.Warn("invalid backup scheduler tick; using default", "env", scheduler.BackupSchedulerTickEnv, "default", scheduler.DefaultBackupSchedulerTick.String(), "error", err)
	} else {
		backupScheduler.WithTick(tick)
	}
	go backupScheduler.Run(ctx)
	var telemetryCollector control.TelemetryCollector
	if collector, ok := provisioner.(control.TelemetryCollector); ok {
		telemetryCollector = collector
	}
	telemetryScheduler := scheduler.NewTelemetryScheduler(store, telemetryCollector, logger)
	if tick, err := scheduler.TelemetrySchedulerTickFromEnv(os.Getenv); err != nil {
		logger.Warn("invalid telemetry scheduler tick; using default", "env", scheduler.TelemetrySchedulerTickEnv, "default", scheduler.DefaultTelemetrySchedulerTick.String(), "error", err)
	} else {
		telemetryScheduler.WithTick(tick)
	}
	go telemetryScheduler.Run(ctx)

	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := server.Shutdown(shutdownCtx); err != nil {
			logger.Error("shutdown failed", "error", err)
			os.Exit(1)
		}
	case err := <-errCh:
		if err != nil && err != http.ErrServerClosed {
			logger.Error("server failed", "error", err)
			os.Exit(1)
		}
	}
}

func bootstrapInitialAdmin(ctx context.Context, store control.Store, logger *slog.Logger) error {
	email := os.Getenv("SUPADUPA_BOOTSTRAP_EMAIL")
	password := os.Getenv("SUPADUPA_BOOTSTRAP_PASSWORD")
	if email == "" && password == "" {
		return nil
	}
	if email == "" || password == "" {
		return fmt.Errorf("SUPADUPA_BOOTSTRAP_EMAIL and SUPADUPA_BOOTSTRAP_PASSWORD must be set together")
	}
	if store.HasUsers(ctx) {
		logger.Info("initial admin bootstrap skipped; users already exist")
		return nil
	}
	user, err := store.CreateUser(ctx, control.CreateUserRequest{
		Email:    email,
		Password: password,
		Role:     "admin",
	})
	if err != nil {
		return err
	}
	control.Audit(ctx, store, "user.bootstrap_env", "user:"+user.ID, map[string]string{"email": user.Email})
	logger.Info("initial admin bootstrapped from environment", "email", user.Email)
	return nil
}

func openMetaDB(ctx context.Context, logger *slog.Logger) (*sql.DB, error) {
	dsn := os.Getenv("SUPADUPA_META_DSN")
	if dsn == "" {
		logger.Warn("SUPADUPA_META_DSN is not set; using in-memory control-plane store")
		return nil, nil
	}
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		return nil, err
	}
	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return nil, err
	}
	migrationDir := os.Getenv("SUPADUPA_MIGRATIONS_DIR")
	if migrationDir == "" {
		migrationDir = "./migrations"
	}
	migrations, err := metadb.LoadMigrations(migrationDir)
	if err != nil {
		_ = db.Close()
		return nil, err
	}
	if err := metadb.Apply(ctx, db, migrations); err != nil {
		_ = db.Close()
		return nil, err
	}
	logger.Info("meta database migrations applied", "migrations", len(migrations), "dir", migrationDir)
	return db, nil
}
