package provisioner

import (
	"fmt"
	"strings"

	"supadupa2026/internal/control"
	composeprovisioner "supadupa2026/internal/provisioner/compose"
	kubernetesprovisioner "supadupa2026/internal/provisioner/kubernetes"
)

func NewFromEnv(getenv func(string) string) (control.Provisioner, error) {
	if getenv == nil {
		getenv = func(string) string { return "" }
	}
	name := strings.ToLower(strings.TrimSpace(getenv("SUPADUPA_PROVISIONER")))
	if name == "" {
		name = "compose"
	}
	switch name {
	case "compose", "docker-compose", "docker":
		return composeprovisioner.New(), nil
	case "kubernetes", "k8s":
		return kubernetesprovisioner.New(), nil
	default:
		return nil, fmt.Errorf("unsupported provisioner %q", name)
	}
}
