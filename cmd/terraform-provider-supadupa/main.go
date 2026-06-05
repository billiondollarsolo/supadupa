package main

import (
	"context"
	"log"

	"github.com/hashicorp/terraform-plugin-framework/providerserver"

	supadupatf "supadupa2026/internal/terraform"
)

var version = "dev"

func main() {
	err := providerserver.Serve(context.Background(), supadupatf.NewProvider(version), providerserver.ServeOpts{
		Address: "registry.terraform.io/supadupa/supadupa",
	})
	if err != nil {
		log.Fatal(err)
	}
}
