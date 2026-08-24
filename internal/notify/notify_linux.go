//go:build linux

package notify

import (
	"context"
	"os/exec"
)

func platformNotify(ctx context.Context, title, message string) error {
	return exec.CommandContext(ctx, "notify-send", "--app-name=clash-guardian", title, message).Run()
}
