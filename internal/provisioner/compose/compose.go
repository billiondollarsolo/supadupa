package compose

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"supadupa2026/internal/control"
)

type Options struct {
	RootDir            string
	Apply              bool
	Command            string
	BranchCloneCommand string
}

type Provisioner struct {
	rootDir            string
	apply              bool
	command            string
	branchCloneCommand string
}

var socialOAuthProviders = map[string]string{
	"apple":         "APPLE",
	"bitbucket":     "BITBUCKET",
	"discord":       "DISCORD",
	"facebook":      "FACEBOOK",
	"figma":         "FIGMA",
	"gitlab":        "GITLAB",
	"kakao":         "KAKAO",
	"keycloak":      "KEYCLOAK",
	"linkedin_oidc": "LINKEDIN_OIDC",
	"notion":        "NOTION",
	"snapchat":      "SNAPCHAT",
	"slack_oidc":    "SLACK_OIDC",
	"spotify":       "SPOTIFY",
	"twitch":        "TWITCH",
	"twitter":       "TWITTER",
	"workos":        "WORKOS",
	"zoom":          "ZOOM",
}

var authHookEnvTypes = map[string]string{
	"before_user_created":           "BEFORE_USER_CREATED",
	"custom_access_token":           "CUSTOM_ACCESS_TOKEN",
	"send_sms":                      "SEND_SMS",
	"send_email":                    "SEND_EMAIL",
	"mfa_verification_attempt":      "MFA_VERIFICATION_ATTEMPT",
	"password_verification_attempt": "PASSWORD_VERIFICATION_ATTEMPT",
}

const (
	bindMountFileMode = 0o644
)

func New() *Provisioner {
	return NewWithOptions(Options{
		RootDir:            envOrDefault("SUPADUPA_PROJECT_ROOT", "./runtime/projects"),
		Apply:              os.Getenv("SUPADUPA_COMPOSE_APPLY") == "true",
		Command:            envOrDefault("SUPADUPA_COMPOSE_COMMAND", "docker compose"),
		BranchCloneCommand: os.Getenv("SUPADUPA_BRANCH_CLONE_COMMAND"),
	})
}

func NewWithOptions(opts Options) *Provisioner {
	if opts.RootDir == "" {
		opts.RootDir = "./runtime/projects"
	}
	if opts.Command == "" {
		opts.Command = "docker compose"
	}
	return &Provisioner{rootDir: opts.RootDir, apply: opts.Apply, command: opts.Command, branchCloneCommand: opts.BranchCloneCommand}
}

func (p *Provisioner) Name() string {
	return "compose"
}

func (p *Provisioner) Create(ctx context.Context, spec control.ProjectSpec) error {
	projectDir := filepath.Join(p.rootDir, spec.Ref)
	if err := os.MkdirAll(projectDir, 0o700); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Join(projectDir, "functions"), 0o700); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Join(projectDir, "functions", "main"), 0o700); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Join(projectDir, "log-drains"), 0o700); err != nil {
		return err
	}
	postgresPassword, err := writeEnvFile(filepath.Join(projectDir, ".env"), spec)
	if err != nil {
		return err
	}
	if err := writeKongConfigFile(filepath.Join(projectDir, "kong.yml"), spec.Ref, spec.Services); err != nil {
		return err
	}
	if err := writeKongEntrypointFile(filepath.Join(projectDir, "kong-entrypoint.sh")); err != nil {
		return err
	}
	if err := writeVectorConfigFile(filepath.Join(projectDir, "vector.yml"), spec.Ref); err != nil {
		return err
	}
	if err := writePoolerConfigFile(filepath.Join(projectDir, "pooler.exs")); err != nil {
		return err
	}
	if err := writeDatabaseInitFile(filepath.Join(projectDir, "00-supadupa-init.sql"), postgresPassword); err != nil {
		return err
	}
	if err := writePostgresHBAFile(filepath.Join(projectDir, "pg_hba.conf")); err != nil {
		return err
	}
	if err := writeDefaultFunctionEntrypoint(filepath.Join(projectDir, "functions", "main", "index.ts")); err != nil {
		return err
	}
	if err := writeAuthHooksFile(filepath.Join(projectDir, "auth-hooks.json"), nil); err != nil {
		return err
	}
	if err := writeComposeFile(filepath.Join(projectDir, "compose.yaml"), spec); err != nil {
		return err
	}
	if !p.apply {
		return nil
	}
	if err := p.runCompose(ctx, projectDir, spec.Ref, "up", "-d", "db"); err != nil {
		return err
	}
	if err := p.applyDatabaseBootstrap(ctx, projectDir, spec.Ref); err != nil {
		return err
	}
	if control.ProjectServiceStates(spec.Services)["pooler"] {
		if err := p.runCompose(ctx, projectDir, spec.Ref, "up", "-d", "--scale", "pooler=0"); err != nil {
			return err
		}
		if err := p.applyDatabaseBootstrap(ctx, projectDir, spec.Ref); err != nil {
			return err
		}
		return p.ensurePoolerStarted(ctx, projectDir, spec.Ref)
	}
	return p.runCompose(ctx, projectDir, spec.Ref, "up", "-d")
}

func (p *Provisioner) SyncSecrets(ctx context.Context, ref string, spec control.ProjectSpec) error {
	projectDir := filepath.Join(p.rootDir, ref)
	if _, err := os.Stat(filepath.Join(projectDir, "compose.yaml")); err != nil {
		return err
	}
	envPath := filepath.Join(projectDir, ".env")
	values, err := readEnvFile(envPath)
	if err != nil {
		return err
	}
	applyRuntimeDefaultEnvValues(values, spec)
	for _, key := range control.ManagedSecretEnvironmentKeys() {
		if value := strings.TrimSpace(spec.Environment[key]); value != "" {
			values[key] = value
		}
	}
	applyDerivedSecretEnvValues(values)
	postgresPassword := strings.TrimSpace(values["POSTGRES_PASSWORD"])
	if postgresPassword == "" {
		return fmt.Errorf("POSTGRES_PASSWORD is required to sync project secrets")
	}
	if err := writeEnvValues(envPath, values); err != nil {
		return err
	}
	if err := writeDatabaseInitFile(filepath.Join(projectDir, "00-supadupa-init.sql"), postgresPassword); err != nil {
		return err
	}
	if err := writePostgresHBAFile(filepath.Join(projectDir, "pg_hba.conf")); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Join(projectDir, "functions", "main"), 0o700); err != nil {
		return err
	}
	if err := writeDefaultFunctionEntrypoint(filepath.Join(projectDir, "functions", "main", "index.ts")); err != nil {
		return err
	}
	if err := writeComposeFile(filepath.Join(projectDir, "compose.yaml"), spec); err != nil {
		return err
	}
	if !p.apply {
		return nil
	}
	if err := p.runCompose(ctx, projectDir, ref, "up", "-d", "--remove-orphans"); err != nil {
		return err
	}
	if err := p.runCompose(ctx, projectDir, ref, "up", "-d", "--force-recreate", "kong"); err != nil {
		return err
	}
	return p.applyDatabaseBootstrap(ctx, projectDir, ref)
}

func (p *Provisioner) SyncConfig(ctx context.Context, ref string, config control.ProjectConfig) error {
	projectDir := filepath.Join(p.rootDir, ref)
	if _, err := os.Stat(filepath.Join(projectDir, "compose.yaml")); err != nil {
		return err
	}
	envPath := filepath.Join(projectDir, ".env")
	values, err := readEnvFile(envPath)
	if err != nil {
		return err
	}
	applyRuntimeDefaultEnvValues(values, control.ProjectSpec{Ref: ref})
	applyConfigEnvValues(values, config)
	if config.Area == "functions" {
		if err := materializeFunctionImportMap(projectDir, values, config.Config["import_map"]); err != nil {
			return err
		}
	}
	if err := writeEnvValues(envPath, values); err != nil {
		return err
	}
	if config.Area == "functions" {
		if err := os.MkdirAll(filepath.Join(projectDir, "functions", "main"), 0o700); err != nil {
			return err
		}
		if err := writeDefaultFunctionEntrypoint(filepath.Join(projectDir, "functions", "main", "index.ts")); err != nil {
			return err
		}
	}
	if !p.apply {
		return nil
	}
	if err := p.runCompose(ctx, projectDir, ref, "up", "-d"); err != nil {
		return err
	}
	if err := p.runCompose(ctx, projectDir, ref, "up", "-d", "--force-recreate", "kong"); err != nil {
		return err
	}
	return p.applyDatabaseBootstrap(ctx, projectDir, ref)
}

func (p *Provisioner) SyncServices(ctx context.Context, ref string, spec control.ProjectSpec) error {
	projectDir := filepath.Join(p.rootDir, ref)
	if _, err := os.Stat(filepath.Join(projectDir, "compose.yaml")); err != nil {
		return err
	}
	envPath := filepath.Join(projectDir, ".env")
	values, err := readEnvFile(envPath)
	if err != nil {
		return err
	}
	applyRuntimeDefaultEnvValues(values, spec)
	applyServiceEnvValues(values, spec.Services)
	if err := writeEnvValues(envPath, values); err != nil {
		return err
	}
	if err := writeKongConfigFile(filepath.Join(projectDir, "kong.yml"), spec.Ref, spec.Services); err != nil {
		return err
	}
	if err := writeKongEntrypointFile(filepath.Join(projectDir, "kong-entrypoint.sh")); err != nil {
		return err
	}
	if err := writeVectorConfigFile(filepath.Join(projectDir, "vector.yml"), spec.Ref); err != nil {
		return err
	}
	if err := writePoolerConfigFile(filepath.Join(projectDir, "pooler.exs")); err != nil {
		return err
	}
	if err := writePostgresHBAFile(filepath.Join(projectDir, "pg_hba.conf")); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Join(projectDir, "functions", "main"), 0o700); err != nil {
		return err
	}
	if err := writeDefaultFunctionEntrypoint(filepath.Join(projectDir, "functions", "main", "index.ts")); err != nil {
		return err
	}
	if err := writeComposeFile(filepath.Join(projectDir, "compose.yaml"), spec); err != nil {
		return err
	}
	if !p.apply {
		return nil
	}
	if err := p.runCompose(ctx, projectDir, ref, "up", "-d", "--remove-orphans"); err != nil {
		return err
	}
	if err := p.runCompose(ctx, projectDir, ref, "up", "-d", "--force-recreate", "kong"); err != nil {
		return err
	}
	functionsEnabled := serviceStatesFromEnv(values)["functions"]
	if service, ok := spec.Services["functions"]; ok {
		functionsEnabled = service.Enabled
	}
	if functionsEnabled {
		return p.runCompose(ctx, projectDir, ref, "up", "-d", "--force-recreate", "edge-runtime")
	}
	return nil
}

func (p *Provisioner) SyncAuthHooks(ctx context.Context, ref string, hooks []control.ProjectAuthHook) error {
	projectDir := filepath.Join(p.rootDir, ref)
	if _, err := os.Stat(filepath.Join(projectDir, "compose.yaml")); err != nil {
		return err
	}
	envPath := filepath.Join(projectDir, ".env")
	values, err := readEnvFile(envPath)
	if err != nil {
		return err
	}
	applyRuntimeDefaultEnvValues(values, control.ProjectSpec{Ref: ref})
	applyAuthHookEnvValues(values, hooks)
	if err := writeEnvValues(envPath, values); err != nil {
		return err
	}
	if err := writeAuthHooksFile(filepath.Join(projectDir, "auth-hooks.json"), hooks); err != nil {
		return err
	}
	if !p.apply {
		return nil
	}
	if err := p.runCompose(ctx, projectDir, ref, "up", "-d"); err != nil {
		return err
	}
	if err := p.runCompose(ctx, projectDir, ref, "up", "-d", "--force-recreate", "kong"); err != nil {
		return err
	}
	return p.applyDatabaseBootstrap(ctx, projectDir, ref)
}

func (p *Provisioner) Destroy(ctx context.Context, ref string) error {
	return p.DestroyWithOptions(ctx, ref, control.DestroyOptions{})
}

func (p *Provisioner) DestroyWithOptions(ctx context.Context, ref string, opts control.DestroyOptions) error {
	projectDir := filepath.Join(p.rootDir, ref)
	if p.apply {
		args := []string{"down"}
		if !opts.RetainVolumes {
			args = append(args, "-v")
		}
		composeFiles, err := projectComposeFilesWithReplicaOverlay(projectDir)
		if err != nil {
			return err
		}
		if err := p.runComposeWithFiles(ctx, projectDir, ref, composeFiles, args...); err != nil {
			return err
		}
		if !opts.RetainVolumes {
			if err := p.removeProjectLabeledVolumes(ctx, ref); err != nil {
				return err
			}
		}
	}
	if opts.RetainVolumes {
		if err := p.writeRetainedVolumeManifest(ref); err != nil {
			return err
		}
	}
	return os.RemoveAll(projectDir)
}

func projectComposeFilesWithReplicaOverlay(projectDir string) ([]string, error) {
	composeFile, err := filepath.Abs(filepath.Join(projectDir, "compose.yaml"))
	if err != nil {
		return nil, fmt.Errorf("resolve compose file path: %w", err)
	}
	files := []string{composeFile}
	replicaOverlay := filepath.Join(projectDir, "replicas", "compose.yaml")
	if _, err := os.Stat(replicaOverlay); err == nil {
		abs, err := filepath.Abs(replicaOverlay)
		if err != nil {
			return nil, fmt.Errorf("resolve replica compose file path: %w", err)
		}
		files = append(files, abs)
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	return files, nil
}

func (p *Provisioner) removeProjectLabeledVolumes(ctx context.Context, ref string) error {
	cli := composeVolumeCLI(p.command)
	if cli == "" {
		return nil
	}
	output, err := exec.CommandContext(ctx, cli, "volume", "ls", "-q", "--filter", "label=com.docker.compose.project="+ref).Output()
	if err != nil {
		return fmt.Errorf("list compose project volumes: %w", err)
	}
	for _, volume := range strings.Fields(string(output)) {
		removeOutput, err := exec.CommandContext(ctx, cli, "volume", "rm", volume).CombinedOutput()
		if err != nil && !strings.Contains(string(removeOutput), "No such volume") {
			return fmt.Errorf("remove compose project volume %s: %w: %s", volume, err, strings.TrimSpace(string(removeOutput)))
		}
	}
	return nil
}

func composeVolumeCLI(command string) string {
	parts := strings.Fields(command)
	if len(parts) >= 2 && parts[1] == "compose" {
		return parts[0]
	}
	return ""
}

func (p *Provisioner) Status(ctx context.Context, ref string) (control.ProjectStatus, error) {
	projectDir := filepath.Join(p.rootDir, ref)
	composeFile := filepath.Join(projectDir, "compose.yaml")
	if _, err := os.Stat(composeFile); err != nil {
		return control.ProjectStatus{Ref: ref, Phase: control.ProjectError, Message: err.Error()}, err
	}
	phase := control.ProjectHealthy
	message := "compose project rendered"
	serviceStates := control.DefaultProjectServiceStates()
	if env, err := readEnvFile(filepath.Join(projectDir, ".env")); err == nil {
		switch strings.TrimSpace(env["SUPADUPA_DESIRED_STATE"]) {
		case "paused":
			phase = control.ProjectPaused
			message = "compose project paused"
		case "running", "":
		default:
			phase = control.ProjectDegraded
			message = "compose render drift: unsupported desired state " + env["SUPADUPA_DESIRED_STATE"]
		}
		serviceStates = serviceStatesFromEnv(env)
	}
	var drift []string
	for _, name := range requiredProjectFiles() {
		if _, err := os.Stat(filepath.Join(projectDir, name)); err != nil {
			drift = append(drift, "missing "+name)
		}
	}
	composePayload, err := os.ReadFile(composeFile)
	if err != nil {
		return control.ProjectStatus{Ref: ref, Phase: control.ProjectError, Message: err.Error()}, err
	}
	for _, required := range requiredComposeFragments(serviceStates) {
		if !strings.Contains(string(composePayload), required) {
			drift = append(drift, "compose missing "+required)
		}
	}
	if len(drift) > 0 {
		sort.Strings(drift)
		return control.ProjectStatus{
			Ref:     ref,
			Phase:   control.ProjectDegraded,
			Message: "compose render drift: " + strings.Join(drift, "; "),
			Endpoints: map[string]string{
				"api":     fmt.Sprintf("https://%s", ref),
				"kong":    fmt.Sprintf("https://%s", ref),
				"studio":  fmt.Sprintf("https://%s/studio", ref),
				"rest":    fmt.Sprintf("https://%s/rest/v1", ref),
				"graphql": fmt.Sprintf("https://%s/graphql/v1", ref),
			},
			Services: renderedRuntimeServices(serviceStates),
		}, nil
	}
	livePhase, liveMessage, runtimeServices, liveErr := p.liveComposeStatus(ctx, projectDir, ref, phase, message, serviceStates)
	if liveErr != nil {
		return control.ProjectStatus{Ref: ref, Phase: control.ProjectError, Message: liveErr.Error()}, liveErr
	}
	phase = livePhase
	message = liveMessage
	endpoints := map[string]string{
		"api":  fmt.Sprintf("https://%s", ref),
		"kong": fmt.Sprintf("https://%s", ref),
	}
	if serviceStates["studio"] {
		endpoints["studio"] = fmt.Sprintf("https://%s/studio", ref)
	}
	if serviceStates["rest"] {
		endpoints["rest"] = fmt.Sprintf("https://%s/rest/v1", ref)
	}
	if serviceStates["graphql"] {
		endpoints["graphql"] = fmt.Sprintf("https://%s/graphql/v1", ref)
	}
	if serviceStates["realtime"] {
		endpoints["realtime"] = fmt.Sprintf("https://%s/realtime/v1", ref)
	}
	if serviceStates["storage"] {
		endpoints["storage"] = fmt.Sprintf("https://%s/storage/v1", ref)
	}
	if serviceStates["functions"] {
		endpoints["functions"] = fmt.Sprintf("https://%s/functions/v1", ref)
	}
	return control.ProjectStatus{
		Ref:       ref,
		Phase:     phase,
		Message:   message,
		Endpoints: endpoints,
		Services:  runtimeServices,
	}, nil
}

func (p *Provisioner) liveComposeStatus(ctx context.Context, projectDir string, ref string, renderedPhase control.ProjectPhase, renderedMessage string, serviceStates map[string]bool) (control.ProjectPhase, string, []control.RuntimeService, error) {
	if !p.apply {
		return renderedPhase, renderedMessage, renderedRuntimeServices(serviceStates), nil
	}
	output, err := p.runComposeOutput(ctx, projectDir, ref, "ps", "--format", "json")
	if err != nil {
		return control.ProjectError, "", nil, err
	}
	services, err := parseComposePS(output)
	if err != nil {
		return control.ProjectError, "", nil, err
	}
	runtimeServices := liveRuntimeServices(serviceStates, services)
	if renderedPhase == control.ProjectPaused {
		expected := expectedComposeServices(serviceStates)
		if missing := missingComposeServices(services, expected); len(missing) > 0 {
			return control.ProjectDegraded, "compose desired paused but live services are missing " + strings.Join(missing, ", "), runtimeServices, nil
		}
		running := runningComposeServices(services, expected)
		if len(running) == 0 {
			return control.ProjectPaused, "compose project paused", runtimeServices, nil
		}
		return control.ProjectDegraded, "compose desired paused but live services still running " + strings.Join(running, ", "), runtimeServices, nil
	}
	missing := missingComposeServices(services, expectedComposeServices(serviceStates))
	unhealthy := unhealthyComposeServices(services, expectedComposeServices(serviceStates))
	unexpected := unexpectedComposeServices(services, expectedComposeServices(serviceStates))
	if len(missing) > 0 || len(unhealthy) > 0 || len(unexpected) > 0 {
		var parts []string
		if len(missing) > 0 {
			parts = append(parts, "missing live services "+strings.Join(missing, ", "))
		}
		if len(unhealthy) > 0 {
			parts = append(parts, "unhealthy live services "+strings.Join(unhealthy, ", "))
		}
		if len(unexpected) > 0 {
			parts = append(parts, "unexpected live services "+strings.Join(unexpected, ", "))
		}
		return control.ProjectDegraded, "compose live drift: " + strings.Join(parts, "; "), runtimeServices, nil
	}
	return control.ProjectHealthy, "compose project running", runtimeServices, nil
}

type composePSService struct {
	Service  string `json:"Service"`
	Name     string `json:"Name"`
	State    string `json:"State"`
	Health   string `json:"Health"`
	ExitCode int    `json:"ExitCode"`
}

func parseComposePS(payload []byte) (map[string]composePSService, error) {
	trimmed := strings.TrimSpace(string(payload))
	if trimmed == "" {
		return nil, fmt.Errorf("compose ps returned no services")
	}
	var rows []composePSService
	if strings.HasPrefix(trimmed, "[") {
		if err := json.Unmarshal([]byte(trimmed), &rows); err != nil {
			return nil, fmt.Errorf("parse compose ps: %w", err)
		}
	} else {
		decoder := json.NewDecoder(strings.NewReader(trimmed))
		for {
			var row composePSService
			if err := decoder.Decode(&row); err != nil {
				if errors.Is(err, io.EOF) {
					break
				}
				return nil, fmt.Errorf("parse compose ps: %w", err)
			}
			rows = append(rows, row)
		}
	}
	services := make(map[string]composePSService, len(rows))
	for _, row := range rows {
		service := strings.TrimSpace(row.Service)
		if service == "" {
			continue
		}
		row.State = strings.ToLower(strings.TrimSpace(row.State))
		row.Health = strings.ToLower(strings.TrimSpace(row.Health))
		services[service] = row
	}
	if len(services) == 0 {
		return nil, fmt.Errorf("compose ps returned no named services")
	}
	return services, nil
}

func expectedComposeServices(serviceStates map[string]bool) []string {
	services := []string{"db", "kong", "meta"}
	for _, service := range control.AllowedProjectServices() {
		if serviceStates[service] {
			if composeService := optionalComposeService(service); composeService != "" {
				services = append(services, composeService)
			}
		}
	}
	sort.Strings(services)
	return services
}

func missingComposeServices(actual map[string]composePSService, expected []string) []string {
	var missing []string
	for _, service := range expected {
		if _, ok := actual[service]; !ok {
			missing = append(missing, service)
		}
	}
	return missing
}

func unexpectedComposeServices(actual map[string]composePSService, expected []string) []string {
	expectedSet := map[string]struct{}{}
	for _, service := range expected {
		expectedSet[service] = struct{}{}
	}
	var unexpected []string
	for service, row := range actual {
		if _, ok := expectedSet[service]; ok {
			continue
		}
		if !knownComposeService(service) {
			continue
		}
		detail := service
		if row.State != "" {
			detail += "=" + row.State
		}
		unexpected = append(unexpected, detail)
	}
	sort.Strings(unexpected)
	return unexpected
}

func unhealthyComposeServices(actual map[string]composePSService, expected []string) []string {
	var unhealthy []string
	for _, service := range expected {
		row, ok := actual[service]
		if !ok {
			continue
		}
		if !composeServiceRunning(row) {
			detail := service
			if row.State != "" {
				detail += "=" + row.State
			}
			if row.Health != "" {
				detail += "/" + row.Health
			}
			unhealthy = append(unhealthy, detail)
		}
	}
	return unhealthy
}

func runningComposeServices(actual map[string]composePSService, expected []string) []string {
	var running []string
	for _, service := range expected {
		row, ok := actual[service]
		if !ok {
			continue
		}
		if composeServiceRunning(row) {
			running = append(running, service)
		}
	}
	return running
}

func composeServiceRunning(row composePSService) bool {
	if row.State != "running" {
		return false
	}
	return row.Health == "" || row.Health == "healthy"
}

func renderedRuntimeServices(serviceStates map[string]bool) []control.RuntimeService {
	services := composeServiceDescriptors(serviceStates)
	out := make([]control.RuntimeService, 0, len(services))
	for _, service := range services {
		state := "rendered"
		message := "service rendered into compose desired state"
		if !service.Desired {
			state = "disabled"
			message = "service disabled in desired state"
		}
		service.State = state
		service.Message = message
		out = append(out, service)
	}
	return out
}

func liveRuntimeServices(serviceStates map[string]bool, actual map[string]composePSService) []control.RuntimeService {
	services := composeServiceDescriptors(serviceStates)
	seen := map[string]struct{}{}
	out := make([]control.RuntimeService, 0, len(services))
	for _, service := range services {
		seen[service.ComposeService] = struct{}{}
		row, ok := actual[service.ComposeService]
		if !service.Desired {
			service.State = "disabled"
			service.Message = "service disabled in desired state"
			if ok {
				service.State = row.State
				service.Health = row.Health
				service.ExitCode = row.ExitCode
				service.Message = "service disabled but still present in Docker Compose state"
			}
			out = append(out, service)
			continue
		}
		if !ok {
			service.State = "missing"
			service.Message = "expected service is missing from Docker Compose state"
			out = append(out, service)
			continue
		}
		service.State = row.State
		service.Health = row.Health
		service.ExitCode = row.ExitCode
		if composeServiceRunning(row) {
			service.Message = "service running"
		} else {
			service.Message = "service not running or unhealthy"
		}
		out = append(out, service)
	}
	for name, row := range actual {
		if _, ok := seen[name]; ok || !knownComposeService(name) {
			continue
		}
		out = append(out, control.RuntimeService{
			Name:           serviceDisplayName(name),
			ComposeService: name,
			Desired:        false,
			State:          row.State,
			Health:         row.Health,
			ExitCode:       row.ExitCode,
			Message:        "unexpected service in Docker Compose state",
		})
	}
	sort.SliceStable(out, func(i, j int) bool {
		return out[i].ComposeService < out[j].ComposeService
	})
	return out
}

func composeServiceDescriptors(serviceStates map[string]bool) []control.RuntimeService {
	out := []control.RuntimeService{
		{Name: "Postgres", ComposeService: "db", Desired: true},
		{Name: "Kong gateway", ComposeService: "kong", Desired: true},
		{Name: "Postgres Meta", ComposeService: "meta", Desired: true},
	}
	for _, service := range control.AllowedProjectServices() {
		composeService := optionalComposeService(service)
		if composeService == "" {
			continue
		}
		out = append(out, control.RuntimeService{
			Name:           serviceDisplayName(composeService),
			ComposeService: composeService,
			Desired:        serviceStates[service],
		})
	}
	sort.SliceStable(out, func(i, j int) bool {
		return out[i].ComposeService < out[j].ComposeService
	})
	return out
}

func optionalComposeService(service string) string {
	switch service {
	case "auth", "rest", "realtime", "storage", "imgproxy", "pooler", "studio", "analytics", "vector":
		return service
	case "functions":
		return "edge-runtime"
	default:
		return ""
	}
}

func knownComposeService(service string) bool {
	if service == "db" || service == "kong" || service == "meta" {
		return true
	}
	for _, allowed := range control.AllowedProjectServices() {
		if optionalComposeService(allowed) == service {
			return true
		}
	}
	return false
}

func serviceDisplayName(service string) string {
	switch service {
	case "db":
		return "Postgres"
	case "kong":
		return "Kong gateway"
	case "meta":
		return "Postgres Meta"
	case "auth":
		return "Auth"
	case "rest":
		return "REST API"
	case "realtime":
		return "Realtime"
	case "storage":
		return "Storage"
	case "imgproxy":
		return "Image proxy"
	case "edge-runtime":
		return "Edge Runtime"
	case "pooler":
		return "Supavisor pooler"
	case "studio":
		return "Studio"
	case "analytics":
		return "Logflare analytics"
	case "vector":
		return "Vector logs"
	default:
		return service
	}
}

func requiredProjectFiles() []string {
	return []string{
		".env",
		"compose.yaml",
		"kong.yml",
		"kong-entrypoint.sh",
		"pooler.exs",
		"vector.yml",
		"pg_hba.conf",
		"00-supadupa-init.sql",
		"auth-hooks.json",
		"log-drains",
	}
}

func requiredComposeFragments(services map[string]bool) []string {
	fragments := []string{
		"supabase/postgres:",
		"supabase/postgres-meta:",
		"./pg_hba.conf:/etc/postgresql/pg_hba.conf:ro",
		"./00-supadupa-init.sql:/etc/postgresql.schema.sql:ro",
		"./kong.yml:/home/kong/kong.yml:ro",
		"./kong-entrypoint.sh:/home/kong/kong-entrypoint.sh:ro",
		"supadupa-ingress:",
		"internal: true",
	}
	optional := map[string]string{
		"auth":      "supabase/gotrue:",
		"rest":      "postgrest/postgrest:",
		"realtime":  "supabase/realtime:",
		"storage":   "supabase/storage-api:",
		"imgproxy":  "darthsim/imgproxy:",
		"functions": "supabase/edge-runtime:",
		"pooler":    "supabase/supavisor:",
		"studio":    "supabase/studio:",
		"analytics": "supabase/logflare:",
		"vector":    "timberio/vector:",
	}
	for _, service := range control.AllowedProjectServices() {
		if services[service] {
			if fragment := optional[service]; fragment != "" {
				fragments = append(fragments, fragment)
			}
		}
	}
	if services["functions"] {
		fragments = append(fragments, "./functions:/home/deno/functions")
	}
	if services["vector"] {
		fragments = append(fragments, "./vector.yml:/etc/vector/vector.yml:ro", "./log-drains:/etc/vector/log-drains:ro")
	}
	return fragments
}

func (p *Provisioner) Upgrade(ctx context.Context, ref string, version string) error {
	version = strings.TrimSpace(version)
	if version == "" {
		return fmt.Errorf("version is required")
	}
	projectDir := filepath.Join(p.rootDir, ref)
	if _, err := os.Stat(filepath.Join(projectDir, "compose.yaml")); err != nil {
		return err
	}
	if err := updateEnvValue(filepath.Join(projectDir, ".env"), "STACK_VERSION", version); err != nil {
		return err
	}
	env, err := readEnvFile(filepath.Join(projectDir, ".env"))
	if err != nil {
		return err
	}
	services := map[string]control.ServiceSpec{}
	for service, enabled := range serviceStatesFromEnv(env) {
		services[service] = control.ServiceSpec{Enabled: enabled}
	}
	spec := control.ProjectSpec{
		Ref:          ref,
		StackVersion: version,
		Services:     services,
	}
	postgresPassword := env["POSTGRES_PASSWORD"]
	if postgresPassword == "" {
		postgresPassword = randomHex(24)
		if err := updateEnvValue(filepath.Join(projectDir, ".env"), "POSTGRES_PASSWORD", postgresPassword); err != nil {
			return err
		}
	}
	if err := writeKongConfigFile(filepath.Join(projectDir, "kong.yml"), ref, spec.Services); err != nil {
		return err
	}
	if err := writeKongEntrypointFile(filepath.Join(projectDir, "kong-entrypoint.sh")); err != nil {
		return err
	}
	if err := writeVectorConfigFile(filepath.Join(projectDir, "vector.yml"), ref); err != nil {
		return err
	}
	if err := writePoolerConfigFile(filepath.Join(projectDir, "pooler.exs")); err != nil {
		return err
	}
	if err := writeDatabaseInitFile(filepath.Join(projectDir, "00-supadupa-init.sql"), postgresPassword); err != nil {
		return err
	}
	if err := writePostgresHBAFile(filepath.Join(projectDir, "pg_hba.conf")); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Join(projectDir, "functions", "main"), 0o700); err != nil {
		return err
	}
	if err := writeDefaultFunctionEntrypoint(filepath.Join(projectDir, "functions", "main", "index.ts")); err != nil {
		return err
	}
	if err := writeComposeFile(filepath.Join(projectDir, "compose.yaml"), spec); err != nil {
		return err
	}
	if !p.apply {
		return nil
	}
	if control.ProjectServiceStates(spec.Services)["pooler"] {
		if err := p.runCompose(ctx, projectDir, ref, "up", "-d", "--scale", "pooler=0"); err != nil {
			return err
		}
		if err := p.runCompose(ctx, projectDir, ref, "up", "-d", "--force-recreate", "kong"); err != nil {
			return err
		}
		if err := p.applyDatabaseBootstrap(ctx, projectDir, ref); err != nil {
			return err
		}
		return p.ensurePoolerStarted(ctx, projectDir, ref)
	}
	if err := p.runCompose(ctx, projectDir, ref, "up", "-d"); err != nil {
		return err
	}
	if err := p.runCompose(ctx, projectDir, ref, "up", "-d", "--force-recreate", "kong"); err != nil {
		return err
	}
	return p.applyDatabaseBootstrap(ctx, projectDir, ref)
}

func (p *Provisioner) Pause(ctx context.Context, ref string) error {
	projectDir := filepath.Join(p.rootDir, ref)
	if _, err := os.Stat(filepath.Join(projectDir, "compose.yaml")); err != nil {
		return err
	}
	if p.apply {
		if err := p.runCompose(ctx, projectDir, ref, "stop"); err != nil {
			return err
		}
	}
	return updateEnvValue(filepath.Join(projectDir, ".env"), "SUPADUPA_DESIRED_STATE", "paused")
}

func (p *Provisioner) Resume(ctx context.Context, ref string) error {
	projectDir := filepath.Join(p.rootDir, ref)
	if _, err := os.Stat(filepath.Join(projectDir, "compose.yaml")); err != nil {
		return err
	}
	if p.apply {
		if err := p.runCompose(ctx, projectDir, ref, "up", "-d"); err != nil {
			return err
		}
	}
	return updateEnvValue(filepath.Join(projectDir, ".env"), "SUPADUPA_DESIRED_STATE", "running")
}

func (p *Provisioner) Scale(ctx context.Context, ref string, tier control.ResourceTier) error {
	if _, ok := map[control.ResourceTier]struct{}{
		control.ResourceTierSmall:  {},
		control.ResourceTierMedium: {},
		control.ResourceTierLarge:  {},
	}[tier]; !ok {
		return fmt.Errorf("unsupported resource tier %q", tier)
	}
	projectDir := filepath.Join(p.rootDir, ref)
	if _, err := os.Stat(filepath.Join(projectDir, "compose.yaml")); err != nil {
		return err
	}
	if err := updateEnvValue(filepath.Join(projectDir, ".env"), "RESOURCE_TIER", string(tier)); err != nil {
		return err
	}
	if err := writeScaleManifest(filepath.Join(projectDir, "scale.yaml"), ref, tier); err != nil {
		return err
	}
	if !p.apply {
		return nil
	}
	return p.runCompose(ctx, projectDir, ref, "up", "-d")
}

func (p *Provisioner) AddReplica(ctx context.Context, ref string, opts control.ReplicaOpts) error {
	projectDir := filepath.Join(p.rootDir, ref)
	if _, err := os.Stat(filepath.Join(projectDir, "compose.yaml")); err != nil {
		return err
	}
	replicaID := strings.TrimSpace(opts.ID)
	if replicaID == "" {
		replicaID = randomHex(8)
	}
	name := strings.TrimSpace(opts.Name)
	if name == "" {
		name = "replica-" + replicaID[:minInt(8, len(replicaID))]
	}
	replicaDir := filepath.Join(projectDir, "replicas")
	if err := os.MkdirAll(replicaDir, 0o700); err != nil {
		return err
	}
	if err := writeReplicaManifest(filepath.Join(replicaDir, replicaID+".yaml"), ref, replicaID, name, opts); err != nil {
		return err
	}
	if err := writeReplicaEnv(filepath.Join(replicaDir, replicaID+".env"), ref, name, opts); err != nil {
		return err
	}
	if !p.apply {
		return nil
	}
	return nil
}

func (p *Provisioner) SyncReplicas(ctx context.Context, ref string, replicas []control.ProjectReplica) error {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return fmt.Errorf("project ref is required")
	}
	projectDir := filepath.Join(p.rootDir, ref)
	composePath := filepath.Join(projectDir, "compose.yaml")
	if _, err := os.Stat(composePath); err != nil {
		return err
	}
	replicaDir := filepath.Join(projectDir, "replicas")
	if err := os.MkdirAll(replicaDir, 0o700); err != nil {
		return err
	}
	if err := writePostgresHBAFile(filepath.Join(projectDir, "pg_hba.conf")); err != nil {
		return err
	}
	if len(replicas) == 0 {
		if err := os.Remove(filepath.Join(replicaDir, "compose.yaml")); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		if err := removeReplicaEnvFiles(replicaDir, map[string]struct{}{}); err != nil {
			return err
		}
		if p.apply {
			return p.runCompose(ctx, projectDir, ref, "up", "-d", "--remove-orphans")
		}
		return nil
	}
	envNames := map[string]struct{}{}
	for _, replica := range replicas {
		name := strings.TrimSpace(replica.Name)
		if name == "" {
			name = replica.ID
		}
		if err := writeReplicaEnv(filepath.Join(replicaDir, replica.ID+".env"), ref, name, control.ReplicaOpts{
			ID:               replica.ID,
			Name:             replica.Name,
			HostID:           replica.HostID,
			Region:           replica.Region,
			Tier:             replica.Tier,
			ReadWeight:       replica.ReadWeight,
			FailoverPriority: replica.FailoverPriority,
		}); err != nil {
			return err
		}
		envNames[replica.ID+".env"] = struct{}{}
	}
	if err := removeReplicaEnvFiles(replicaDir, envNames); err != nil {
		return err
	}
	overlayPath := filepath.Join(replicaDir, "compose.yaml")
	if err := writeReplicasComposeFile(overlayPath, ref, replicas); err != nil {
		return err
	}
	if !p.apply {
		return nil
	}
	if err := p.runCompose(ctx, projectDir, ref, "exec", "-T", "db", "psql", "-U", "supabase_admin", "-d", "postgres", "-c", "SELECT pg_reload_conf()"); err != nil {
		return err
	}
	if err := p.runComposeWithFiles(ctx, projectDir, ref, []string{composePath, overlayPath}, "up", "-d", "--remove-orphans"); err != nil {
		return err
	}
	for _, replica := range replicas {
		if err := p.waitForReplicaRecovery(ctx, projectDir, ref, overlayPath, replica); err != nil {
			return err
		}
	}
	return nil
}

func (p *Provisioner) CloneBranch(ctx context.Context, opts control.BranchCloneOptions) (control.BranchCloneResult, error) {
	sourceRef := strings.TrimSpace(opts.SourceRef)
	branchRef := strings.TrimSpace(opts.BranchRef)
	if sourceRef == "" {
		return control.BranchCloneResult{}, fmt.Errorf("source ref is required")
	}
	if branchRef == "" {
		return control.BranchCloneResult{}, fmt.Errorf("branch ref is required")
	}
	sourceDir := filepath.Join(p.rootDir, sourceRef)
	branchDir := filepath.Join(p.rootDir, branchRef)
	if _, err := os.Stat(filepath.Join(sourceDir, "compose.yaml")); err != nil {
		return control.BranchCloneResult{}, fmt.Errorf("source project is not rendered: %w", err)
	}
	if _, err := os.Stat(filepath.Join(branchDir, "compose.yaml")); err != nil {
		return control.BranchCloneResult{}, fmt.Errorf("branch project is not rendered: %w", err)
	}
	cloneDir := filepath.Join(branchDir, "branch-clone")
	if err := os.MkdirAll(cloneDir, 0o700); err != nil {
		return control.BranchCloneResult{}, err
	}
	if strings.TrimSpace(p.branchCloneCommand) == "" {
		path := filepath.Join(cloneDir, "clone-plan.sql")
		if err := writeBranchClonePlan(path, opts, sourceDir, branchDir); err != nil {
			return control.BranchCloneResult{}, err
		}
		return control.BranchCloneResult{Path: path, State: "dry-run"}, nil
	}
	path := filepath.Join(cloneDir, "clone.log")
	if err := p.runBranchCloneCommand(ctx, path, opts, sourceDir, branchDir); err != nil {
		return control.BranchCloneResult{}, err
	}
	return control.BranchCloneResult{Path: path, State: "completed"}, nil
}

func (p *Provisioner) CollectProjectTelemetry(ctx context.Context, ref string) (control.TelemetrySampleInput, error) {
	ref = strings.ToLower(strings.TrimSpace(ref))
	if ref == "" {
		return control.TelemetrySampleInput{}, fmt.Errorf("project ref is required")
	}
	projectDir := filepath.Join(p.rootDir, ref)
	if _, err := os.Stat(filepath.Join(projectDir, "compose.yaml")); err != nil {
		return control.TelemetrySampleInput{}, err
	}
	if !p.apply {
		return control.TelemetrySampleInput{}, fmt.Errorf("compose telemetry requires SUPADUPA_COMPOSE_APPLY=true")
	}
	output, err := p.runComposeOutput(ctx, projectDir, ref, "stats", "--no-stream", "--format", "json")
	if err != nil {
		return control.TelemetrySampleInput{}, err
	}
	return parseComposeStats(output, time.Now().UTC())
}

func (p *Provisioner) runCompose(ctx context.Context, projectDir string, ref string, args ...string) error {
	_, err := p.runComposeOutput(ctx, projectDir, ref, args...)
	return err
}

func (p *Provisioner) runComposeWithFiles(ctx context.Context, projectDir string, ref string, composeFiles []string, args ...string) error {
	_, err := p.runComposeOutputWithFiles(ctx, projectDir, ref, composeFiles, args...)
	return err
}

func (p *Provisioner) ensurePoolerStarted(ctx context.Context, projectDir string, ref string) error {
	if err := p.runCompose(ctx, projectDir, ref, "up", "-d", "pooler"); err != nil {
		return err
	}
	if err := p.applyDatabaseBootstrap(ctx, projectDir, ref); err != nil {
		return err
	}
	if err := p.runCompose(ctx, projectDir, ref, "up", "-d", "--force-recreate", "pooler"); err != nil {
		return err
	}
	return p.waitForComposeServiceRunning(ctx, projectDir, ref, "pooler", poolerRestartStableDuration())
}

func poolerStartStableDuration() time.Duration {
	return envDurationSeconds("SUPADUPA_POOLER_START_STABLE_SECONDS", 18*time.Second)
}

func poolerRestartStableDuration() time.Duration {
	return envDurationSeconds("SUPADUPA_POOLER_RESTART_STABLE_SECONDS", 30*time.Second)
}

func envDurationSeconds(name string, fallback time.Duration) time.Duration {
	raw := strings.TrimSpace(os.Getenv(name))
	if raw == "" {
		return fallback
	}
	seconds, err := strconv.Atoi(raw)
	if err != nil || seconds < 0 {
		return fallback
	}
	return time.Duration(seconds) * time.Second
}

func (p *Provisioner) waitForComposeServiceRunning(ctx context.Context, projectDir string, ref string, service string, stableFor time.Duration) error {
	deadline := time.Now().Add(stableFor)
	for {
		output, err := p.runComposeOutput(ctx, projectDir, ref, "ps", "--format", "json", service)
		if err != nil {
			return err
		}
		services, err := parseComposePS(output)
		if err != nil {
			return err
		}
		row, ok := services[service]
		if !ok {
			return fmt.Errorf("compose service %s missing", service)
		}
		if row.State != "running" {
			return fmt.Errorf("compose service %s is %s", service, row.State)
		}
		if time.Now().After(deadline) {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(3 * time.Second):
		}
	}
}

func (p *Provisioner) applyDatabaseBootstrap(ctx context.Context, projectDir string, ref string) error {
	deadline := time.Now().Add(90 * time.Second)
	var lastErr error
	for {
		err := p.runCompose(ctx, projectDir, ref, "exec", "-T", "db", "psql", "-v", "ON_ERROR_STOP=1", "-U", "supabase_admin", "-d", "postgres", "-f", "/etc/postgresql.schema.sql")
		if err == nil {
			return nil
		}
		lastErr = err
		if time.Now().After(deadline) {
			return fmt.Errorf("apply database runtime bootstrap: %w", lastErr)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(3 * time.Second):
		}
	}
}

func (p *Provisioner) runComposeOutput(ctx context.Context, projectDir string, ref string, args ...string) ([]byte, error) {
	composeFile, err := filepath.Abs(filepath.Join(projectDir, "compose.yaml"))
	if err != nil {
		return nil, fmt.Errorf("resolve compose file path: %w", err)
	}
	return p.runComposeOutputWithFiles(ctx, projectDir, ref, []string{composeFile}, args...)
}

func (p *Provisioner) runComposeOutputWithFiles(ctx context.Context, projectDir string, ref string, composeFiles []string, args ...string) ([]byte, error) {
	parts := strings.Fields(p.command)
	if len(parts) == 0 {
		return nil, fmt.Errorf("compose command is empty")
	}
	commandArgs := append([]string{}, parts[1:]...)
	commandArgs = append(commandArgs, "-p", ref)
	for _, file := range composeFiles {
		abs, err := filepath.Abs(file)
		if err != nil {
			return nil, fmt.Errorf("resolve compose file path: %w", err)
		}
		commandArgs = append(commandArgs, "-f", abs)
	}
	commandArgs = append(commandArgs, args...)
	cmd := exec.CommandContext(ctx, parts[0], commandArgs...)
	cmd.Dir = projectDir
	cmd.Env = append(os.Environ(), "COMPOSE_PROJECT_NAME="+ref)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	output, err := cmd.Output()
	if err != nil {
		details := strings.TrimSpace(stderr.String())
		if details == "" {
			details = strings.TrimSpace(string(output))
		}
		return nil, fmt.Errorf("compose %s failed: %w: %s", strings.Join(args, " "), err, details)
	}
	return output, nil
}

func (p *Provisioner) waitForReplicaRecovery(ctx context.Context, projectDir string, ref string, overlayPath string, replica control.ProjectReplica) error {
	service := replicaComposeServiceName(replica.Name)
	deadline := time.Now().Add(replicaRecoveryTimeout())
	var lastErr error
	for {
		output, err := p.runComposeOutputWithFiles(ctx, projectDir, ref, []string{filepath.Join(projectDir, "compose.yaml"), overlayPath}, "exec", "-T", service, "psql", "-U", "postgres", "-d", "postgres", "-Atc", "select pg_is_in_recovery()")
		if err == nil && strings.TrimSpace(string(output)) == "t" {
			return nil
		}
		if err != nil {
			lastErr = err
		} else {
			lastErr = fmt.Errorf("replica %s is not in recovery: %s", replica.Name, strings.TrimSpace(string(output)))
		}
		if time.Now().After(deadline) {
			return lastErr
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(3 * time.Second):
		}
	}
}

func replicaRecoveryTimeout() time.Duration {
	timeout := 240 * time.Second
	raw := strings.TrimSpace(os.Getenv("SUPADUPA_REPLICA_READY_TIMEOUT_SECONDS"))
	if raw == "" {
		return timeout
	}
	seconds, err := strconv.Atoi(raw)
	if err != nil || seconds <= 0 {
		return timeout
	}
	return time.Duration(seconds) * time.Second
}

type composeStatsRow struct {
	Name     string `json:"Name"`
	CPUPerc  string `json:"CPUPerc"`
	MemUsage string `json:"MemUsage"`
	NetIO    string `json:"NetIO"`
}

func parseComposeStats(payload []byte, sampledAt time.Time) (control.TelemetrySampleInput, error) {
	decoder := json.NewDecoder(strings.NewReader(strings.TrimSpace(string(payload))))
	sample := control.TelemetrySampleInput{
		Source:    "compose",
		SampledAt: sampledAt.UTC(),
	}
	containers := 0
	for {
		var row composeStatsRow
		if err := decoder.Decode(&row); err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			return control.TelemetrySampleInput{}, fmt.Errorf("parse compose stats: %w", err)
		}
		containers++
		cpuPercent, err := parseComposePercent(row.CPUPerc)
		if err != nil {
			return control.TelemetrySampleInput{}, fmt.Errorf("parse cpu percent for %s: %w", row.Name, err)
		}
		memUsed, memLimit, err := parseComposeBytePair(row.MemUsage)
		if err != nil {
			return control.TelemetrySampleInput{}, fmt.Errorf("parse memory usage for %s: %w", row.Name, err)
		}
		rx, tx, err := parseComposeBytePair(row.NetIO)
		if err != nil {
			return control.TelemetrySampleInput{}, fmt.Errorf("parse network io for %s: %w", row.Name, err)
		}
		sample.CPUPercent += cpuPercent
		sample.MemoryBytes += memUsed
		if memLimit > sample.MemoryLimitBytes {
			sample.MemoryLimitBytes = memLimit
		}
		sample.NetworkRxBytes += rx
		sample.NetworkTxBytes += tx
	}
	if containers == 0 {
		return control.TelemetrySampleInput{}, fmt.Errorf("compose stats returned no containers")
	}
	return sample, nil
}

func parseComposePercent(raw string) (float64, error) {
	value := strings.TrimSpace(strings.TrimSuffix(raw, "%"))
	if value == "" {
		return 0, nil
	}
	percent, err := strconv.ParseFloat(value, 64)
	if err != nil {
		return 0, err
	}
	if percent < 0 {
		return 0, fmt.Errorf("percent cannot be negative")
	}
	return percent, nil
}

func parseComposeBytePair(raw string) (int64, int64, error) {
	parts := strings.Split(raw, "/")
	if len(parts) != 2 {
		return 0, 0, fmt.Errorf("expected byte pair, got %q", raw)
	}
	first, err := parseComposeBytes(parts[0])
	if err != nil {
		return 0, 0, err
	}
	second, err := parseComposeBytes(parts[1])
	if err != nil {
		return 0, 0, err
	}
	return first, second, nil
}

func parseComposeBytes(raw string) (int64, error) {
	value := strings.TrimSpace(strings.ReplaceAll(raw, ",", ""))
	if value == "" {
		return 0, nil
	}
	split := 0
	for split < len(value) {
		ch := value[split]
		if (ch >= '0' && ch <= '9') || ch == '.' || ch == '+' {
			split++
			continue
		}
		break
	}
	if split == 0 {
		return 0, fmt.Errorf("missing byte value in %q", raw)
	}
	number, err := strconv.ParseFloat(value[:split], 64)
	if err != nil {
		return 0, err
	}
	if number < 0 {
		return 0, fmt.Errorf("byte value cannot be negative")
	}
	unit := strings.ToLower(strings.TrimSpace(value[split:]))
	unit = strings.ReplaceAll(unit, " ", "")
	multiplier := float64(1)
	switch unit {
	case "", "b", "byte", "bytes":
	case "kb":
		multiplier = 1000
	case "kib":
		multiplier = 1024
	case "mb":
		multiplier = 1000 * 1000
	case "mib":
		multiplier = 1024 * 1024
	case "gb":
		multiplier = 1000 * 1000 * 1000
	case "gib":
		multiplier = 1024 * 1024 * 1024
	case "tb":
		multiplier = 1000 * 1000 * 1000 * 1000
	case "tib":
		multiplier = 1024 * 1024 * 1024 * 1024
	default:
		return 0, fmt.Errorf("unsupported byte unit %q", unit)
	}
	return int64(math.Round(number * multiplier)), nil
}

func (p *Provisioner) writeRetainedVolumeManifest(ref string) error {
	dir := filepath.Join(p.rootDir, "_retained")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	now := time.Now().UTC()
	composeProject := ref
	volumes := []string{
		composeProject + "_db-data",
		composeProject + "_storage-data",
		composeProject + "_logs",
	}
	payload, err := json.MarshalIndent(map[string]any{
		"project_ref":     ref,
		"compose_project": composeProject,
		"retained_at":     now,
		"volumes":         volumes,
		"instructions": []string{
			"These Docker Compose named volumes were intentionally retained by DELETE /v1/projects/{ref}?retain_volumes=true.",
			"Remove them manually with docker volume rm after confirming the data is no longer needed.",
		},
	}, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, ref+"-"+now.Format("20060102T150405Z")+".json"), append(payload, '\n'), 0o600)
}

func writeAuthHooksFile(path string, hooks []control.ProjectAuthHook) error {
	out := append([]control.ProjectAuthHook(nil), hooks...)
	if out == nil {
		out = []control.ProjectAuthHook{}
	}
	for index := range out {
		out[index].Headers = cloneStringMap(out[index].Headers)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].HookType == out[j].HookType {
			return out[i].ID < out[j].ID
		}
		return out[i].HookType < out[j].HookType
	})
	payload, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(payload, '\n'), 0o600)
}

func cloneStringMap(input map[string]string) map[string]string {
	if input == nil {
		return nil
	}
	out := make(map[string]string, len(input))
	for key, value := range input {
		out[key] = value
	}
	return out
}

func (p *Provisioner) runBranchCloneCommand(ctx context.Context, path string, opts control.BranchCloneOptions, sourceDir string, branchDir string) error {
	command := renderBranchCloneCommand(p.branchCloneCommand, opts, sourceDir, branchDir, path)
	output, err := exec.CommandContext(ctx, "sh", "-c", command).CombinedOutput()
	if err != nil {
		return fmt.Errorf("branch clone command failed: %w: %s", err, strings.TrimSpace(string(output)))
	}
	if len(output) == 0 {
		output = []byte("branch clone command completed without output\n")
	}
	return os.WriteFile(path, output, 0o600)
}

func writeEnvFile(path string, spec control.ProjectSpec) (string, error) {
	stackVersion := control.NormalizeStackReleaseVersion(spec.StackVersion)
	projectDomain := strings.TrimSpace(spec.Domain)
	if projectDomain == "" {
		projectDomain = "supadupa.test"
	}
	resourceTier := string(spec.ResourceTier)
	if resourceTier == "" {
		resourceTier = string(control.ResourceTierSmall)
	}
	stackProfile := string(spec.Profile)
	if stackProfile == "" {
		stackProfile = string(control.StackProfileFull)
	}
	orioleDBProfile := "off"
	if spec.Profile == control.StackProfileOrioleDB {
		orioleDBProfile = "preview"
	}
	apiExternalURL := fmt.Sprintf("https://%s.%s", spec.Ref, projectDomain)
	storageExternalURL := fmt.Sprintf("https://storage-%s.%s", spec.Ref, projectDomain)
	jwtSecret := randomHex(32)
	if value := strings.TrimSpace(spec.Environment["JWT_SECRET"]); value != "" {
		jwtSecret = value
	}
	postgresPassword := randomHex(24)
	if value := strings.TrimSpace(spec.Environment["POSTGRES_PASSWORD"]); value != "" {
		postgresPassword = value
	}
	values := map[string]string{
		"ANON_KEY":                          "generated-by-control-plane",
		"API_EXTERNAL_URL":                  apiExternalURL,
		"DB_AFTER_CONNECT_QUERY":            "SET search_path TO _realtime",
		"DB_ENC_KEY":                        randomHex(8),
		"DB_HOST":                           "db",
		"DB_NAME":                           "postgres",
		"DB_PASSWORD":                       postgresPassword,
		"DB_PORT":                           "5432",
		"DB_USER":                           "supabase_admin",
		"DASHBOARD_USERNAME":                "supadupa",
		"DASHBOARD_PASSWORD":                randomHex(18),
		"EDGE_RUNTIME_POLICY":               "oneshot",
		"FILE_SIZE_LIMIT":                   "52428800",
		"FUNCTIONS_VERIFY_JWT":              "true",
		"FUNCTION_WORKER_TIMEOUT_MS":        "60000",
		"GOTRUE_API_HOST":                   "0.0.0.0",
		"GOTRUE_API_PORT":                   "9999",
		"GOTRUE_DB_DATABASE_URL":            fmt.Sprintf("postgres://supabase_auth_admin:%s@db:5432/postgres", postgresPassword),
		"GOTRUE_DB_DRIVER":                  "postgres",
		"GOTRUE_JWT_ADMIN_ROLES":            "service_role",
		"GOTRUE_JWT_AUD":                    "authenticated",
		"GOTRUE_JWT_DEFAULT_GROUP_NAME":     "authenticated",
		"GOTRUE_JWT_EXP":                    "3600",
		"GOTRUE_JWT_SECRET":                 jwtSecret,
		"GOTRUE_SITE_URL":                   apiExternalURL,
		"IMGPROXY_AUTO_WEBP":                "true",
		"IMGPROXY_BIND":                     ":5001",
		"JWT_SECRET":                        jwtSecret,
		"LOGFLARE_API_KEY":                  randomHex(24),
		"LOGFLARE_LOGGER_BACKEND_API_KEY":   randomHex(24),
		"LOGFLARE_PRIVATE_ACCESS_TOKEN":     randomHex(24),
		"LOGFLARE_PUBLIC_ACCESS_TOKEN":      randomHex(24),
		"PGRST_DB_ANON_ROLE":                "anon",
		"PGRST_DB_SCHEMAS":                  "public,storage,graphql_public",
		"PGRST_DB_URI":                      fmt.Sprintf("postgres://authenticator:%s@db:5432/postgres", postgresPassword),
		"PGRST_JWT_SECRET":                  jwtSecret,
		"POSTGRES_DB":                       "postgres",
		"POSTGRES_HOST":                     "db",
		"POSTGRES_PASSWORD":                 postgresPassword,
		"POSTGRES_PORT":                     "5432",
		"POSTGRES_USER":                     "supabase_admin",
		"PROJECT_DOMAIN":                    projectDomain,
		"PROJECT_REF":                       spec.Ref,
		"REGION":                            "local",
		"REQUEST_ALLOW_X_FORWARDED_PATH":    "true",
		"REALTIME_DB_HOST":                  "db",
		"REALTIME_DB_NAME":                  "postgres",
		"REALTIME_DB_PASSWORD":              postgresPassword,
		"REALTIME_DB_PORT":                  "5432",
		"REALTIME_DB_USER":                  "supabase_admin",
		"REALTIME_JWT_SECRET":               jwtSecret,
		"RESOURCE_TIER":                     resourceTier,
		"SECRET_KEY_BASE":                   randomHex(48),
		"SERVICE_ROLE_KEY":                  "generated-by-control-plane",
		"SITE_URL":                          apiExternalURL,
		"SMTP_ADMIN_EMAIL":                  "",
		"SMTP_HOST":                         "",
		"SMTP_PASS":                         "",
		"SMTP_PORT":                         "587",
		"SMTP_SENDER_NAME":                  "",
		"SMTP_TLS_MODE":                     "starttls",
		"SMTP_USER":                         "",
		"STACK_VERSION":                     stackVersion,
		"STACK_PROFILE":                     stackProfile,
		"STORAGE_BACKEND":                   "file",
		"STORAGE_FILE_SIZE_LIMIT":           "52428800",
		"STORAGE_IMGPROXY_URL":              "http://imgproxy:5001",
		"STORAGE_PUBLIC_URL":                storageExternalURL,
		"STORAGE_TENANT_ID":                 spec.Ref,
		"TUS_URL_EXPIRY_MS":                 "3600000",
		"TUS_URL_PATH":                      "/upload/resumable",
		"UPLOAD_FILE_SIZE_LIMIT":            "524288000",
		"UPLOAD_FILE_SIZE_LIMIT_STANDARD":   "52428800",
		"UPLOAD_SIGNED_URL_EXPIRATION_TIME": "120",
		"GLOBAL_S3_BUCKET":                  spec.Ref,
		"S3_PROTOCOL_ACCESS_KEY_ID":         randomHex(24),
		"S3_PROTOCOL_ACCESS_KEY_SECRET":     randomHex(32),
		"STUDIO_DEFAULT_ORGANIZATION":       "supadupa",
		"STUDIO_DEFAULT_PROJECT":            spec.Ref,
		"STUDIO_PG_META_URL":                "http://meta:8080",
		"SUPABASE_PUBLIC_URL":               apiExternalURL,
		"SUPADUPA_DESIRED_STATE":            "running",
		"SUPADUPA_ORIOLEDB_PROFILE":         orioleDBProfile,
		"SUPADUPA_STACK_PROFILE":            stackProfile,
		"SUPAVISOR_DB_HOST":                 "db",
		"SUPAVISOR_DB_NAME":                 "postgres",
		"SUPAVISOR_DB_PASSWORD":             postgresPassword,
		"SUPAVISOR_DB_PORT":                 "5432",
		"SUPAVISOR_DB_USER":                 "supabase_admin",
		"VAULT_ENC_KEY":                     randomHex(16),
	}
	for key, value := range spec.Environment {
		values[key] = value
	}
	applyDerivedSecretEnvValues(values)
	applyServiceEnvValues(values, spec.Services)
	applyAuthHookEnvValues(values, nil)

	return values["POSTGRES_PASSWORD"], writeEnvValues(path, values)
}

func applyRuntimeDefaultEnvValues(values map[string]string, spec control.ProjectSpec) {
	ref := strings.TrimSpace(spec.Ref)
	if strings.TrimSpace(values["REQUEST_ALLOW_X_FORWARDED_PATH"]) == "" {
		values["REQUEST_ALLOW_X_FORWARDED_PATH"] = "true"
	}
	if strings.TrimSpace(values["GLOBAL_S3_BUCKET"]) == "" {
		values["GLOBAL_S3_BUCKET"] = ref
	}
	if strings.TrimSpace(values["STORAGE_PUBLIC_URL"]) == "" {
		domain := strings.TrimSpace(spec.Domain)
		if domain == "" {
			domain = strings.TrimSpace(values["PROJECT_DOMAIN"])
		}
		if ref != "" && domain != "" {
			values["STORAGE_PUBLIC_URL"] = fmt.Sprintf("https://storage-%s.%s", ref, domain)
		}
	}
	if strings.TrimSpace(values["S3_PROTOCOL_ACCESS_KEY_ID"]) == "" {
		values["S3_PROTOCOL_ACCESS_KEY_ID"] = randomHex(24)
	}
	if strings.TrimSpace(values["S3_PROTOCOL_ACCESS_KEY_SECRET"]) == "" {
		values["S3_PROTOCOL_ACCESS_KEY_SECRET"] = randomHex(32)
	}
	if strings.TrimSpace(values["TUS_URL_PATH"]) == "" {
		values["TUS_URL_PATH"] = "/upload/resumable"
	}
	if strings.TrimSpace(values["TUS_URL_EXPIRY_MS"]) == "" {
		values["TUS_URL_EXPIRY_MS"] = "3600000"
	}
	if strings.TrimSpace(values["UPLOAD_FILE_SIZE_LIMIT"]) == "" {
		values["UPLOAD_FILE_SIZE_LIMIT"] = "524288000"
	}
	if strings.TrimSpace(values["UPLOAD_FILE_SIZE_LIMIT_STANDARD"]) == "" {
		values["UPLOAD_FILE_SIZE_LIMIT_STANDARD"] = "52428800"
	}
	if strings.TrimSpace(values["UPLOAD_SIGNED_URL_EXPIRATION_TIME"]) == "" {
		values["UPLOAD_SIGNED_URL_EXPIRATION_TIME"] = "120"
	}
	if strings.TrimSpace(values["FUNCTION_WORKER_TIMEOUT_MS"]) == "" {
		values["FUNCTION_WORKER_TIMEOUT_MS"] = "60000"
	}
}

func applyServiceEnvValues(values map[string]string, services map[string]control.ServiceSpec) {
	for service, enabled := range control.ProjectServiceStates(services) {
		values[serviceEnvKey(service)] = strconv.FormatBool(enabled)
	}
}

func serviceStatesFromEnv(values map[string]string) map[string]bool {
	states := control.DefaultProjectServiceStates()
	for _, service := range control.AllowedProjectServices() {
		raw := strings.TrimSpace(values[serviceEnvKey(service)])
		if raw == "" {
			continue
		}
		parsed, err := strconv.ParseBool(raw)
		if err == nil {
			states[service] = parsed
		}
	}
	return states
}

func serviceEnvKey(service string) string {
	return "SUPADUPA_SERVICE_" + strings.ToUpper(strings.ReplaceAll(service, "-", "_")) + "_ENABLED"
}

func applyAuthHookEnvValues(values map[string]string, hooks []control.ProjectAuthHook) {
	for _, envType := range sortedAuthHookEnvTypes() {
		prefix := "GOTRUE_HOOK_" + envType
		values[prefix+"_ENABLED"] = "false"
		values[prefix+"_URI"] = ""
		values[prefix+"_SECRETS"] = ""
		metadataPrefix := "SUPADUPA_AUTH_HOOK_" + envType
		values[metadataPrefix+"_SECRET_HANDLE"] = ""
		values[metadataPrefix+"_TIMEOUT_MS"] = ""
		values[metadataPrefix+"_RETRY_ATTEMPTS"] = ""
	}
	for _, hook := range hooks {
		envType, ok := authHookEnvTypes[hook.HookType]
		if !ok {
			continue
		}
		prefix := "GOTRUE_HOOK_" + envType
		values[prefix+"_ENABLED"] = strconv.FormatBool(hook.Enabled)
		values[prefix+"_URI"] = authHookRuntimeURI(values, hook)
		values[prefix+"_SECRETS"] = strings.TrimSpace(hook.RuntimeSecret)
		metadataPrefix := "SUPADUPA_AUTH_HOOK_" + envType
		values[metadataPrefix+"_SECRET_HANDLE"] = strings.TrimSpace(hook.SecretHandle)
		values[metadataPrefix+"_TIMEOUT_MS"] = strconv.Itoa(hook.TimeoutMS)
		values[metadataPrefix+"_RETRY_ATTEMPTS"] = strconv.Itoa(hook.RetryAttempts)
	}
}

func sortedAuthHookEnvTypes() []string {
	types := make([]string, 0, len(authHookEnvTypes))
	for _, envType := range authHookEnvTypes {
		types = append(types, envType)
	}
	sort.Strings(types)
	return types
}

func authHookRuntimeURI(values map[string]string, hook control.ProjectAuthHook) string {
	if targetURI := strings.TrimSpace(hook.TargetURI); targetURI != "" {
		return targetURI
	}
	edgeFunction := strings.Trim(strings.TrimSpace(hook.EdgeFunction), "/")
	if edgeFunction == "" {
		return ""
	}
	baseURL := strings.TrimRight(strings.TrimSpace(values["API_EXTERNAL_URL"]), "/")
	if baseURL == "" {
		baseURL = "http://edge-runtime:9000"
	}
	return baseURL + "/functions/v1/" + edgeFunction
}

func applyConfigEnvValues(values map[string]string, config control.ProjectConfig) {
	switch config.Area {
	case "auth":
		copyConfigValue(values, config.Config, "email_enabled", "GOTRUE_EXTERNAL_EMAIL_ENABLED")
		copyConfigValue(values, config.Config, "magic_link_enabled", "SUPADUPA_AUTH_MAGIC_LINK_ENABLED")
		copyConfigValue(values, config.Config, "mfa_totp_enabled", "SUPADUPA_AUTH_MFA_TOTP_ENABLED")
		if _, ok := config.Config["mfa_totp_enabled"]; ok {
			enabled := strings.TrimSpace(config.Config["mfa_totp_enabled"])
			values["GOTRUE_MFA_TOTP_ENROLL_ENABLED"] = enabled
			values["GOTRUE_MFA_TOTP_VERIFY_ENABLED"] = enabled
		}
		copyConfigValue(values, config.Config, "mfa_totp_enroll_enabled", "GOTRUE_MFA_TOTP_ENROLL_ENABLED")
		copyConfigValue(values, config.Config, "mfa_totp_verify_enabled", "GOTRUE_MFA_TOTP_VERIFY_ENABLED")
		copyConfigValue(values, config.Config, "mfa_phone_enabled", "SUPADUPA_AUTH_MFA_PHONE_ENABLED")
		if _, ok := config.Config["mfa_phone_enabled"]; ok {
			enabled := strings.TrimSpace(config.Config["mfa_phone_enabled"])
			values["GOTRUE_MFA_PHONE_ENROLL_ENABLED"] = enabled
			values["GOTRUE_MFA_PHONE_VERIFY_ENABLED"] = enabled
		}
		copyConfigValue(values, config.Config, "mfa_phone_enroll_enabled", "GOTRUE_MFA_PHONE_ENROLL_ENABLED")
		copyConfigValue(values, config.Config, "mfa_phone_verify_enabled", "GOTRUE_MFA_PHONE_VERIFY_ENABLED")
		copyConfigValue(values, config.Config, "mfa_phone_otp_length", "GOTRUE_MFA_PHONE_OTP_LENGTH")
		copyConfigValue(values, config.Config, "mfa_phone_max_frequency", "GOTRUE_MFA_PHONE_MAX_FREQUENCY")
		copyConfigValue(values, config.Config, "captcha_provider", "GOTRUE_SECURITY_CAPTCHA_PROVIDER")
		copyConfigValue(values, config.Config, "captcha_provider", "SUPADUPA_AUTH_CAPTCHA_PROVIDER")
		copyConfigValue(values, config.Config, "captcha_site_key", "GOTRUE_SECURITY_CAPTCHA_SITE_KEY")
		copyConfigValue(values, config.Config, "captcha_site_key", "SUPADUPA_AUTH_CAPTCHA_SITE_KEY")
		copyConfigSecretValue(values, config.Config, "captcha_secret_handle", "GOTRUE_SECURITY_CAPTCHA_SECRET", "SUPADUPA_AUTH_CAPTCHA_SECRET_HANDLE")
		if _, ok := config.Config["captcha_provider"]; ok {
			values["GOTRUE_SECURITY_CAPTCHA_ENABLED"] = strconv.FormatBool(strings.TrimSpace(config.Config["captcha_provider"]) != "")
		}
		copyConfigValue(values, config.Config, "site_url", "SITE_URL")
		copyConfigValue(values, config.Config, "site_url", "GOTRUE_SITE_URL")
		copyConfigValue(values, config.Config, "additional_redirects", "ADDITIONAL_REDIRECT_URLS")
		copyConfigValue(values, config.Config, "additional_redirects", "GOTRUE_URI_ALLOW_LIST")
	case "auth_providers":
		copyConfigValue(values, config.Config, "oauth_google_enabled", "GOTRUE_EXTERNAL_GOOGLE_ENABLED")
		copyConfigValue(values, config.Config, "oauth_google_client_id", "GOTRUE_EXTERNAL_GOOGLE_CLIENT_ID")
		copyConfigSecretValue(values, config.Config, "oauth_google_client_secret_handle", "GOTRUE_EXTERNAL_GOOGLE_SECRET", "")
		copyConfigValue(values, config.Config, "oauth_github_enabled", "GOTRUE_EXTERNAL_GITHUB_ENABLED")
		copyConfigValue(values, config.Config, "oauth_github_client_id", "GOTRUE_EXTERNAL_GITHUB_CLIENT_ID")
		copyConfigSecretValue(values, config.Config, "oauth_github_client_secret_handle", "GOTRUE_EXTERNAL_GITHUB_SECRET", "")
		copyConfigValue(values, config.Config, "oauth_azure_enabled", "GOTRUE_EXTERNAL_AZURE_ENABLED")
		copyConfigValue(values, config.Config, "oauth_azure_client_id", "GOTRUE_EXTERNAL_AZURE_CLIENT_ID")
		copyConfigSecretValue(values, config.Config, "oauth_azure_client_secret_handle", "GOTRUE_EXTERNAL_AZURE_SECRET", "")
		for provider, envProvider := range socialOAuthProviders {
			prefix := "oauth_" + provider
			envPrefix := "GOTRUE_EXTERNAL_" + envProvider
			copyConfigValue(values, config.Config, prefix+"_enabled", envPrefix+"_ENABLED")
			copyConfigValue(values, config.Config, prefix+"_client_id", envPrefix+"_CLIENT_ID")
			copyConfigSecretValue(values, config.Config, prefix+"_client_secret_handle", envPrefix+"_SECRET", "")
			copyConfigValue(values, config.Config, prefix+"_url", envPrefix+"_URL")
			copyConfigValue(values, config.Config, prefix+"_redirect_uri", envPrefix+"_REDIRECT_URI")
			copyConfigValue(values, config.Config, prefix+"_skip_nonce_check", envPrefix+"_SKIP_NONCE_CHECK")
		}
		copyConfigValue(values, config.Config, "oauth_oidc_enabled", "SUPADUPA_AUTH_OIDC_ENABLED")
		copyConfigValue(values, config.Config, "oauth_oidc_issuer_url", "SUPADUPA_AUTH_OIDC_ISSUER_URL")
		copyConfigValue(values, config.Config, "oauth_oidc_client_id", "SUPADUPA_AUTH_OIDC_CLIENT_ID")
		copyConfigValue(values, config.Config, "oauth_oidc_client_secret_handle", "SUPADUPA_AUTH_OIDC_CLIENT_SECRET_HANDLE")
		copyConfigValue(values, config.Config, "oauth_oidc_scopes", "SUPADUPA_AUTH_OIDC_SCOPES")
		copyConfigValue(values, config.Config, "phone_enabled", "GOTRUE_EXTERNAL_PHONE_ENABLED")
		copyConfigValue(values, config.Config, "sms_provider", "GOTRUE_SMS_PROVIDER")
		copyConfigValue(values, config.Config, "sms_otp_exp", "GOTRUE_SMS_OTP_EXP")
		copyConfigValue(values, config.Config, "sms_otp_length", "GOTRUE_SMS_OTP_LENGTH")
		copyConfigValue(values, config.Config, "sms_max_frequency", "GOTRUE_SMS_MAX_FREQUENCY")
		copyConfigValue(values, config.Config, "sms_template", "GOTRUE_SMS_TEMPLATE")
		copyConfigSecretValue(values, config.Config, "sms_test_otp_handle", "GOTRUE_SMS_TEST_OTP", "SUPADUPA_SMS_TEST_OTP_HANDLE")
		copyConfigValue(values, config.Config, "sms_test_otp_valid_until", "GOTRUE_SMS_TEST_OTP_VALID_UNTIL")
		copyConfigValue(values, config.Config, "sms_twilio_account_sid", "GOTRUE_SMS_TWILIO_ACCOUNT_SID")
		copyConfigSecretValue(values, config.Config, "sms_twilio_auth_token_handle", "GOTRUE_SMS_TWILIO_AUTH_TOKEN", "")
		copyConfigValue(values, config.Config, "sms_twilio_message_service_sid", "GOTRUE_SMS_TWILIO_MESSAGE_SERVICE_SID")
		copyConfigValue(values, config.Config, "sms_messagebird_originator", "GOTRUE_SMS_MESSAGEBIRD_ORIGINATOR")
		copyConfigSecretValue(values, config.Config, "sms_messagebird_access_key_handle", "GOTRUE_SMS_MESSAGEBIRD_ACCESS_KEY", "")
		copyConfigValue(values, config.Config, "sms_textlocal_sender", "GOTRUE_SMS_TEXTLOCAL_SENDER")
		copyConfigSecretValue(values, config.Config, "sms_textlocal_api_key_handle", "GOTRUE_SMS_TEXTLOCAL_API_KEY", "")
		copyConfigValue(values, config.Config, "sms_vonage_from", "GOTRUE_SMS_VONAGE_FROM")
		copyConfigValue(values, config.Config, "sms_vonage_api_key", "GOTRUE_SMS_VONAGE_API_KEY")
		copyConfigSecretValue(values, config.Config, "sms_vonage_api_secret_handle", "GOTRUE_SMS_VONAGE_API_SECRET", "")
		copyConfigValue(values, config.Config, "saml_enabled", "GOTRUE_SAML_ENABLED")
		copyConfigValue(values, config.Config, "saml_metadata_url", "GOTRUE_SAML_METADATA_URL")
		copyConfigValue(values, config.Config, "saml_entity_id", "GOTRUE_SAML_ENTITY_ID")
		copyConfigValue(values, config.Config, "third_party_jwt_issuer", "GOTRUE_JWT_EXTERNAL_ISSUER")
		copyConfigValue(values, config.Config, "third_party_jwt_audience", "GOTRUE_JWT_EXTERNAL_AUDIENCE")
		copyConfigValue(values, config.Config, "web3_ethereum_enabled", "SUPADUPA_AUTH_WEB3_ETHEREUM_ENABLED")
		copyConfigValue(values, config.Config, "web3_solana_enabled", "SUPADUPA_AUTH_WEB3_SOLANA_ENABLED")
	case "email_templates":
		copyConfigValue(values, config.Config, "confirmation_subject", "GOTRUE_MAILER_SUBJECTS_CONFIRMATION")
		copyConfigValue(values, config.Config, "confirmation_body", "GOTRUE_MAILER_TEMPLATES_CONFIRMATION")
		copyConfigValue(values, config.Config, "recovery_subject", "GOTRUE_MAILER_SUBJECTS_RECOVERY")
		copyConfigValue(values, config.Config, "recovery_body", "GOTRUE_MAILER_TEMPLATES_RECOVERY")
		copyConfigValue(values, config.Config, "magic_link_subject", "GOTRUE_MAILER_SUBJECTS_MAGIC_LINK")
		copyConfigValue(values, config.Config, "magic_link_body", "GOTRUE_MAILER_TEMPLATES_MAGIC_LINK")
		copyConfigValue(values, config.Config, "invite_subject", "GOTRUE_MAILER_SUBJECTS_INVITE")
		copyConfigValue(values, config.Config, "invite_body", "GOTRUE_MAILER_TEMPLATES_INVITE")
		copyConfigValue(values, config.Config, "email_change_subject", "GOTRUE_MAILER_SUBJECTS_EMAIL_CHANGE")
		copyConfigValue(values, config.Config, "email_change_body", "GOTRUE_MAILER_TEMPLATES_EMAIL_CHANGE")
		copyConfigValue(values, config.Config, "sms_otp_message", "GOTRUE_SMS_TEMPLATE")
		copyConfigValue(values, config.Config, "sms_otp_message", "GOTRUE_SMS_OTP_MESSAGE")
		for _, notification := range []string{
			"password_changed",
			"email_changed",
			"phone_changed",
			"mfa_factor_enrolled",
			"mfa_factor_unenrolled",
			"identity_linked",
			"identity_unlinked",
		} {
			envSuffix := strings.ToUpper(notification)
			envPrefix := "SUPADUPA_EMAIL_NOTIFICATION_" + envSuffix
			copyConfigValue(values, config.Config, "notification_"+notification+"_enabled", envPrefix+"_ENABLED")
			copyConfigValue(values, config.Config, "notification_"+notification+"_subject", envPrefix+"_SUBJECT")
			copyConfigValue(values, config.Config, "notification_"+notification+"_body", envPrefix+"_BODY")
			copyConfigValue(values, config.Config, "notification_"+notification+"_enabled", "GOTRUE_MAILER_NOTIFICATIONS_"+envSuffix+"_ENABLED")
			copyConfigValue(values, config.Config, "notification_"+notification+"_body", "GOTRUE_MAILER_TEMPLATES_"+envSuffix+"_NOTIFICATION")
		}
	case "storage":
		if limitMB := strings.TrimSpace(config.Config["file_size_limit_mb"]); limitMB != "" {
			if parsed, err := strconv.Atoi(limitMB); err == nil && parsed > 0 {
				value := strconv.Itoa(parsed * 1024 * 1024)
				values["FILE_SIZE_LIMIT"] = value
				values["STORAGE_FILE_SIZE_LIMIT"] = value
			}
		}
		copyConfigValue(values, config.Config, "image_transform_enabled", "ENABLE_IMAGE_TRANSFORMATION")
		copyConfigValue(values, config.Config, "resumable_upload_enabled", "STORAGE_TUS_ENABLED")
		copyConfigValue(values, config.Config, "s3_compat_enabled", "STORAGE_S3_PROTOCOL_ENABLED")
	case "functions":
		copyConfigValue(values, config.Config, "runtime_enabled", "EDGE_RUNTIME_ENABLED")
		copyConfigValue(values, config.Config, "verify_jwt_by_default", "FUNCTIONS_VERIFY_JWT")
		copyConfigValue(values, config.Config, "worker_timeout_ms", "FUNCTION_WORKER_TIMEOUT_MS")
		copyConfigValue(values, config.Config, "import_map", "EDGE_RUNTIME_IMPORT_MAP")
		copyConfigValue(values, config.Config, "deployment_policy", "SUPADUPA_FUNCTION_DEPLOYMENT_POLICY")
		copyConfigValue(values, config.Config, "secret_sync_enabled", "SUPADUPA_FUNCTION_SECRET_SYNC_ENABLED")
	case "realtime":
		copyConfigValue(values, config.Config, "postgres_changes_enabled", "REALTIME_POSTGRES_CHANGES_ENABLED")
		copyConfigValue(values, config.Config, "broadcast_enabled", "REALTIME_BROADCAST_ENABLED")
		copyConfigValue(values, config.Config, "presence_enabled", "REALTIME_PRESENCE_ENABLED")
		copyConfigValue(values, config.Config, "broadcast_replay", "REALTIME_BROADCAST_REPLAY_ENABLED")
		copyConfigValue(values, config.Config, "broadcast_from_database", "REALTIME_BROADCAST_FROM_DATABASE_ENABLED")
	case "pooler":
		copyConfigValue(values, config.Config, "dedicated_pooler_enabled", "SUPADUPA_DEDICATED_POOLER_ENABLED")
		copyConfigValue(values, config.Config, "dedicated_pooler_tier", "SUPADUPA_DEDICATED_POOLER_TIER")
		copyConfigValue(values, config.Config, "pool_mode", "SUPADUPA_POOLER_MODE")
		copyConfigValue(values, config.Config, "default_pool_size", "SUPADUPA_POOLER_DEFAULT_POOL_SIZE")
		copyConfigValue(values, config.Config, "max_client_connections", "SUPADUPA_POOLER_MAX_CLIENT_CONNECTIONS")
	case "database":
		copyConfigValue(values, config.Config, "pg_graphql_enabled", "SUPADUPA_PG_GRAPHQL_ENABLED")
		copyConfigValue(values, config.Config, "database_webhooks", "SUPADUPA_DATABASE_WEBHOOKS_ENABLED")
		copyConfigValue(values, config.Config, "pg_cron_enabled", "SUPADUPA_PG_CRON_ENABLED")
		copyConfigValue(values, config.Config, "pgmq_enabled", "SUPADUPA_PGMQ_ENABLED")
		copyConfigValue(values, config.Config, "fdw_enabled", "SUPADUPA_FDW_ENABLED")
		copyConfigValue(values, config.Config, "vault_enabled", "SUPADUPA_VAULT_ENABLED")
		copyConfigValue(values, config.Config, "pgvector_enabled", "SUPADUPA_PGVECTOR_ENABLED")
		copyConfigValue(values, config.Config, "supavisor_enabled", "SUPADUPA_SUPAVISOR_ENABLED")
		copyConfigValue(values, config.Config, "ssl_enforced", "SUPADUPA_DB_SSL_ENFORCED")
		copyConfigValue(values, config.Config, "extension_toggle_ui", "SUPADUPA_EXTENSION_TOGGLE_UI")
		copyConfigValue(values, config.Config, "performance_advisor_mode", "SUPADUPA_PERFORMANCE_ADVISOR_MODE")
		copyConfigValue(values, config.Config, "orioledb_profile", "SUPADUPA_ORIOLEDB_PROFILE")
	case "smtp":
		copyConfigValue(values, config.Config, "enabled", "SUPADUPA_SMTP_ENABLED")
		copyConfigValue(values, config.Config, "host", "SMTP_HOST")
		copyConfigValue(values, config.Config, "host", "GOTRUE_SMTP_HOST")
		copyConfigValue(values, config.Config, "port", "SMTP_PORT")
		copyConfigValue(values, config.Config, "port", "GOTRUE_SMTP_PORT")
		copyConfigValue(values, config.Config, "sender_name", "SMTP_SENDER_NAME")
		copyConfigValue(values, config.Config, "sender_name", "GOTRUE_SMTP_SENDER_NAME")
		copyConfigValue(values, config.Config, "sender_email", "SMTP_ADMIN_EMAIL")
		copyConfigValue(values, config.Config, "sender_email", "GOTRUE_SMTP_ADMIN_EMAIL")
		copyConfigValue(values, config.Config, "username", "SMTP_USER")
		copyConfigValue(values, config.Config, "username", "GOTRUE_SMTP_USER")
		copyConfigSecretValue(values, config.Config, "password_handle", "SMTP_PASS", "SUPADUPA_SMTP_PASSWORD_HANDLE")
		copyConfigSecretValue(values, config.Config, "password_handle", "GOTRUE_SMTP_PASS", "")
		copyConfigValue(values, config.Config, "tls_mode", "SMTP_TLS_MODE")
		copyConfigValue(values, config.Config, "tls_mode", "GOTRUE_SMTP_TLS_MODE")
	case "ai":
		copyConfigValue(values, config.Config, "openai_enabled", "SUPADUPA_AI_OPENAI_ENABLED")
		copyConfigValue(values, config.Config, "openai_api_key_handle", "SUPADUPA_AI_OPENAI_API_KEY_HANDLE")
		copyConfigValue(values, config.Config, "huggingface_enabled", "SUPADUPA_AI_HUGGINGFACE_ENABLED")
		copyConfigValue(values, config.Config, "huggingface_api_key_handle", "SUPADUPA_AI_HUGGINGFACE_API_KEY_HANDLE")
		copyConfigValue(values, config.Config, "default_embedding_provider", "SUPADUPA_AI_DEFAULT_EMBEDDING_PROVIDER")
		copyConfigValue(values, config.Config, "default_embedding_model", "SUPADUPA_AI_DEFAULT_EMBEDDING_MODEL")
		copyConfigValue(values, config.Config, "default_embedding_dimension", "SUPADUPA_AI_DEFAULT_EMBEDDING_DIMENSION")
		copyConfigValue(values, config.Config, "embedding_queue_enabled", "SUPADUPA_AI_EMBEDDING_QUEUE_ENABLED")
		copyConfigValue(values, config.Config, "studio_assistant_enabled", "SUPADUPA_STUDIO_AI_ASSISTANT_ENABLED")
		copyConfigValue(values, config.Config, "studio_assistant_provider", "SUPADUPA_STUDIO_AI_ASSISTANT_PROVIDER")
		copyConfigValue(values, config.Config, "studio_assistant_model", "SUPADUPA_STUDIO_AI_ASSISTANT_MODEL")
		copyConfigValue(values, config.Config, "studio_assistant_key_handle", "SUPADUPA_STUDIO_AI_ASSISTANT_KEY_HANDLE")
	}
}

func copyConfigValue(values map[string]string, config map[string]string, configKey string, envKey string) {
	if value, ok := config[configKey]; ok {
		values[envKey] = strings.TrimSpace(value)
	}
}

func materializeFunctionImportMap(projectDir string, values map[string]string, importMap string) error {
	importMap = strings.TrimSpace(importMap)
	importMapPath := filepath.Join(projectDir, "functions", "import_map.json")
	if importMap == "" {
		delete(values, "EDGE_RUNTIME_IMPORT_MAP")
		if err := os.Remove(importMapPath); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		return nil
	}
	if !strings.HasPrefix(importMap, "{") && !strings.HasPrefix(importMap, "[") {
		return nil
	}
	var parsed any
	if err := json.Unmarshal([]byte(importMap), &parsed); err != nil {
		return fmt.Errorf("functions import_map must be valid JSON: %w", err)
	}
	payload, err := json.MarshalIndent(parsed, "", "  ")
	if err != nil {
		return err
	}
	functionsDir := filepath.Join(projectDir, "functions")
	if err := os.MkdirAll(functionsDir, 0o700); err != nil {
		return err
	}
	if err := os.WriteFile(importMapPath, append(payload, '\n'), bindMountFileMode); err != nil {
		return err
	}
	values["EDGE_RUNTIME_IMPORT_MAP"] = "/home/deno/functions/import_map.json"
	return nil
}

func copyConfigSecretValue(values map[string]string, config map[string]string, handleKey string, runtimeEnvKey string, handleEnvKey string) {
	handle, ok := config[handleKey]
	if !ok {
		return
	}
	resolved := strings.TrimSpace(config[runtimeResolvedSecretKey(handleKey)])
	values[runtimeEnvKey] = resolved
	if handleEnvKey != "" {
		values[handleEnvKey] = strings.TrimSpace(handle)
	}
}

func runtimeResolvedSecretKey(handleKey string) string {
	return "__resolved_" + handleKey
}

func writeEnvValues(path string, values map[string]string) error {
	var builder strings.Builder
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		value := values[key]
		builder.WriteString(key)
		builder.WriteString("=")
		builder.WriteString(envFileValue(value))
		builder.WriteString("\n")
	}
	return os.WriteFile(path, []byte(builder.String()), bindMountFileMode)
}

func envFileValue(value string) string {
	value = strings.ReplaceAll(value, "\r\n", "\n")
	value = strings.ReplaceAll(value, "\r", "\n")
	return strings.ReplaceAll(value, "\n", `\n`)
}

func applyDerivedSecretEnvValues(values map[string]string) {
	jwtSecret := strings.TrimSpace(values["JWT_SECRET"])
	if jwtSecret != "" {
		values["API_JWT_SECRET"] = jwtSecret
		values["AUTH_JWT_SECRET"] = jwtSecret
		values["GOTRUE_JWT_SECRET"] = jwtSecret
		values["METRICS_JWT_SECRET"] = jwtSecret
		values["PGRST_JWT_SECRET"] = jwtSecret
		values["REALTIME_JWT_SECRET"] = jwtSecret
	}
	postgresPassword := strings.TrimSpace(values["POSTGRES_PASSWORD"])
	if postgresPassword != "" {
		values["DB_PASSWORD"] = postgresPassword
		values["GOTRUE_DB_DATABASE_URL"] = fmt.Sprintf("postgres://supabase_auth_admin:%s@db:5432/postgres", postgresPassword)
		values["PGRST_DB_URI"] = fmt.Sprintf("postgres://authenticator:%s@db:5432/postgres", postgresPassword)
		values["REALTIME_DB_PASSWORD"] = postgresPassword
		values["SUPAVISOR_DB_PASSWORD"] = postgresPassword
	}
}

func updateEnvValue(path string, key string, value string) error {
	payload, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	lines := strings.Split(string(payload), "\n")
	found := false
	for index, line := range lines {
		if strings.HasPrefix(line, key+"=") {
			lines[index] = key + "=" + value
			found = true
		}
	}
	if !found {
		lines = append(lines, key+"="+value)
	}
	return os.WriteFile(path, []byte(strings.Join(lines, "\n")), bindMountFileMode)
}

func readEnvFile(path string) (map[string]string, error) {
	payload, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	values := map[string]string{}
	for _, line := range strings.Split(string(payload), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		values[key] = value
	}
	return values, nil
}

func writeComposeFile(path string, spec control.ProjectSpec) error {
	release := composeStackReleaseManifest(spec.StackVersion)
	services := control.ProjectServiceStates(spec.Services)
	depends := kongDependencies(services)

	var builder strings.Builder
	builder.WriteString(fmt.Sprintf(`name: supadupa-%s
services:
  db:
    image: supabase/postgres:%s
    env_file: .env
    environment:
      POSTGRES_DB: ${POSTGRES_DB}
      POSTGRES_HOST: /var/run/postgresql
      POSTGRES_PASSWORD: ${POSTGRES_PASSWORD}
      POSTGRES_USER: ${POSTGRES_USER}
    networks:
      internal: {}
      supadupa-ingress:
        aliases:
          - %s-db
    healthcheck:
      test: ["CMD-SHELL", "pg_isready -U ${POSTGRES_USER} -d ${POSTGRES_DB}"]
      interval: 5s
      timeout: 5s
      retries: 20
    volumes:
      - db-data:/var/lib/postgresql/data
      - ./pg_hba.conf:/etc/postgresql/pg_hba.conf:ro
      - ./00-supadupa-init.sql:/etc/postgresql.schema.sql:ro
  kong:
    image: kong/kong:%s
    env_file: .env
    environment:
      KONG_DATABASE: "off"
      KONG_DECLARATIVE_CONFIG: /usr/local/kong/kong.yml
      KONG_DNS_ORDER: LAST,A,CNAME,AAAA
      KONG_DNS_NOT_FOUND_TTL: 1
      KONG_NGINX_PROXY_PROXY_BUFFER_SIZE: 160k
      KONG_NGINX_PROXY_PROXY_BUFFERS: 64 160k
      KONG_PLUGINS: request-transformer,cors,key-auth,acl,basic-auth,post-function
      SUPABASE_ANON_KEY: ${ANON_KEY}
      SUPABASE_SERVICE_KEY: ${SERVICE_ROLE_KEY}
      SUPABASE_PUBLISHABLE_KEY: ${SUPABASE_PUBLISHABLE_KEY}
      SUPABASE_SECRET_KEY: ${SUPABASE_SECRET_KEY}
      ANON_KEY_ASYMMETRIC: ${ANON_KEY}
      SERVICE_ROLE_KEY_ASYMMETRIC: ${SERVICE_ROLE_KEY}
      DASHBOARD_USERNAME: ${DASHBOARD_USERNAME}
      DASHBOARD_PASSWORD: ${DASHBOARD_PASSWORD}
    entrypoint: /home/kong/kong-entrypoint.sh
    networks:
      internal: {}
      supadupa-ingress:
        aliases:
          - %s-kong
    volumes:
      - ./kong.yml:/home/kong/kong.yml:ro
      - ./kong-entrypoint.sh:/home/kong/kong-entrypoint.sh:ro
    depends_on:
`, spec.Ref, release.Postgres, spec.Ref, release.Kong, spec.Ref))
	for _, dependency := range depends {
		builder.WriteString(fmt.Sprintf("      - %s\n", dependency))
	}
	if services["studio"] {
		builder.WriteString(fmt.Sprintf(`  studio:
    image: supabase/studio:%s
    env_file: .env
    environment:
      HOSTNAME: "0.0.0.0"
    networks:
      internal: {}
      supadupa-ingress:
        aliases:
          - %s-studio
    depends_on: [meta]
`, release.Studio, spec.Ref))
	}
	builder.WriteString(fmt.Sprintf(`  meta:
    image: supabase/postgres-meta:%s
    env_file: .env
    environment:
      PG_META_DB_HOST: db
      PG_META_DB_NAME: ${POSTGRES_DB}
      PG_META_DB_PASSWORD: ${POSTGRES_PASSWORD}
      PG_META_DB_PORT: ${POSTGRES_PORT}
      PG_META_DB_USER: ${POSTGRES_USER}
    networks: [internal]
    depends_on:
      db:
        condition: service_healthy
`, release.PostgresMeta))
	if services["auth"] {
		builder.WriteString(fmt.Sprintf(`  auth:
    image: supabase/gotrue:%s
    env_file: .env
    environment:
      GOTRUE_DB_DRIVER: postgres
    networks: [internal, egress]
    depends_on:
      db:
        condition: service_healthy
`, release.Auth))
	}
	if services["rest"] {
		builder.WriteString(fmt.Sprintf(`  rest:
    image: postgrest/postgrest:%s
    env_file: .env
    networks: [internal]
    depends_on:
      db:
        condition: service_healthy
`, release.REST))
	}
	if services["realtime"] {
		builder.WriteString(fmt.Sprintf(`  realtime:
    image: supabase/realtime:%s
    env_file: .env
    environment:
      PORT: 4000
      DB_HOST: db
      DB_PORT: ${POSTGRES_PORT}
      DB_USER: supabase_admin
      DB_PASSWORD: ${POSTGRES_PASSWORD}
      DB_NAME: ${POSTGRES_DB}
      DB_AFTER_CONNECT_QUERY: "SET search_path TO _realtime"
      DB_ENC_KEY: ${DB_ENC_KEY}
      API_JWT_SECRET: ${JWT_SECRET}
      METRICS_JWT_SECRET: ${JWT_SECRET}
      SECRET_KEY_BASE: ${SECRET_KEY_BASE}
      ERL_AFLAGS: "-proto_dist inet_tcp"
      DNS_NODES: "''"
      RLIMIT_NOFILE: "10000"
      APP_NAME: realtime
      SEED_SELF_HOST: "true"
      SELF_HOST_TENANT_NAME: ${PROJECT_REF}
      RUN_JANITOR: "true"
      DISABLE_HEALTHCHECK_LOGGING: "true"
    networks:
      internal:
        aliases:
          - %s.supabase-realtime
    depends_on:
      db:
        condition: service_healthy
`, release.Realtime, spec.Ref))
	}
	if services["storage"] {
		builder.WriteString(fmt.Sprintf(`  storage:
    image: supabase/storage-api:%s
    env_file: .env
    environment:
      ANON_KEY: ${ANON_KEY}
      SERVICE_KEY: ${SERVICE_ROLE_KEY}
      POSTGREST_URL: http://rest:3000
      AUTH_JWT_SECRET: ${JWT_SECRET}
      DATABASE_URL: postgres://supabase_storage_admin:${POSTGRES_PASSWORD}@db:5432/${POSTGRES_DB}
      STORAGE_PUBLIC_URL: ${STORAGE_PUBLIC_URL}
      FILE_SIZE_LIMIT: ${FILE_SIZE_LIMIT}
      STORAGE_BACKEND: ${STORAGE_BACKEND}
      GLOBAL_S3_BUCKET: ${GLOBAL_S3_BUCKET}
      REQUEST_ALLOW_X_FORWARDED_PATH: ${REQUEST_ALLOW_X_FORWARDED_PATH}
      FILE_STORAGE_BACKEND_PATH: /var/lib/storage
      TENANT_ID: ${STORAGE_TENANT_ID}
      REGION: ${REGION}
      ENABLE_IMAGE_TRANSFORMATION: "true"
      IMGPROXY_URL: ${STORAGE_IMGPROXY_URL}
      UPLOAD_FILE_SIZE_LIMIT: ${UPLOAD_FILE_SIZE_LIMIT}
      UPLOAD_FILE_SIZE_LIMIT_STANDARD: ${UPLOAD_FILE_SIZE_LIMIT_STANDARD}
      UPLOAD_SIGNED_URL_EXPIRATION_TIME: ${UPLOAD_SIGNED_URL_EXPIRATION_TIME}
      TUS_URL_PATH: ${TUS_URL_PATH}
      TUS_URL_EXPIRY_MS: ${TUS_URL_EXPIRY_MS}
      S3_PROTOCOL_ACCESS_KEY_ID: ${S3_PROTOCOL_ACCESS_KEY_ID}
      S3_PROTOCOL_ACCESS_KEY_SECRET: ${S3_PROTOCOL_ACCESS_KEY_SECRET}
    networks: [internal, egress]
    volumes:
      - storage-data:/var/lib/storage
    depends_on:
      db:
        condition: service_healthy
`, release.Storage))
		if services["rest"] {
			builder.WriteString("      rest:\n        condition: service_started\n")
		}
		if services["imgproxy"] {
			builder.WriteString("      imgproxy:\n        condition: service_started\n")
		}
	}
	if services["imgproxy"] {
		builder.WriteString(fmt.Sprintf(`  imgproxy:
    image: darthsim/imgproxy:%s
    env_file: .env
    environment:
      IMGPROXY_LOCAL_FILESYSTEM_ROOT: /
    networks: [internal]
    volumes:
      - storage-data:/var/lib/storage:ro
`, release.Imgproxy))
	}
	if services["functions"] {
		builder.WriteString(fmt.Sprintf(`  edge-runtime:
    image: supabase/edge-runtime:%s
    env_file: .env
    environment:
      JWT_SECRET: ${JWT_SECRET}
      SUPABASE_URL: http://kong:8000
      SUPABASE_PUBLIC_URL: ${SUPABASE_PUBLIC_URL}
      SUPABASE_ANON_KEY: ${ANON_KEY}
      SUPABASE_SERVICE_ROLE_KEY: ${SERVICE_ROLE_KEY}
      SUPABASE_PUBLISHABLE_KEYS: "{\"default\":\"${SUPABASE_PUBLISHABLE_KEY}\"}"
      SUPABASE_SECRET_KEYS: "{\"default\":\"${SUPABASE_SECRET_KEY}\"}"
      SUPABASE_DB_URL: postgresql://postgres:${POSTGRES_PASSWORD}@db:5432/${POSTGRES_DB}
      VERIFY_JWT: ${FUNCTIONS_VERIFY_JWT}
      SUPADUPA_FUNCTION_STORAGE_ROOT: /mnt/.supadupa-storage/${STORAGE_TENANT_ID}/${GLOBAL_S3_BUCKET}
    networks: [internal, egress]
    volumes:
      - ./functions:/home/deno/functions
      - storage-data:/mnt/.supadupa-storage:ro
    command: ["start", "--main-service", "/home/deno/functions/main"]
`, release.EdgeRuntime))
	}
	if services["pooler"] {
		builder.WriteString(fmt.Sprintf(`  pooler:
    image: supabase/supavisor:%s
    restart: unless-stopped
    env_file: .env
    environment:
      PORT: 4000
      POSTGRES_PORT: ${POSTGRES_PORT}
      POSTGRES_HOST: db
      POSTGRES_DB: ${POSTGRES_DB}
      POSTGRES_PASSWORD: ${POSTGRES_PASSWORD}
      DATABASE_URL: ecto://supabase_admin:${POSTGRES_PASSWORD}@db:5432/_supabase
      CLUSTER_POSTGRES: "true"
      SECRET_KEY_BASE: ${SECRET_KEY_BASE}
      VAULT_ENC_KEY: ${VAULT_ENC_KEY}
      API_JWT_SECRET: ${JWT_SECRET}
      METRICS_JWT_SECRET: ${JWT_SECRET}
      REGION: ${REGION}
      POOLER_TENANT_ID: ${PROJECT_REF}
      POOLER_DEFAULT_POOL_SIZE: "20"
      POOLER_MAX_CLIENT_CONN: "200"
      POOLER_POOL_MODE: transaction
      DB_POOL_SIZE: "5"
    networks:
      internal: {}
      supadupa-ingress:
        aliases:
          - %s-pooler
    volumes:
      - ./pooler.exs:/etc/pooler/pooler.exs:ro
    depends_on:
      db:
        condition: service_healthy
    command: ["/bin/sh", "-c", "/app/bin/migrate && /app/bin/supavisor eval \"$$(cat /etc/pooler/pooler.exs)\" && /app/bin/server"]
`, release.Pooler, spec.Ref))
	}
	if services["analytics"] {
		builder.WriteString(fmt.Sprintf(`  analytics:
    image: supabase/logflare:%s
    env_file: .env
    environment:
      LOGFLARE_NODE_HOST: 127.0.0.1
      DB_USERNAME: supabase_admin
      DB_DATABASE: ${POSTGRES_DB}
      DB_HOSTNAME: db
      DB_PORT: ${POSTGRES_PORT}
      DB_PASSWORD: ${POSTGRES_PASSWORD}
      DB_SCHEMA: _analytics
      LOGFLARE_SINGLE_TENANT: "true"
      LOGFLARE_SUPABASE_MODE: "true"
      LOGFLARE_PUBLIC_ACCESS_TOKEN: ${LOGFLARE_PUBLIC_ACCESS_TOKEN}
      LOGFLARE_PRIVATE_ACCESS_TOKEN: ${LOGFLARE_PRIVATE_ACCESS_TOKEN}
      LOGFLARE_FEATURE_FLAG_OVERRIDE: multibackend=true
      POSTGRES_BACKEND_URL: postgresql://supabase_admin:${POSTGRES_PASSWORD}@db:5432/${POSTGRES_DB}
      POSTGRES_BACKEND_SCHEMA: _analytics
    networks: [internal, egress]
    depends_on:
      db:
        condition: service_healthy
`, release.Analytics))
	}
	if services["vector"] {
		builder.WriteString(fmt.Sprintf(`  vector:
    image: timberio/vector:%s
    env_file: .env
    networks: [internal, egress]
    volumes:
      - ./vector.yml:/etc/vector/vector.yml:ro
      - ./log-drains:/etc/vector/log-drains:ro
      - /var/run/docker.sock:/var/run/docker.sock:ro
    command: ["--config", "/etc/vector/vector.yml", "--config-dir", "/etc/vector/log-drains"]
`, release.Vector))
	}
	builder.WriteString(`networks:
  internal:
    internal: true
  egress: {}
  supadupa-ingress:
    external: true
volumes:
  db-data:
  storage-data:
`)
	return os.WriteFile(path, []byte(builder.String()), bindMountFileMode)
}

func kongDependencies(services map[string]bool) []string {
	var depends []string
	for _, name := range []string{"auth", "rest", "realtime", "storage", "functions", "studio"} {
		if !services[name] {
			continue
		}
		if name == "functions" {
			depends = append(depends, "edge-runtime")
			continue
		}
		depends = append(depends, name)
	}
	if len(depends) == 0 {
		return []string{"db"}
	}
	return depends
}

func writeKongConfigFile(path string, ref string, services map[string]control.ServiceSpec) error {
	states := control.ProjectServiceStates(services)
	var builder strings.Builder
	builder.WriteString(`_format_version: "2.1"
_transform: true

consumers:
  - username: anon
    keyauth_credentials:
      - key: $SUPABASE_ANON_KEY
      - key: $SUPABASE_PUBLISHABLE_KEY
  - username: service_role
    keyauth_credentials:
      - key: $SUPABASE_SERVICE_KEY
      - key: $SUPABASE_SECRET_KEY

acls:
  - consumer: anon
    group: anon
  - consumer: service_role
    group: admin

services:
`)
	if states["auth"] {
		builder.WriteString(`  - name: auth-v1-health
    url: http://auth:9999/health
    routes:
      - name: auth-v1-health
        strip_path: true
        paths: [/auth/v1/health]
    plugins:
      - name: cors
  - name: auth-v1-open-verify
    url: http://auth:9999/verify
    routes:
      - name: auth-v1-open-verify
        strip_path: true
        paths: [/auth/v1/verify]
    plugins:
      - name: cors
  - name: auth-v1-open-callback
    url: http://auth:9999/callback
    routes:
      - name: auth-v1-open-callback
        strip_path: true
        paths: [/auth/v1/callback]
    plugins:
      - name: cors
  - name: auth-v1-open-authorize
    url: http://auth:9999/authorize
    routes:
      - name: auth-v1-open-authorize
        strip_path: true
        paths: [/auth/v1/authorize]
    plugins:
      - name: cors
  - name: auth-v1-open-jwks
    url: http://auth:9999/.well-known/jwks.json
    routes:
      - name: auth-v1-open-jwks
        strip_path: true
        paths: [/auth/v1/.well-known/jwks.json]
    plugins:
      - name: cors
  - name: auth-v1
    url: http://auth:9999/
    routes:
      - name: auth-v1
        strip_path: true
        paths: [/auth/v1/]
    plugins:
      - name: cors
      - name: key-auth
        config:
          hide_credentials: false
      - name: request-transformer
        config:
          add:
            headers:
              - "Authorization: $LUA_AUTH_EXPR"
          replace:
            headers:
              - "Authorization: $LUA_AUTH_EXPR"
      - name: acl
        config:
          hide_groups_header: true
          allow:
            - admin
            - anon
`)
	}
	if states["rest"] {
		builder.WriteString(`  - name: rest-v1
    url: http://rest:3000/
    routes:
      - name: rest-v1
        strip_path: true
        paths: [/rest/v1/]
    plugins:
      - name: cors
      - name: key-auth
        config:
          hide_credentials: false
      - name: request-transformer
        config:
          add:
            headers:
              - "Authorization: $LUA_AUTH_EXPR"
          replace:
            headers:
              - "Authorization: $LUA_AUTH_EXPR"
      - name: acl
        config:
          hide_groups_header: true
          allow:
            - admin
            - anon
`)
	}
	if states["graphql"] && states["rest"] {
		builder.WriteString(`  - name: graphql-v1
    url: http://rest:3000/rpc/graphql
    routes:
      - name: graphql-v1
        strip_path: true
        paths: [/graphql/v1]
    plugins:
      - name: cors
      - name: key-auth
        config:
          hide_credentials: false
      - name: request-transformer
        config:
          add:
            headers:
              - "Content-Profile: graphql_public"
              - "Authorization: $LUA_AUTH_EXPR"
          replace:
            headers:
              - "Authorization: $LUA_AUTH_EXPR"
      - name: acl
        config:
          hide_groups_header: true
          allow:
            - admin
            - anon
`)
	}
	if states["realtime"] {
		realtimeHost := fmt.Sprintf("%s.supabase-realtime", ref)
		builder.WriteString(fmt.Sprintf(`  - name: realtime-v1-ws
    url: http://%s:4000/socket
    protocol: ws
    routes:
      - name: realtime-v1-ws
        strip_path: true
        paths: [/realtime/v1/]
    plugins:
      - name: cors
      - name: key-auth
        config:
          hide_credentials: false
      - name: request-transformer
        config:
          add:
            headers:
              - "x-api-key:$LUA_RT_WS_EXPR"
          replace:
            querystring:
              - "apikey:$LUA_RT_WS_EXPR"
      - name: acl
        config:
          hide_groups_header: true
          allow:
            - admin
            - anon
  - name: realtime-v1-rest
    url: http://%s:4000/api
    protocol: http
    routes:
      - name: realtime-v1-rest
        strip_path: true
        paths: [/realtime/v1/api]
    plugins:
      - name: cors
      - name: key-auth
        config:
          hide_credentials: false
      - name: request-transformer
        config:
          add:
            headers:
              - "Authorization: $LUA_AUTH_EXPR"
          replace:
            headers:
              - "Authorization: $LUA_AUTH_EXPR"
      - name: acl
        config:
          hide_groups_header: true
          allow:
            - admin
            - anon
`, realtimeHost, realtimeHost))
	}
	if states["storage"] {
		builder.WriteString(`  - name: storage-v1
    url: http://storage:5000/
    routes:
      - name: storage-v1
        strip_path: true
        paths: [/storage/v1/]
    plugins:
      - name: cors
      - name: request-transformer
        config:
          add:
            headers:
              - "Authorization: $LUA_AUTH_EXPR"
          replace:
            headers:
              - "Authorization: $LUA_AUTH_EXPR"
      - name: post-function
        config:
          access:
            - |
              local auth = kong.request.get_header("authorization")
              if auth == nil or auth == "" or auth:find("^%s*$") then
                kong.service.request.clear_header("authorization")
              end
`)
	}
	if states["functions"] {
		builder.WriteString(`  - name: functions-v1
    url: http://edge-runtime:9000/
    routes:
      - name: functions-v1
        strip_path: true
        paths: [/functions/v1/]
    plugins:
      - name: cors
`)
	}
	if states["analytics"] {
		builder.WriteString(`  - name: analytics
    url: http://analytics:4000
    routes:
      - name: analytics
        strip_path: true
        paths: [/analytics/v1]
`)
	}
	return os.WriteFile(path, []byte(builder.String()), bindMountFileMode)
}

func writeKongEntrypointFile(path string) error {
	body := `#!/bin/sh
set -eu

if [ -n "${SUPABASE_SECRET_KEY:-}" ] && [ -n "${SUPABASE_PUBLISHABLE_KEY:-}" ]; then
  export LUA_AUTH_EXPR="\$((headers.authorization ~= nil and headers.authorization:sub(1, 10) ~= 'Bearer sb_' and headers.authorization) or (headers.apikey == '$SUPABASE_SECRET_KEY' and 'Bearer $SERVICE_ROLE_KEY_ASYMMETRIC') or (headers.apikey == '$SUPABASE_PUBLISHABLE_KEY' and 'Bearer $ANON_KEY_ASYMMETRIC') or (headers.apikey ~= nil and 'Bearer ' .. headers.apikey))"
  export LUA_RT_WS_EXPR="\$((query_params.apikey == '$SUPABASE_SECRET_KEY' and '$SERVICE_ROLE_KEY_ASYMMETRIC') or (query_params.apikey == '$SUPABASE_PUBLISHABLE_KEY' and '$ANON_KEY_ASYMMETRIC') or query_params.apikey)"
else
  export LUA_AUTH_EXPR="\$((headers.authorization ~= nil and headers.authorization:sub(1, 10) ~= 'Bearer sb_' and headers.authorization) or (headers.apikey ~= nil and 'Bearer ' .. headers.apikey))"
  export LUA_RT_WS_EXPR="\$(query_params.apikey)"
fi

awk '{
  result = ""
  rest = $0
  while (match(rest, /\$[A-Za-z_][A-Za-z_0-9]*/)) {
    varname = substr(rest, RSTART + 1, RLENGTH - 1)
    if (varname in ENVIRON) {
      result = result substr(rest, 1, RSTART - 1) ENVIRON[varname]
    } else {
      result = result substr(rest, 1, RSTART + RLENGTH - 1)
    }
    rest = substr(rest, RSTART + RLENGTH)
  }
  print result rest
}' /home/kong/kong.yml > "$KONG_DECLARATIVE_CONFIG"

sed -i '/^[[:space:]]*- key:[[:space:]]*$/d' "$KONG_DECLARATIVE_CONFIG"

exec /entrypoint.sh kong docker-start
`
	if err := os.WriteFile(path, []byte(body), 0o755); err != nil {
		return err
	}
	return os.Chmod(path, 0o755)
}

func writeVectorConfigFile(path string, ref string) error {
	body := fmt.Sprintf(`sources:
  project_logs:
    type: docker_logs
    docker_host: unix:///var/run/docker.sock
    include_labels:
      - com.docker.compose.project=%s

transforms:
  add_project:
    type: remap
    inputs: [project_logs]
    source: |
      .project_ref = "%s"

sinks:
  stdout:
    type: console
    inputs: [add_project]
    encoding:
      codec: json
`, ref, ref)
	return os.WriteFile(path, []byte(body), bindMountFileMode)
}

func writePoolerConfigFile(path string) error {
	body := `{:ok, _} = Application.ensure_all_started(:supavisor)

{:ok, version} =
  case Supavisor.Repo.query!("select version()") do
    %{rows: [[ver]]} -> Supavisor.Helpers.parse_pg_version(ver)
    _ -> nil
  end

params = %{
  "external_id" => System.get_env("POOLER_TENANT_ID"),
  "db_host" => System.get_env("POSTGRES_HOST") || "db",
  "db_port" => System.get_env("POSTGRES_PORT"),
  "db_database" => System.get_env("POSTGRES_DB"),
  "require_user" => true,
  "default_max_clients" => System.get_env("POOLER_MAX_CLIENT_CONN"),
  "default_pool_size" => System.get_env("POOLER_DEFAULT_POOL_SIZE"),
  "default_parameter_status" => %{"server_version" => version},
  "users" => [
    %{
      "db_user" => "postgres",
      "db_password" => System.get_env("POSTGRES_PASSWORD"),
      "mode_type" => "transaction",
      "pool_size" => System.get_env("POOLER_DEFAULT_POOL_SIZE")
    }
  ]
}

if Supavisor.Tenants.get_tenant_by_external_id(params["external_id"]) do
  _ = Supavisor.Tenants.delete_tenant_by_external_id(params["external_id"])
end

{:ok, _} = Supavisor.Tenants.create_tenant(params)
`
	return os.WriteFile(path, []byte(body), bindMountFileMode)
}

func writeDefaultFunctionEntrypoint(path string) error {
	body := `const FUNCTION_NAME_RE = /^[A-Za-z0-9_-]+$/;

function functionNameFromPath(pathname: string): string {
  const parts = pathname.split("/").filter(Boolean);
  if (parts[0] === "functions" && parts[1] === "v1") {
    return parts[2] ?? "";
  }
  return parts[0] ?? "";
}

function json(status: number, body: Record<string, unknown>): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { "Content-Type": "application/json" },
  });
}

function base64URLDecode(value: string): Uint8Array {
  const padded = value.replaceAll("-", "+").replaceAll("_", "/").padEnd(Math.ceil(value.length / 4) * 4, "=");
  return Uint8Array.from(atob(padded), (char) => char.charCodeAt(0));
}

async function verifyHS256JWT(jwt: string, secret: string): Promise<boolean> {
  const parts = jwt.split(".");
  if (parts.length !== 3) {
    return false;
  }
  try {
    const header = JSON.parse(new TextDecoder().decode(base64URLDecode(parts[0])));
    if (header.alg !== "HS256") {
      return false;
    }
    const payload = JSON.parse(new TextDecoder().decode(base64URLDecode(parts[1])));
    const now = Math.floor(Date.now() / 1000);
    if (typeof payload.exp === "number" && payload.exp <= now) {
      return false;
    }
    if (typeof payload.nbf === "number" && payload.nbf > now) {
      return false;
    }
    const key = await crypto.subtle.importKey(
      "raw",
      new TextEncoder().encode(secret),
      { name: "HMAC", hash: "SHA-256" },
      false,
      ["verify"],
    );
    return await crypto.subtle.verify(
      "HMAC",
      key,
      base64URLDecode(parts[2]),
      new TextEncoder().encode(parts[0] + "." + parts[1]),
    );
  } catch (_error) {
    return false;
  }
}

function bearerToken(req: Request): string {
  const header = req.headers.get("authorization") ?? "";
  const [scheme, token] = header.split(" ");
  if (scheme.toLowerCase() !== "bearer" || !token) {
    return "";
  }
  return token;
}

function parseOpaqueKeys(value: string | undefined): Set<string> {
  const keys = new Set<string>();
  if (!value) {
    return keys;
  }
  try {
    const parsed = JSON.parse(value);
    for (const candidate of Object.values(parsed)) {
      if (typeof candidate === "string" && candidate !== "") {
        keys.add(candidate);
      }
    }
  } catch (_error) {
    if (value !== "") {
      keys.add(value);
    }
  }
  return keys;
}

async function loadFunctionEnv(servicePath: string): Promise<Record<string, string>> {
  try {
    const env = await Deno.readTextFile(servicePath + "/.env");
    const values: Record<string, string> = {};
    for (const line of env.split("\n")) {
      const trimmed = line.trim();
      if (!trimmed || trimmed.startsWith("#")) {
        continue;
      }
      const separator = trimmed.indexOf("=");
      if (separator <= 0) {
        continue;
      }
      values[trimmed.slice(0, separator)] = trimmed.slice(separator + 1);
    }
    return values;
  } catch (error) {
    if (!(error instanceof Deno.errors.NotFound)) {
      console.error("failed to read function env", error);
    }
    return {};
  }
}

async function loadFunctionEntrypoint(servicePath: string): Promise<string> {
  try {
    const metadata = JSON.parse(await Deno.readTextFile(servicePath + "/metadata.json"));
    const entrypoint = String(metadata.entrypoint ?? "").replaceAll("\\", "/");
    if (entrypoint && !entrypoint.startsWith("/") && !entrypoint.includes("..")) {
      return cleanRelativePath(entrypoint) || "index.ts";
    }
  } catch (error) {
    if (!(error instanceof Deno.errors.NotFound)) {
      console.error("failed to read function metadata", error);
    }
  }
  return "index.ts";
}

type StorageMount = {
  id?: string;
  bucket_name?: string;
  mount_path?: string;
  read_only?: boolean;
  prefix?: string;
  env_alias?: string;
};

type FunctionRegion = {
  region?: string;
  routing_policy?: string;
};

type PreparedStorageMounts = {
  env: Record<string, string>;
  servicePath: string;
  staticPatterns: string[];
};

function cleanRelativePath(value: string): string {
  return value.split("/").filter((part) => part !== "" && part !== ".").join("/");
}

function joinPath(...parts: string[]): string {
  const cleaned = parts
    .map((part, index) => {
      const value = part.replaceAll("\\", "/");
      if (index === 0) {
        return value.replace(/\/+$/, "");
      }
      return value.replace(/^\/+|\/+$/g, "");
    })
    .filter((part) => part !== "");
  if (cleaned.length === 0) {
    return "/";
  }
  return cleaned.join("/");
}

function defaultStorageRoot(): string {
  const configured = Deno.env.get("SUPADUPA_FUNCTION_STORAGE_ROOT");
  if (configured) {
    return configured.replace(/\/+$/, "");
  }
  const tenant = Deno.env.get("STORAGE_TENANT_ID") ?? Deno.env.get("PROJECT_REF") ?? "";
  const globalBucket = Deno.env.get("GLOBAL_S3_BUCKET") ?? tenant;
  return joinPath("/mnt/.supadupa-storage", tenant, globalBucket);
}

async function removePathIfExists(path: string): Promise<void> {
  try {
    await Deno.remove(path, { recursive: true });
  } catch (error) {
    if (!(error instanceof Deno.errors.NotFound)) {
      throw error;
    }
  }
}

async function makeReadablePath(path: string): Promise<void> {
  const normalized = path.replaceAll("\\", "/");
  const parts = normalized.split("/").filter((part) => part !== "");
  let current = normalized.startsWith("/") ? "" : ".";
  for (const part of parts) {
    current = current === "" ? "/" + part : joinPath(current, part);
    if (!current.startsWith("/home/deno/functions") && !current.startsWith("/tmp")) {
      continue;
    }
    try {
      await Deno.chmod(current, 0o755);
    } catch (_error) {
      // Some runtime filesystems do not support chmod; keep going and rely on existing permissions.
    }
  }
}

async function ensureReadableDirectory(path: string): Promise<void> {
  await Deno.mkdir(path, { recursive: true });
  await makeReadablePath(path);
}

async function copyFileIntoView(targetPath: string, sourcePath: string): Promise<void> {
  await ensureReadableDirectory(targetPath.substring(0, targetPath.lastIndexOf("/")));
  await Deno.writeFile(targetPath, await Deno.readFile(sourcePath));
  try {
    await Deno.chmod(targetPath, 0o644);
  } catch (_error) {
    // chmod is best-effort for runtime filesystems.
  }
}

async function makeReadOnlyPath(path: string): Promise<void> {
  try {
    const stat = await Deno.stat(path);
    if (stat.isDirectory) {
      for await (const entry of Deno.readDir(path)) {
        await makeReadOnlyPath(joinPath(path, entry.name));
      }
      await Deno.chmod(path, 0o555);
      return;
    }
    await Deno.chmod(path, 0o444);
  } catch (error) {
    if (!(error instanceof Deno.errors.NotFound)) {
      throw error;
    }
  }
}

async function latestFileInDirectory(path: string): Promise<string> {
  let latestPath = "";
  let latestTime = 0;
  try {
    for await (const entry of Deno.readDir(path)) {
      if (!entry.isFile || entry.name.endsWith(".json")) {
        continue;
      }
      const candidate = joinPath(path, entry.name);
      const stat = await Deno.stat(candidate);
      const modified = stat.mtime?.getTime() ?? 0;
      if (!latestPath || modified >= latestTime) {
        latestPath = candidate;
        latestTime = modified;
      }
    }
  } catch (error) {
    if (!(error instanceof Deno.errors.NotFound)) {
      throw error;
    }
  }
  return latestPath;
}

async function materializeStorageObjectLinks(sourceRoot: string, targetRoot: string, currentPath = sourceRoot): Promise<string[]> {
  const copiedFiles: string[] = [];
  const latest = await latestFileInDirectory(currentPath);
  if (latest) {
    const relative = currentPath.slice(sourceRoot.length).replace(/^\/+/, "");
    if (relative) {
      const targetPath = joinPath(targetRoot, relative);
      await copyFileIntoView(targetPath, latest);
      copiedFiles.push(targetPath);
    }
    return copiedFiles;
  }
  try {
    for await (const entry of Deno.readDir(currentPath)) {
      if (entry.isDirectory) {
        copiedFiles.push(...await materializeStorageObjectLinks(sourceRoot, targetRoot, joinPath(currentPath, entry.name)));
      }
    }
  } catch (error) {
    if (!(error instanceof Deno.errors.NotFound)) {
      throw error;
    }
  }
  return copiedFiles;
}

async function prepareStorageMountView(mountPath: string, sourcePath: string, readOnly: boolean): Promise<string[]> {
  await removePathIfExists(mountPath);
  await ensureReadableDirectory(mountPath);
  const copiedFiles = await materializeStorageObjectLinks(sourcePath, mountPath);
  if (readOnly) {
    await makeReadOnlyPath(mountPath);
  }
  return copiedFiles;
}

function mountWorkDirName(mount: StorageMount): string {
  const alias = String(mount.env_alias ?? "").toLowerCase().replaceAll("_", "-");
  if (/^[a-z0-9][a-z0-9-]*$/.test(alias)) {
    return alias;
  }
  return String(mount.bucket_name ?? "mount");
}

async function prepareStorageMounts(manifestServicePath: string, workerSourcePath: string): Promise<PreparedStorageMounts> {
  let mounts: StorageMount[] = [];
  try {
    mounts = JSON.parse(await Deno.readTextFile(manifestServicePath + "/storage-mounts.json"));
  } catch (error) {
    if (!(error instanceof Deno.errors.NotFound)) {
      console.error("failed to read function storage mounts", error);
    }
    return { env: {}, servicePath: workerSourcePath, staticPatterns: [] };
  }
  if (mounts.length === 0) {
    return { env: {}, servicePath: workerSourcePath, staticPatterns: [] };
  }
  const workerServicePath = workerSourcePath;
  const storageEnv: Record<string, string> = {};
  const staticPatterns: string[] = [];
  const storageRoot = defaultStorageRoot();
  await makeReadablePath(workerServicePath);
  for (const mount of mounts) {
    const bucketName = String(mount.bucket_name ?? "");
    const mountPath = String(mount.mount_path ?? "");
    const envAlias = String(mount.env_alias ?? "");
    if (!/^[a-z0-9][a-z0-9-]{1,62}[a-z0-9]$/.test(bucketName)) {
      continue;
    }
    if (!mountPath.startsWith("/mnt/") || mountPath.startsWith("/mnt/.supadupa-storage/")) {
      continue;
    }
    const prefix = cleanRelativePath(String(mount.prefix ?? ""));
    const sourcePath = joinPath(storageRoot, bucketName, prefix);
    const preparedMountRelativePath = joinPath(".supadupa-mounts", mountWorkDirName(mount));
    const preparedMountPath = joinPath(workerServicePath, preparedMountRelativePath);
    const copiedFiles = await prepareStorageMountView(preparedMountPath, sourcePath, mount.read_only === true);
    for (const copiedFile of copiedFiles) {
      staticPatterns.push(copiedFile);
    }
    staticPatterns.push(preparedMountPath + "/*");
    staticPatterns.push(preparedMountPath + "/**/*");
    if (/^[A-Z_][A-Z0-9_]*$/.test(envAlias)) {
      storageEnv[envAlias] = preparedMountRelativePath;
    }
  }
  return { env: storageEnv, servicePath: workerServicePath, staticPatterns };
}

async function resolveFunctionServicePath(functionName: string, servicePath: string, env: Record<string, string>): Promise<string> {
  const version = env.SUPABASE_FUNCTION_VERSION ?? "";
  if (!/^[0-9]+$/.test(version)) {
    return servicePath;
  }
  const runtimeServicePath = "/home/deno/functions/.supadupa-runtime/" + functionName + "-v" + version;
  try {
    const stat = await Deno.stat(runtimeServicePath);
    if (stat.isDirectory) {
      return runtimeServicePath;
    }
  } catch (error) {
    if (!(error instanceof Deno.errors.NotFound)) {
      console.error("failed to stat versioned function runtime", error);
    }
  }
  return servicePath;
}

async function requestIsAuthorized(req: Request, env: Record<string, string>): Promise<boolean> {
  if (req.method === "OPTIONS") {
    return true;
  }
  const verifyJWT = (env.VERIFY_JWT ?? Deno.env.get("VERIFY_JWT") ?? "true") === "true";
  if (!verifyJWT) {
    return true;
  }
  const token = bearerToken(req);
  if (!token) {
    return false;
  }
  const opaqueKeys = new Set<string>([
    ...parseOpaqueKeys(Deno.env.get("SUPABASE_PUBLISHABLE_KEYS")),
    ...parseOpaqueKeys(Deno.env.get("SUPABASE_SECRET_KEYS")),
  ]);
  if (opaqueKeys.has(token)) {
    return true;
  }
  const jwtSecret = Deno.env.get("JWT_SECRET");
  return jwtSecret ? await verifyHS256JWT(token, jwtSecret) : false;
}

function positiveIntegerValue(value: string | undefined, fallback: number, min: number, max: number): number {
  if (!value) {
    return fallback;
  }
  const parsed = Number(value);
  if (!Number.isInteger(parsed) || parsed < min || parsed > max) {
    return fallback;
  }
  return parsed;
}

function requestTimeoutMs(env: Record<string, string>): number {
  return positiveIntegerValue(env.FUNCTION_WORKER_TIMEOUT_MS ?? Deno.env.get("FUNCTION_WORKER_TIMEOUT_MS"), 60000, 100, 300000);
}

function normalizeRequestedRegion(value: string | null): string {
  const region = (value ?? "").trim().toLowerCase();
  if (!region || region.length > 64 || /[\s/\\]/.test(region)) {
    return "";
  }
  return region;
}

async function loadFunctionRegions(servicePath: string): Promise<FunctionRegion[]> {
  try {
    const regions = JSON.parse(await Deno.readTextFile(servicePath + "/regions.json"));
    return Array.isArray(regions) ? regions : [];
  } catch (error) {
    if (!(error instanceof Deno.errors.NotFound)) {
      console.error("failed to read function regions", error);
    }
    return [];
  }
}

async function resolveFunctionRegion(req: Request, servicePath: string): Promise<string> {
  const url = new URL(req.url);
  const requested = normalizeRequestedRegion(url.searchParams.get("forceFunctionRegion") ?? req.headers.get("x-region"));
  const configured = await loadFunctionRegions(servicePath);
  const configuredRegions = new Set(
    configured
      .map((item) => normalizeRequestedRegion(String(item.region ?? "")))
      .filter((region) => region !== ""),
  );
  if (requested !== "" && (configuredRegions.size === 0 || configuredRegions.has(requested))) {
    return requested;
  }
  const defaultRegion = normalizeRequestedRegion(Deno.env.get("REGION"));
  return defaultRegion || "local";
}

async function fetchWithTimeout(worker: { fetch: (req: Request) => Promise<Response> }, req: Request, timeoutMs: number): Promise<Response> {
  let timer: number | undefined;
  try {
    const timeout = new Promise<Response>((resolve) => {
      timer = setTimeout(() => resolve(json(504, { msg: "function timed out", timeout_ms: timeoutMs })), timeoutMs);
    });
    return await Promise.race([worker.fetch(req), timeout]);
  } finally {
    if (timer !== undefined) {
      clearTimeout(timer);
    }
  }
}

Deno.serve(async (req: Request) => {
  const url = new URL(req.url);
  const functionName = functionNameFromPath(url.pathname);
  if (!functionName) {
    return json(400, { msg: "missing function name in request" });
  }
  if (!FUNCTION_NAME_RE.test(functionName)) {
    return json(400, { msg: "invalid function name in request" });
  }

  const servicePath = "/home/deno/functions/" + functionName;
  try {
    const stat = await Deno.stat(servicePath);
    if (!stat.isDirectory) {
      return json(404, { msg: "function not found" });
    }
  } catch (error) {
    if (error instanceof Deno.errors.NotFound) {
      return json(404, { msg: "function not found" });
    }
    throw error;
  }

  const functionEnv = await loadFunctionEnv(servicePath);
  if (!(await requestIsAuthorized(req, functionEnv))) {
    return json(401, { msg: "Invalid JWT" });
  }
  const resolvedRegion = await resolveFunctionRegion(req, servicePath);
  const runtimeServicePath = await resolveFunctionServicePath(functionName, servicePath, functionEnv);
  const functionEntrypoint = await loadFunctionEntrypoint(runtimeServicePath);
  let storageMounts: PreparedStorageMounts = { env: {}, servicePath: runtimeServicePath, staticPatterns: [] };
  try {
    storageMounts = await prepareStorageMounts(servicePath, runtimeServicePath);
  } catch (error) {
    console.error("failed to prepare function storage mounts", error);
    return json(500, { msg: "function storage mount prepare failed", error: String(error) });
  }

  const env = { ...Deno.env.toObject(), ...functionEnv, ...storageMounts.env, SB_REGION: resolvedRegion };
  const envVars = Object.entries(env);
  const timeoutMs = requestTimeoutMs(env);
  try {
    const worker = await EdgeRuntime.userWorkers.create({
      servicePath: storageMounts.servicePath,
      memoryLimitMb: Number(Deno.env.get("FUNCTION_MEMORY_LIMIT_MB") ?? "150"),
      workerTimeoutMs: timeoutMs,
      noModuleCache: Deno.env.get("EDGE_RUNTIME_DISABLE_MODULE_CACHE") !== "false",
      forceCreate: Deno.env.get("EDGE_RUNTIME_FORCE_CREATE") !== "false",
      staticPatterns: storageMounts.staticPatterns,
      maybeEntrypoint: "file://" + joinPath(storageMounts.servicePath, functionEntrypoint),
      context: {
        importMapPath: Deno.env.get("EDGE_RUNTIME_IMPORT_MAP") || null,
        useReadSyncFileAPI: true,
      },
      envVars,
    });
    const response = await fetchWithTimeout(worker, req, timeoutMs);
    const headers = new Headers(response.headers);
    headers.set("x-sb-edge-region", resolvedRegion);
    return new Response(response.body, {
      status: response.status,
      statusText: response.statusText,
      headers,
    });
  } catch (error) {
    return json(500, { msg: String(error) });
  }
});
`
	return os.WriteFile(path, []byte(body), bindMountFileMode)
}

func writeDatabaseInitFile(path string, postgresPassword string) error {
	passwordLiteral := sqlQuoteLiteral(postgresPassword)
	body := fmt.Sprintf(`-- supadupa per-project database post-bootstrap.
-- The Supabase Postgres image runs this after its bundled init scripts and migrations.

CREATE SCHEMA IF NOT EXISTS auth;
CREATE SCHEMA IF NOT EXISTS storage;
CREATE SCHEMA IF NOT EXISTS graphql;
CREATE SCHEMA IF NOT EXISTS graphql_public;
CREATE SCHEMA IF NOT EXISTS realtime;
CREATE SCHEMA IF NOT EXISTS _realtime;
CREATE SCHEMA IF NOT EXISTS vault;
CREATE SCHEMA IF NOT EXISTS extensions;
CREATE SCHEMA IF NOT EXISTS pgmq;
CREATE SCHEMA IF NOT EXISTS _analytics;

SELECT 'CREATE DATABASE _supabase'
WHERE NOT EXISTS (SELECT FROM pg_database WHERE datname = '_supabase')\gexec

\c _supabase
CREATE SCHEMA IF NOT EXISTS _supavisor;
ALTER SCHEMA _supavisor OWNER TO supabase_admin;
\c postgres

DO $$
BEGIN
  IF NOT EXISTS (SELECT FROM pg_roles WHERE rolname = 'anon') THEN
    CREATE ROLE anon NOLOGIN NOINHERIT;
  END IF;
  IF NOT EXISTS (SELECT FROM pg_roles WHERE rolname = 'authenticated') THEN
    CREATE ROLE authenticated NOLOGIN NOINHERIT;
  END IF;
  IF NOT EXISTS (SELECT FROM pg_roles WHERE rolname = 'service_role') THEN
    CREATE ROLE service_role NOLOGIN NOINHERIT BYPASSRLS;
  END IF;
  IF NOT EXISTS (SELECT FROM pg_roles WHERE rolname = 'authenticator') THEN
    CREATE ROLE authenticator NOINHERIT LOGIN;
  END IF;
  IF NOT EXISTS (SELECT FROM pg_roles WHERE rolname = 'supabase_auth_admin') THEN
    CREATE ROLE supabase_auth_admin LOGIN;
  END IF;
  IF NOT EXISTS (SELECT FROM pg_roles WHERE rolname = 'supabase_storage_admin') THEN
    CREATE ROLE supabase_storage_admin LOGIN;
  END IF;
  IF NOT EXISTS (SELECT FROM pg_roles WHERE rolname = 'supabase_admin') THEN
    CREATE ROLE supabase_admin LOGIN;
  END IF;
  IF NOT EXISTS (SELECT FROM pg_roles WHERE rolname = 'supabase_replication_admin') THEN
    CREATE ROLE supabase_replication_admin LOGIN REPLICATION;
  END IF;
  IF NOT EXISTS (SELECT FROM pg_roles WHERE rolname = 'supabase_read_only_user') THEN
    CREATE ROLE supabase_read_only_user LOGIN BYPASSRLS;
  END IF;
  IF NOT EXISTS (SELECT FROM pg_roles WHERE rolname = 'dashboard_user') THEN
    CREATE ROLE dashboard_user NOSUPERUSER CREATEDB CREATEROLE REPLICATION;
  END IF;
  IF NOT EXISTS (SELECT FROM pg_roles WHERE rolname = 'pgbouncer') THEN
    CREATE ROLE pgbouncer LOGIN;
  END IF;
END
$$;

ALTER ROLE authenticator WITH PASSWORD %s;
ALTER ROLE supabase_auth_admin WITH PASSWORD %s;
ALTER ROLE supabase_storage_admin WITH PASSWORD %s;
ALTER ROLE supabase_admin WITH LOGIN SUPERUSER CREATEDB CREATEROLE REPLICATION BYPASSRLS PASSWORD %s;
ALTER ROLE supabase_replication_admin WITH PASSWORD %s;
ALTER ROLE pgbouncer WITH PASSWORD %s;
GRANT pg_read_all_data TO supabase_read_only_user;

CREATE EXTENSION IF NOT EXISTS pgcrypto WITH SCHEMA extensions;
CREATE EXTENSION IF NOT EXISTS "uuid-ossp" WITH SCHEMA extensions;
CREATE EXTENSION IF NOT EXISTS pg_graphql WITH SCHEMA graphql;
CREATE EXTENSION IF NOT EXISTS pg_stat_statements WITH SCHEMA extensions;
CREATE EXTENSION IF NOT EXISTS pg_cron WITH SCHEMA extensions;
CREATE EXTENSION IF NOT EXISTS pgmq WITH SCHEMA pgmq;
CREATE EXTENSION IF NOT EXISTS vector WITH SCHEMA extensions;
CREATE EXTENSION IF NOT EXISTS supabase_vault WITH SCHEMA vault;

DO $$
BEGIN
  IF NOT EXISTS (SELECT 1 FROM pg_publication WHERE pubname = 'supabase_realtime') THEN
    EXECUTE 'CREATE PUBLICATION supabase_realtime';
  END IF;
END
$$;

GRANT anon, authenticated, service_role TO authenticator;
GRANT USAGE ON SCHEMA public, auth, storage, graphql_public, realtime, _realtime, extensions TO anon, authenticated, service_role;
GRANT ALL PRIVILEGES ON SCHEMA auth TO supabase_auth_admin;
GRANT ALL PRIVILEGES ON SCHEMA storage TO supabase_storage_admin;
GRANT ALL PRIVILEGES ON SCHEMA public, graphql_public, realtime, _realtime, vault, extensions TO service_role;
GRANT ALL PRIVILEGES ON SCHEMA _realtime TO supabase_admin;

ALTER DEFAULT PRIVILEGES IN SCHEMA public GRANT SELECT, INSERT, UPDATE, DELETE ON TABLES TO authenticated, service_role;
ALTER DEFAULT PRIVILEGES IN SCHEMA public GRANT USAGE, SELECT ON SEQUENCES TO authenticated, service_role;
	`, passwordLiteral, passwordLiteral, passwordLiteral, passwordLiteral, passwordLiteral, passwordLiteral)
	return os.WriteFile(path, []byte(body), bindMountFileMode)
}

func writePostgresHBAFile(path string) error {
	body := `# supadupa per-project PostgreSQL client authentication.
# Local socket and loopback are for container-local bootstrap/admin tooling.
local all all trust
host all all 127.0.0.1/32 trust
host all all ::1/128 trust
local replication supabase_replication_admin scram-sha-256
host replication supabase_replication_admin 127.0.0.1/32 scram-sha-256
host replication supabase_replication_admin ::1/128 scram-sha-256
host replication supabase_replication_admin 0.0.0.0/0 scram-sha-256
host replication supabase_replication_admin ::/0 scram-sha-256

# Docker-network, Traefik-routed public DB, and pooler clients must authenticate.
host all all 0.0.0.0/0 scram-sha-256
host all all ::/0 scram-sha-256
`
	return os.WriteFile(path, []byte(body), bindMountFileMode)
}

func writeReplicaManifest(path string, ref string, replicaID string, name string, opts control.ReplicaOpts) error {
	body := fmt.Sprintf(`name: supadupa-%s-replica-%s
services:
  db-replica-%s:
    image: supabase/postgres:latest
    env_file:
      - ../.env
      - %s.env
    networks: [internal]
    volumes:
      - db-replica-%s-data:/var/lib/postgresql/data
networks:
  internal:
    internal: true
volumes:
  db-replica-%s-data:
`, ref, name, name, replicaID, name, name)
	return os.WriteFile(path, []byte(body), 0o600)
}

func writeReplicasComposeFile(path string, ref string, replicas []control.ProjectReplica) error {
	var builder strings.Builder
	builder.WriteString("services:\n")
	for _, replica := range replicas {
		name := strings.TrimSpace(replica.Name)
		if name == "" {
			name = replica.ID
		}
		service := replicaComposeServiceName(name)
		volume := service + "-data"
		fmt.Fprintf(&builder, `  %s:
    image: supabase/postgres:${STACK_VERSION}
    env_file:
      - .env
      - replicas/%s.env
    environment:
      PGPASSWORD: ${POSTGRES_PASSWORD}
    networks:
      internal: {}
      supadupa-ingress:
        aliases:
          - %s-%s
    depends_on:
      db:
        condition: service_healthy
    volumes:
      - %s:/var/lib/postgresql/data
    command:
      - bash
      - -lc
      - |
        set -euo pipefail
        export PGPASSWORD="$${POSTGRES_PASSWORD}"
        if [ ! -s "$${PGDATA}/standby.signal" ]; then
          rm -rf "$${PGDATA:?}"/*
          until pg_basebackup -h db -p 5432 -U supabase_replication_admin -D "$${PGDATA}" -Fp -Xs -P -R; do
            sleep 5
          done
        fi
        exec docker-entrypoint.sh postgres -c hot_standby=on
    healthcheck:
      test: ["CMD-SHELL", "psql -U postgres -d postgres -Atc 'select pg_is_in_recovery()' | grep -q '^t$'"]
      interval: 5s
      timeout: 5s
      retries: 30
`, service, replica.ID, ref, service, volume)
	}
	builder.WriteString("volumes:\n")
	for _, replica := range replicas {
		name := strings.TrimSpace(replica.Name)
		if name == "" {
			name = replica.ID
		}
		fmt.Fprintf(&builder, "  %s-data:\n", replicaComposeServiceName(name))
	}
	builder.WriteString("networks:\n")
	builder.WriteString("  internal:\n")
	builder.WriteString("    internal: true\n")
	builder.WriteString("  supadupa-ingress:\n")
	builder.WriteString("    external: true\n")
	return os.WriteFile(path, []byte(builder.String()), 0o600)
}

func replicaComposeServiceName(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		name = "replica"
	}
	var builder strings.Builder
	builder.WriteString("db-replica-")
	for _, r := range strings.ToLower(name) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' {
			builder.WriteRune(r)
			continue
		}
		builder.WriteRune('-')
	}
	return strings.TrimRight(builder.String(), "-")
}

func removeReplicaEnvFiles(replicaDir string, keep map[string]struct{}) error {
	entries, err := os.ReadDir(replicaDir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".env") {
			continue
		}
		if _, ok := keep[entry.Name()]; ok {
			continue
		}
		if err := os.Remove(filepath.Join(replicaDir, entry.Name())); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	return nil
}

func writeScaleManifest(path string, ref string, tier control.ResourceTier) error {
	resources := map[control.ResourceTier]struct {
		CPU    string
		Memory string
	}{
		control.ResourceTierSmall:  {CPU: "1.0", Memory: "2048M"},
		control.ResourceTierMedium: {CPU: "2.0", Memory: "4096M"},
		control.ResourceTierLarge:  {CPU: "4.0", Memory: "8192M"},
	}
	selected := resources[tier]
	body := fmt.Sprintf(`project: %s
resource_tier: %s
services:
  db:
    cpus: "%s"
    memory: %s
  kong:
    cpus: "0.25"
    memory: 256M
  studio:
    cpus: "0.25"
    memory: 512M
`, ref, tier, selected.CPU, selected.Memory)
	return os.WriteFile(path, []byte(body), 0o600)
}

func writeReplicaEnv(path string, ref string, name string, opts control.ReplicaOpts) error {
	values := map[string]string{
		"PROJECT_REF":          ref,
		"REPLICA_ID":           opts.ID,
		"REPLICA_NAME":         name,
		"REPLICA_REGION":       opts.Region,
		"REPLICA_HOST_ID":      opts.HostID,
		"REPLICA_TIER":         string(opts.Tier),
		"REPLICA_READ_WEIGHT":  fmt.Sprintf("%d", opts.ReadWeight),
		"FAILOVER_PRIORITY":    fmt.Sprintf("%d", opts.FailoverPriority),
		"REPLICATION_MODE":     "read_replica",
		"PRIMARY_SERVICE_HOST": "db",
	}
	var builder strings.Builder
	for key, value := range values {
		builder.WriteString(key)
		builder.WriteString("=")
		builder.WriteString(value)
		builder.WriteString("\n")
	}
	return os.WriteFile(path, []byte(builder.String()), 0o600)
}

func writeBranchClonePlan(path string, opts control.BranchCloneOptions, sourceDir string, branchDir string) error {
	expiresAt := ""
	if opts.ExpiresAt != nil {
		expiresAt = opts.ExpiresAt.UTC().Format("2006-01-02T15:04:05Z07:00")
	}
	body := fmt.Sprintf(`-- supadupa branch clone plan
-- mode: dry-run
-- source_ref: %s
-- branch_ref: %s
-- branch_id: %s
-- branch_name: %s
-- expires_at: %s
-- source_dir: %s
-- branch_dir: %s
--
-- Configure SUPADUPA_BRANCH_CLONE_COMMAND to run a real dump/restore clone.
-- Template variables include {{source_ref}}, {{branch_ref}}, {{branch_id}},
-- {{branch_name}}, {{source_dir}}, {{branch_dir}}, and {{clone_path}}.

BEGIN;
-- no-op dry-run clone marker for %s from %s
ROLLBACK;
`, opts.SourceRef, opts.BranchRef, opts.BranchID, opts.Name, expiresAt, sourceDir, branchDir, opts.BranchRef, opts.SourceRef)
	return os.WriteFile(path, []byte(body), 0o600)
}

func renderBranchCloneCommand(template string, opts control.BranchCloneOptions, sourceDir string, branchDir string, clonePath string) string {
	expiresAt := ""
	if opts.ExpiresAt != nil {
		expiresAt = opts.ExpiresAt.UTC().Format("2006-01-02T15:04:05Z07:00")
	}
	replacer := strings.NewReplacer(
		"{{source_ref}}", shellQuote(opts.SourceRef),
		"{{branch_ref}}", shellQuote(opts.BranchRef),
		"{{branch_id}}", shellQuote(opts.BranchID),
		"{{branch_name}}", shellQuote(opts.Name),
		"{{expires_at}}", shellQuote(expiresAt),
		"{{source_dir}}", shellQuote(sourceDir),
		"{{branch_dir}}", shellQuote(branchDir),
		"{{clone_path}}", shellQuote(clonePath),
	)
	return replacer.Replace(template)
}

func shellQuote(value string) string {
	if value == "" {
		return "''"
	}
	return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'"
}

func randomHex(bytes int) string {
	data := make([]byte, bytes)
	if _, err := rand.Read(data); err != nil {
		return "change-me"
	}
	return hex.EncodeToString(data)
}

func minInt(left int, right int) int {
	if left < right {
		return left
	}
	return right
}

func composeStackReleaseManifest(version string) control.StackReleaseManifest {
	manifest, ok := control.ResolveStackReleaseManifestFromEnv(os.Getenv, version)
	if ok {
		return manifest
	}
	manifest, _ = control.ResolveStackReleaseManifestFromEnv(os.Getenv, control.DefaultStackReleaseVersion)
	normalized := control.NormalizeStackReleaseVersion(version)
	manifest.Version = normalized
	manifest.Postgres = normalized
	return manifest
}

func sqlQuoteLiteral(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "''") + "'"
}

func envOrDefault(key string, fallback string) string {
	value := os.Getenv(key)
	if value == "" {
		return fallback
	}
	return value
}
