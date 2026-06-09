package terraform

import (
	"context"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	resourceschema "github.com/hashicorp/terraform-plugin-framework/resource/schema"
)

func TestProviderSchemasAvoidReservedRootNames(t *testing.T) {
	ctx := context.Background()
	reserved := map[string]struct{}{
		"provider":   {},
		"count":      {},
		"for_each":   {},
		"lifecycle":  {},
		"depends_on": {},
	}

	provider := NewProvider("test")()
	for _, factory := range provider.Resources(ctx) {
		res := factory()
		var meta resource.MetadataResponse
		res.Metadata(ctx, resource.MetadataRequest{ProviderTypeName: "supadupa"}, &meta)
		var schema resource.SchemaResponse
		res.Schema(ctx, resource.SchemaRequest{}, &schema)
		if schema.Diagnostics.HasError() {
			t.Fatalf("%s resource schema diagnostics: %v", meta.TypeName, schema.Diagnostics)
		}
		for name := range schema.Schema.Attributes {
			if _, ok := reserved[name]; ok {
				t.Fatalf("%s resource uses reserved root attribute %q", meta.TypeName, name)
			}
		}
	}

	for _, factory := range provider.DataSources(ctx) {
		ds := factory()
		var meta datasource.MetadataResponse
		ds.Metadata(ctx, datasource.MetadataRequest{ProviderTypeName: "supadupa"}, &meta)
		var schema datasource.SchemaResponse
		ds.Schema(ctx, datasource.SchemaRequest{}, &schema)
		if schema.Diagnostics.HasError() {
			t.Fatalf("%s data source schema diagnostics: %v", meta.TypeName, schema.Diagnostics)
		}
		for name := range schema.Schema.Attributes {
			if _, ok := reserved[name]; ok {
				t.Fatalf("%s data source uses reserved root attribute %q", meta.TypeName, name)
			}
		}
	}
}

func TestProjectDefaultedAttributesAreOptionalComputed(t *testing.T) {
	ctx := context.Background()
	res := NewProjectResource()
	var schema resource.SchemaResponse
	res.Schema(ctx, resource.SchemaRequest{}, &schema)
	if schema.Diagnostics.HasError() {
		t.Fatalf("project resource schema diagnostics: %v", schema.Diagnostics)
	}

	for _, name := range []string{"host_id", "domain", "stack_version", "profile", "resource_tier"} {
		attr, ok := schema.Schema.Attributes[name].(resourceschema.StringAttribute)
		if !ok {
			t.Fatalf("project attribute %s is not a string attribute", name)
		}
		if !attr.Optional || !attr.Computed {
			t.Fatalf("project attribute %s must be optional+computed because the API can supply a default, got optional=%v computed=%v", name, attr.Optional, attr.Computed)
		}
	}
}
