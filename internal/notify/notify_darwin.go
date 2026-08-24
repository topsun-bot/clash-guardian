//go:build darwin

package notify

import (
	"context"
	"os/exec"
)

func platformNotify(ctx context.Context, title, message string) error {
	script := `on run argv
display notification (item 2 of argv) with title (item 1 of argv)
end run`
	return exec.CommandContext(ctx, "osascript", "-e", script, title, message).Run()
}
