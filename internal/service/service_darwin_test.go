//go:build darwin

package service

import (
	"bytes"
	"os/exec"
	"strings"
	"testing"
)

func TestRenderPlistIsValid(t *testing.T) {
	data := renderPlist("/tmp/a & b/guardian", "/tmp/config.json", "/tmp/out.log", "/tmp/err.log")
	if strings.Contains(data, "a & b") || !strings.Contains(data, "a &amp; b") {
		t.Fatalf("路径没有进行 XML 转义: %s", data)
	}
	cmd := exec.Command("plutil", "-lint", "-")
	cmd.Stdin = bytes.NewBufferString(data)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("plist 无效: %v (%s)", err, output)
	}
}
