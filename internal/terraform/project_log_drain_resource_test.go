package terraform

import "testing"

func TestPreserveMaskedConfigValues(t *testing.T) {
	merged := preserveMaskedConfigValues(
		map[string]string{
			"url":   "https://logs.example.com/ingest",
			"token": "********",
		},
		map[string]string{
			"token": "secret://projects/alpha/logs",
		},
	)
	if merged["url"] != "https://logs.example.com/ingest" {
		t.Fatalf("expected remote url, got %#v", merged)
	}
	if merged["token"] != "secret://projects/alpha/logs" {
		t.Fatalf("expected previous token, got %#v", merged)
	}
}
