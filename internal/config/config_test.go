package config

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestSaveLoadAndPermissions(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "config.json")
	cfg := Default()
	cfg.Group = "Proxy"
	cfg.Secret = "private-secret"
	if err := Save(path, cfg); err != nil {
		t.Fatal(err)
	}
	loaded, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Secret != cfg.Secret || loaded.Group != cfg.Group {
		t.Fatalf("配置往返不一致: %#v", loaded)
	}
	if runtime.GOOS != "windows" {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if got := info.Mode().Perm(); got != 0o600 {
			t.Fatalf("配置权限=%o，期望 600", got)
		}
	}
}

func TestValidateRequiresTwoHTTPSChecks(t *testing.T) {
	cfg := Default()
	cfg.Group = "Proxy"
	cfg.Checks = []string{"http://example.com"}
	if err := cfg.Validate(); err == nil {
		t.Fatal("期望校验失败")
	}
}
