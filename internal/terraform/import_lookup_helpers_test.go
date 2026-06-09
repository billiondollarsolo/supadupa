package terraform

import (
	"errors"
	"testing"
)

func TestOnePartImportID(t *testing.T) {
	value, ok := onePartImportID(" alpha ")
	if !ok || value != "alpha" {
		t.Fatalf("expected trimmed alpha, got value=%q ok=%t", value, ok)
	}
	if value, ok := onePartImportID("   "); ok {
		t.Fatalf("expected blank import to be invalid, got %q", value)
	}
}

func TestTwoPartImportID(t *testing.T) {
	for _, input := range []string{"alpha/assets", " alpha / assets ", "alpha:assets"} {
		first, second, ok := twoPartImportID(input)
		if !ok || first != "alpha" || second != "assets" {
			t.Fatalf("expected %q to parse as alpha/assets, got first=%q second=%q ok=%t", input, first, second, ok)
		}
	}
	for _, input := range []string{"", "alpha", "/assets", "alpha/", " : assets"} {
		if first, second, ok := twoPartImportID(input); ok {
			t.Fatalf("expected %q to be invalid, got first=%q second=%q", input, first, second)
		}
	}
}

func TestThreePartImportID(t *testing.T) {
	for _, input := range []string{"alpha/team/platform", " alpha / team / platform "} {
		first, second, third, ok := threePartImportID(input, false)
		if !ok || first != "alpha" || second != "team" || third != "platform" {
			t.Fatalf("expected %q to parse as alpha/team/platform, got first=%q second=%q third=%q ok=%t", input, first, second, third, ok)
		}
	}
	first, second, third, ok := threePartImportID("alpha:app-schema:20260605_001", true)
	if !ok || first != "alpha" || second != "app-schema" || third != "20260605_001" {
		t.Fatalf("expected colon import to parse, got first=%q second=%q third=%q ok=%t", first, second, third, ok)
	}
	if _, _, _, ok := threePartImportID("alpha:team:platform", false); ok {
		t.Fatal("expected colon import to be invalid when allowColon is false")
	}
	for _, input := range []string{"", "alpha/team", "/team/platform", "alpha//platform", "alpha/team/"} {
		if first, second, third, ok := threePartImportID(input, true); ok {
			t.Fatalf("expected %q to be invalid, got first=%q second=%q third=%q", input, first, second, third)
		}
	}
}

func TestFindInList(t *testing.T) {
	type item struct {
		Name string
	}
	found, err := findInList([]item{{Name: "one"}, {Name: "two"}}, func(candidate item) bool {
		return candidate.Name == "two"
	})
	if err != nil || found.Name != "two" {
		t.Fatalf("expected to find two, got item=%#v err=%v", found, err)
	}
	_, err = findInList([]item{{Name: "one"}}, func(candidate item) bool {
		return candidate.Name == "missing"
	})
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}
