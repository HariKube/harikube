package mysql

import (
	cryptotls "crypto/tls"
	"testing"
)

func TestPrepareConfigSeparateHistoryFlag(t *testing.T) {
	cfg, separateHistory, err := prepareConfig("user:pass@tcp(localhost:3306)/kubernetes?_kine_separate_history=true", &cryptotls.Config{})
	if err != nil {
		t.Fatalf("prepareConfig() error = %v", err)
	}
	if !separateHistory {
		t.Fatal("prepareConfig() did not enable separate history flag")
	}
	if _, ok := cfg.Params[separateHistoryFlag]; ok {
		t.Fatalf("prepareConfig() left %q in DSN params", separateHistoryFlag)
	}
}

func TestPrepareConfigSeparateHistoryFlagDisabledByDefault(t *testing.T) {
	cfg, separateHistory, err := prepareConfig("user:pass@tcp(localhost:3306)/kubernetes", nil)
	if err != nil {
		t.Fatalf("prepareConfig() error = %v", err)
	}
	if separateHistory {
		t.Fatal("prepareConfig() unexpectedly enabled separate history flag")
	}
	if cfg.DBName != "kubernetes" {
		t.Fatalf("prepareConfig() DBName = %q, want kubernetes", cfg.DBName)
	}
}
