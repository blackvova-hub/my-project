package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAuthorizerClaimCannotBeReused(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	a, err := openAuthorizer(path, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := a.claim(100, "long-enough-secret", "long-enough-secret"); err != nil {
		t.Fatal(err)
	}
	if !a.allowed(100) {
		t.Fatal("claimed chat must be allowed")
	}
	if err := a.claim(200, "long-enough-secret", "long-enough-secret"); err == nil {
		t.Fatal("bootstrap secret must not be reusable")
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatal(err)
	}
}

func TestAuthorizerDoesNotRemoveLastAdmin(t *testing.T) {
	a, err := openAuthorizer(filepath.Join(t.TempDir(), "state.json"), []int64{1})
	if err != nil {
		t.Fatal(err)
	}
	if err := a.revoke(1, 1); err == nil {
		t.Fatal("must keep one administrator")
	}
}

func TestDockerFrames(t *testing.T) {
	frame := append([]byte{1, 0, 0, 0, 0, 0, 0, 3}, []byte("one")...)
	frame = append(frame, []byte{2, 0, 0, 0, 0, 0, 0, 3}...)
	frame = append(frame, []byte("two")...)
	if got := string(dockerFrames(frame)); got != "onetwo" {
		t.Fatalf("dockerFrames = %q", got)
	}
}

func TestSplitMessagePreservesAllRunes(t *testing.T) {
	input := strings.Repeat("я", 20) + "\n" + strings.Repeat("x", 20)
	parts := splitMessage(input, 18)
	if strings.Join(parts, "") != input {
		t.Fatalf("message changed: %#v", parts)
	}
	for _, part := range parts {
		if len([]rune(part)) > 18 {
			t.Fatalf("part too large: %d", len([]rune(part)))
		}
	}
}

func TestRedact(t *testing.T) {
	got := redact("TOKEN=abc password=123 normal=value")
	if strings.Contains(got, "abc") || strings.Contains(got, "123") || !strings.Contains(got, "normal=value") {
		t.Fatalf("unexpected redaction %q", got)
	}
}
