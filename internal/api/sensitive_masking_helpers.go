package api

import "strings"

const maskedSensitiveValue = "********"

func maskSensitiveStringMap(input map[string]string, isSensitive func(string) bool) map[string]string {
	masked := map[string]string{}
	for key, value := range input {
		if isSensitive(key) && strings.TrimSpace(value) != "" {
			masked[key] = maskedSensitiveValue
			continue
		}
		masked[key] = value
	}
	return masked
}

func isSensitiveMetadataKey(key string) bool {
	switch strings.ToLower(strings.TrimSpace(key)) {
	case "api_key", "token", "secret", "password", "access_key", "secret_key", "access_token", "bearer_token", "authorization", "x-api-key":
		return true
	default:
		return false
	}
}
