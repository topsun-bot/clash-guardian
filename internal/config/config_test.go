package config

import (
	"encoding/json"
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

func TestOldConfigGetsCountryPriorityDefaults(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	cfg := Default()
	cfg.Group = "Proxy"
	data, err := json.Marshal(map[string]any{
		"controller": cfg.Controller,
		"group":      cfg.Group,
		"checks":     cfg.Checks,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	loaded, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"日本", "香港", "美国"}
	if len(loaded.CountryPriority) != len(want) {
		t.Fatalf("国家默认顺序错误: %#v", loaded.CountryPriority)
	}
	for i := range want {
		if loaded.CountryPriority[i] != want[i] {
			t.Fatalf("国家默认顺序错误: %#v", loaded.CountryPriority)
		}
	}
	if loaded.CountryFallback != "any" {
		t.Fatalf("兜底策略=%q，期望 any", loaded.CountryFallback)
	}
}

func TestValidateCountrySettings(t *testing.T) {
	cfg := Default()
	cfg.Group = "Proxy"
	cfg.CountryPriority = []string{"日本", "日本"}
	if err := cfg.Validate(); err == nil {
		t.Fatal("重复国家应校验失败")
	}
	cfg.CountryPriority = []string{"日本"}
	cfg.CountryFallback = "invalid"
	if err := cfg.Validate(); err == nil {
		t.Fatal("未知兜底策略应校验失败")
	}
}
