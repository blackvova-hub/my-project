package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDockerContextExcludesLocalDevCredentials(t *testing.T) {
	dockerignore := readSecurityFixture(t, ".dockerignore")
	ignoreEntries := make(map[string]struct{})
	for _, line := range strings.Split(dockerignore, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		ignoreEntries[line] = struct{}{}
	}
	for _, required := range []string{".env", ".env.*", "*.key", "*.pem", "*.p12", "*.pfx"} {
		if _, ok := ignoreEntries[required]; !ok {
			t.Errorf(".dockerignore must contain %q", required)
		}
	}

	dockerfile := readSecurityFixture(t, "Dockerfile")
	if strings.Contains(dockerfile, "COPY . .") {
		t.Fatal("Dockerfile must not copy the whole worker context")
	}
	for _, required := range []string{"COPY cmd ./cmd", "COPY internal ./internal"} {
		if !strings.Contains(dockerfile, required) {
			t.Errorf("Dockerfile must contain %q", required)
		}
	}
}

func readSecurityFixture(t *testing.T, path string) string {
	t.Helper()
	contents, err := os.ReadFile(filepath.Join("..", "..", path))
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(contents)
}
