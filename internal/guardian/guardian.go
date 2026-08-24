package guardian

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os/exec"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/clash-guardian/clash-guardian/internal/clash"
	"github.com/clash-guardian/clash-guardian/internal/config"
	"github.com/clash-guardian/clash-guardian/internal/notify"
	"github.com/clash-guardian/clash-guardian/internal/state"
)

var ErrNoUsableCandidate = errors.New("没有找到可用的候选节点")

type ProbeResult struct {
	URL   string
	Delay int
	Err   error
}

type HealthResult struct {
	Group      string
	GroupType  string
	Selected   string
	Effective  string
	Healthy    bool
	Successful int
	Probes     []ProbeResult
}

type CandidateResult struct {
	Name       string
	Healthy    bool
	Successful int
	Score      int
	Probes     []ProbeResult
}

type SwitchResult struct {
	From string
	To   string
}

type Runner struct {
	cfg       config.Config
	api       clash.API
	notifier  notify.Notifier
	logger    *log.Logger
	state     state.State
	stateFile state.Store
	now       func() time.Time
}

func New(cfg config.Config, api clash.API, notifier notify.Notifier, logger *log.Logger, stateFile state.Store) *Runner {
	if logger == nil {
		logger = log.Default()
	}
	value, err := stateFile.Load()
	if err != nil {
		logger.Printf("读取运行状态失败，将从空状态启动: %v", err)
	}
	return &Runner{
		cfg: cfg, api: api, notifier: notifier, logger: logger,
		state: value, stateFile: stateFile, now: time.Now,
	}
}

func (r *Runner) State() state.State {
	return r.state
}

func (r *Runner) Run(ctx context.Context) error {
	r.logger.Printf("等待 Clash/Mihomo 控制接口，策略组=%q", r.cfg.Group)
	if err := r.waitForAPI(ctx); err != nil {
		return err
	}
	ticker := time.NewTicker(time.Duration(r.cfg.CheckIntervalSeconds) * time.Second)
	defer ticker.Stop()

	for {
		if err := r.Step(ctx); err != nil {
			r.logger.Printf("检测周期失败: %v", err)
		}
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
		}
	}
}

func (r *Runner) waitForAPI(ctx context.Context) error {
	lastMessage := ""
	for {
		probeCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
		version, err := r.api.Version(probeCtx)
		cancel()
		if err == nil {
			r.logger.Printf("Clash/Mihomo 已就绪，内核版本=%s", version.Version)
			return nil
		}
		message := err.Error()
		if message != lastMessage {
			r.logger.Printf("控制接口尚未就绪: %v", err)
			lastMessage = message
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(3 * time.Second):
		}
	}
}

func (r *Runner) Step(ctx context.Context) error {
	result, err := r.CheckCurrent(ctx)
	if err != nil {
		return err
	}
	now := r.now()
	r.state.LastCheck = now
	r.state.CurrentNode = result.Effective

	if result.Healthy {
		wasOutage := r.state.Outage
		r.state.ConsecutiveFailures = 0
		r.state.Outage = false
		r.state.NextRetry = time.Time{}
		r.state.LastSuccess = now
		r.state.LastMessage = fmt.Sprintf("线路正常：%s", result.Effective)
		if wasOutage {
			r.notifier.Send(ctx, "Clash Guardian", fmt.Sprintf("网络已恢复，当前节点：%s", result.Effective))
			r.logger.Printf("网络已恢复，当前节点=%q", result.Effective)
		}
		return r.saveState()
	}

	r.state.ConsecutiveFailures++
	r.state.LastMessage = fmt.Sprintf("线路检测失败（%d/%d）：%s", r.state.ConsecutiveFailures, r.cfg.ConsecutiveFailures, result.Effective)
	r.logger.Printf("线路检测失败 %d/%d，当前节点=%q", r.state.ConsecutiveFailures, r.cfg.ConsecutiveFailures, result.Effective)
	if r.state.ConsecutiveFailures < r.cfg.ConsecutiveFailures {
		return r.saveState()
	}

	firstOutage := !r.state.Outage
	r.state.Outage = true
	if firstOutage {
		r.notifier.Send(ctx, "Clash Guardian", fmt.Sprintf("检测到线路故障，正在为 %s 选择备用节点", r.cfg.Group))
	}
	if !r.state.NextRetry.IsZero() && now.Before(r.state.NextRetry) {
		return r.saveState()
	}

	switched, err := r.Failover(ctx)
	if err == nil {
		r.state.ConsecutiveFailures = 0
		r.state.Outage = false
		r.state.CurrentNode = switched.To
		r.state.LastSwitch = now
		r.state.LastSuccess = now
		r.state.NextRetry = time.Time{}
		r.state.LastMessage = fmt.Sprintf("已从 %s 切换到 %s，网络恢复", switched.From, switched.To)
		r.notifier.Send(ctx, "Clash Guardian", r.state.LastMessage)
		r.logger.Printf("%s", r.state.LastMessage)
		return r.saveState()
	}

	r.logger.Printf("自动切换未成功: %v", err)
	refreshCount, refreshErr := r.Refresh(ctx)
	if refreshErr != nil {
		r.logger.Printf("刷新订阅/代理提供者失败: %v", refreshErr)
		r.state.LastMessage = fmt.Sprintf("所有候选节点均不可用，刷新失败：%v", refreshErr)
	} else {
		r.logger.Printf("已触发 %d 个刷新动作，将稍后重试", refreshCount)
		r.state.LastMessage = fmt.Sprintf("所有候选节点均不可用，已触发 %d 个刷新动作", refreshCount)
	}
	r.state.NextRetry = now.Add(time.Duration(r.cfg.ProviderRetrySeconds) * time.Second)
	if firstOutage {
		r.notifier.Send(ctx, "Clash Guardian", r.state.LastMessage)
	}
	return r.saveState()
}

func (r *Runner) CheckCurrent(ctx context.Context) (HealthResult, error) {
	proxies, err := r.api.Proxies(ctx)
	if err != nil {
		return HealthResult{}, err
	}
	group, ok := proxies[r.cfg.Group]
	if !ok {
		return HealthResult{}, fmt.Errorf("配置的策略组 %q 不存在，请重新运行 init", r.cfg.Group)
	}
	effective, err := ResolveEffective(proxies, r.cfg.Group)
	if err != nil {
		return HealthResult{}, err
	}
	probes := r.probe(ctx, effective)
	successes := countSuccessful(probes)
	return HealthResult{
		Group: r.cfg.Group, GroupType: group.Type, Selected: group.Now, Effective: effective,
		Healthy: successes >= r.cfg.MinimumSuccessfulChecks, Successful: successes, Probes: probes,
	}, nil
}

func ResolveEffective(proxies map[string]clash.Proxy, start string) (string, error) {
	current := start
	seen := map[string]bool{}
	for depth := 0; depth < 16; depth++ {
		if seen[current] {
			return "", fmt.Errorf("策略组存在循环引用: %q", current)
		}
		seen[current] = true
		proxy, ok := proxies[current]
		if !ok {
			if current == start {
				return "", fmt.Errorf("找不到策略组或节点 %q", start)
			}
			return current, nil
		}
		if proxy.Now == "" {
			return current, nil
		}
		current = proxy.Now
	}
	return "", fmt.Errorf("策略组嵌套超过安全深度: %q", start)
}

func (r *Runner) Failover(ctx context.Context) (SwitchResult, error) {
	proxies, err := r.api.Proxies(ctx)
	if err != nil {
		return SwitchResult{}, err
	}
	group, ok := proxies[r.cfg.Group]
	if !ok {
		return SwitchResult{}, fmt.Errorf("策略组 %q 不存在", r.cfg.Group)
	}
	if !strings.EqualFold(group.Type, "Selector") {
		return SwitchResult{}, fmt.Errorf("策略组 %q 类型为 %s，由 Clash 自动管理；不会强制固定节点", r.cfg.Group, group.Type)
	}
	from, err := ResolveEffective(proxies, r.cfg.Group)
	if err != nil {
		return SwitchResult{}, err
	}
	candidates := make([]string, 0, len(group.All))
	for _, name := range group.All {
		if name == group.Now || name == from || isExcluded(proxies[name]) {
			continue
		}
		candidates = append(candidates, name)
	}
	if len(candidates) == 0 {
		return SwitchResult{}, ErrNoUsableCandidate
	}

	tested := r.testCandidates(ctx, candidates)
	healthy := make([]CandidateResult, 0, len(tested))
	for _, candidate := range tested {
		if candidate.Healthy {
			healthy = append(healthy, candidate)
		}
	}
	if len(healthy) == 0 {
		return SwitchResult{}, ErrNoUsableCandidate
	}
	sort.SliceStable(healthy, func(i, j int) bool {
		if healthy[i].Successful != healthy[j].Successful {
			return healthy[i].Successful > healthy[j].Successful
		}
		if healthy[i].Score != healthy[j].Score {
			return healthy[i].Score < healthy[j].Score
		}
		return healthy[i].Name < healthy[j].Name
	})

	for _, candidate := range healthy {
		r.logger.Printf("尝试切换到候选节点=%q，成功检测=%d/%d，评分=%dms", candidate.Name, candidate.Successful, len(r.cfg.Checks), candidate.Score)
		if err := r.api.Select(ctx, r.cfg.Group, candidate.Name); err != nil {
			r.logger.Printf("切换到 %q 失败: %v", candidate.Name, err)
			continue
		}
		if r.cfg.SwitchSettleSeconds > 0 {
			select {
			case <-ctx.Done():
				return SwitchResult{}, ctx.Err()
			case <-time.After(time.Duration(r.cfg.SwitchSettleSeconds) * time.Second):
			}
		}
		updated, err := r.api.Proxies(ctx)
		if err != nil {
			r.logger.Printf("切换后读取策略组失败: %v", err)
			continue
		}
		to, err := ResolveEffective(updated, r.cfg.Group)
		if err != nil {
			r.logger.Printf("切换后解析节点失败: %v", err)
			continue
		}
		verification := r.probe(ctx, to)
		if countSuccessful(verification) >= r.cfg.MinimumSuccessfulChecks {
			return SwitchResult{From: from, To: to}, nil
		}
		r.logger.Printf("节点 %q 切换后复检失败，继续尝试下一个", to)
	}
	return SwitchResult{}, ErrNoUsableCandidate
}

func (r *Runner) Refresh(ctx context.Context) (int, error) {
	count := 0
	var messages []string
	providers, err := r.api.Providers(ctx)
	if err != nil {
		messages = append(messages, err.Error())
	} else {
		for name, provider := range providers {
			if !strings.EqualFold(provider.VehicleType, "HTTP") {
				continue
			}
			if err := r.api.UpdateProvider(ctx, name); err != nil {
				messages = append(messages, fmt.Sprintf("provider %s: %v", name, err))
				continue
			}
			count++
		}
	}
	if len(r.cfg.RefreshCommand) > 0 {
		commandCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
		defer cancel()
		cmd := exec.CommandContext(commandCtx, r.cfg.RefreshCommand[0], r.cfg.RefreshCommand[1:]...)
		if output, commandErr := cmd.CombinedOutput(); commandErr != nil {
			messages = append(messages, fmt.Sprintf("refresh_command: %v (%s)", commandErr, strings.TrimSpace(string(output))))
		} else {
			count++
		}
	}
	if count == 0 {
		if len(messages) == 0 {
			return 0, errors.New("没有可刷新的 HTTP proxy-provider；可在配置中设置 refresh_command")
		}
		return 0, errors.New(strings.Join(messages, "; "))
	}
	if len(messages) > 0 {
		return count, fmt.Errorf("部分刷新失败: %s", strings.Join(messages, "; "))
	}
	return count, nil
}

func (r *Runner) probe(ctx context.Context, target string) []ProbeResult {
	results := make([]ProbeResult, len(r.cfg.Checks))
	var wg sync.WaitGroup
	for i, checkURL := range r.cfg.Checks {
		i, checkURL := i, checkURL
		wg.Add(1)
		go func() {
			defer wg.Done()
			timeout := time.Duration(r.cfg.RequestTimeoutSeconds) * time.Second
			probeCtx, cancel := context.WithTimeout(ctx, timeout+2*time.Second)
			defer cancel()
			delay, err := r.api.Delay(probeCtx, target, checkURL, timeout)
			results[i] = ProbeResult{URL: checkURL, Delay: delay, Err: err}
		}()
	}
	wg.Wait()
	return results
}

func (r *Runner) testCandidates(ctx context.Context, names []string) []CandidateResult {
	jobs := make(chan string)
	results := make(chan CandidateResult, len(names))
	workers := r.cfg.CandidateConcurrency
	if workers > len(names) {
		workers = len(names)
	}
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for name := range jobs {
				probes := r.probe(ctx, name)
				successes := countSuccessful(probes)
				score := scoreProbes(probes, r.cfg.RequestTimeoutSeconds*1000)
				results <- CandidateResult{
					Name: name, Healthy: successes >= r.cfg.MinimumSuccessfulChecks,
					Successful: successes, Score: score, Probes: probes,
				}
			}
		}()
	}
	go func() {
		defer close(jobs)
		for _, name := range names {
			select {
			case <-ctx.Done():
				return
			case jobs <- name:
			}
		}
	}()
	wg.Wait()
	close(results)
	all := make([]CandidateResult, 0, len(names))
	for result := range results {
		all = append(all, result)
	}
	return all
}

func countSuccessful(results []ProbeResult) int {
	count := 0
	for _, result := range results {
		if result.Err == nil && result.Delay > 0 {
			count++
		}
	}
	return count
}

func scoreProbes(results []ProbeResult, failurePenalty int) int {
	if len(results) == 0 {
		return failurePenalty
	}
	total := 0
	for _, result := range results {
		if result.Err != nil || result.Delay <= 0 {
			total += failurePenalty
		} else {
			total += result.Delay
		}
	}
	return total / len(results)
}

func isExcluded(proxy clash.Proxy) bool {
	switch strings.ToLower(proxy.Type) {
	case "direct", "reject", "rejectdrop", "pass", "compatible":
		return true
	default:
		return false
	}
}

func (r *Runner) saveState() error {
	if err := r.stateFile.Save(r.state); err != nil {
		return fmt.Errorf("保存运行状态: %w", err)
	}
	return nil
}
