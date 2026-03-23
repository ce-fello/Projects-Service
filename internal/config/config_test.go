package config

import "testing"

func TestLoadRequiresJWTSecret(t *testing.T) {
	t.Setenv("ENABLE_DEMO_SEED", "")
	t.Setenv("JWT_SECRET", "")
	t.Setenv("DATABASE_URL", "postgres://localhost/test")

	_, err := Load()
	if err == nil {
		t.Fatalf("Load() error = nil, want error")
	}
}

func TestLoadParsesEnableDemoSeed(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://localhost/test")
	t.Setenv("JWT_SECRET", "secret")
	t.Setenv("ENABLE_DEMO_SEED", "true")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if !cfg.EnableDemoSeed {
		t.Fatalf("Load() EnableDemoSeed = false, want true")
	}
}

func TestLoadRejectsInvalidEnableDemoSeed(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://localhost/test")
	t.Setenv("JWT_SECRET", "secret")
	t.Setenv("ENABLE_DEMO_SEED", "not-bool")

	_, err := Load()
	if err == nil {
		t.Fatalf("Load() error = nil, want error")
	}
}
