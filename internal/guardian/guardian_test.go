package guardian

import (
	"context"
	"errors"
	"io"
	"log"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/clash-guardian/clash-guardian/internal/clash"
	"github.com/clash-guardian/clash-guardian/internal/config"
	"github.com/clash-guardian/clash-guardian/internal/state"
)

type fakeAPI struct {
	mu          sync.Mutex
	proxies     map[string]clash.Proxy
	delays      map[string]map[string]int
	delaySeries map[string]map[string][]int
	providers   map[string]clash.Provider
	selectCalls []string
	updates     []string
}

func (f *fakeAPI) Version(context.Context) (clash.Version, error) {
	return clash.Version{Meta: true, Version: "test"}, nil
}

func (f *fakeAPI) Proxies(context.Context) (map[string]clash.Proxy, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	copyMap := make(map[string]clash.Proxy, len(f.proxies))
	for key, value := range f.proxies {
		copyMap[key] = value
	}
	return copyMap, nil
}

func (f *fakeAPI) Delay(_ context.Context, name, checkURL string, _ time.Duration) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if series := f.delaySeries[name][checkURL]; len(series) > 0 {
		delay := series[0]
		f.delaySeries[name][checkURL] = series[1:]
		if delay > 0 {
			return delay, nil
		}
		return 0, errors.New("timeout")
	}
	if delay := f.delays[name][checkURL]; delay > 0 {
		return delay, nil
	}
	return 0, errors.New("timeout")
}

func (f *fakeAPI) Select(_ context.Context, group, name string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	value := f.proxies[group]
	value.Now = name
	f.proxies[group] = value
	f.selectCalls = append(f.selectCalls, name)
	return nil
}

func (f *fakeAPI) Providers(context.Context) (map[string]clash.Provider, error) {
	return f.providers, nil
}

func (f *fakeAPI) UpdateProvider(_ context.Context, name string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.updates = append(f.updates, name)
	return nil
}

type fakeNotifier struct {
	mu       sync.Mutex
	messages []string
}

func (f *fakeNotifier) Send(_ context.Context, title, message string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.messages = append(f.messages, title+": "+message)
}

func testConfig() config.Config {
	return config.Config{
		Controller: "http://127.0.0.1:9090", Group: "Proxy",
		Checks:               []string{"https://one.example", "https://two.example"},
		CheckIntervalSeconds: 15, RequestTimeoutSeconds: 1,
		ConsecutiveFailures: 3, CandidateConcurrency: 2,
		MinimumSuccessfulChecks: 1, ProviderRetrySeconds: 30,
	}
}

func TestThreeFailuresSwitchesToMostReliableCandidate(t *testing.T) {
	api := &fakeAPI{
		proxies: map[string]clash.Proxy{
			"Proxy": {Name: "Proxy", Type: "Selector", Now: "bad", All: []string{"bad", "fast-partial", "reliable"}},
			"bad":   {Name: "bad", Type: "Trojan"}, "fast-partial": {Name: "fast-partial", Type: "Trojan"},
			"reliable": {Name: "reliable", Type: "Trojan"},
		},
		delays: map[string]map[string]int{
			"bad":          {},
			"fast-partial": {"https://one.example": 30},
			"reliable":     {"https://one.example": 100, "https://two.example": 120},
		},
	}
	n := &fakeNotifier{}
	runner := New(testConfig(), api, n, log.New(io.Discard, "", 0), state.Store{Path: filepath.Join(t.TempDir(), "state.json")})
	for i := 0; i < 3; i++ {
		if err := runner.Step(context.Background()); err != nil {
			t.Fatal(err)
		}
		if i < 2 && len(api.selectCalls) != 0 {
			t.Fatalf("连续失败不足三次时不应切换，calls=%#v", api.selectCalls)
		}
	}
	if got := api.proxies["Proxy"].Now; got != "reliable" {
		t.Fatalf("切换到 %q，期望 reliable", got)
	}
	if got := runner.State(); got.Outage || got.ConsecutiveFailures != 0 || got.CurrentNode != "reliable" {
		t.Fatalf("切换后的状态错误: %#v", got)
	}
	if len(n.messages) < 2 {
		t.Fatalf("应发送故障和恢复通知，得到 %#v", n.messages)
	}
}

func TestFailedPostSwitchVerificationContinuesToNextCandidate(t *testing.T) {
	cfg := testConfig()
	cfg.ConsecutiveFailures = 1
	api := &fakeAPI{
		proxies: map[string]clash.Proxy{
			"Proxy": {Name: "Proxy", Type: "Selector", Now: "bad", All: []string{"bad", "flaky", "stable"}},
			"bad":   {Name: "bad", Type: "Trojan"}, "flaky": {Name: "flaky", Type: "Trojan"},
			"stable": {Name: "stable", Type: "Trojan"},
		},
		delays: map[string]map[string]int{"bad": {}},
		delaySeries: map[string]map[string][]int{
			"flaky": {
				"https://one.example": {10, 0},
				"https://two.example": {10, 0},
			},
			"stable": {
				"https://one.example": {50, 50},
				"https://two.example": {50, 50},
			},
		},
	}
	runner := New(cfg, api, &fakeNotifier{}, log.New(io.Discard, "", 0), state.Store{Path: filepath.Join(t.TempDir(), "state.json")})
	if err := runner.Step(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := api.proxies["Proxy"].Now; got != "stable" {
		t.Fatalf("复检失败后应继续切换到 stable，得到 %q", got)
	}
	if len(api.selectCalls) != 2 || api.selectCalls[0] != "flaky" || api.selectCalls[1] != "stable" {
		t.Fatalf("切换顺序错误: %#v", api.selectCalls)
	}
}

func TestAllCandidatesFailRefreshesHTTPProviders(t *testing.T) {
	cfg := testConfig()
	cfg.ConsecutiveFailures = 1
	api := &fakeAPI{
		proxies: map[string]clash.Proxy{
			"Proxy": {Name: "Proxy", Type: "Selector", Now: "bad", All: []string{"bad", "also-bad"}},
			"bad":   {Name: "bad", Type: "Trojan"}, "also-bad": {Name: "also-bad", Type: "Trojan"},
		},
		delays: map[string]map[string]int{"bad": {}, "also-bad": {}},
		providers: map[string]clash.Provider{
			"subscription": {Name: "subscription", VehicleType: "HTTP"},
			"inline":       {Name: "inline", VehicleType: "Compatible"},
		},
	}
	runner := New(cfg, api, &fakeNotifier{}, log.New(io.Discard, "", 0), state.Store{Path: filepath.Join(t.TempDir(), "state.json")})
	if err := runner.Step(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(api.updates) != 1 || api.updates[0] != "subscription" {
		t.Fatalf("刷新调用错误: %#v", api.updates)
	}
	if !runner.State().Outage || runner.State().NextRetry.IsZero() {
		t.Fatalf("故障状态错误: %#v", runner.State())
	}
}

func TestResolveEffectiveNestedGroup(t *testing.T) {
	proxies := map[string]clash.Proxy{
		"Main":   {Type: "Selector", Now: "Auto"},
		"Auto":   {Type: "Fallback", Now: "node-a"},
		"node-a": {Type: "Trojan"},
	}
	got, err := ResolveEffective(proxies, "Main")
	if err != nil || got != "node-a" {
		t.Fatalf("ResolveEffective() = %q, %v", got, err)
	}
}

func TestFallbackGroupIsNotPinned(t *testing.T) {
	cfg := testConfig()
	cfg.Group = "Auto"
	api := &fakeAPI{
		proxies: map[string]clash.Proxy{
			"Auto": {Name: "Auto", Type: "Fallback", Now: "bad", All: []string{"bad", "good"}},
			"bad":  {Name: "bad", Type: "Trojan"},
			"good": {Name: "good", Type: "Trojan"},
		},
		delays: map[string]map[string]int{
			"bad": {}, "good": {"https://one.example": 10, "https://two.example": 10},
		},
	}
	runner := New(cfg, api, &fakeNotifier{}, log.New(io.Discard, "", 0), state.Store{Path: filepath.Join(t.TempDir(), "state.json")})
	if _, err := runner.Failover(context.Background()); err == nil || !strings.Contains(err.Error(), "自动管理") {
		t.Fatalf("Fallback 组应交给 Clash 自动管理，得到 %v", err)
	}
	if len(api.selectCalls) != 0 {
		t.Fatalf("不应固定 Fallback 组: %#v", api.selectCalls)
	}
}
