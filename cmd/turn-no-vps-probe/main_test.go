package main

import (
	"reflect"
	"testing"
)

func TestNormalizeVKHash(t *testing.T) {
	cases := map[string]string{
		"abc123":                                  "abc123",
		" https://vk.com/call/join/abc123 ":       "abc123",
		"https://vk.com/call/join/abc123?foo=bar": "abc123",
		"<VK_HASH>":                               "",
		"":                                        "",
	}

	for input, want := range cases {
		if got := normalizeVKHash(input); got != want {
			t.Fatalf("normalizeVKHash(%q)=%q, want %q", input, got, want)
		}
	}
}

func TestParseTargets(t *testing.T) {
	got, err := parseTargets("149.154.175.50:443, 149.154.167.51:443,149.154.175.50:443")
	if err != nil {
		t.Fatalf("parseTargets: %v", err)
	}

	want := []string{
		"149.154.175.50:443",
		"149.154.167.51:443",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("parseTargets=%v, want %v", got, want)
	}
}

func TestParseTargetsRejectsInvalid(t *testing.T) {
	if _, err := parseTargets("149.154.175.50"); err == nil {
		t.Fatal("expected invalid target error")
	}
	if _, err := parseTargets(" , "); err == nil {
		t.Fatal("expected empty target list error")
	}
}
