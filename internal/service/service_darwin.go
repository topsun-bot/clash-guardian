//go:build darwin

package service

import (
	"bytes"
	"fmt"
	"html"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
)

func Install(binaryPath, configPath string) (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	launchDir := filepath.Join(home, "Library", "LaunchAgents")
	logDir := filepath.Join(home, "Library", "Logs")
	if err := os.MkdirAll(launchDir, 0o755); err != nil {
		return "", err
	}
	if err := os.MkdirAll(logDir, 0o755); err != nil {
		return "", err
	}
	path := filepath.Join(launchDir, Label+".plist")
	data := []byte(renderPlist(
		binaryPath,
		configPath,
		filepath.Join(logDir, "clash-guardian.log"),
		filepath.Join(logDir, "clash-guardian.error.log"),
	))
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return "", err
	}
	domain := "gui/" + strconv.Itoa(os.Getuid())
	_ = exec.Command("launchctl", "bootout", domain+"/"+Label).Run()
	if output, err := exec.Command("launchctl", "bootstrap", domain, path).CombinedOutput(); err != nil {
		return path, fmt.Errorf("launchctl bootstrap: %w (%s)", err, bytes.TrimSpace(output))
	}
	_ = exec.Command("launchctl", "kickstart", "-k", domain+"/"+Label).Run()
	return path, nil
}

func renderPlist(binaryPath, configPath, stdoutPath, stderrPath string) string {
	escape := html.EscapeString
	return `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>Label</key>
  <string>` + Label + `</string>
  <key>ProgramArguments</key>
  <array>
    <string>` + escape(binaryPath) + `</string>
    <string>run</string>
    <string>--config</string>
    <string>` + escape(configPath) + `</string>
  </array>
  <key>RunAtLoad</key>
  <true/>
  <key>KeepAlive</key>
  <true/>
  <key>StandardOutPath</key>
  <string>` + escape(stdoutPath) + `</string>
  <key>StandardErrorPath</key>
  <string>` + escape(stderrPath) + `</string>
</dict>
</plist>
`
}

func Uninstall() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	path := filepath.Join(home, "Library", "LaunchAgents", Label+".plist")
	domain := "gui/" + strconv.Itoa(os.Getuid())
	_ = exec.Command("launchctl", "bootout", domain+"/"+Label).Run()
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return path, err
	}
	return path, nil
}
