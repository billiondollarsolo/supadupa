package terraform

import (
	"context"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
)

func setOnePartImportState(ctx context.Context, id string, resp *resource.ImportStateResponse, attribute string, detail string) {
	value, ok := onePartImportID(id)
	if !ok {
		resp.Diagnostics.AddError("Invalid import ID", detail)
		return
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root(attribute), value)...)
}

func setTwoPartImportState(ctx context.Context, id string, resp *resource.ImportStateResponse, firstAttribute string, secondAttribute string, detail string) {
	first, second, ok := twoPartImportID(id)
	if !ok {
		resp.Diagnostics.AddError("Invalid import ID", detail)
		return
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root(firstAttribute), first)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root(secondAttribute), second)...)
}

func setThreePartImportState(ctx context.Context, id string, resp *resource.ImportStateResponse, firstAttribute string, secondAttribute string, thirdAttribute string, detail string, allowColon bool) {
	first, second, third, ok := threePartImportID(id, allowColon)
	if !ok {
		resp.Diagnostics.AddError("Invalid import ID", detail)
		return
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root(firstAttribute), first)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root(secondAttribute), second)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root(thirdAttribute), third)...)
}

func onePartImportID(id string) (string, bool) {
	value := strings.TrimSpace(id)
	return value, value != ""
}

func twoPartImportID(id string) (string, string, bool) {
	first, second, ok := strings.Cut(id, "/")
	if !ok {
		first, second, ok = strings.Cut(id, ":")
	}
	first = strings.TrimSpace(first)
	second = strings.TrimSpace(second)
	if !ok || first == "" || second == "" {
		return "", "", false
	}
	return first, second, true
}

func threePartImportID(id string, allowColon bool) (string, string, string, bool) {
	parts := strings.Split(id, "/")
	if len(parts) != 3 && allowColon {
		parts = strings.Split(id, ":")
	}
	if len(parts) != 3 {
		return "", "", "", false
	}
	first := strings.TrimSpace(parts[0])
	second := strings.TrimSpace(parts[1])
	third := strings.TrimSpace(parts[2])
	if first == "" || second == "" || third == "" {
		return "", "", "", false
	}
	return first, second, third, true
}

func findInList[T any](items []T, matches func(T) bool) (T, error) {
	for _, item := range items {
		if matches(item) {
			return item, nil
		}
	}
	var zero T
	return zero, ErrNotFound
}
