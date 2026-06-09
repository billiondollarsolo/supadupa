package terraform

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
)

func requireResourceReplaceOnUpdate(_ context.Context, req resource.ModifyPlanRequest, resp *resource.ModifyPlanResponse, attr string) {
	if req.State.Raw.IsNull() || req.Plan.Raw.IsNull() {
		return
	}
	resp.RequiresReplace = append(resp.RequiresReplace, path.Root(attr))
}

func reportUnsupportedInPlaceUpdate(resp *resource.UpdateResponse, resourceName string) {
	resp.Diagnostics.AddError(
		"Resource requires replacement",
		resourceName+" does not support in-place updates. Terraform should plan this change as a replacement; run terraform plan again and verify the resource is shown as destroy/create before applying.",
	)
}
