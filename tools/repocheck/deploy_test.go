package repocheck

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"gopkg.in/yaml.v3"

	"amdl/internal/server"
)

// The checks on the deployment files under deploy/.

// docker-compose.yml's stop_grace_period must outlast the whole shutdown
// budget, or Docker kills the backend before it closes the store.
func TestShutdownBudgetFitsComposeGracePeriod(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "..", "deploy", "docker-compose.yml"))
	if err != nil {
		t.Fatal(err)
	}
	var compose struct {
		Services map[string]struct {
			StopGracePeriod string `yaml:"stop_grace_period"`
		} `yaml:"services"`
	}
	if err := yaml.Unmarshal(raw, &compose); err != nil {
		t.Fatal(err)
	}
	grace, err := time.ParseDuration(compose.Services["amdl-backend"].StopGracePeriod)
	if err != nil {
		t.Fatalf("amdl-backend stop_grace_period: %v", err)
	}
	if budget := server.ShutdownBudget(); budget >= grace {
		t.Fatalf("shutdown budget %s does not fit stop_grace_period %s", budget, grace)
	}
}
