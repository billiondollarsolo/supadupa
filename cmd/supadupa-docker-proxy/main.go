package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"supadupa2026/internal/env"
	"time"
)

var dockerAPIVersionPrefix = regexp.MustCompile(`^/v[0-9]+(\.[0-9]+)?(/|$)`)
var dockerComposeProjectRefPattern = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9-]{1,53}[a-z0-9])$`)
var dockerObjectIDPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]{0,127}$`)
var dockerImageReferencePattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:@/-]{0,254}$`)
var dockerImageTagPattern = regexp.MustCompile(`^[A-Za-z0-9_][A-Za-z0-9_.-]{0,127}$`)

const maxDockerCreateRequestBytes = 1 << 20

func main() {
	addr := env.OrDefault("SUPADUPA_DOCKER_PROXY_ADDR", ":2375")
	socketPath := env.OrDefault("SUPADUPA_DOCKER_SOCKET", "/var/run/docker.sock")
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))

	server := &http.Server{
		Addr:              addr,
		Handler:           requestLogger(logger, dockerProxyHandler(socketPath)),
		ReadHeaderTimeout: 5 * time.Second,
	}
	logger.Info("starting docker socket proxy", "addr", addr, "socket", socketPath)
	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		logger.Error("docker socket proxy stopped", "error", err)
		os.Exit(1)
	}
}

func dockerProxyHandler(socketPath string) http.Handler {
	target := &url.URL{Scheme: "http", Host: "docker"}
	proxy := httputil.NewSingleHostReverseProxy(target)
	proxy.Transport = &http.Transport{
		DialContext: func(ctx context.Context, network string, addr string) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(ctx, "unix", socketPath)
		},
	}
	proxy.ErrorHandler = func(w http.ResponseWriter, r *http.Request, err error) {
		http.Error(w, "docker proxy upstream error", http.StatusBadGateway)
	}
	proxy.ModifyResponse = func(response *http.Response) error {
		if response.Request == nil || response.Request.Method != http.MethodGet {
			return nil
		}
		switch normalizeDockerAPIPath(response.Request.URL.EscapedPath()) {
		case "/containers/json":
			return filterDockerContainerListResponse(response)
		case "/networks":
			return filterDockerNetworkListResponse(response)
		case "/volumes":
			return filterDockerVolumeListResponse(response)
		default:
			return nil
		}
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !dockerProxyAllowed(r.Method, r.URL.EscapedPath()) {
			http.Error(w, fmt.Sprintf("docker API route is not allowed: %s %s", r.Method, normalizeDockerAPIPath(r.URL.EscapedPath())), http.StatusForbidden)
			return
		}
		if err := validateDockerProxyRequest(r, socketPath); err != nil {
			http.Error(w, err.Error(), http.StatusForbidden)
			return
		}
		proxy.ServeHTTP(w, r)
	})
}

func filterDockerContainerListResponse(response *http.Response) error {
	payload, ok, err := readDockerListResponsePayload(response, "container")
	if err != nil {
		return err
	}
	if !ok {
		return nil
	}
	var containers []map[string]any
	if err := json.Unmarshal(payload, &containers); err != nil {
		return fmt.Errorf("docker container list response must be valid JSON")
	}
	filtered := containers[:0]
	for _, container := range containers {
		if composeProjectLabelAllowed(dockerLabelsFromJSONValue(container["Labels"])) {
			filtered = append(filtered, container)
		}
	}
	return replaceDockerListResponsePayload(response, filtered, "container")
}

func filterDockerNetworkListResponse(response *http.Response) error {
	payload, ok, err := readDockerListResponsePayload(response, "network")
	if err != nil {
		return err
	}
	if !ok {
		return nil
	}
	var networks []map[string]any
	if err := json.Unmarshal(payload, &networks); err != nil {
		return fmt.Errorf("docker network list response must be valid JSON")
	}
	filtered := networks[:0]
	for _, network := range networks {
		if composeProjectLabelAllowed(dockerLabelsFromJSONValue(network["Labels"])) {
			filtered = append(filtered, network)
		}
	}
	return replaceDockerListResponsePayload(response, filtered, "network")
}

func filterDockerVolumeListResponse(response *http.Response) error {
	payload, ok, err := readDockerListResponsePayload(response, "volume")
	if err != nil {
		return err
	}
	if !ok {
		return nil
	}
	var volumes map[string]any
	if err := json.Unmarshal(payload, &volumes); err != nil {
		return fmt.Errorf("docker volume list response must be valid JSON")
	}
	rawList, ok := volumes["Volumes"].([]any)
	if !ok && volumes["Volumes"] != nil {
		return fmt.Errorf("docker volume list response must contain a volume array")
	}
	filtered := rawList[:0]
	for _, rawVolume := range rawList {
		volume, ok := rawVolume.(map[string]any)
		if !ok {
			return fmt.Errorf("docker volume list response must contain volume objects")
		}
		if composeProjectLabelAllowed(dockerLabelsFromJSONValue(volume["Labels"])) {
			filtered = append(filtered, rawVolume)
		}
	}
	volumes["Volumes"] = filtered
	return replaceDockerListResponsePayload(response, volumes, "volume")
}

func readDockerListResponsePayload(response *http.Response, label string) ([]byte, bool, error) {
	if response.Body == nil || response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, false, nil
	}
	payload, err := io.ReadAll(io.LimitReader(response.Body, maxDockerCreateRequestBytes+1))
	_ = response.Body.Close()
	if err != nil {
		return nil, false, fmt.Errorf("read docker %s list response: %w", label, err)
	}
	if len(payload) > maxDockerCreateRequestBytes {
		return nil, false, fmt.Errorf("docker %s list response exceeds proxy limit", label)
	}
	return payload, true, nil
}

func replaceDockerListResponsePayload(response *http.Response, payload any, label string) error {
	filteredPayload, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("encode filtered docker %s list response: %w", label, err)
	}
	response.Body = io.NopCloser(bytes.NewReader(filteredPayload))
	response.ContentLength = int64(len(filteredPayload))
	response.Header.Set("Content-Length", fmt.Sprintf("%d", len(filteredPayload)))
	return nil
}

func dockerLabelsFromJSONValue(value any) map[string]string {
	rawLabels, ok := value.(map[string]any)
	if !ok {
		return nil
	}
	labels := make(map[string]string, len(rawLabels))
	for name, rawValue := range rawLabels {
		value, ok := rawValue.(string)
		if !ok {
			continue
		}
		labels[name] = value
	}
	return labels
}

func dockerProxyAllowed(method string, rawPath string) bool {
	method = strings.ToUpper(strings.TrimSpace(method))
	path := normalizeDockerAPIPath(rawPath)
	switch {
	case method == http.MethodGet && (path == "/_ping" || path == "/version" || path == "/info" || path == "/events"):
		return true
	case method == http.MethodHead && path == "/_ping":
		return true
	case method == http.MethodGet && (path == "/containers/json" || dockerContainerReadAllowed(path)):
		return true
	case method == http.MethodPost && path == "/containers/create":
		return true
	case method == http.MethodPost && strings.HasPrefix(path, "/containers/"):
		return dockerContainerMutationAllowed(path)
	case method == http.MethodDelete && strings.HasPrefix(path, "/containers/"):
		return dockerPathObjectOnly(path, "/containers/")
	case method == http.MethodGet && (path == "/images/json" || dockerImageInspectReadAllowed(path)):
		return true
	case method == http.MethodPost && path == "/images/create":
		return true
	case method == http.MethodGet && (path == "/networks" || dockerPathObjectOnly(path, "/networks/")):
		return true
	case method == http.MethodPost && (path == "/networks/create" || dockerNetworkMutationAllowed(path)):
		return true
	case method == http.MethodDelete && strings.HasPrefix(path, "/networks/"):
		return dockerPathObjectOnly(path, "/networks/")
	case method == http.MethodGet && (path == "/volumes" || dockerPathObjectOnly(path, "/volumes/")):
		return true
	case method == http.MethodPost && path == "/volumes/create":
		return true
	case method == http.MethodDelete && strings.HasPrefix(path, "/volumes/"):
		return dockerPathObjectOnly(path, "/volumes/")
	case method == http.MethodGet && strings.HasPrefix(path, "/exec/"):
		return dockerExecReadAllowed(path)
	case method == http.MethodPost && strings.HasPrefix(path, "/exec/"):
		return dockerExecMutationAllowed(path)
	default:
		return false
	}
}

func dockerContainerMutationAllowed(path string) bool {
	_, action, ok := dockerPathAction(path, "/containers/")
	if !ok {
		return false
	}
	for _, allowed := range []string{"start", "stop", "restart", "kill", "wait", "exec", "resize", "rename"} {
		if action == allowed {
			return true
		}
	}
	return false
}

func dockerContainerReadAllowed(path string) bool {
	_, action, ok := dockerPathAction(path, "/containers/")
	if !ok {
		return false
	}
	for _, allowed := range []string{"json", "logs", "stats", "top"} {
		if action == allowed {
			return true
		}
	}
	return false
}

func dockerNetworkMutationAllowed(path string) bool {
	_, action, ok := dockerPathAction(path, "/networks/")
	return ok && (action == "connect" || action == "disconnect")
}

func dockerExecMutationAllowed(path string) bool {
	_, action, ok := dockerPathAction(path, "/exec/")
	return ok && (action == "start" || action == "resize")
}

func dockerExecReadAllowed(path string) bool {
	_, action, ok := dockerPathAction(path, "/exec/")
	return ok && action == "json"
}

func dockerImageInspectReadAllowed(path string) bool {
	_, ok := dockerImageInspectReference(path)
	return ok
}

func validateDockerProxyRequest(r *http.Request, socketPath string) error {
	method := strings.ToUpper(strings.TrimSpace(r.Method))
	path := normalizeDockerAPIPath(r.URL.EscapedPath())
	switch {
	case method == http.MethodGet && path == "/events":
		return validateDockerEventsRequest(r)
	case method == http.MethodGet && path == "/images/json":
		return validateDockerImageListRequest(r)
	case method == http.MethodGet && strings.HasPrefix(path, "/images/"):
		image, ok := dockerImageInspectReference(path)
		if !ok || !dockerImagePullReferenceAllowed(image) {
			return fmt.Errorf("docker image inspect request must use a valid image reference")
		}
		return nil
	case method == http.MethodPost && path == "/images/create":
		return validateDockerImageCreateRequest(r)
	case method == http.MethodPost && path == "/containers/create":
		return validateDockerContainerCreateRequest(r)
	case strings.HasPrefix(path, "/containers/") && path != "/containers/json":
		id, ok := dockerPathObjectID(path, "/containers/")
		if !ok {
			return fmt.Errorf("container id is required")
		}
		var err error
		if method == http.MethodGet {
			err = validateDockerLabeledObjectIfPresent(socketPath, "/containers/"+id+"/json", "container")
		} else {
			err = validateDockerLabeledObject(socketPath, "/containers/"+id+"/json", "container")
		}
		if err != nil {
			return err
		}
		if method == http.MethodPost && dockerContainerExecCreatePath(path) {
			return validateDockerExecCreateRequest(r)
		}
		return nil
	case strings.HasPrefix(path, "/exec/"):
		id, ok := dockerPathObjectID(path, "/exec/")
		if !ok {
			return fmt.Errorf("exec id is required")
		}
		containerID, err := dockerExecContainerID(socketPath, id)
		if err != nil {
			return err
		}
		return validateDockerLabeledObject(socketPath, "/containers/"+containerID+"/json", "exec container")
	case method == http.MethodPost && path == "/networks/create":
		return validateDockerLabeledCreateRequest(r, "network create")
	case method == http.MethodGet && strings.HasPrefix(path, "/networks/"):
		id, ok := dockerPathObjectID(path, "/networks/")
		if !ok {
			return fmt.Errorf("network id is required")
		}
		return validateDockerNetworkObjectIfPresent(socketPath, id)
	case method == http.MethodPost && dockerNetworkMutationAllowed(path):
		id, ok := dockerPathObjectID(path, "/networks/")
		if !ok {
			return fmt.Errorf("network id is required")
		}
		if err := validateDockerNetworkObject(socketPath, id); err != nil {
			return err
		}
		return validateDockerNetworkMutationRequest(r, socketPath)
	case method == http.MethodDelete && strings.HasPrefix(path, "/networks/"):
		id, ok := dockerPathObjectID(path, "/networks/")
		if !ok {
			return fmt.Errorf("network id is required")
		}
		return validateDockerLabeledObject(socketPath, "/networks/"+id, "network")
	case method == http.MethodPost && path == "/volumes/create":
		return validateDockerLabeledCreateRequest(r, "volume create")
	case method == http.MethodGet && strings.HasPrefix(path, "/volumes/"):
		id, ok := dockerPathObjectID(path, "/volumes/")
		if !ok {
			return fmt.Errorf("volume name is required")
		}
		return validateDockerLabeledObjectIfPresent(socketPath, "/volumes/"+id, "volume")
	case method == http.MethodDelete && strings.HasPrefix(path, "/volumes/"):
		id, ok := dockerPathObjectID(path, "/volumes/")
		if !ok {
			return fmt.Errorf("volume name is required")
		}
		return validateDockerLabeledObject(socketPath, "/volumes/"+id, "volume")
	default:
		return nil
	}
}

func validateDockerEventsRequest(r *http.Request) error {
	if !dockerFiltersIncludeAllowedComposeProject(r.URL.Query()) {
		return fmt.Errorf("docker events request must include a Supadupa Compose project label filter")
	}
	return nil
}

func validateDockerImageListRequest(r *http.Request) error {
	references, err := dockerQueryFilterValues(r.URL.Query(), "reference")
	if err != nil {
		return err
	}
	for _, reference := range references {
		if dockerImageFilterReferenceAllowed(reference) {
			return nil
		}
	}
	return fmt.Errorf("docker image list request must include a reference filter")
}

func validateDockerImageCreateRequest(r *http.Request) error {
	query := r.URL.Query()
	if strings.TrimSpace(query.Get("fromSrc")) != "" {
		return fmt.Errorf("docker image imports are not allowed through docker proxy")
	}
	if image := strings.TrimSpace(query.Get("fromImage")); !dockerImagePullReferenceAllowed(image) {
		return fmt.Errorf("docker image create request must include a valid fromImage pull reference")
	}
	if tag := strings.TrimSpace(query.Get("tag")); tag != "" && !dockerImageTagPattern.MatchString(tag) {
		return fmt.Errorf("docker image create request tag is invalid")
	}
	if r.Body == nil {
		return nil
	}
	payload, err := io.ReadAll(io.LimitReader(r.Body, maxDockerCreateRequestBytes+1))
	if err != nil {
		return fmt.Errorf("read image create request: %w", err)
	}
	if len(payload) > maxDockerCreateRequestBytes {
		return fmt.Errorf("image create request exceeds proxy limit")
	}
	r.Body = io.NopCloser(bytes.NewReader(payload))
	if strings.TrimSpace(string(payload)) != "" {
		return fmt.Errorf("docker image create request bodies are not allowed through docker proxy")
	}
	return nil
}

func validateDockerContainerCreateRequest(r *http.Request) error {
	if r.Body == nil {
		return fmt.Errorf("container create request body is required")
	}
	payload, err := io.ReadAll(io.LimitReader(r.Body, maxDockerCreateRequestBytes+1))
	if err != nil {
		return fmt.Errorf("read container create request: %w", err)
	}
	if len(payload) > maxDockerCreateRequestBytes {
		return fmt.Errorf("container create request exceeds proxy limit")
	}
	r.Body = io.NopCloser(bytes.NewReader(payload))

	var create dockerContainerCreatePayload
	if err := json.Unmarshal(payload, &create); err != nil {
		return fmt.Errorf("container create request must be valid JSON")
	}
	projectRef, ok := composeProjectLabelValue(create.Labels, create.Config.Labels)
	if !ok {
		return fmt.Errorf("container create request must target a Supadupa Compose project")
	}
	return validateDockerHostConfig(projectRef, create.HostConfig, create.Mounts)
}

func validateDockerExecCreateRequest(r *http.Request) error {
	payload, err := readAndRestoreDockerProxyBody(r, "container exec create")
	if err != nil {
		return err
	}
	var create dockerExecCreatePayload
	if err := json.Unmarshal(payload, &create); err != nil {
		return fmt.Errorf("container exec create request must be valid JSON")
	}
	if create.Privileged {
		return fmt.Errorf("privileged exec processes are not allowed through docker proxy")
	}
	return nil
}

func validateDockerNetworkMutationRequest(r *http.Request, socketPath string) error {
	payload, err := readAndRestoreDockerProxyBody(r, "network mutation")
	if err != nil {
		return err
	}
	var mutation dockerNetworkContainerPayload
	if err := json.Unmarshal(payload, &mutation); err != nil {
		return fmt.Errorf("network mutation request must be valid JSON")
	}
	containerID := strings.TrimSpace(mutation.Container)
	if !dockerObjectIDAllowed(containerID) {
		return fmt.Errorf("network mutation request must include a valid container id")
	}
	// The shared edge-router (a platform container under compose project
	// "supadupa", which the per-project label guard otherwise rejects) is allowed
	// to attach to a project's edge network so Traefik can route to that isolated
	// project. The target network is still validated separately, so this can only
	// connect the router to a real per-project network or the shared ingress.
	if edgeRouterContainerAllowed(containerID) {
		return nil
	}
	return validateDockerLabeledObject(socketPath, "/containers/"+containerID+"/json", "network mutation container")
}

func validateDockerLabeledCreateRequest(r *http.Request, label string) error {
	payload, err := readAndRestoreDockerProxyBody(r, label)
	if err != nil {
		return err
	}

	var create dockerLabeledCreatePayload
	if err := json.Unmarshal(payload, &create); err != nil {
		return fmt.Errorf("%s request must be valid JSON", label)
	}
	if !composeProjectLabelAllowed(create.Labels) {
		return fmt.Errorf("%s request must target a Supadupa Compose project", label)
	}
	return nil
}

func readAndRestoreDockerProxyBody(r *http.Request, label string) ([]byte, error) {
	if r.Body == nil {
		return nil, fmt.Errorf("%s request body is required", label)
	}
	payload, err := io.ReadAll(io.LimitReader(r.Body, maxDockerCreateRequestBytes+1))
	if err != nil {
		return nil, fmt.Errorf("read %s request: %w", label, err)
	}
	if len(payload) > maxDockerCreateRequestBytes {
		return nil, fmt.Errorf("%s request exceeds proxy limit", label)
	}
	r.Body = io.NopCloser(bytes.NewReader(payload))
	return payload, nil
}

type dockerContainerCreatePayload struct {
	Labels     map[string]string `json:"Labels"`
	Config     dockerConfig      `json:"Config"`
	HostConfig dockerHostConfig  `json:"HostConfig"`
	Mounts     []dockerMount     `json:"Mounts"`
}

type dockerConfig struct {
	Labels map[string]string `json:"Labels"`
}

type dockerLabeledCreatePayload struct {
	Labels map[string]string `json:"Labels"`
}

type dockerExecCreatePayload struct {
	Privileged bool `json:"Privileged"`
}

type dockerNetworkContainerPayload struct {
	Container string `json:"Container"`
}

type dockerContainerListItem struct {
	ID     string            `json:"Id,omitempty"`
	Names  []string          `json:"Names,omitempty"`
	Labels map[string]string `json:"Labels,omitempty"`
}

type dockerHostConfig struct {
	Binds       []string      `json:"Binds"`
	Mounts      []dockerMount `json:"Mounts"`
	Privileged  bool          `json:"Privileged"`
	CapAdd      []string      `json:"CapAdd"`
	Devices     []any         `json:"Devices"`
	NetworkMode string        `json:"NetworkMode"`
	PidMode     string        `json:"PidMode"`
	IpcMode     string        `json:"IpcMode"`
}

type dockerMount struct {
	Type   string `json:"Type"`
	Source string `json:"Source"`
	Target string `json:"Target"`
}

func composeProjectLabelAllowed(labelSets ...map[string]string) bool {
	_, ok := composeProjectLabelValue(labelSets...)
	return ok
}

func composeProjectLabelValue(labelSets ...map[string]string) (string, bool) {
	for _, labels := range labelSets {
		project := strings.TrimSpace(labels["com.docker.compose.project"])
		if dockerComposeProjectRefPattern.MatchString(project) && project != "supadupa" {
			return project, true
		}
	}
	return "", false
}

func dockerFiltersIncludeAllowedComposeProject(query url.Values) bool {
	labels, err := dockerQueryFilterValues(query, "label")
	if err != nil {
		return false
	}
	projectLabels := 0
	for _, label := range labels {
		name, value, ok := strings.Cut(strings.TrimSpace(label), "=")
		if !ok || strings.TrimSpace(name) != "com.docker.compose.project" {
			continue
		}
		projectLabels++
		if projectLabels > 1 || !composeProjectLabelAllowed(map[string]string{"com.docker.compose.project": value}) {
			return false
		}
	}
	return projectLabels == 1
}

func dockerQueryFilterValues(query url.Values, name string) ([]string, error) {
	raw := strings.TrimSpace(query.Get("filters"))
	if raw == "" {
		return nil, nil
	}
	var filters map[string]any
	if err := json.Unmarshal([]byte(raw), &filters); err != nil {
		return nil, fmt.Errorf("docker filter query must be valid JSON")
	}
	rawValues, ok := filters[name]
	if !ok {
		return nil, nil
	}
	values, ok := rawValues.([]any)
	if !ok {
		return nil, fmt.Errorf("docker %s filter must be an array", name)
	}
	filterValues := make([]string, 0, len(values))
	for _, rawValue := range values {
		value, ok := rawValue.(string)
		if !ok {
			return nil, fmt.Errorf("docker %s filter values must be strings", name)
		}
		filterValues = append(filterValues, value)
	}
	return filterValues, nil
}

func dockerImageFilterReferenceAllowed(value string) bool {
	value = strings.TrimSpace(value)
	if value == "" || strings.Count(value, "*") > 1 {
		return false
	}
	if strings.ReplaceAll(value, "*", "") == "" {
		return false
	}
	if strings.Contains(value, "*") {
		if !strings.HasSuffix(value, ":*") || strings.HasPrefix(value, "*") {
			return false
		}
		return dockerImageReferenceShapeAllowed(strings.Replace(value, "*", "x", 1))
	}
	return dockerImageReferenceShapeAllowed(value)
}

func dockerImagePullReferenceAllowed(value string) bool {
	value = strings.TrimSpace(value)
	if strings.Contains(value, "*") {
		return false
	}
	return dockerImageReferenceShapeAllowed(value)
}

func dockerImageReferenceShapeAllowed(value string) bool {
	value = strings.TrimSpace(value)
	if value == "" || !dockerImageReferencePattern.MatchString(value) {
		return false
	}
	if strings.Contains(value, "://") || strings.Contains(value, "//") {
		return false
	}
	if strings.ContainsAny(value, "?&#=%") {
		return false
	}
	for _, r := range value {
		if r < 0x21 || r == 0x7f {
			return false
		}
	}
	for _, part := range strings.Split(value, "/") {
		if part == "" || part == "." || part == ".." {
			return false
		}
	}
	return true
}

func dockerImageInspectReference(path string) (string, bool) {
	value := strings.TrimPrefix(path, "/images/")
	image, ok := strings.CutSuffix(value, "/json")
	if !ok {
		return "", false
	}
	image = strings.TrimSpace(image)
	if image == "" {
		return "", false
	}
	decoded, err := url.PathUnescape(image)
	if err != nil {
		return "", false
	}
	return strings.TrimSpace(decoded), true
}

func validateDockerLabeledObject(socketPath string, inspectPath string, label string) error {
	labels, err := dockerObjectLabels(socketPath, inspectPath)
	if err != nil {
		return err
	}
	if !composeProjectLabelAllowed(labels) {
		return fmt.Errorf("%s is not part of a Supadupa Compose project", label)
	}
	return nil
}

func validateDockerLabeledObjectIfPresent(socketPath string, inspectPath string, label string) error {
	labels, err := dockerObjectLabels(socketPath, inspectPath)
	if err != nil {
		if isDockerInspectHTTPStatus(err, http.StatusNotFound, nil) {
			return nil
		}
		return err
	}
	if !composeProjectLabelAllowed(labels) {
		return fmt.Errorf("%s is not part of a Supadupa Compose project", label)
	}
	return nil
}

func validateDockerNetworkObject(socketPath string, id string) error {
	name, labels, err := dockerNetworkObject(socketPath, id)
	if err != nil {
		return err
	}
	if composeProjectLabelAllowed(labels) || sharedIngressNetworkAllowed(id, name) {
		return nil
	}
	return fmt.Errorf("network is not part of a Supadupa Compose project")
}

func validateDockerNetworkObjectIfPresent(socketPath string, id string) error {
	name, labels, err := dockerNetworkObject(socketPath, id)
	if err != nil {
		if isDockerInspectHTTPStatus(err, http.StatusNotFound, nil) {
			return nil
		}
		return err
	}
	if composeProjectLabelAllowed(labels) || sharedIngressNetworkAllowed(id, name) {
		return nil
	}
	return fmt.Errorf("network is not part of a Supadupa Compose project")
}

func dockerNetworkObject(socketPath string, id string) (string, map[string]string, error) {
	var payload struct {
		Name   string            `json:"Name"`
		Labels map[string]string `json:"Labels"`
	}
	var err error
	for attempt := 0; attempt < 5; attempt++ {
		err = dockerInspect(socketPath, "/networks/"+id, &payload)
		if err == nil {
			break
		}
		if !isDockerInspectHTTPStatus(err, http.StatusNotFound, nil) {
			return "", nil, err
		}
		time.Sleep(time.Duration(attempt+1) * 50 * time.Millisecond)
	}
	if err != nil {
		return "", nil, err
	}
	return strings.TrimSpace(payload.Name), payload.Labels, nil
}

func edgeRouterContainerAllowed(container string) bool {
	name := strings.TrimSpace(env.OrDefault("SUPADUPA_EDGE_ROUTER_CONTAINER", "supadupa-edge-router-1"))
	return name != "" && strings.TrimSpace(container) == name
}

func sharedIngressNetworkAllowed(id string, name string) bool {
	ingressNetwork := strings.TrimSpace(env.OrDefault("SUPADUPA_INGRESS_NETWORK", "supadupa-ingress"))
	if ingressNetwork == "" {
		return false
	}
	return id == ingressNetwork || name == ingressNetwork
}

func dockerObjectLabels(socketPath string, inspectPath string) (map[string]string, error) {
	var payload struct {
		Config struct {
			Labels map[string]string `json:"Labels"`
		} `json:"Config"`
		Labels map[string]string `json:"Labels"`
	}
	var err error
	for attempt := 0; attempt < 5; attempt++ {
		err = dockerInspect(socketPath, inspectPath, &payload)
		if err == nil {
			break
		}
		var inspectErr dockerInspectError
		if !isDockerInspectHTTPStatus(err, http.StatusNotFound, &inspectErr) {
			return nil, err
		}
		time.Sleep(time.Duration(attempt+1) * 50 * time.Millisecond)
	}
	if err != nil {
		return nil, err
	}
	if composeProjectLabelAllowed(payload.Labels) {
		return payload.Labels, nil
	}
	return payload.Config.Labels, nil
}

func dockerExecContainerID(socketPath string, execID string) (string, error) {
	var payload struct {
		ContainerID string `json:"ContainerID"`
	}
	if err := dockerInspect(socketPath, "/exec/"+execID+"/json", &payload); err != nil {
		return "", err
	}
	containerID := strings.TrimSpace(payload.ContainerID)
	if containerID == "" {
		return "", fmt.Errorf("exec container id is missing")
	}
	return containerID, nil
}

func dockerInspect(socketPath string, inspectPath string, out any) error {
	transport := &http.Transport{
		DialContext: func(ctx context.Context, network string, addr string) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(ctx, "unix", socketPath)
		},
	}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, Timeout: 5 * time.Second}
	response, err := client.Get("http://docker" + inspectPath)
	if err != nil {
		return fmt.Errorf("inspect docker object: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return dockerInspectError{Path: inspectPath, StatusCode: response.StatusCode}
	}
	if err := json.NewDecoder(io.LimitReader(response.Body, maxDockerCreateRequestBytes)).Decode(out); err != nil {
		return fmt.Errorf("decode docker object inspect response: %w", err)
	}
	return nil
}

type dockerInspectError struct {
	Path       string
	StatusCode int
}

func (e dockerInspectError) Error() string {
	return fmt.Sprintf("inspect docker object %s returned HTTP %d", e.Path, e.StatusCode)
}

func isDockerInspectHTTPStatus(err error, status int, out *dockerInspectError) bool {
	if err == nil {
		return false
	}
	var inspectErr dockerInspectError
	if !errors.As(err, &inspectErr) {
		return false
	}
	if out != nil {
		*out = inspectErr
	}
	return inspectErr.StatusCode == status
}

func dockerPathObjectID(path string, prefix string) (string, bool) {
	value := strings.TrimPrefix(path, prefix)
	value, _, _ = strings.Cut(value, "/")
	value = strings.TrimSpace(value)
	if value == "" {
		return "", false
	}
	decoded, err := url.PathUnescape(value)
	if err != nil {
		return "", false
	}
	decoded = strings.TrimSpace(decoded)
	return decoded, dockerObjectIDAllowed(decoded)
}

func dockerPathAction(path string, prefix string) (string, string, bool) {
	value := strings.TrimPrefix(path, prefix)
	id, action, ok := strings.Cut(value, "/")
	if !ok || action == "" || strings.Contains(action, "/") {
		return "", "", false
	}
	id, err := url.PathUnescape(strings.TrimSpace(id))
	if err != nil || !dockerObjectIDAllowed(id) {
		return "", "", false
	}
	return id, action, true
}

func dockerPathObjectOnly(path string, prefix string) bool {
	value := strings.TrimPrefix(path, prefix)
	if value == "" || strings.Contains(value, "/") {
		return false
	}
	_, ok := dockerPathObjectID(path, prefix)
	return ok
}

func dockerContainerExecCreatePath(path string) bool {
	_, action, ok := dockerPathAction(path, "/containers/")
	return ok && action == "exec"
}

func dockerObjectIDAllowed(value string) bool {
	return dockerObjectIDPattern.MatchString(strings.TrimSpace(value))
}

func validateDockerHostConfig(projectRef string, config dockerHostConfig, extraMounts []dockerMount) error {
	if config.Privileged {
		return fmt.Errorf("privileged containers are not allowed through docker proxy")
	}
	if len(config.CapAdd) > 0 {
		return fmt.Errorf("additional container capabilities are not allowed through docker proxy")
	}
	if len(config.Devices) > 0 {
		return fmt.Errorf("container device mappings are not allowed through docker proxy")
	}
	for name, mode := range map[string]string{
		"network": config.NetworkMode,
		"pid":     config.PidMode,
		"ipc":     config.IpcMode,
	} {
		if strings.EqualFold(strings.TrimSpace(mode), "host") {
			return fmt.Errorf("host %s namespace is not allowed through docker proxy", name)
		}
	}
	for _, bind := range config.Binds {
		source, _ := splitDockerBind(bind)
		if !projectBindSourceAllowed(projectRef, source) {
			return fmt.Errorf("bind mount source %q is outside the project's allowed paths", source)
		}
	}
	for _, mount := range append(config.Mounts, extraMounts...) {
		if !strings.EqualFold(strings.TrimSpace(mount.Type), "bind") {
			continue
		}
		if !projectBindSourceAllowed(projectRef, mount.Source) {
			return fmt.Errorf("bind mount source %q is outside the project's allowed paths", mount.Source)
		}
	}
	return nil
}

func splitDockerBind(bind string) (string, string) {
	source, rest, ok := strings.Cut(bind, ":")
	if !ok {
		return strings.TrimSpace(bind), ""
	}
	target, _, _ := strings.Cut(rest, ":")
	return strings.TrimSpace(source), strings.TrimSpace(target)
}

// projectBindSourceAllowed is an ALLOWLIST for container bind-mount sources
// arriving through the proxy. The proxy is the single privileged boundary
// (it holds the real root-owned docker socket), so instead of denylisting known
// dangerous host paths we permit ONLY:
//   - named docker volumes / in-project relative paths that don't traverse up
//     (e.g. "db-data", "./pg_hba.conf"), and
//   - absolute host paths inside THIS project's own directory under the
//     configured SUPADUPA_PROJECT_HOST_ROOT.
//
// Everything else — the docker socket, /var/lib/docker, /home, /etc, another
// project's directory, or any host path outside the project root — is rejected,
// closing the host-takeover and cross-tenant escape surface a denylist missed.
func projectBindSourceAllowed(projectRef string, source string) bool {
	source = strings.TrimSpace(source)
	if source == "" {
		return true
	}
	if !strings.HasPrefix(source, "/") {
		// Named volume or in-project relative path; allow unless it escapes upward.
		cleaned := filepath.Clean(source)
		return cleaned != ".." && !strings.HasPrefix(cleaned, ".."+string(os.PathSeparator))
	}
	return allowedProjectBindMountPath(projectRef, source)
}

func allowedProjectBindMountPath(projectRef string, path string) bool {
	projectRef = strings.TrimSpace(projectRef)
	if !dockerComposeProjectRefPattern.MatchString(projectRef) || projectRef == "supadupa" {
		return false
	}
	root := strings.TrimSpace(os.Getenv("SUPADUPA_PROJECT_HOST_ROOT"))
	if root == "" {
		return false
	}
	source := filepath.Clean(strings.TrimSpace(path))
	projectRoot := filepath.Join(filepath.Clean(root), projectRef)
	return source == projectRoot || strings.HasPrefix(source, projectRoot+string(os.PathSeparator))
}

func normalizeDockerAPIPath(path string) string {
	if path == "" {
		return "/"
	}
	normalized := dockerAPIVersionPrefix.ReplaceAllString(path, "/")
	if normalized == "" {
		return "/"
	}
	if !strings.HasPrefix(normalized, "/") {
		return "/" + normalized
	}
	return normalized
}

func requestLogger(logger *slog.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		next.ServeHTTP(w, r)
		logger.Debug("docker proxy request", "method", r.Method, "path", r.URL.EscapedPath())
	})
}
