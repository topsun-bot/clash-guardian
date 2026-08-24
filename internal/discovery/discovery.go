package discovery

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/clash-guardian/clash-guardian/internal/clash"
)

type Endpoint struct {
	Controller string
	UnixSocket string
	NeedsAuth  bool
	Version    string
}

func Detect(ctx context.Context, secret string) ([]Endpoint, error) {
	var endpoints []Endpoint
	seen := map[string]bool{}
	add := func(endpoint Endpoint) {
		key := endpoint.Controller + "|" + endpoint.UnixSocket
		if !seen[key] {
			seen[key] = true
			endpoints = append(endpoints, endpoint)
		}
	}

	if socket := os.Getenv("CLASH_UNIX_SOCKET"); socket != "" {
		probe(ctx, Endpoint{UnixSocket: socket}, secret, add)
	}
	for _, pattern := range []string{
		"/tmp/verge/verge-mihomo.sock",
		"/tmp/mihomo.sock",
		"/tmp/clash*.sock",
		"/tmp/*/verge-mihomo.sock",
	} {
		matches, _ := filepath.Glob(pattern)
		for _, socket := range matches {
			if info, err := os.Stat(socket); err == nil && info.Mode()&os.ModeSocket != 0 {
				probe(ctx, Endpoint{UnixSocket: socket}, secret, add)
			}
		}
	}

	controllers := []string{
		os.Getenv("CLASH_CONTROLLER"),
		"http://127.0.0.1:9090",
		"http://127.0.0.1:9097",
		"http://127.0.0.1:9093",
	}
	for _, controller := range controllers {
		if controller != "" {
			probe(ctx, Endpoint{Controller: controller}, secret, add)
		}
	}
	if len(endpoints) == 0 {
		return nil, fmt.Errorf("没有发现可访问的 Clash/Mihomo 控制接口")
	}
	return endpoints, nil
}

func probe(ctx context.Context, endpoint Endpoint, secret string, add func(Endpoint)) {
	client, err := clash.NewClient(endpoint.Controller, endpoint.UnixSocket, secret, 1200*time.Millisecond)
	if err != nil {
		return
	}
	probeCtx, cancel := context.WithTimeout(ctx, 1500*time.Millisecond)
	defer cancel()
	version, err := client.Version(probeCtx)
	if err == nil {
		endpoint.Version = version.Version
		add(endpoint)
		return
	}
	if clash.IsUnauthorized(err) {
		endpoint.NeedsAuth = true
		add(endpoint)
	}
}

type GroupCandidate struct {
	Name    string
	Current string
	Members int
	Score   int
}

func RecommendGroups(proxies map[string]clash.Proxy, mode string) []GroupCandidate {
	candidates := make([]GroupCandidate, 0)
	for name, proxy := range proxies {
		if !strings.EqualFold(proxy.Type, "Selector") || len(proxy.All) < 2 {
			continue
		}
		lower := strings.ToLower(name)
		score := len(proxy.All)
		switch lower {
		case "proxy", "global", "节点选择", "代理线路", "代理":
			score += 1000
		}
		for _, token := range []string{"代理线路", "节点选择", "proxy", "global", "select", "代理"} {
			if strings.Contains(lower, token) {
				score += 300
			}
		}
		for _, token := range []string{"direct", "不代理", "苹果", "apple", "ai", "官网", "广告", "reject"} {
			if strings.Contains(lower, token) {
				score -= 800
			}
		}
		if lower == "global" {
			if strings.EqualFold(mode, "global") {
				score += 2000
			} else if strings.EqualFold(mode, "rule") {
				score -= 1000
			}
		}
		candidates = append(candidates, GroupCandidate{Name: name, Current: proxy.Now, Members: len(proxy.All), Score: score})
	}
	sort.SliceStable(candidates, func(i, j int) bool {
		if candidates[i].Score != candidates[j].Score {
			return candidates[i].Score > candidates[j].Score
		}
		return candidates[i].Name < candidates[j].Name
	})
	return candidates
}
