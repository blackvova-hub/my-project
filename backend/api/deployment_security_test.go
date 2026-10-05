package backend

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBackendDockerContextIsExplicitAndSecretFree(t *testing.T) {
	dockerfile := readDeploymentFixture(t, "Dockerfile")
	if strings.Contains(dockerfile, "COPY . .") {
		t.Fatal("Backend Dockerfile must not copy the entire build context")
	}
	for _, required := range []string{

		"COPY cmd ./cmd",

		"COPY internal ./internal",
	} {
		if !strings.Contains(dockerfile, required) {
			t.Errorf("Backend Dockerfile must contain %q", required)
		}
	}

	dockerignore := ignoreEntries(readDeploymentFixture(t, ".dockerignore"))
	for _, required := range []string{".env", ".env.*", "*.key", "*.pem", "*.p12", "*.pfx", "certs"} {
		if _, ok := dockerignore[required]; !ok {
			t.Errorf("Backend .dockerignore must contain %q", required)
		}
	}
}

func TestComposeRedisAndDatabaseExposureIsFailClosed(t *testing.T) {
	mainCompose := readDeploymentFixture(t, "..", "compose.yaml")
	if !strings.Contains(mainCompose, `"${POSTGRES_BIND_IP:-127.0.0.1}:5435:5432"`) {
		t.Error("PostgreSQL host port must default to loopback")
	}
	if !strings.Contains(mainCompose, `"${REDIS_BIND_IP:-127.0.0.1}:6379:6379"`) {
		t.Error("Redis host port must default to loopback and require an explicit private bind override")
	}
	if strings.Contains(mainCompose, "${POSTGRES_PASSWORD}") {
		t.Error("PostgreSQL password must never silently default to an empty value")
	}
	if !strings.Contains(mainCompose, `POSTGRES_PASSWORD: "${POSTGRES_PASSWORD:?POSTGRES_PASSWORD must be set}"`) {
		t.Error("PostgreSQL service must require an explicit password")
	}
	for _, required := range []string{
		`REDIS_PASSWORD: "${REDIS_PASSWORD:?REDIS_PASSWORD must be set}"`,
		"- --requirepass",
		`REDISCLI_AUTH=\"$$REDIS_PASSWORD\"`,
	} {
		if !strings.Contains(mainCompose, required) {
			t.Errorf("main compose must contain %q", required)
		}
	}

	mainServices := composeServiceBlocks(mainCompose)
	redisClients := 0
	for name, block := range mainServices {
		if !strings.Contains(block, "REDIS_ADDR:") {
			continue
		}
		redisClients++
		if !strings.Contains(block, `REDIS_PASSWORD: "${REDIS_PASSWORD:?REDIS_PASSWORD must be set}"`) {
			t.Errorf("Redis client service %q does not receive mandatory authentication", name)
		}
	}
	for name, block := range mainServices {
		if (strings.Contains(block, "PG_DSN:") || strings.Contains(block, "DATABASE_URL:")) &&
			!strings.Contains(block, "${POSTGRES_PASSWORD:?POSTGRES_PASSWORD must be set}") {
			t.Errorf("PostgreSQL client service %q does not require the database password", name)
		}
	}
	if redisClients == 0 {
		t.Fatal("compose parser did not find Redis client services")
	}

	for _, name := range []string{
		"scanner_okx_liquidations",
		"scanner_bitget_liquidations",
		"scanner_gateio_liquidations",
	} {
		block, ok := mainServices[name]
		if !ok {
			t.Fatalf("missing service %q", name)
		}
		if strings.Contains(block, "replicas:") {
			t.Errorf("%s must remain a single publisher until explicit ownership exists", name)
		}
	}
	for name, block := range mainServices {
		if strings.Contains(block, "container_name:") && strings.Contains(block, "replicas:") {
			t.Errorf("service %q combines a fixed container_name with replicas", name)
		}
	}
}

func TestSecondServerUsesExplicitPrivateAuthenticatedRedis(t *testing.T) {
	secondCompose := readDeploymentFixture(t, "..", "..", "frontend", "deploy", "secondary-frontend", "compose.yaml")
	for _, required := range []string{
		`REDIS_ADDR: "${MAIN_REDIS_PRIVATE_ADDR:?MAIN_REDIS_PRIVATE_ADDR must be set}"`,
		`REDIS_PASSWORD: "${REDIS_PASSWORD:?REDIS_PASSWORD must be set}"`,
	} {
		if !strings.Contains(secondCompose, required) {
			t.Errorf("second-server compose must contain %q", required)
		}
	}
	if strings.Contains(secondCompose, `REDIS_ADDR: "10.20.0.2:6379"`) {
		t.Error("second-server compose must not hard-code the previous Redis endpoint")
	}
}

func TestRepositoryIgnoresCredentialFiles(t *testing.T) {
	rootIgnore := ignoreEntries(readDeploymentFixture(t, "..", "..", ".gitignore"))
	for _, required := range []string{"**/.env", "**/.env.*", "**/*.key", "**/*.pem", "**/*.p12", "**/*.pfx"} {
		if _, ok := rootIgnore[required]; !ok {
			t.Errorf("root .gitignore must contain %q", required)
		}
	}
}

func composeServiceBlocks(contents string) map[string]string {
	services := make(map[string]string)
	lines := strings.Split(contents, "\n")
	inServices := false
	current := ""
	var block strings.Builder
	flush := func() {
		if current != "" {
			services[current] = block.String()
		}
		block.Reset()
	}
	for _, line := range lines {
		if strings.TrimSpace(line) == "services:" {
			inServices = true
			continue
		}
		if !inServices {
			continue
		}
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			if current != "" {
				block.WriteByte('\n')
			}
			continue
		}
		if line[0] != ' ' {
			flush()
			break
		}
		if strings.HasPrefix(line, "  ") && !strings.HasPrefix(line, "    ") && strings.HasSuffix(trimmed, ":") && !strings.HasPrefix(trimmed, "#") {
			flush()
			current = strings.TrimSuffix(trimmed, ":")
			continue
		}
		if current != "" {
			block.WriteString(line)
			block.WriteByte('\n')
		}
	}
	flush()
	return services
}

func ignoreEntries(contents string) map[string]struct{} {
	entries := make(map[string]struct{})
	for _, line := range strings.Split(contents, "\n") {
		line = strings.TrimSpace(line)
		if line != "" && !strings.HasPrefix(line, "#") {
			entries[line] = struct{}{}
		}
	}
	return entries
}

func readDeploymentFixture(t *testing.T, parts ...string) string {
	t.Helper()
	contents, err := os.ReadFile(filepath.Join(parts...))
	if err != nil {
		t.Fatalf("read %s: %v", filepath.Join(parts...), err)
	}
	return string(contents)
}
