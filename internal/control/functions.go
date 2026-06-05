package control

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
)

type FunctionDeploymentService struct {
	projectRoot string
}

type FunctionDeploymentOptions struct {
	ProjectRoot string
}

type FunctionArtifact struct {
	Directory    string `json:"directory"`
	Entrypoint   string `json:"entrypoint"`
	SourcePath   string `json:"source_path"`
	SecretsPath  string `json:"secrets_path"`
	MetadataPath string `json:"metadata_path"`
}

func NewFunctionDeploymentService() *FunctionDeploymentService {
	return NewFunctionDeploymentServiceWithOptions(FunctionDeploymentOptions{})
}

func NewFunctionDeploymentServiceWithOptions(opts FunctionDeploymentOptions) *FunctionDeploymentService {
	root := strings.TrimSpace(opts.ProjectRoot)
	if root == "" {
		root = os.Getenv("SUPADUPA_PROJECT_ROOT")
	}
	if root == "" {
		root = "./runtime/projects"
	}
	return &FunctionDeploymentService{projectRoot: root}
}

func (s *FunctionDeploymentService) Deploy(ctx context.Context, function ProjectFunction, input ProjectFunctionInput) (FunctionArtifact, error) {
	select {
	case <-ctx.Done():
		return FunctionArtifact{}, ctx.Err()
	default:
	}
	entrypoint, err := normalizeFunctionEntrypoint(function.Entrypoint)
	if err != nil {
		return FunctionArtifact{}, err
	}
	source := strings.TrimSpace(input.Source)
	if source == "" {
		return FunctionArtifact{}, fmt.Errorf("function source is required")
	}
	secrets, err := normalizeConfigValues(input.Secrets)
	if err != nil {
		return FunctionArtifact{}, err
	}

	dir := filepath.Join(s.projectRoot, function.ProjectRef, "functions", function.Name)
	sourcePath := filepath.Join(dir, filepath.FromSlash(entrypoint))
	if err := os.MkdirAll(filepath.Dir(sourcePath), 0o700); err != nil {
		return FunctionArtifact{}, err
	}
	if err := os.WriteFile(sourcePath, []byte(source), 0o600); err != nil {
		return FunctionArtifact{}, err
	}

	secretsPath := filepath.Join(dir, ".env")
	if err := writeFunctionSecretsFile(secretsPath, function, secrets); err != nil {
		return FunctionArtifact{}, err
	}
	metadataPath := filepath.Join(dir, "metadata.json")
	if err := writeFunctionMetadataFile(metadataPath, function, entrypoint); err != nil {
		return FunctionArtifact{}, err
	}
	return FunctionArtifact{
		Directory:    dir,
		Entrypoint:   entrypoint,
		SourcePath:   sourcePath,
		SecretsPath:  secretsPath,
		MetadataPath: metadataPath,
	}, nil
}

func (s *FunctionDeploymentService) Delete(ctx context.Context, ref string, name string) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}
	normalized, err := normalizeFunctionName(name)
	if err != nil {
		return err
	}
	return os.RemoveAll(filepath.Join(s.projectRoot, ref, "functions", normalized))
}

func (s *FunctionDeploymentService) SyncRegionalInvocations(ctx context.Context, ref string, regions []ProjectFunctionRegion) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}
	functionsRoot := filepath.Join(s.projectRoot, ref, "functions")
	stale, err := filepath.Glob(filepath.Join(functionsRoot, "*", "regions.json"))
	if err != nil {
		return err
	}
	for _, path := range stale {
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	grouped := map[string][]ProjectFunctionRegion{}
	for _, region := range regions {
		grouped[region.FunctionName] = append(grouped[region.FunctionName], region)
	}
	for functionName, functionRegions := range grouped {
		dir := filepath.Join(functionsRoot, functionName)
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return err
		}
		payload, err := json.MarshalIndent(functionRegions, "", "  ")
		if err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(dir, "regions.json"), append(payload, '\n'), 0o600); err != nil {
			return err
		}
	}
	return nil
}

func (s *FunctionDeploymentService) SyncStorageMounts(ctx context.Context, ref string, mounts []ProjectFunctionStorageMount) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}
	functionsRoot := filepath.Join(s.projectRoot, ref, "functions")
	stale, err := filepath.Glob(filepath.Join(functionsRoot, "*", "storage-mounts.json"))
	if err != nil {
		return err
	}
	for _, path := range stale {
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	grouped := map[string][]ProjectFunctionStorageMount{}
	for _, mount := range mounts {
		grouped[mount.FunctionName] = append(grouped[mount.FunctionName], mount)
	}
	for functionName, functionMounts := range grouped {
		dir := filepath.Join(functionsRoot, functionName)
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return err
		}
		payload, err := json.MarshalIndent(functionMounts, "", "  ")
		if err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(dir, "storage-mounts.json"), append(payload, '\n'), 0o600); err != nil {
			return err
		}
	}
	return nil
}

func normalizeFunctionEntrypoint(entrypoint string) (string, error) {
	entrypoint = strings.TrimSpace(strings.ReplaceAll(entrypoint, "\\", "/"))
	if entrypoint == "" {
		entrypoint = "index.ts"
	}
	cleaned := path.Clean(entrypoint)
	if cleaned == "." || cleaned == ".." || strings.HasPrefix(cleaned, "../") || strings.HasPrefix(cleaned, "/") || strings.Contains(cleaned, "/../") {
		return "", fmt.Errorf("function entrypoint must be a relative path inside the function directory")
	}
	return cleaned, nil
}

func writeFunctionSecretsFile(path string, function ProjectFunction, secrets map[string]string) error {
	values := map[string]string{
		"SUPABASE_FUNCTION_NAME":    function.Name,
		"SUPABASE_FUNCTION_VERSION": fmt.Sprintf("%d", function.Version),
		"VERIFY_JWT":                fmt.Sprintf("%t", function.VerifyJWT),
	}
	for key, value := range secrets {
		if strings.TrimSpace(value) != "" {
			values[key] = value
		}
	}
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	var builder strings.Builder
	for _, key := range keys {
		builder.WriteString(key)
		builder.WriteString("=")
		builder.WriteString(values[key])
		builder.WriteString("\n")
	}
	return os.WriteFile(path, []byte(builder.String()), 0o600)
}

func writeFunctionMetadataFile(path string, function ProjectFunction, entrypoint string) error {
	payload, err := json.MarshalIndent(map[string]any{
		"name":         function.Name,
		"version":      function.Version,
		"entrypoint":   entrypoint,
		"verify_jwt":   function.VerifyJWT,
		"source_hash":  function.SourceHash,
		"source_bytes": function.SourceBytes,
		"updated_at":   function.UpdatedAt,
	}, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(payload, '\n'), 0o600)
}
