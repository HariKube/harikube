package pgsql

import (
	"testing"

	pkgttl "github.com/k3s-io/kine/pkg/tls"
)

func TestPrepareConfigSeparateHistoryFlag(t *testing.T) {
	cfg, separateHistory, err := prepareConfig("postgres:postgres@localhost/kubernetes?_kine_separate_history=true", pkgttl.Config{})
	if err != nil {
		t.Fatalf("prepareConfig() error = %v", err)
	}
	if !separateHistory {
		t.Fatal("prepareConfig() did not enable separate history flag")
	}
	if got := cfg.RuntimeParams[separateHistoryFlag]; got != "" {
		t.Fatalf("prepareConfig() left %q runtime param behind: %q", separateHistoryFlag, got)
	}
}

func TestPrepareConfigSeparateHistoryFlagDisabledByDefault(t *testing.T) {
	cfg, separateHistory, err := prepareConfig("postgres:postgres@localhost/kubernetes", pkgttl.Config{})
	if err != nil {
		t.Fatalf("prepareConfig() error = %v", err)
	}
	if separateHistory {
		t.Fatal("prepareConfig() unexpectedly enabled separate history flag")
	}
	if cfg.Database != "kubernetes" {
		t.Fatalf("prepareConfig() database = %q, want kubernetes", cfg.Database)
	}
}
