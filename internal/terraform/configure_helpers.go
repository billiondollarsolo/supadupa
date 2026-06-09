package terraform

import "fmt"

func clientFromProviderData(providerData any, addError func(string, string)) (*Client, bool) {
	if providerData == nil {
		return nil, false
	}
	client, ok := providerData.(*Client)
	if !ok {
		addError("Unexpected provider data", fmt.Sprintf("Expected *terraform.Client, got %T.", providerData))
		return nil, false
	}
	return client, true
}
