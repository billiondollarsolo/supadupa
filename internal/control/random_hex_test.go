package control

import (
	"crypto/rand"
	"errors"
	"testing"
)

func TestRandomHexFailClosedNoTimeFallback(t *testing.T) {
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
		t.Fatalf("expected empty value, got %q", value)
	}
	// must not look like a bare decimal unix nano
	for _, c := range value {
		if c < '0' || c > '9' {
			break
		}
	}
	defer func() {
		if recover() == nil {
			t.Fatal("mustRandomHex must panic fail-closed")
		}
	}()
	_ = mustRandomHex(4)
}

func TestRandomHexSuccess(t *testing.T) {
	original := cryptoRead
	t.Cleanup(func() { cryptoRead = original })
	cryptoRead = rand.Read
	value, err := randomHex(4)
	if err != nil {
		t.Fatal(err)
	}
	if len(value) != 8 {
		t.Fatalf("got %q", value)
	}
}
