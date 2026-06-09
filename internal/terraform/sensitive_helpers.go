package terraform

import (
	"context"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/types"
)

func optionalConfigMapFromTerraform(ctx context.Context, value types.Map, addError func(string, string)) (map[string]string, bool) {
	if value.IsNull() || value.IsUnknown() {
		return map[string]string{}, true
	}
	return configMapFromTerraform(ctx, value, addError)
}

func previousSensitiveString(value types.String) string {
	if value.IsNull() || value.IsUnknown() {
		return ""
	}
	return value.ValueString()
}

func preserveMaskedSensitiveValue(remote string, previous string) string {
	if remote == "********" && strings.TrimSpace(previous) != "" {
		return previous
	}
	return remote
}

func preserveMaskedConfigValues(remote map[string]string, previous map[string]string) map[string]string {
	merged := map[string]string{}
	for key, value := range remote {
		if value == "********" {
			if previousValue, ok := previous[key]; ok && strings.TrimSpace(previousValue) != "" {
				merged[key] = previousValue
				continue
			}
		}
		merged[key] = value
	}
	return merged
}

func sensitiveStringValue(value string) types.String {
	if strings.TrimSpace(value) == "" {
		return types.StringNull()
	}
	return types.StringValue(value)
}
