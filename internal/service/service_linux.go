//go:build linux

package service

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

func Install(binaryPath, configPath string) (string, error) {
	configDir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	unitDir := filepath.Join(configDir, "systemd", "user")
	if err := os.MkdirAll(unitDir, 0o755); err != nil {
		return "", err
	}
	path := filepath.Join(unitDir, "clash-guardian.service")
	unit := `[Unit]
Description=Clash Guardian network watchdog
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
ExecStart=` + systemdQuote(binaryPath) + ` run --config ` + systemdQuote(configPath) + `
Restart=always
RestartSec=3

[Install]
WantedBy=default.target
`
	if err := os.WriteFile(path, []byte(unit), 0o644); err != nil {
		return "", err
	}
	if output, err := exec.Command("systemctl", "--user", "daemon-reload").CombinedOutput(); err != nil {
		return path, fmt.Errorf("systemctl daemon-reload: %w (%s)", err, bytes.TrimSpace(output))
	}
	if output, err := exec.Command("systemctl", "--user", "enable", "--now", "clash-guardian.service").CombinedOutput(); err != nil {
		return path, fmt.Errorf("systemctl enable: %w (%s)", err, bytes.TrimSpace(output))
	}
	return path, nil
}

func Uninstall() (string, error) {
	configDir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	path := filepath.Join(configDir, "systemd", "user", "clash-guardian.service")
	_ = exec.Command("systemctl", "--user", "disable", "--now", "clash-guardian.service").Run()
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return path, err
	}
	_ = exec.Command("systemctl", "--user", "daemon-reload").Run()
	return path, nil
}

func systemdQuote(value string) string {
	return `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(value) + `"`
}
