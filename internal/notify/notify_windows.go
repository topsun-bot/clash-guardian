//go:build windows

package notify

import (
	"context"
	"os/exec"
	"strings"
)

func platformNotify(ctx context.Context, title, message string) error {
	escape := func(value string) string { return strings.ReplaceAll(value, "'", "''") }
	script := `Add-Type -AssemblyName System.Windows.Forms; ` +
		`$n=New-Object System.Windows.Forms.NotifyIcon; ` +
		`$n.Icon=[System.Drawing.SystemIcons]::Information; ` +
		`$n.BalloonTipTitle='` + escape(title) + `'; ` +
		`$n.BalloonTipText='` + escape(message) + `'; ` +
		`$n.Visible=$true; $n.ShowBalloonTip(5000); Start-Sleep -Seconds 6; $n.Dispose()`
	return exec.CommandContext(ctx, "powershell.exe", "-NoProfile", "-NonInteractive", "-WindowStyle", "Hidden", "-Command", script).Run()
}
