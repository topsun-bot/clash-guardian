//go:build windows

package service

import (
	"bytes"
	"fmt"
	"os/exec"
)

func Install(binaryPath, configPath string) (string, error) {
	taskRun := quoteWindows(binaryPath) + " run --config " + quoteWindows(configPath)
	args := []string{"/Create", "/TN", "Clash Guardian", "/SC", "ONLOGON", "/TR", taskRun, "/RL", "LIMITED", "/F"}
	if output, err := exec.Command("schtasks.exe", args...).CombinedOutput(); err != nil {
		return "Task Scheduler: Clash Guardian", fmt.Errorf("schtasks create: %w (%s)", err, bytes.TrimSpace(output))
	}
	_ = exec.Command("schtasks.exe", "/Run", "/TN", "Clash Guardian").Run()
	return "Task Scheduler: Clash Guardian", nil
}

func Uninstall() (string, error) {
	output, err := exec.Command("schtasks.exe", "/Delete", "/TN", "Clash Guardian", "/F").CombinedOutput()
	if err != nil {
		return "Task Scheduler: Clash Guardian", fmt.Errorf("schtasks delete: %w (%s)", err, bytes.TrimSpace(output))
	}
	return "Task Scheduler: Clash Guardian", nil
}

func quoteWindows(value string) string {
	return `"` + value + `"`
}
