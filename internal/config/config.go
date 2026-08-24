package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
)

const appName = "clash-guardian"

type Config struct {
	Controller              string   `json:"controller,omitempty"`
	UnixSocket              string   `json:"unix_socket,omitempty"`
	Secret                  string   `json:"secret,omitempty"`
	Group                   string   `json:"group"`
	Checks                  []string `json:"checks"`
	CheckIntervalSeconds    int      `json:"check_interval_seconds"`
	RequestTimeoutSeconds   int      `json:"request_timeout_seconds"`
	ConsecutiveFailures     int      `json:"consecutive_failures"`
	CandidateConcurrency    int      `json:"candidate_concurrency"`
	MinimumSuccessfulChecks int      `json:"minimum_successful_checks"`
	SwitchSettleSeconds     int      `json:"switch_settle_seconds"`
	ProviderRetrySeconds    int      `json:"provider_retry_seconds"`
	CountryPriority         []string `json:"country_priority"`
	CountryFallback         string   `json:"country_fallback"`
	Notify                  bool     `json:"notify"`
	RefreshCommand          []string `json:"refresh_command,omitempty"`
}

func Default() Config {
	return Config{
		Controller:              "http://127.0.0.1:9090",
		Checks:                  []string{"https://cp.cloudflare.com/generate_204", "https://www.gstatic.com/generate_204"},
		CheckIntervalSeconds:    15,
		RequestTimeoutSeconds:   8,
		ConsecutiveFailures:     3,
		CandidateConcurrency:    8,
		MinimumSuccessfulChecks: 1,
		SwitchSettleSeconds:     1,
		ProviderRetrySeconds:    30,
		CountryPriority:         []string{"日本", "香港", "美国"},
		CountryFallback:         "any",
		Notify:                  true,
	}
}

func (c *Config) ApplyDefaults() {
	d := Default()
	if c.Controller == "" && c.UnixSocket == "" {
		c.Controller = d.Controller
	}
	if len(c.Checks) == 0 {
		c.Checks = d.Checks
	}
	if c.CheckIntervalSeconds == 0 {
		c.CheckIntervalSeconds = d.CheckIntervalSeconds
	}
	if c.RequestTimeoutSeconds == 0 {
		c.RequestTimeoutSeconds = d.RequestTimeoutSeconds
	}
	if c.ConsecutiveFailures == 0 {
		c.ConsecutiveFailures = d.ConsecutiveFailures
	}
	if c.CandidateConcurrency == 0 {
		c.CandidateConcurrency = d.CandidateConcurrency
	}
	if c.MinimumSuccessfulChecks == 0 {
		c.MinimumSuccessfulChecks = d.MinimumSuccessfulChecks
	}
	if c.SwitchSettleSeconds == 0 {
		c.SwitchSettleSeconds = d.SwitchSettleSeconds
	}
	if c.ProviderRetrySeconds == 0 {
		c.ProviderRetrySeconds = d.ProviderRetrySeconds
	}
	if c.CountryPriority == nil {
		c.CountryPriority = append([]string(nil), d.CountryPriority...)
	}
	if c.CountryFallback == "" {
		c.CountryFallback = d.CountryFallback
	}
}

func (c Config) Validate() error {
	if c.Controller == "" && c.UnixSocket == "" {
		return errors.New("controller 或 unix_socket 至少需要配置一个")
	}
	if c.Controller != "" {
		u, err := url.Parse(c.Controller)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
			return fmt.Errorf("controller 地址无效: %q", c.Controller)
		}
	}
	if c.Group == "" {
		return errors.New("group 不能为空，请先运行 init")
	}
	if len(c.Checks) < 2 {
		return errors.New("至少需要两个 HTTPS 检测地址")
	}
	for _, raw := range c.Checks {
		u, err := url.Parse(raw)
		if err != nil || u.Scheme != "https" || u.Host == "" {
			return fmt.Errorf("检测地址必须是有效 HTTPS URL: %q", raw)
		}
	}
	if c.CheckIntervalSeconds < 1 || c.RequestTimeoutSeconds < 1 || c.ConsecutiveFailures < 1 {
		return errors.New("检测间隔、超时和连续失败次数必须大于 0")
	}
	if c.CandidateConcurrency < 1 || c.CandidateConcurrency > 64 {
		return errors.New("candidate_concurrency 必须在 1 到 64 之间")
	}
	if c.MinimumSuccessfulChecks < 1 || c.MinimumSuccessfulChecks > len(c.Checks) {
		return errors.New("minimum_successful_checks 超出检测地址数量")
	}
	if c.SwitchSettleSeconds < 0 || c.ProviderRetrySeconds < 1 {
		return errors.New("切换等待不能为负数，重试间隔必须大于 0")
	}
	seenCountries := map[string]bool{}
	for _, country := range c.CountryPriority {
		country = strings.TrimSpace(country)
		if country == "" {
			return errors.New("country_priority 不能包含空值")
		}
		key := strings.ToLower(country)
		if seenCountries[key] {
			return fmt.Errorf("country_priority 包含重复项: %q", country)
		}
		seenCountries[key] = true
	}
	if c.CountryFallback != "any" && c.CountryFallback != "none" {
		return errors.New("country_fallback 只能是 any 或 none")
	}
	if len(c.RefreshCommand) == 1 && c.RefreshCommand[0] == "" {
		return errors.New("refresh_command 不能是空命令")
	}
	return nil
}

func DefaultPath() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, appName, "config.json"), nil
}

func StatePath(configPath string) string {
	return filepath.Join(filepath.Dir(configPath), "state.json")
}

func Load(path string) (Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Config{}, err
	}
	var c Config
	if err := json.Unmarshal(data, &c); err != nil {
		return Config{}, fmt.Errorf("解析配置: %w", err)
	}
	c.ApplyDefaults()
	if err := c.Validate(); err != nil {
		return Config{}, err
	}
	return c, nil
}

func Save(path string, c Config) error {
	c.ApplyDefaults()
	if err := c.Validate(); err != nil {
		return err
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	_ = os.Chmod(dir, 0o700)
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return atomicWrite(path, data, 0o600)
}

func atomicWrite(path string, data []byte, mode os.FileMode) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), ".tmp-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if err := tmp.Chmod(mode); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpName, path); err != nil {
		return err
	}
	return os.Chmod(path, mode)
}
