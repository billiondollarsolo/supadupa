package main

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"runtime"
	"strconv"
	"strings"
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
	if err := bootstrapPlatformDefaults(context.Background(), store, logger); err != nil {
		logger.Error("platform defaults bootstrap failed", "error", err)
		os.Exit(1)
	}
	if err := bootstrapDefaultBackupStorageTarget(context.Background(), store, logger); err != nil {
		logger.Error("backup storage target bootstrap failed", "error", err)
		os.Exit(1)
	}
	provisioner, err := provisionerfactory.NewFromEnv(os.Getenv)
	if err != nil {
		logger.Error("invalid provisioner", "error", err)
		os.Exit(1)
	}
	if err := registerLocalHostCapacity(context.Background(), store, logger); err != nil {
		logger.Warn("local host capacity registration skipped", "error", err)
	}
	if err := reconcileExistingProjectRoutes(context.Background(), store, logger); err != nil {
		logger.Error("project route reconciliation failed", "error", err)
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
	go func() {
		if err := reconcileExistingProjectRuntime(ctx, store, provisioner, logger); err != nil {
			logger.Warn("project runtime reconciliation failed", "error", err)
		}
	}()
	go reconciler.New(store, provisioner, logger).Run(ctx)
	backupScheduler := scheduler.NewBackupScheduler(store, nil, logger)
	if tick, err := scheduler.BackupSchedulerTickFromEnv(os.Getenv); err != nil {
		logger.Warn("invalid backup scheduler tick; using default", "env", scheduler.BackupSchedulerTickEnv, "default", scheduler.DefaultBackupSchedulerTick.String(), "error", err)
	} else {
		backupScheduler.WithTick(tick)
	}
	if interval, err := scheduler.WALArchiveIntervalFromEnv(os.Getenv); err != nil {
		logger.Warn("invalid WAL archive interval; using default", "env", scheduler.WALArchiveIntervalEnv, "default", scheduler.DefaultWALArchiveInterval.String(), "error", err)
	} else {
		backupScheduler.WithWALArchiveInterval(interval)
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

func reconcileExistingProjectSecrets(ctx context.Context, store control.Store, provisioner control.Provisioner, logger *slog.Logger) error {
	if store == nil || provisioner == nil {
		return nil
	}
	if logger == nil {
		logger = slog.Default()
	}
	projects, err := store.ListProjects(ctx)
	if err != nil {
		return err
	}
	for _, project := range projects {
		if project.Status == control.ProjectDestroying {
			continue
		}
		secrets, err := store.EnsureProjectSecrets(ctx, project.Ref)
		if err != nil {
			return err
		}
		spec := control.ProjectSpecWithSecrets(project.Spec, secrets)
		if err := provisioner.SyncSecrets(ctx, project.Ref, spec); err != nil {
			_, _ = store.UpdateProjectStatus(ctx, project.Ref, control.ProjectDegraded, err.Error())
			control.LogProject(ctx, store, project.Ref, "error", "Startup secret sync failed", map[string]string{"error": err.Error()})
			control.Audit(ctx, store, "project.startup_secret_sync_failed", "project:"+project.Ref, map[string]string{"error": err.Error()})
			logger.Warn("project secret reconciliation failed", "project_ref", project.Ref, "error", err)
			continue
		}
		logger.Info("project secrets reconciled", "project_ref", project.Ref)
	}
	return nil
}

func reconcileExistingProjectRuntime(ctx context.Context, store control.Store, provisioner control.Provisioner, logger *slog.Logger) error {
	if store == nil || provisioner == nil {
		return nil
	}
	if logger == nil {
		logger = slog.Default()
	}
	if err := reconcileExistingProjectSecrets(ctx, store, provisioner, logger); err != nil {
		return err
	}
	projects, err := store.ListProjects(ctx)
	if err != nil {
		return err
	}
	for _, project := range projects {
		if project.Status == control.ProjectDestroying {
			continue
		}
		if syncer, ok := provisioner.(control.ServiceSyncer); ok {
			if err := syncer.SyncServices(ctx, project.Ref, project.Spec); err != nil {
				recordStartupRuntimeSyncFailure(ctx, store, logger, project.Ref, "services", err)
				continue
			}
		}
		if syncer, ok := provisioner.(control.ConfigSyncer); ok {
			configs, err := store.ListProjectConfigs(ctx, project.Ref)
			if err != nil {
				return err
			}
			for _, config := range configs {
				if config.Area == "network" {
					continue
				}
				runtimeConfig, err := materializeStartupProjectConfig(ctx, store, project.Ref, config)
				if err != nil {
					recordStartupRuntimeSyncFailure(ctx, store, logger, project.Ref, "config:"+config.Area, err)
					continue
				}
				if err := syncer.SyncConfig(ctx, project.Ref, runtimeConfig); err != nil {
					recordStartupRuntimeSyncFailure(ctx, store, logger, project.Ref, "config:"+config.Area, err)
					continue
				}
			}
		}
		if syncer, ok := provisioner.(control.AuthHookSyncer); ok {
			hooks, err := store.ListProjectAuthHooks(ctx, project.Ref)
			if err != nil {
				return err
			}
			runtimeHooks, err := materializeStartupProjectAuthHooks(ctx, store, project.Ref, hooks)
			if err != nil {
				recordStartupRuntimeSyncFailure(ctx, store, logger, project.Ref, "auth_hooks", err)
			} else if err := syncer.SyncAuthHooks(ctx, project.Ref, runtimeHooks); err != nil {
				recordStartupRuntimeSyncFailure(ctx, store, logger, project.Ref, "auth_hooks", err)
			}
		}
		if syncer, ok := provisioner.(control.ReplicaSyncer); ok {
			replicas, err := store.ListProjectReplicas(ctx, project.Ref)
			if err != nil {
				return err
			}
			if err := syncer.SyncReplicas(ctx, project.Ref, replicas); err != nil {
				recordStartupRuntimeSyncFailure(ctx, store, logger, project.Ref, "replicas", err)
				continue
			}
		}
		logger.Info("project runtime reconciled", "project_ref", project.Ref)
	}
	return nil
}

func recordStartupRuntimeSyncFailure(ctx context.Context, store control.Store, logger *slog.Logger, ref string, area string, err error) {
	_, _ = store.UpdateProjectStatus(ctx, ref, control.ProjectDegraded, err.Error())
	metadata := map[string]string{"area": area, "error": err.Error()}
	control.LogProject(ctx, store, ref, "error", "Startup runtime sync failed", metadata)
	control.Audit(ctx, store, "project.startup_runtime_sync_failed", "project:"+ref, metadata)
	logger.Warn("project runtime reconciliation failed", "project_ref", ref, "area", area, "error", err)
}

func materializeStartupProjectConfig(ctx context.Context, store control.Store, ref string, config control.ProjectConfig) (control.ProjectConfig, error) {
	runtimeConfig := config
	runtimeConfig.Config = cloneStartupStringMap(config.Config)
	for _, key := range startupRuntimeSecretHandleKeys(config.Area) {
		handle := strings.TrimSpace(config.Config[key])
		if handle == "" {
			continue
		}
		value, err := resolveStartupProjectSecretHandleValue(ctx, store, ref, handle, key)
		if err != nil {
			return control.ProjectConfig{}, err
		}
		runtimeConfig.Config["__resolved_"+key] = value
	}
	return runtimeConfig, nil
}

func startupRuntimeSecretHandleKeys(area string) []string {
	switch area {
	case "auth":
		return []string{"captcha_secret_handle"}
	case "auth_providers":
		keys := []string{
			"oauth_google_client_secret_handle",
			"oauth_github_client_secret_handle",
			"oauth_azure_client_secret_handle",
			"sms_twilio_auth_token_handle",
			"sms_messagebird_access_key_handle",
			"sms_textlocal_api_key_handle",
			"sms_vonage_api_secret_handle",
		}
		for _, provider := range []string{
			"apple",
			"bitbucket",
			"discord",
			"facebook",
			"gitlab",
			"kakao",
			"keycloak",
			"linkedin_oidc",
			"notion",
			"slack_oidc",
			"spotify",
			"twitch",
			"twitter",
			"workos",
			"zoom",
		} {
			keys = append(keys, "oauth_"+provider+"_client_secret_handle")
		}
		return keys
	case "smtp":
		return []string{"password_handle"}
	default:
		return nil
	}
}

func materializeStartupProjectAuthHooks(ctx context.Context, store control.Store, ref string, hooks []control.ProjectAuthHook) ([]control.ProjectAuthHook, error) {
	out := append([]control.ProjectAuthHook(nil), hooks...)
	for index := range out {
		out[index].Headers = cloneStartupStringMap(out[index].Headers)
		out[index].RuntimeHeaders = cloneStartupStringMap(out[index].Headers)
		if !out[index].Enabled {
			continue
		}
		if handle := strings.TrimSpace(out[index].SecretHandle); handle != "" {
			resolved, err := resolveStartupProjectSecretHandleValue(ctx, store, ref, handle, "auth hook secret_handle")
			if err != nil {
				return nil, fmt.Errorf("auth hook secret_handle: %w", err)
			}
			out[index].RuntimeSecret = resolved
		}
		for key, value := range out[index].Headers {
			if !startupSensitiveHeaderKey(key) || strings.TrimSpace(value) == "" {
				continue
			}
			resolved, err := resolveStartupProjectSecretHandleValue(ctx, store, ref, value, "auth hook header "+key)
			if err != nil {
				return nil, fmt.Errorf("auth hook header %s: %w", key, err)
			}
			out[index].RuntimeHeaders[key] = resolved
		}
	}
	return out, nil
}

func resolveStartupProjectSecretHandleValue(ctx context.Context, store control.Store, ref string, handle string, label string) (string, error) {
	prefix := "secret://projects/" + ref + "/"
	if !strings.HasPrefix(handle, prefix) {
		return "", fmt.Errorf("%s must reference project %s", label, ref)
	}
	kind := strings.TrimSpace(strings.TrimPrefix(handle, prefix))
	if strings.Contains(kind, "/") || kind == "" {
		return "", fmt.Errorf("%s %s is not revealable by this control plane", label, handle)
	}
	secret, err := store.RevealProjectSecret(ctx, ref, kind)
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(secret.Value) == "" {
		return "", fmt.Errorf("%s %s has no value", label, handle)
	}
	return secret.Value, nil
}

func startupSensitiveHeaderKey(key string) bool {
	normalized := strings.ToLower(strings.TrimSpace(key))
	return normalized == "authorization" ||
		normalized == "proxy-authorization" ||
		normalized == "x-api-key" ||
		normalized == "x-auth-token" ||
		strings.Contains(normalized, "secret") ||
		strings.Contains(normalized, "token") ||
		strings.Contains(normalized, "password") ||
		strings.Contains(normalized, "key")
}

func cloneStartupStringMap(input map[string]string) map[string]string {
	out := map[string]string{}
	for key, value := range input {
		out[key] = value
	}
	return out
}

func registerLocalHostCapacity(ctx context.Context, store control.Store, logger *slog.Logger) error {
	if !shouldRegisterLocalHost() {
		return nil
	}
	hosts, err := store.ListHosts(ctx)
	if err != nil {
		return err
	}
	if len(hosts) > 0 {
		return nil
	}
	capacity := localHostCapacity()
	host, err := store.CreateHost(ctx, control.CreateHostRequest{
		Name:     envOrDefault("SUPADUPA_LOCAL_HOST_NAME", "local-docker"),
		Address:  localHostAddress(),
		Capacity: capacity,
	})
	if err != nil {
		return err
	}
	logger.Info("local compose host capacity registered", "host_id", host.ID, "cpu", capacity.CPU, "ram_mb", capacity.RAMMB, "disk_gb", capacity.DiskGB, "projects", capacity.Project)
	return nil
}

func reconcileExistingProjectRoutes(ctx context.Context, store control.Store, logger *slog.Logger) error {
	projects, err := store.ListProjects(ctx)
	if err != nil {
		return err
	}
	routing := control.NewRoutingService("")
	for _, project := range projects {
		domains, err := store.ListProjectDomains(ctx, project.Ref)
		if err != nil {
			return err
		}
		networkConfig, err := store.GetProjectConfig(ctx, project.Ref, "network")
		if err != nil {
			return err
		}
		cdnPolicy, err := store.GetProjectCDNPolicy(ctx, project.Ref)
		if err != nil {
			return err
		}
		replicas, err := store.ListProjectReplicas(ctx, project.Ref)
		if err != nil {
			return err
		}
		routes, err := store.UpsertProjectRoutes(ctx, project.Ref, control.RoutesForProjectDomainsWithNetworkAndCDN(project, domains, networkConfig, cdnPolicy))
		if err != nil {
			return err
		}
		tcpRoutes := control.TCPRoutesForProjectWithNetworkAndReplicas(project, networkConfig, replicas)
		routePath, err := routing.RenderProjectWithTCPRoutes(project, routes, tcpRoutes)
		if err != nil {
			return err
		}
		logger.Info("project routes reconciled", "project_ref", project.Ref, "routes", len(routes), "path", routePath)
	}
	return nil
}

func shouldRegisterLocalHost() bool {
	value := strings.ToLower(strings.TrimSpace(os.Getenv("SUPADUPA_AUTO_REGISTER_LOCAL_HOST")))
	if value == "false" || value == "0" || value == "off" {
		return false
	}
	provisioner := strings.ToLower(strings.TrimSpace(os.Getenv("SUPADUPA_PROVISIONER")))
	return provisioner == "" || provisioner == "compose" || provisioner == "docker-compose" || provisioner == "docker"
}

func localHostCapacity() control.HostCapacity {
	return control.HostCapacity{
		CPU:      envInt("SUPADUPA_LOCAL_HOST_CPU", runtime.NumCPU()),
		RAMMB:    envInt("SUPADUPA_LOCAL_HOST_RAM_MB", detectRAMMB()),
		DiskGB:   envInt("SUPADUPA_LOCAL_HOST_DISK_GB", detectDiskGB()),
		DiskIOPS: envInt("SUPADUPA_LOCAL_HOST_DISK_IOPS", 48000),
		Project:  envInt("SUPADUPA_LOCAL_HOST_PROJECTS", 20),
	}
}

func localHostAddress() string {
	if address := strings.TrimSpace(os.Getenv("SUPADUPA_LOCAL_HOST_ADDRESS")); address != "" {
		return address
	}
	if address := strings.TrimSpace(os.Getenv("PUBLIC_HOST_IP")); address != "" {
		return address
	}
	if hostname, err := os.Hostname(); err == nil && strings.TrimSpace(hostname) != "" {
		return strings.TrimSpace(hostname)
	}
	return "localhost"
}

func detectRAMMB() int {
	payload, err := os.ReadFile("/proc/meminfo")
	if err != nil {
		return 0
	}
	for _, line := range strings.Split(string(payload), "\n") {
		fields := strings.Fields(line)
		if len(fields) >= 2 && fields[0] == "MemTotal:" {
			kb, err := strconv.Atoi(fields[1])
			if err != nil {
				return 0
			}
			return kb / 1024
		}
	}
	return 0
}

func detectDiskGB() int {
	var stat syscall.Statfs_t
	if err := syscall.Statfs("/", &stat); err != nil {
		return 0
	}
	bytes := uint64(stat.Blocks) * uint64(stat.Bsize)
	return int(bytes / 1024 / 1024 / 1024)
}

func envInt(key string, fallback int) int {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	parsed, err := strconv.Atoi(value)
	if err != nil || parsed < 0 {
		return fallback
	}
	return parsed
}

func envOrDefault(key string, fallback string) string {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	return value
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

func bootstrapPlatformDefaults(ctx context.Context, store control.Store, logger *slog.Logger) error {
	projectDomain := envFirst("SUPADUPA_PROJECT_DOMAIN", "SUPADUPA_APPS_DOMAIN")
	stackVersion := envFirst("SUPADUPA_DEFAULT_STACK_VERSION")
	profile := envFirst("SUPADUPA_DEFAULT_PROFILE")
	resourceTier := envFirst("SUPADUPA_DEFAULT_RESOURCE_TIER")
	backupSchedule := envFirst("SUPADUPA_DEFAULT_BACKUP_SCHEDULE")
	if projectDomain == "" && stackVersion == "" && profile == "" && resourceTier == "" && backupSchedule == "" {
		return nil
	}
	defaults, err := store.GetPlatformDefaults(ctx)
	if err != nil {
		return err
	}
	input := control.PlatformDefaultsInput{
		Domain:         defaults.Domain,
		StackVersion:   defaults.StackVersion,
		Profile:        defaults.Profile,
		ResourceTier:   defaults.ResourceTier,
		BackupSchedule: defaults.BackupSchedule,
		FeatureFlags:   defaults.FeatureFlags,
		SMTP:           defaults.SMTP,
	}
	changes := map[string]string{}
	if projectDomain != "" && defaults.Domain != projectDomain {
		forceDomain := strings.TrimSpace(os.Getenv("SUPADUPA_PROJECT_DOMAIN")) != "" || envBoolAny(false, "SUPADUPA_PLATFORM_DEFAULTS_FORCE")
		if forceDomain || defaults.Domain == "" || defaults.Domain == "supadupa.test" {
			input.Domain = projectDomain
			changes["domain"] = projectDomain
		} else {
			logger.Info("platform project domain bootstrap skipped; existing defaults are user-configured", "existing_domain", defaults.Domain, "env_domain", projectDomain)
		}
	}
	if stackVersion != "" && defaults.StackVersion != stackVersion {
		input.StackVersion = stackVersion
		changes["stack_version"] = stackVersion
	}
	if profile != "" && string(defaults.Profile) != profile {
		input.Profile = control.StackProfile(profile)
		changes["profile"] = profile
	}
	if resourceTier != "" && string(defaults.ResourceTier) != resourceTier {
		input.ResourceTier = control.ResourceTier(resourceTier)
		changes["resource_tier"] = resourceTier
	}
	if backupSchedule != "" && defaults.BackupSchedule != backupSchedule {
		input.BackupSchedule = backupSchedule
		changes["backup_schedule"] = backupSchedule
	}
	if len(changes) == 0 {
		logger.Info("platform defaults already match environment")
		return nil
	}
	updated, err := store.UpdatePlatformDefaults(ctx, input)
	if err != nil {
		return err
	}
	control.Audit(ctx, store, "platform_defaults.bootstrap_env", "platform:defaults", changes)
	logger.Info("platform defaults bootstrapped from environment", "domain", updated.Domain, "stack_version", updated.StackVersion, "profile", updated.Profile, "resource_tier", updated.ResourceTier, "backup_schedule", updated.BackupSchedule)
	return nil
}

func bootstrapDefaultBackupStorageTarget(ctx context.Context, store control.Store, logger *slog.Logger) error {
	if !backupStorageTargetEnvPresent() {
		return nil
	}
	name := envFirst("SUPADUPA_BACKUP_TARGET_NAME", "SUPADUPA_BACKUP_S3_NAME")
	if name == "" {
		name = "default-s3"
	}
	existing, ok, err := findBackupStorageTargetByName(ctx, store, name)
	if err != nil {
		return err
	}
	input := control.BackupStorageTargetInput{
		Name:            name,
		Type:            envFirstOrDefault("s3", "SUPADUPA_BACKUP_TARGET_TYPE", "SUPADUPA_BACKUP_S3_TYPE"),
		Endpoint:        envFirstOrDefault(existing.Endpoint, "SUPADUPA_BACKUP_TARGET_ENDPOINT", "SUPADUPA_BACKUP_S3_ENDPOINT"),
		Region:          envFirstOrDefault(existing.Region, "SUPADUPA_BACKUP_TARGET_REGION", "SUPADUPA_BACKUP_S3_REGION"),
		Bucket:          envFirstOrDefault(existing.Bucket, "SUPADUPA_BACKUP_TARGET_BUCKET", "SUPADUPA_BACKUP_S3_BUCKET"),
		Prefix:          envFirstOrDefault(existing.Prefix, "SUPADUPA_BACKUP_TARGET_PREFIX", "SUPADUPA_BACKUP_S3_PREFIX"),
		AccessKeyID:     envFirstOrDefault(existing.AccessKeyID, "SUPADUPA_BACKUP_TARGET_ACCESS_KEY_ID", "SUPADUPA_BACKUP_S3_ACCESS_KEY_ID"),
		SecretAccessKey: envFirst("SUPADUPA_BACKUP_TARGET_SECRET_ACCESS_KEY", "SUPADUPA_BACKUP_S3_SECRET_ACCESS_KEY"),
		ForcePathStyle:  envBoolAny(false, "SUPADUPA_BACKUP_TARGET_FORCE_PATH_STYLE", "SUPADUPA_BACKUP_S3_FORCE_PATH_STYLE"),
		Default:         true,
	}
	if ok {
		if envFirst("SUPADUPA_BACKUP_TARGET_FORCE_PATH_STYLE", "SUPADUPA_BACKUP_S3_FORCE_PATH_STYLE") == "" {
			input.ForcePathStyle = existing.ForcePathStyle
		}
		if backupStorageTargetMatchesInput(existing, input) {
			logger.Info("default backup storage target already matches environment", "target_id", existing.ID, "name", existing.Name, "bucket", existing.Bucket)
			return maybeTestBootstrappedBackupStorageTarget(ctx, store, logger, existing.ID)
		}
		target, err := store.UpdateBackupStorageTarget(ctx, existing.ID, input)
		if err != nil {
			return err
		}
		control.Audit(ctx, store, "backup_storage_target.bootstrap_env_update", "backup-storage-target:"+target.ID, map[string]string{"name": target.Name, "bucket": target.Bucket})
		logger.Info("default backup storage target updated from environment", "target_id", target.ID, "name", target.Name, "bucket", target.Bucket)
		return maybeTestBootstrappedBackupStorageTarget(ctx, store, logger, target.ID)
	}
	target, err := store.CreateBackupStorageTarget(ctx, input)
	if err != nil {
		return err
	}
	control.Audit(ctx, store, "backup_storage_target.bootstrap_env_create", "backup-storage-target:"+target.ID, map[string]string{"name": target.Name, "bucket": target.Bucket})
	logger.Info("default backup storage target bootstrapped from environment", "target_id", target.ID, "name", target.Name, "bucket", target.Bucket)
	return maybeTestBootstrappedBackupStorageTarget(ctx, store, logger, target.ID)
}

func maybeTestBootstrappedBackupStorageTarget(ctx context.Context, store control.Store, logger *slog.Logger, targetID string) error {
	if !envBoolAny(false, "SUPADUPA_BACKUP_TARGET_AUTO_TEST", "SUPADUPA_BACKUP_S3_AUTO_TEST") {
		return nil
	}
	target, err := store.GetBackupStorageTarget(ctx, targetID)
	if err != nil {
		return err
	}
	testCtx, cancel := context.WithTimeout(ctx, backupStorageTargetAutoTestTimeout())
	defer cancel()
	testedAt := time.Now().UTC()
	status := "passed"
	message := ""
	if err := control.TestBackupStorageTarget(testCtx, target); err != nil {
		status = "failed"
		message = err.Error()
	}
	updated, err := store.UpdateBackupStorageTargetTestResult(ctx, targetID, testedAt, status, message)
	if err != nil {
		return err
	}
	metadata := map[string]string{"name": updated.Name, "bucket": updated.Bucket, "test_status": status}
	if message != "" {
		metadata["test_error"] = message
	}
	control.Audit(ctx, store, "backup_storage_target.bootstrap_env_test", "backup-storage-target:"+updated.ID, metadata)
	if status == "passed" {
		logger.Info("default backup storage target tested from environment", "target_id", updated.ID, "name", updated.Name, "bucket", updated.Bucket)
		return nil
	}
	logger.Warn("default backup storage target test failed", "target_id", updated.ID, "name", updated.Name, "bucket", updated.Bucket, "error", message)
	return nil
}

func backupStorageTargetAutoTestTimeout() time.Duration {
	raw := strings.TrimSpace(os.Getenv("SUPADUPA_BACKUP_TARGET_AUTO_TEST_TIMEOUT"))
	if raw == "" {
		raw = strings.TrimSpace(os.Getenv("SUPADUPA_BACKUP_S3_AUTO_TEST_TIMEOUT"))
	}
	if raw == "" {
		return 30 * time.Second
	}
	parsed, err := time.ParseDuration(raw)
	if err != nil || parsed <= 0 {
		return 30 * time.Second
	}
	return parsed
}

func backupStorageTargetMatchesInput(existing control.BackupStorageTarget, input control.BackupStorageTargetInput) bool {
	if existing.Name != input.Name ||
		existing.Type != input.Type ||
		existing.Endpoint != input.Endpoint ||
		existing.Region != input.Region ||
		existing.Bucket != input.Bucket ||
		existing.Prefix != input.Prefix ||
		existing.AccessKeyID != input.AccessKeyID ||
		existing.ForcePathStyle != input.ForcePathStyle ||
		!existing.Default {
		return false
	}
	if strings.TrimSpace(input.SecretAccessKey) != "" && existing.SecretAccessKey != input.SecretAccessKey {
		return false
	}
	return true
}

func backupStorageTargetEnvPresent() bool {
	keys := []string{
		"SUPADUPA_BACKUP_TARGET_NAME",
		"SUPADUPA_BACKUP_TARGET_TYPE",
		"SUPADUPA_BACKUP_TARGET_ENDPOINT",
		"SUPADUPA_BACKUP_TARGET_REGION",
		"SUPADUPA_BACKUP_TARGET_BUCKET",
		"SUPADUPA_BACKUP_TARGET_PREFIX",
		"SUPADUPA_BACKUP_TARGET_ACCESS_KEY_ID",
		"SUPADUPA_BACKUP_TARGET_SECRET_ACCESS_KEY",
		"SUPADUPA_BACKUP_TARGET_FORCE_PATH_STYLE",
		"SUPADUPA_BACKUP_S3_NAME",
		"SUPADUPA_BACKUP_S3_TYPE",
		"SUPADUPA_BACKUP_S3_ENDPOINT",
		"SUPADUPA_BACKUP_S3_REGION",
		"SUPADUPA_BACKUP_S3_BUCKET",
		"SUPADUPA_BACKUP_S3_PREFIX",
		"SUPADUPA_BACKUP_S3_ACCESS_KEY_ID",
		"SUPADUPA_BACKUP_S3_SECRET_ACCESS_KEY",
		"SUPADUPA_BACKUP_S3_FORCE_PATH_STYLE",
	}
	for _, key := range keys {
		if strings.TrimSpace(os.Getenv(key)) != "" {
			return true
		}
	}
	return false
}

func findBackupStorageTargetByName(ctx context.Context, store control.Store, name string) (control.BackupStorageTarget, bool, error) {
	targets, err := store.ListBackupStorageTargets(ctx)
	if err != nil {
		return control.BackupStorageTarget{}, false, err
	}
	for _, target := range targets {
		if target.Name != name {
			continue
		}
		fullTarget, err := store.GetBackupStorageTarget(ctx, target.ID)
		if err != nil {
			return control.BackupStorageTarget{}, false, err
		}
		return fullTarget, true, nil
	}
	return control.BackupStorageTarget{}, false, nil
}

func envFirst(keys ...string) string {
	for _, key := range keys {
		if value := strings.TrimSpace(os.Getenv(key)); value != "" {
			return value
		}
	}
	return ""
}

func envFirstOrDefault(fallback string, keys ...string) string {
	if value := envFirst(keys...); value != "" {
		return value
	}
	return fallback
}

func envBoolAny(fallback bool, keys ...string) bool {
	value := strings.ToLower(envFirst(keys...))
	if value == "" {
		return fallback
	}
	return value == "1" || value == "true" || value == "yes" || value == "on"
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
