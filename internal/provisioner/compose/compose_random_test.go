package compose

import (
	"crypto/rand"
	"errors"
	"strings"
	"testing"
)

func TestRandomHexFailClosedNeverReturnsChangeMe(t *testing.T) {
	original := cryptoRead
	t.Cleanup(func() { cryptoRead = original })

	cryptoRead = func(b []byte) (int, error) {
		return 0, errors.New("forced entropy failure")
	}
	value, err := randomHex(16)
	if err == nil {
		t.Fatal("expected error on CSPRNG failure")
	}
	if value != "" {
		t.Fatalf("expected empty value on failure, got %q", value)
	}
	if value == "change-me" || strings.Contains(value, "change-me") {
		t.Fatalf("must never return weak placeholder, got %q", value)
	}

	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("mustRandomHex must panic on CSPRNG failure (fail-closed)")
		}
	}()
	_ = mustRandomHex(8)
}

func TestRandomHexSuccess(t *testing.T) {
	original := cryptoRead
	t.Cleanup(func() { cryptoRead = original })
	cryptoRead = rand.Read

	value, err := randomHex(8)
	if err != nil {
		t.Fatal(err)
	}
	if len(value) != 16 {
		t.Fatalf("expected 16 hex chars, got %q", value)
	}
}
