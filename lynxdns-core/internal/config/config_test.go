package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()

	if cfg.Server.ListenPort != 5334 {
		t.Errorf("expected default port 5334, got %d", cfg.Server.ListenPort)
	}
	if cfg.Server.ListenAddr != "0.0.0.0" {
		t.Errorf("expected default addr 0.0.0.0, got %s", cfg.Server.ListenAddr)
	}
	if cfg.API.Port != 5335 {
		t.Errorf("expected API port 5335, got %d", cfg.API.Port)
	}
	if cfg.DNS.Default != "domestic" {
		t.Errorf("expected default DNS 'domestic', got %s", cfg.DNS.Default)
	}
	if cfg.Advanced.Concurrency != 3 {
		t.Errorf("expected concurrency 3, got %d", cfg.Advanced.Concurrency)
	}
	if cfg.Advanced.Cache.Size != 4096 {
		t.Errorf("expected cache size 4096, got %d", cfg.Advanced.Cache.Size)
	}
	if !cfg.Advanced.Cache.Lazy {
		t.Error("expected lazy cache to be true")
	}
	if !cfg.Advanced.LeakProtection.Enabled {
		t.Error("expected leak protection to be enabled")
	}
	if cfg.Advanced.LeakProtection.Mode != "loose" {
		t.Errorf("expected leak protection mode 'loose', got %s", cfg.Advanced.LeakProtection.Mode)
	}
	if len(cfg.DNS.Domestic) != 3 {
		t.Errorf("expected 3 domestic DNS servers, got %d", len(cfg.DNS.Domestic))
	}
	if len(cfg.DNS.Remote) != 2 {
		t.Errorf("expected 2 remote DNS servers, got %d", len(cfg.DNS.Remote))
	}
}

func TestLoadAndSave(t *testing.T) {
	tmpDir := t.TempDir()
	cfgPath := filepath.Join(tmpDir, "config.yaml")

	mgr := NewManager(cfgPath)

	mgr.Update(map[string]interface{}{
		"server": map[string]interface{}{
			"listen_port": 5354,
		},
		"api": map[string]interface{}{
			"secret": "test-secret",
		},
	})

	if err := mgr.Save(); err != nil {
		t.Fatalf("failed to save config: %v", err)
	}

	mgr2 := NewManager(cfgPath)
	if err := mgr2.Load(); err != nil {
		t.Fatalf("failed to load config: %v", err)
	}

	loaded := mgr2.Get()
	if loaded.Server.ListenPort != 5354 {
		t.Errorf("expected port 5354, got %d", loaded.Server.ListenPort)
	}
	if loaded.API.Secret != "test-secret" {
		t.Errorf("expected secret 'test-secret', got %s", loaded.API.Secret)
	}
}

func TestLoadNonexistent(t *testing.T) {
	mgr := NewManager("/nonexistent/path/config.yaml")
	err := mgr.Load()
	if err != nil {
		t.Fatalf("loading nonexistent file should not error, got: %v", err)
	}

	cfg := mgr.Get()
	if cfg.Server.ListenPort != 5334 {
		t.Errorf("expected default port, got %d", cfg.Server.ListenPort)
	}
}

func TestUpdate(t *testing.T) {
	mgr := NewManager("")

	err := mgr.Update(map[string]interface{}{
		"server": map[string]interface{}{
			"listen_port": 5355,
		},
	})
	if err != nil {
		t.Fatalf("update failed: %v", err)
	}

	cfg := mgr.Get()
	if cfg.Server.ListenPort != 5355 {
		t.Errorf("expected port 5355 after update, got %d", cfg.Server.ListenPort)
	}
}

func TestOnChange(t *testing.T) {
	mgr := NewManager("")
	called := false
	lastCfg := (*FullConfig)(nil)

	mgr.OnChange(func(cfg *FullConfig) {
		called = true
		lastCfg = cfg
	})

	mgr.Update(map[string]interface{}{
		"server": map[string]interface{}{
			"listen_port": 5356,
		},
	})

	if !called {
		t.Error("expected OnChange callback to be called")
	}
	if lastCfg == nil {
		t.Error("expected config to be passed to callback")
	}
	if lastCfg.Server.ListenPort != 5356 {
		t.Errorf("expected port 5356 in callback, got %d", lastCfg.Server.ListenPort)
	}
}

func TestYAMLRoundTrip(t *testing.T) {
	tmpDir := t.TempDir()
	cfgPath := filepath.Join(tmpDir, "config.yaml")

	orig := NewManager(cfgPath)
	orig.Update(map[string]interface{}{
		"dns": map[string]interface{}{
			"domestic": []string{"udp://1.1.1.1:53"},
		},
		"advanced": map[string]interface{}{
			"cache": map[string]interface{}{
				"size": 8192,
			},
		},
		"routing": map[string]interface{}{
			"geosite": map[string]interface{}{
				"remote": []string{"test-category"},
			},
		},
	})
	orig.Save()

	loaded := NewManager(cfgPath)
	loaded.Load()

	cfg := loaded.Get()
	if len(cfg.DNS.Domestic) != 1 || cfg.DNS.Domestic[0] != "udp://1.1.1.1:53" {
		t.Errorf("domestic DNS mismatch: %v", cfg.DNS.Domestic)
	}
	if cfg.Advanced.Cache.Size != 8192 {
		t.Errorf("cache size mismatch: %d", cfg.Advanced.Cache.Size)
	}
	if len(cfg.Routing.Geosite.Remote) != 1 || cfg.Routing.Geosite.Remote[0] != "test-category" {
		t.Errorf("routing mismatch: %v", cfg.Routing.Geosite.Remote)
	}
}

func TestConfigFileContent(t *testing.T) {
	tmpDir := t.TempDir()
	cfgPath := filepath.Join(tmpDir, "config.yaml")

	mgr := NewManager(cfgPath)
	mgr.Update(map[string]interface{}{
		"server": map[string]interface{}{
			"enabled":     true,
			"listen_port": 5334,
		},
	})
	mgr.Save()

	data, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatalf("failed to read config file: %v", err)
	}

	content := string(data)
	if !contains(content, "listen_port: 5334") {
		t.Errorf("config file does not contain expected content")
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsSubstr(s, substr))
}

func containsSubstr(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
