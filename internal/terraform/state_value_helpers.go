package terraform

import (
	"context"
	"strings"
	"time"

	"github.com/hashicorp/terraform-plugin-framework/types"
)

func optionalStringValue(value string) types.String {
	if strings.TrimSpace(value) == "" {
		return types.StringNull()
	}
	return types.StringValue(value)
}

func optionalTimeString(value time.Time) types.String {
	if value.IsZero() {
		return types.StringValue("")
	}
	return types.StringValue(value.Format("2006-01-02T15:04:05Z07:00"))
}

func optionalTimePointerString(value *time.Time) types.String {
	if value == nil || value.IsZero() {
		return types.StringValue("")
	}
	return optionalTimeString(*value)
}

func stringListStateValue(ctx context.Context, name string, values []string, addError func(string, string)) (types.List, bool) {
	value, diags := types.ListValueFrom(ctx, types.StringType, values)
	if diags.HasError() {
		addError("Unable to encode "+name+" list", diags.Errors()[0].Detail())
		return types.ListNull(types.StringType), false
	}
	return value, true
}

func stringMapStateValue(ctx context.Context, name string, values map[string]string, addError func(string, string)) (types.Map, bool) {
	value, diags := types.MapValueFrom(ctx, types.StringType, values)
	if diags.HasError() {
		addError("Unable to encode "+name+" map", diags.Errors()[0].Detail())
		return types.MapNull(types.StringType), false
	}
	return value, true
}

func sensitiveStringMapStateValue(ctx context.Context, name string, remote map[string]string, previous map[string]string, addError func(string, string)) (types.Map, bool) {
	return stringMapStateValue(ctx, name, preserveMaskedConfigValues(remote, previous), addError)
}
