package terraform

import "testing"

func TestClientFromProviderData(t *testing.T) {
	var errors []string
	addError := func(title string, detail string) {
		errors = append(errors, title+"|"+detail)
	}

	client, ok := clientFromProviderData(nil, addError)
	if ok || client != nil || len(errors) != 0 {
		t.Fatalf("nil provider data should not configure or report errors: client=%#v ok=%t errors=%#v", client, ok, errors)
	}

	client, ok = clientFromProviderData("wrong", addError)
	if ok || client != nil || len(errors) != 1 || errors[0] != "Unexpected provider data|Expected *terraform.Client, got string." {
		t.Fatalf("wrong provider data should report one diagnostic: client=%#v ok=%t errors=%#v", client, ok, errors)
	}

	expected := &Client{baseURL: "https://api.example.test"}
	client, ok = clientFromProviderData(expected, addError)
	if !ok || client != expected || len(errors) != 1 {
		t.Fatalf("valid provider data should return the same client without new errors: client=%#v ok=%t errors=%#v", client, ok, errors)
	}
}
