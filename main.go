package main

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/clash-guardian/clash-guardian/internal/clash"
	"github.com/clash-guardian/clash-guardian/internal/config"
	"github.com/clash-guardian/clash-guardian/internal/discovery"
	"github.com/clash-guardian/clash-guardian/internal/guardian"
	"github.com/clash-guardian/clash-guardian/internal/notify"
	"github.com/clash-guardian/clash-guardian/internal/service"
	"github.com/clash-guardian/clash-guardian/internal/state"
)

const version = "0.2.0"

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "错误:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) == 0 {
		usage()
		return nil
	}
	switch args[0] {
	case "init":
		return initCommand(args[1:])
	case "run":
		return runCommand(args[1:])
	case "check", "doctor":
		return checkCommand(args[1:])
	case "status":
		return statusCommand(args[1:])
	case "install":
		return installCommand(args[1:])
	case "uninstall":
		return uninstallCommand(args[1:])
	case "version", "--version", "-v":
		fmt.Println("clash-guardian", version)
		return nil
	case "help", "--help", "-h":
		usage()
		return nil
	default:
		usage()
		return fmt.Errorf("未知命令 %q", args[0])
	}
}

func usage() {
	fmt.Print(`clash-guardian - Clash/Mihomo 网络守护程序

用法:
  clash-guardian init       自动发现 Clash 并生成配置
  clash-guardian check      只读检测当前策略组，不会切换
  clash-guardian run        前台运行守护程序
  clash-guardian status     查看当前节点和最近状态
  clash-guardian install    安装并启动开机自启
  clash-guardian uninstall  移除开机自启
  clash-guardian version    显示版本

每个命令都支持 --config 指定配置文件。
`)
}

func initCommand(args []string) error {
	defaultPath, err := config.DefaultPath()
	if err != nil {
		return err
	}
	fs := flag.NewFlagSet("init", flag.ContinueOnError)
	configPath := fs.String("config", defaultPath, "配置文件路径")
	controller := fs.String("controller", "", "Clash HTTP controller，例如 http://127.0.0.1:9090")
	unixSocket := fs.String("socket", "", "Clash Unix Socket 路径")
	secret := fs.String("secret", os.Getenv("CLASH_SECRET"), "Clash API secret")
	groupName := fs.String("group", "", "需要守护的主策略组")
	defaults := config.Default()
	countryPriority := fs.String("country-priority", strings.Join(defaults.CountryPriority, ","), "国家优先级，使用逗号分隔；留空表示不限制")
	countryFallback := fs.String("country-fallback", defaults.CountryFallback, "优先国家全部不可用时的行为：any 或 none")
	yes := fs.Bool("yes", false, "接受自动推荐，适合无人值守初始化")
	if err := fs.Parse(args); err != nil {
		return err
	}

	reader := bufio.NewReader(os.Stdin)
	if _, err := os.Stat(*configPath); err == nil && !*yes {
		answer, err := prompt(reader, fmt.Sprintf("配置文件已存在：%s，是否覆盖？[y/N] ", *configPath))
		if err != nil {
			return err
		}
		if !strings.EqualFold(answer, "y") && !strings.EqualFold(answer, "yes") {
			return errors.New("已取消初始化")
		}
	}

	ctx := context.Background()
	endpoint := discovery.Endpoint{Controller: *controller, UnixSocket: *unixSocket}
	if endpoint.Controller == "" && endpoint.UnixSocket == "" {
		fmt.Println("正在自动发现 Clash/Mihomo 控制接口……")
		endpoints, detectErr := discovery.Detect(ctx, *secret)
		if detectErr != nil {
			return fmt.Errorf("%w；可使用 --controller 或 --socket 手动指定", detectErr)
		}
		endpoint = endpoints[0]
		if len(endpoints) > 1 && !*yes {
			fmt.Println("发现多个控制接口：")
			for i, item := range endpoints {
				fmt.Printf("  %d. %s %s\n", i+1, endpointLabel(item), item.Version)
			}
			answer, err := prompt(reader, "请选择 [1]: ")
			if err != nil {
				return err
			}
			if answer != "" {
				index, parseErr := strconv.Atoi(answer)
				if parseErr != nil || index < 1 || index > len(endpoints) {
					return errors.New("控制接口序号无效")
				}
				endpoint = endpoints[index-1]
			}
		}
	}

	if endpoint.NeedsAuth && *secret == "" {
		value, err := prompt(reader, "Clash API 需要 secret，请输入：")
		if err != nil {
			return err
		}
		*secret = value
	}
	api, err := clash.NewClient(endpoint.Controller, endpoint.UnixSocket, *secret, 8*time.Second)
	if err != nil {
		return err
	}
	versionInfo, err := api.Version(ctx)
	if err != nil && clash.IsUnauthorized(err) && *secret == "" {
		value, promptErr := prompt(reader, "Clash API 需要 secret，请输入：")
		if promptErr != nil {
			return promptErr
		}
		*secret = value
		api, err = clash.NewClient(endpoint.Controller, endpoint.UnixSocket, *secret, 8*time.Second)
		if err != nil {
			return err
		}
		versionInfo, err = api.Version(ctx)
	}
	if err != nil {
		if clash.IsUnauthorized(err) {
			return errors.New("Clash API secret 不正确")
		}
		return fmt.Errorf("连接控制接口: %w", err)
	}
	fmt.Printf("已连接：%s，内核 %s\n", endpointLabel(endpoint), versionInfo.Version)

	proxies, err := api.Proxies(ctx)
	if err != nil {
		return fmt.Errorf("读取策略组: %w", err)
	}
	runtimeConfig, _ := api.RuntimeConfig(ctx)
	candidates := discovery.RecommendGroups(proxies, runtimeConfig.Mode)
	if len(candidates) == 0 {
		return errors.New("没有找到包含至少两个成员的 Selector 主策略组")
	}
	selectedGroup := *groupName
	if selectedGroup != "" {
		proxy, ok := proxies[selectedGroup]
		if !ok || !strings.EqualFold(proxy.Type, "Selector") || len(proxy.All) < 2 {
			return fmt.Errorf("%q 不是可切换的 Selector 主策略组", selectedGroup)
		}
	} else {
		fmt.Println("候选主策略组：")
		for i, candidate := range candidates {
			marker := ""
			if i == 0 {
				marker = "（推荐）"
			}
			fmt.Printf("  %d. %s%s，当前=%s，成员=%d\n", i+1, candidate.Name, marker, candidate.Current, candidate.Members)
		}
		choice := 1
		if !*yes {
			answer, err := prompt(reader, "请选择需要自动切换的主策略组 [1]: ")
			if err != nil {
				return err
			}
			if answer != "" {
				value, parseErr := strconv.Atoi(answer)
				if parseErr != nil || value < 1 || value > len(candidates) {
					return errors.New("策略组序号无效")
				}
				choice = value
			}
		}
		selectedGroup = candidates[choice-1].Name
	}

	cfg := config.Default()
	cfg.Controller = endpoint.Controller
	cfg.UnixSocket = endpoint.UnixSocket
	cfg.Secret = *secret
	cfg.Group = selectedGroup
	cfg.CountryPriority = splitCommaList(*countryPriority)
	cfg.CountryFallback = strings.ToLower(strings.TrimSpace(*countryFallback))
	if cfg.UnixSocket != "" {
		cfg.Controller = ""
	}
	if err := config.Save(*configPath, cfg); err != nil {
		return err
	}
	fmt.Printf("配置已保存：%s（仅当前用户可读）\n", *configPath)
	fmt.Printf("主策略组：%s\n", selectedGroup)
	if len(cfg.CountryPriority) > 0 {
		fmt.Printf("国家优先级：%s；兜底策略：%s\n", strings.Join(cfg.CountryPriority, " → "), cfg.CountryFallback)
	} else {
		fmt.Println("国家优先级：未限制")
	}
	if providers, providerErr := api.Providers(ctx); providerErr == nil {
		hasHTTPProvider := false
		for _, provider := range providers {
			if strings.EqualFold(provider.VehicleType, "HTTP") {
				hasHTTPProvider = true
				break
			}
		}
		if !hasHTTPProvider {
			fmt.Println("提示：当前客户端使用完整配置订阅，没有标准 HTTP proxy-provider；必要时可在配置中设置 refresh_command。")
		}
	}
	fmt.Printf("下一步可运行：clash-guardian check --config %q\n", *configPath)
	return nil
}

func runCommand(args []string) error {
	configPath, cfg, err := loadCommandConfig("run", args)
	if err != nil {
		return err
	}
	api, err := newClient(cfg)
	if err != nil {
		return err
	}
	logger := log.New(os.Stdout, "clash-guardian ", log.LstdFlags)
	n := notify.New(cfg.Notify, logger)
	runner := guardian.New(cfg, api, n, logger, state.Store{Path: config.StatePath(configPath)})
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	return runner.Run(ctx)
}

func checkCommand(args []string) error {
	configPath, cfg, err := loadCommandConfig("check", args)
	if err != nil {
		return err
	}
	api, err := newClient(cfg)
	if err != nil {
		return err
	}
	runner := guardian.New(cfg, api, notify.New(false, nil), log.New(os.Stderr, "", 0), state.Store{Path: config.StatePath(configPath)})
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(cfg.RequestTimeoutSeconds+5)*time.Second)
	defer cancel()
	result, err := runner.CheckCurrent(ctx)
	if err != nil {
		return err
	}
	fmt.Printf("策略组：%s (%s)\n", result.Group, result.GroupType)
	fmt.Printf("当前选择：%s\n", result.Selected)
	fmt.Printf("实际节点：%s\n", result.Effective)
	for _, probe := range result.Probes {
		if probe.Err != nil {
			fmt.Printf("  失败  %s  %v\n", probe.URL, probe.Err)
		} else {
			fmt.Printf("  正常  %s  %dms\n", probe.URL, probe.Delay)
		}
	}
	if !result.Healthy {
		return fmt.Errorf("当前线路检测失败（成功 %d/%d，需要至少 %d）", result.Successful, len(result.Probes), cfg.MinimumSuccessfulChecks)
	}
	fmt.Printf("结论：当前线路正常（成功 %d/%d）\n", result.Successful, len(result.Probes))
	return nil
}

func statusCommand(args []string) error {
	configPath, cfg, err := loadCommandConfig("status", args)
	if err != nil {
		return err
	}
	api, err := newClient(cfg)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	versionInfo, err := api.Version(ctx)
	if err != nil {
		return err
	}
	proxies, err := api.Proxies(ctx)
	if err != nil {
		return err
	}
	group, ok := proxies[cfg.Group]
	if !ok {
		return fmt.Errorf("策略组 %q 不存在", cfg.Group)
	}
	effective, err := guardian.ResolveEffective(proxies, cfg.Group)
	if err != nil {
		return err
	}
	value, _ := (state.Store{Path: config.StatePath(configPath)}).Load()
	fmt.Printf("内核：%s\n", versionInfo.Version)
	fmt.Printf("策略组：%s (%s)\n", cfg.Group, group.Type)
	fmt.Printf("当前选择：%s\n", group.Now)
	fmt.Printf("实际节点：%s\n", effective)
	if len(cfg.CountryPriority) > 0 {
		fmt.Printf("国家优先级：%s；兜底策略：%s\n", strings.Join(cfg.CountryPriority, " → "), cfg.CountryFallback)
	}
	if !value.LastCheck.IsZero() {
		fmt.Printf("最近检测：%s\n", value.LastCheck.Local().Format(time.RFC3339))
		fmt.Printf("连续失败：%d\n", value.ConsecutiveFailures)
		fmt.Printf("状态：%s\n", value.LastMessage)
	} else {
		fmt.Println("守护状态：尚无运行记录")
	}
	return nil
}

func installCommand(args []string) error {
	configPath, _, err := loadCommandConfig("install", args)
	if err != nil {
		return err
	}
	binaryPath, err := os.Executable()
	if err != nil {
		return err
	}
	if strings.Contains(binaryPath, string(filepath.Separator)+"go-build") {
		return errors.New("不能从 go run 安装服务，请先执行 go build 并运行生成的二进制文件")
	}
	if resolved, resolveErr := filepath.EvalSymlinks(binaryPath); resolveErr == nil {
		binaryPath = resolved
	}
	location, err := service.Install(binaryPath, configPath)
	if err != nil {
		return err
	}
	fmt.Printf("开机自启已安装并启动：%s\n", location)
	return nil
}

func uninstallCommand(args []string) error {
	fs := flag.NewFlagSet("uninstall", flag.ContinueOnError)
	_ = fs.String("config", "", "为保持命令一致性而保留")
	if err := fs.Parse(args); err != nil {
		return err
	}
	location, err := service.Uninstall()
	if err != nil {
		return err
	}
	fmt.Printf("开机自启已移除：%s\n", location)
	return nil
}

func loadCommandConfig(name string, args []string) (string, config.Config, error) {
	defaultPath, err := config.DefaultPath()
	if err != nil {
		return "", config.Config{}, err
	}
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	path := fs.String("config", defaultPath, "配置文件路径")
	if err := fs.Parse(args); err != nil {
		return "", config.Config{}, err
	}
	cfg, err := config.Load(*path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return "", config.Config{}, fmt.Errorf("配置不存在，请先运行 clash-guardian init：%s", *path)
		}
		return "", config.Config{}, err
	}
	return *path, cfg, nil
}

func newClient(cfg config.Config) (*clash.Client, error) {
	return clash.NewClient(cfg.Controller, cfg.UnixSocket, cfg.Secret, time.Duration(cfg.RequestTimeoutSeconds)*time.Second)
}

func prompt(reader *bufio.Reader, label string) (string, error) {
	fmt.Print(label)
	value, err := reader.ReadString('\n')
	if err != nil && len(value) == 0 {
		return "", err
	}
	return strings.TrimSpace(value), nil
}

func endpointLabel(endpoint discovery.Endpoint) string {
	if endpoint.UnixSocket != "" {
		return "unix://" + endpoint.UnixSocket
	}
	return endpoint.Controller
}

func splitCommaList(value string) []string {
	parts := strings.Split(value, ",")
	result := make([]string, 0, len(parts))
	for _, part := range parts {
		if item := strings.TrimSpace(part); item != "" {
			result = append(result, item)
		}
	}
	return result
}
