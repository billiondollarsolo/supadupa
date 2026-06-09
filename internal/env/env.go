// Package env centralizes the small environment-variable helpers that were
// previously copy-pasted across the control plane, provisioners, operator, and
// docker proxy. Values are trimmed; boolean parsing accepts the common truthy
// spellings (1/true/yes/y/on).
package env

import (
	"os"
	"strings"
)

// OrDefault returns the trimmed value of the environment variable key, or
// fallback when it is unset or blank.
func OrDefault(key string, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return fallback
}

// Bool reports whether the environment variable key is set to a truthy value.
func Bool(key string) bool {
	return BoolValue(os.Getenv(key))
}

// BoolValue reports whether value is one of the accepted truthy spellings.
func BoolValue(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "1", "true", "yes", "y", "on":
		return true
	default:
		return false
	}
}

// FirstNonEmpty returns value when it is non-blank, otherwise fallback. Unlike
// OrDefault it operates on a literal string rather than an env lookup.
func FirstNonEmpty(value string, fallback string) string {
	if strings.TrimSpace(value) != "" {
		return value
	}
	return fallback
}
