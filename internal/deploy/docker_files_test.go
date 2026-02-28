package deploy

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDockerfileContainsHealthcheck(t *testing.T) {
	content, err := os.ReadFile(filepath.Join("..", "..", "Dockerfile"))
	if err != nil {
		t.Fatalf("read Dockerfile: %v", err)
	}
	text := string(content)
	if !strings.Contains(text, "HEALTHCHECK") {
		t.Fatalf("expected Dockerfile to define HEALTHCHECK")
	}
	if !strings.Contains(text, "CMD [\"web\", \"--port\", \"18080\"]") {
		t.Fatalf("expected Dockerfile default command for web ui")
	}
}

func TestComposeContainsHealthcheck(t *testing.T) {
	content, err := os.ReadFile(filepath.Join("..", "..", "docker-compose.yml"))
	if err != nil {
		t.Fatalf("read docker-compose.yml: %v", err)
	}
	text := string(content)
	if !strings.Contains(text, "healthcheck:") {
		t.Fatalf("expected compose file to include healthcheck")
	}
	if !strings.Contains(text, "coco-web") {
		t.Fatalf("expected compose file to include coco-web service")
	}
}
