package compose

import (
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
	"gitlab":        "GITLAB",
	"kakao":         "KAKAO",
	"keycloak":      "KEYCLOAK",
	"linkedin_oidc": "LINKEDIN_OIDC",
	"notion":        "NOTION",
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
	if err := os.MkdirAll(filepath.Join(projectDir, "log-drains"), 0o700); err != nil {
		return err
	}
	postgresPassword, err := writeEnvFile(filepath.Join(projectDir, ".env"), spec)
	if err != nil {
		return err
	}
	if err := writeKongConfigFile(filepath.Join(projectDir, "kong.yml"), spec.Services); err != nil {
		return err
	}
	if err := writeVectorConfigFile(filepath.Join(projectDir, "vector.yml"), spec.Ref); err != nil {
		return err
	}
	if err := writeDatabaseInitFile(filepath.Join(projectDir, "00-supadupa-init.sql"), postgresPassword); err != nil {
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
	if !p.apply {
		return nil
	}
	return p.runCompose(ctx, projectDir, ref, "up", "-d")
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
	applyConfigEnvValues(values, config)
	if err := writeEnvValues(envPath, values); err != nil {
		return err
	}
	if !p.apply {
		return nil
	}
	return p.runCompose(ctx, projectDir, ref, "up", "-d")
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
	applyServiceEnvValues(values, spec.Services)
	if err := writeEnvValues(envPath, values); err != nil {
		return err
	}
	if err := writeKongConfigFile(filepath.Join(projectDir, "kong.yml"), spec.Services); err != nil {
		return err
	}
	if err := writeComposeFile(filepath.Join(projectDir, "compose.yaml"), spec); err != nil {
		return err
	}
	if !p.apply {
		return nil
	}
	return p.runCompose(ctx, projectDir, ref, "up", "-d", "--remove-orphans")
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
	return p.runCompose(ctx, projectDir, ref, "up", "-d")
}

func (p *Provisioner) Destroy(ctx context.Context, ref string) error {
	return p.DestroyWithOptions(ctx, ref, control.DestroyOptions{})
}

func (p *Provisioner) DestroyWithOptions(ctx context.Context, ref string, opts control.DestroyOptions) error {
	projectDir := filepath.Join(p.rootDir, ref)
	if p.apply {
		if err := p.runCompose(ctx, projectDir, ref, "down"); err != nil {
			return err
		}
	}
	if opts.RetainVolumes {
		if err := p.writeRetainedVolumeManifest(ref); err != nil {
			return err
		}
	}
	return os.RemoveAll(projectDir)
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
		"vector.yml",
		"00-supadupa-init.sql",
		"auth-hooks.json",
		"log-drains",
	}
}

func requiredComposeFragments(services map[string]bool) []string {
	fragments := []string{
		"supabase/postgres:",
		"supabase/postgres-meta:",
		"./00-supadupa-init.sql:/docker-entrypoint-initdb.d/00-supadupa-init.sql:ro",
		"./kong.yml:/etc/kong/kong.yml:ro",
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
		fragments = append(fragments, "./functions:/home/deno/functions:ro")
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
	spec := control.ProjectSpec{
		Ref:          ref,
		StackVersion: version,
	}
	if err := updateEnvValue(filepath.Join(projectDir, ".env"), "STACK_VERSION", version); err != nil {
		return err
	}
	env, err := readEnvFile(filepath.Join(projectDir, ".env"))
	if err != nil {
		return err
	}
	postgresPassword := env["POSTGRES_PASSWORD"]
	if postgresPassword == "" {
		postgresPassword = randomHex(24)
		if err := updateEnvValue(filepath.Join(projectDir, ".env"), "POSTGRES_PASSWORD", postgresPassword); err != nil {
			return err
		}
	}
	if err := writeKongConfigFile(filepath.Join(projectDir, "kong.yml"), spec.Services); err != nil {
		return err
	}
	if err := writeVectorConfigFile(filepath.Join(projectDir, "vector.yml"), ref); err != nil {
		return err
	}
	if err := writeDatabaseInitFile(filepath.Join(projectDir, "00-supadupa-init.sql"), postgresPassword); err != nil {
		return err
	}
	if err := writeComposeFile(filepath.Join(projectDir, "compose.yaml"), spec); err != nil {
		return err
	}
	if !p.apply {
		return nil
	}
	return p.runCompose(ctx, projectDir, ref, "up", "-d")
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

func (p *Provisioner) runComposeOutput(ctx context.Context, projectDir string, ref string, args ...string) ([]byte, error) {
	parts := strings.Fields(p.command)
	if len(parts) == 0 {
		return nil, fmt.Errorf("compose command is empty")
	}
	commandArgs := append(parts[1:], append([]string{"-p", ref, "-f", filepath.Join(projectDir, "compose.yaml")}, args...)...)
	cmd := exec.CommandContext(ctx, parts[0], commandArgs...)
	cmd.Dir = projectDir
	cmd.Env = append(os.Environ(), "COMPOSE_PROJECT_NAME="+ref)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("compose %s failed: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(string(output)))
	}
	return output, nil
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
	stackVersion := normalizeStackVersion(spec.StackVersion)
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
	jwtSecret := randomHex(32)
	if value := strings.TrimSpace(spec.Environment["JWT_SECRET"]); value != "" {
		jwtSecret = value
	}
	postgresPassword := randomHex(24)
	if value := strings.TrimSpace(spec.Environment["POSTGRES_PASSWORD"]); value != "" {
		postgresPassword = value
	}
	values := map[string]string{
		"ANON_KEY":                        "generated-by-control-plane",
		"API_EXTERNAL_URL":                apiExternalURL,
		"DASHBOARD_USERNAME":              "supadupa",
		"DASHBOARD_PASSWORD":              randomHex(18),
		"EDGE_RUNTIME_POLICY":             "oneshot",
		"FILE_SIZE_LIMIT":                 "52428800",
		"FUNCTIONS_VERIFY_JWT":            "true",
		"GOTRUE_API_HOST":                 "0.0.0.0",
		"GOTRUE_API_PORT":                 "9999",
		"GOTRUE_DB_DATABASE_URL":          fmt.Sprintf("postgres://supabase_auth_admin:%s@db:5432/postgres", postgresPassword),
		"GOTRUE_JWT_ADMIN_ROLES":          "service_role",
		"GOTRUE_JWT_AUD":                  "authenticated",
		"GOTRUE_JWT_DEFAULT_GROUP_NAME":   "authenticated",
		"GOTRUE_JWT_EXP":                  "3600",
		"GOTRUE_JWT_SECRET":               jwtSecret,
		"GOTRUE_SITE_URL":                 apiExternalURL,
		"IMGPROXY_BIND":                   ":5001",
		"JWT_SECRET":                      jwtSecret,
		"LOGFLARE_API_KEY":                randomHex(24),
		"LOGFLARE_LOGGER_BACKEND_API_KEY": randomHex(24),
		"PGRST_DB_ANON_ROLE":              "anon",
		"PGRST_DB_SCHEMAS":                "public,storage,graphql_public",
		"PGRST_DB_URI":                    fmt.Sprintf("postgres://authenticator:%s@db:5432/postgres", postgresPassword),
		"PGRST_JWT_SECRET":                jwtSecret,
		"POSTGRES_DB":                     "postgres",
		"POSTGRES_HOST":                   "db",
		"POSTGRES_PASSWORD":               postgresPassword,
		"POSTGRES_PORT":                   "5432",
		"POSTGRES_USER":                   "postgres",
		"PROJECT_DOMAIN":                  projectDomain,
		"PROJECT_REF":                     spec.Ref,
		"REALTIME_DB_HOST":                "db",
		"REALTIME_DB_NAME":                "postgres",
		"REALTIME_DB_PASSWORD":            postgresPassword,
		"REALTIME_DB_PORT":                "5432",
		"REALTIME_DB_USER":                "supabase_admin",
		"REALTIME_JWT_SECRET":             jwtSecret,
		"RESOURCE_TIER":                   resourceTier,
		"SERVICE_ROLE_KEY":                "generated-by-control-plane",
		"SITE_URL":                        apiExternalURL,
		"SMTP_ADMIN_EMAIL":                "",
		"SMTP_HOST":                       "",
		"SMTP_PASS":                       "",
		"SMTP_PORT":                       "587",
		"SMTP_SENDER_NAME":                "",
		"SMTP_TLS_MODE":                   "starttls",
		"SMTP_USER":                       "",
		"STACK_VERSION":                   stackVersion,
		"STACK_PROFILE":                   stackProfile,
		"STORAGE_BACKEND":                 "file",
		"STORAGE_FILE_SIZE_LIMIT":         "52428800",
		"STORAGE_IMGPROXY_URL":            "http://imgproxy:5001",
		"STUDIO_DEFAULT_ORGANIZATION":     "supadupa",
		"STUDIO_DEFAULT_PROJECT":          spec.Ref,
		"STUDIO_PG_META_URL":              "http://meta:8080",
		"SUPABASE_PUBLIC_URL":             apiExternalURL,
		"SUPADUPA_DESIRED_STATE":          "running",
		"SUPADUPA_ORIOLEDB_PROFILE":       orioleDBProfile,
		"SUPADUPA_STACK_PROFILE":          stackProfile,
		"SUPAVISOR_DB_HOST":               "db",
		"SUPAVISOR_DB_NAME":               "postgres",
		"SUPAVISOR_DB_PASSWORD":           postgresPassword,
		"SUPAVISOR_DB_PORT":               "5432",
		"SUPAVISOR_DB_USER":               "supabase_admin",
	}
	for key, value := range spec.Environment {
		values[key] = value
	}
	applyDerivedSecretEnvValues(values)
	applyServiceEnvValues(values, spec.Services)
	applyAuthHookEnvValues(values, nil)

	return values["POSTGRES_PASSWORD"], writeEnvValues(path, values)
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
		copyConfigValue(values, config.Config, "captcha_secret_handle", "GOTRUE_SECURITY_CAPTCHA_SECRET")
		copyConfigValue(values, config.Config, "captcha_secret_handle", "SUPADUPA_AUTH_CAPTCHA_SECRET_HANDLE")
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
		copyConfigValue(values, config.Config, "oauth_google_client_secret_handle", "GOTRUE_EXTERNAL_GOOGLE_SECRET")
		copyConfigValue(values, config.Config, "oauth_github_enabled", "GOTRUE_EXTERNAL_GITHUB_ENABLED")
		copyConfigValue(values, config.Config, "oauth_github_client_id", "GOTRUE_EXTERNAL_GITHUB_CLIENT_ID")
		copyConfigValue(values, config.Config, "oauth_github_client_secret_handle", "GOTRUE_EXTERNAL_GITHUB_SECRET")
		copyConfigValue(values, config.Config, "oauth_azure_enabled", "GOTRUE_EXTERNAL_AZURE_ENABLED")
		copyConfigValue(values, config.Config, "oauth_azure_client_id", "GOTRUE_EXTERNAL_AZURE_CLIENT_ID")
		copyConfigValue(values, config.Config, "oauth_azure_client_secret_handle", "GOTRUE_EXTERNAL_AZURE_SECRET")
		for provider, envProvider := range socialOAuthProviders {
			prefix := "oauth_" + provider
			envPrefix := "GOTRUE_EXTERNAL_" + envProvider
			copyConfigValue(values, config.Config, prefix+"_enabled", envPrefix+"_ENABLED")
			copyConfigValue(values, config.Config, prefix+"_client_id", envPrefix+"_CLIENT_ID")
			copyConfigValue(values, config.Config, prefix+"_client_secret_handle", envPrefix+"_SECRET")
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
		copyConfigValue(values, config.Config, "sms_twilio_account_sid", "GOTRUE_SMS_TWILIO_ACCOUNT_SID")
		copyConfigValue(values, config.Config, "sms_twilio_auth_token_handle", "GOTRUE_SMS_TWILIO_AUTH_TOKEN")
		copyConfigValue(values, config.Config, "sms_twilio_message_service_sid", "GOTRUE_SMS_TWILIO_MESSAGE_SERVICE_SID")
		copyConfigValue(values, config.Config, "sms_messagebird_originator", "GOTRUE_SMS_MESSAGEBIRD_ORIGINATOR")
		copyConfigValue(values, config.Config, "sms_messagebird_access_key_handle", "GOTRUE_SMS_MESSAGEBIRD_ACCESS_KEY")
		copyConfigValue(values, config.Config, "sms_textlocal_sender", "GOTRUE_SMS_TEXTLOCAL_SENDER")
		copyConfigValue(values, config.Config, "sms_textlocal_api_key_handle", "GOTRUE_SMS_TEXTLOCAL_API_KEY")
		copyConfigValue(values, config.Config, "sms_vonage_from", "GOTRUE_SMS_VONAGE_FROM")
		copyConfigValue(values, config.Config, "sms_vonage_api_key", "GOTRUE_SMS_VONAGE_API_KEY")
		copyConfigValue(values, config.Config, "sms_vonage_api_secret_handle", "GOTRUE_SMS_VONAGE_API_SECRET")
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
		copyConfigValue(values, config.Config, "transaction_port", "SUPADUPA_POOLER_TRANSACTION_PORT")
		copyConfigValue(values, config.Config, "session_port", "SUPADUPA_POOLER_SESSION_PORT")
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
		copyConfigValue(values, config.Config, "password_handle", "SMTP_PASS")
		copyConfigValue(values, config.Config, "password_handle", "GOTRUE_SMTP_PASS")
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
	return os.WriteFile(path, []byte(builder.String()), 0o600)
}

func envFileValue(value string) string {
	value = strings.ReplaceAll(value, "\r\n", "\n")
	value = strings.ReplaceAll(value, "\r", "\n")
	return strings.ReplaceAll(value, "\n", `\n`)
}

func applyDerivedSecretEnvValues(values map[string]string) {
	jwtSecret := strings.TrimSpace(values["JWT_SECRET"])
	if jwtSecret != "" {
		values["GOTRUE_JWT_SECRET"] = jwtSecret
		values["PGRST_JWT_SECRET"] = jwtSecret
		values["REALTIME_JWT_SECRET"] = jwtSecret
	}
	postgresPassword := strings.TrimSpace(values["POSTGRES_PASSWORD"])
	if postgresPassword != "" {
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
	return os.WriteFile(path, []byte(strings.Join(lines, "\n")), 0o600)
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
	stackVersion := normalizeStackVersion(spec.StackVersion)
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
      POSTGRES_PASSWORD: ${POSTGRES_PASSWORD}
      POSTGRES_USER: ${POSTGRES_USER}
    networks: [internal]
    volumes:
      - db-data:/var/lib/postgresql/data
      - ./00-supadupa-init.sql:/docker-entrypoint-initdb.d/00-supadupa-init.sql:ro
  kong:
    image: kong:2.8.1
    env_file: .env
    environment:
      KONG_DATABASE: "off"
      KONG_DECLARATIVE_CONFIG: /etc/kong/kong.yml
      KONG_DNS_ORDER: LAST,A,CNAME,AAAA
      KONG_NGINX_PROXY_PROXY_BUFFER_SIZE: 160k
      KONG_NGINX_PROXY_PROXY_BUFFERS: 64 160k
      KONG_PLUGINS: request-transformer,cors,key-auth,acl,basic-auth
    networks:
      internal: {}
      supadupa-ingress:
        aliases:
          - %s-kong
    volumes:
      - ./kong.yml:/etc/kong/kong.yml:ro
    depends_on:
`, spec.Ref, stackVersion, spec.Ref))
	for _, dependency := range depends {
		builder.WriteString(fmt.Sprintf("      - %s\n", dependency))
	}
	if services["studio"] {
		builder.WriteString(fmt.Sprintf(`  studio:
    image: supabase/studio:latest
    env_file: .env
    networks:
      internal: {}
      supadupa-ingress:
        aliases:
          - %s-studio
    depends_on: [meta]
`, spec.Ref))
	}
	builder.WriteString(`  meta:
    image: supabase/postgres-meta:latest
    env_file: .env
    environment:
      PG_META_DB_HOST: db
      PG_META_DB_NAME: ${POSTGRES_DB}
      PG_META_DB_PASSWORD: ${POSTGRES_PASSWORD}
      PG_META_DB_PORT: ${POSTGRES_PORT}
      PG_META_DB_USER: ${POSTGRES_USER}
    networks: [internal]
    depends_on: [db]
`)
	if services["auth"] {
		builder.WriteString(`  auth:
    image: supabase/gotrue:latest
    env_file: .env
    networks: [internal]
    depends_on: [db]
`)
	}
	if services["rest"] {
		builder.WriteString(`  rest:
    image: postgrest/postgrest:latest
    env_file: .env
    networks: [internal]
    depends_on: [db]
`)
	}
	if services["realtime"] {
		builder.WriteString(`  realtime:
    image: supabase/realtime:latest
    env_file: .env
    networks: [internal]
    depends_on: [db]
`)
	}
	if services["storage"] {
		builder.WriteString(`  storage:
    image: supabase/storage-api:latest
    env_file: .env
    networks: [internal]
    volumes:
      - storage-data:/var/lib/storage
    depends_on:
      - db
`)
		if services["imgproxy"] {
			builder.WriteString("      - imgproxy\n")
		}
	}
	if services["imgproxy"] {
		builder.WriteString(`  imgproxy:
    image: darthsim/imgproxy:latest
    env_file: .env
    networks: [internal]
`)
	}
	if services["functions"] {
		builder.WriteString(`  edge-runtime:
    image: supabase/edge-runtime:latest
    env_file: .env
    networks: [internal]
    volumes:
      - ./functions:/home/deno/functions:ro
`)
	}
	if services["pooler"] {
		builder.WriteString(`  pooler:
    image: supabase/supavisor:latest
    env_file: .env
    networks: [internal]
    depends_on: [db]
`)
	}
	if services["analytics"] {
		builder.WriteString(`  analytics:
    image: supabase/logflare:latest
    env_file: .env
    networks: [internal]
    depends_on: [db]
`)
	}
	if services["vector"] {
		builder.WriteString(`  vector:
    image: timberio/vector:latest-alpine
    env_file: .env
    networks: [internal]
    volumes:
      - ./vector.yml:/etc/vector/vector.yml:ro
      - ./log-drains:/etc/vector/log-drains:ro
      - logs:/var/log/supadupa
    command: ["--config", "/etc/vector/vector.yml", "--config-dir", "/etc/vector/log-drains"]
`)
	}
	builder.WriteString(`networks:
  internal:
    internal: true
  supadupa-ingress:
    external: true
volumes:
  db-data:
  storage-data:
  logs:
`)
	return os.WriteFile(path, []byte(builder.String()), 0o600)
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

func writeKongConfigFile(path string, services map[string]control.ServiceSpec) error {
	states := control.ProjectServiceStates(services)
	var builder strings.Builder
	builder.WriteString("_format_version: \"2.1\"\n_transform: true\n\nservices:\n")
	if states["auth"] {
		builder.WriteString(`  - name: auth-v1
    url: http://auth:9999
    routes:
      - name: auth-v1
        strip_path: true
        paths: [/auth/v1]
`)
	}
	if states["rest"] {
		builder.WriteString(`  - name: rest-v1
    url: http://rest:3000
    routes:
      - name: rest-v1
        strip_path: true
        paths: [/rest/v1]
`)
	}
	if states["graphql"] && states["rest"] {
		builder.WriteString(`  - name: graphql-v1
    url: http://rest:3000/rpc/graphql
    routes:
      - name: graphql-v1
        strip_path: true
        paths: [/graphql/v1]
`)
	}
	if states["realtime"] {
		builder.WriteString(`  - name: realtime-v1
    url: http://realtime:4000/socket/
    routes:
      - name: realtime-v1
        strip_path: true
        paths: [/realtime/v1]
`)
	}
	if states["storage"] {
		builder.WriteString(`  - name: storage-v1
    url: http://storage:5000
    routes:
      - name: storage-v1
        strip_path: true
        paths: [/storage/v1]
`)
	}
	if states["functions"] {
		builder.WriteString(`  - name: functions-v1
    url: http://edge-runtime:9000
    routes:
      - name: functions-v1
        strip_path: true
        paths: [/functions/v1]
`)
	}
	if states["studio"] {
		builder.WriteString(`  - name: studio
    url: http://studio:3000
    routes:
      - name: studio
        strip_path: true
        paths: [/studio]
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
	return os.WriteFile(path, []byte(builder.String()), 0o600)
}

func writeVectorConfigFile(path string, ref string) error {
	body := fmt.Sprintf(`sources:
  project_logs:
    type: file
    include:
      - /var/log/supadupa/*.log

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
`, ref)
	return os.WriteFile(path, []byte(body), 0o600)
}

func writeDatabaseInitFile(path string, postgresPassword string) error {
	passwordLiteral := sqlQuoteLiteral(postgresPassword)
	body := fmt.Sprintf(`-- supadupa per-project database bootstrap.
-- This runs once when the project Postgres volume is first initialized.

CREATE SCHEMA IF NOT EXISTS auth;
CREATE SCHEMA IF NOT EXISTS storage;
CREATE SCHEMA IF NOT EXISTS graphql;
CREATE SCHEMA IF NOT EXISTS graphql_public;
CREATE SCHEMA IF NOT EXISTS realtime;
CREATE SCHEMA IF NOT EXISTS vault;
CREATE SCHEMA IF NOT EXISTS extensions;

CREATE EXTENSION IF NOT EXISTS pgcrypto WITH SCHEMA extensions;
CREATE EXTENSION IF NOT EXISTS "uuid-ossp" WITH SCHEMA extensions;
CREATE EXTENSION IF NOT EXISTS pg_graphql WITH SCHEMA graphql;
CREATE EXTENSION IF NOT EXISTS pg_stat_statements WITH SCHEMA extensions;
CREATE EXTENSION IF NOT EXISTS pg_cron WITH SCHEMA extensions;
CREATE EXTENSION IF NOT EXISTS pgmq WITH SCHEMA extensions;
CREATE EXTENSION IF NOT EXISTS vector WITH SCHEMA extensions;
CREATE EXTENSION IF NOT EXISTS supabase_vault WITH SCHEMA vault;

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
    CREATE ROLE authenticator NOINHERIT LOGIN PASSWORD %s;
  END IF;
  IF NOT EXISTS (SELECT FROM pg_roles WHERE rolname = 'supabase_auth_admin') THEN
    CREATE ROLE supabase_auth_admin LOGIN PASSWORD %s;
  END IF;
  IF NOT EXISTS (SELECT FROM pg_roles WHERE rolname = 'supabase_storage_admin') THEN
    CREATE ROLE supabase_storage_admin LOGIN PASSWORD %s;
  END IF;
  IF NOT EXISTS (SELECT FROM pg_roles WHERE rolname = 'supabase_admin') THEN
    CREATE ROLE supabase_admin LOGIN PASSWORD %s;
  END IF;
END
$$;

GRANT anon, authenticated, service_role TO authenticator;
GRANT USAGE ON SCHEMA public, auth, storage, graphql_public, realtime, extensions TO anon, authenticated, service_role;
GRANT ALL PRIVILEGES ON SCHEMA auth TO supabase_auth_admin;
GRANT ALL PRIVILEGES ON SCHEMA storage TO supabase_storage_admin;
GRANT ALL PRIVILEGES ON SCHEMA public, graphql_public, realtime, vault, extensions TO service_role;

ALTER DEFAULT PRIVILEGES IN SCHEMA public GRANT SELECT, INSERT, UPDATE, DELETE ON TABLES TO authenticated, service_role;
ALTER DEFAULT PRIVILEGES IN SCHEMA public GRANT USAGE, SELECT ON SEQUENCES TO authenticated, service_role;
`, passwordLiteral, passwordLiteral, passwordLiteral, passwordLiteral)
	return os.WriteFile(path, []byte(body), 0o600)
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

func normalizeStackVersion(version string) string {
	version = strings.TrimSpace(version)
	if version == "" {
		return "latest"
	}
	return version
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
