# Clash Guardian

[已实现](#已实现) · [快速开始](#快速开始) · [命令](#命令) · [平台行为](#平台行为) · [默认故障逻辑](#默认故障逻辑) · [国家优先级](#国家优先级)


一个小型、跨平台的 Clash/Mihomo 网络守护程序。它等待 Clash 启动，持续检查主策略组的实际节点，在连续故障后测试并切换备用节点，恢复时发送系统通知。

适合 Clash Verge Rev、Clash Nyanpasu、ClashX Meta，以及其他开放 Mihomo REST API 或 Unix Socket 的客户端。

## 已实现

- 开机后等待 Clash/Mihomo 控制接口就绪
- 默认每 15 秒通过当前实际节点检测两个境外 HTTPS 地址
- 连续失败 3 次后才触发故障转移
- 从 `/proxies` 自动读取主策略组及候选节点
- 严格按国家优先级逐组检测，在同一国家内并发测试并选择稳定、低延迟节点
- 切换后重新验证；失败会继续尝试下一节点
- 所有候选节点不可用时刷新 HTTP `proxy-provider`，或运行可配置的刷新命令
- 网络恢复、节点切换及持续故障时发送系统通知
- macOS LaunchAgent、Linux systemd 用户服务、Windows 登录计划任务
- 配置文件权限限制为仅当前用户可读（Unix 为 `0600`）

程序只操作初始化时确认的一个 `Selector` 主策略组，不会批量修改其他策略组。`rule` 模式下不会误把通常不承载规则流量的 `GLOBAL` 当作首选。

## 快速开始

需要 Go 1.24 或更新版本。

```bash
go build -o clash-guardian .
./clash-guardian init
./clash-guardian check
./clash-guardian install
```

`init` 会依次完成：

1. 自动发现常用 HTTP controller 和 Clash Verge Rev 的 Unix Socket。
2. 如果 API 需要 `secret`，只在首次初始化时询问。
3. 读取 Selector 策略组并推荐最可能的主代理组。
4. 显示推荐结果，等待用户确认。
5. 将配置以仅当前用户可读的权限保存。

Clash Verge Rev 在 macOS 上通常无需额外设置，程序可以自动发现 `/tmp/verge/verge-mihomo.sock`。其他客户端如果没有被发现，可手动指定：

```bash
./clash-guardian init --controller http://127.0.0.1:9090
./clash-guardian init --socket /path/to/mihomo.sock
./clash-guardian init --country-priority 日本,香港,美国 --country-fallback any
```

Mihomo HTTP API 的典型配置为：

```yaml
external-controller: 127.0.0.1:9090
secret: "请设置一个随机值"
```

不要把 controller 暴露到公网。标准控制接口与切换 API 见 [Mihomo API 文档](https://wiki.metacubex.one/en/api/) 和 [外部控制配置](https://wiki.metacubex.one/en/config/general/)。

## 命令

```text
init       自动发现 Clash、确认主策略组并生成配置
check      只读检查当前实际节点，不会切换
run        前台运行守护程序
status     查看当前节点和最近守护状态
install    安装并启动开机自启
uninstall  停止并移除开机自启
version    显示版本
```

所有命令都可以使用 `--config /path/config.json` 指定配置文件。

## 平台行为

### macOS

`install` 创建用户级 LaunchAgent：

```text
~/Library/LaunchAgents/io.github.clash-guardian.plist
```

日志写入：

```text
~/Library/Logs/clash-guardian.log
~/Library/Logs/clash-guardian.error.log
```

### Linux

`install` 创建并启动 systemd 用户服务：

```text
~/.config/systemd/user/clash-guardian.service
```

可以用 `journalctl --user -u clash-guardian -f` 查看日志。用户登录后服务会自动启动；如果需要无人登录也在开机时运行，可由管理员为该用户启用 systemd linger。

### Windows

`install` 创建名为 `Clash Guardian` 的当前用户登录计划任务。它不要求把 API secret 放进命令行，secret 只保存在用户配置文件中。

## 默认故障逻辑

一次检测周期会同时检测：

- `https://cp.cloudflare.com/generate_204`
- `https://www.gstatic.com/generate_204`

默认至少一个成功即认为当前线路可用；可以将 `minimum_successful_checks` 改为 `2`，要求两个地址都成功。达到三次连续失败后：

1. 读取配置主策略组的当前成员。
2. 跳过当前节点、`DIRECT` 和 `REJECT` 等非代理成员。
3. 按 `country_priority` 从第一项开始，先只测试该国家的节点。
4. 当前国家没有可用节点才进入下一国家；同一国家内优先选择成功地址更多的节点，其次比较平均评分。
5. 切换后再次检测两个地址。
6. 优先国家全部失败后，根据 `country_fallback` 决定是否测试其他国家。
7. 全部失败则刷新远程 provider，等待 `provider_retry_seconds` 后再试。

检测通过 Mihomo 的节点延迟 API 执行，因此检查的是目标策略组的实际节点，不会被其他分流规则误导。

## 国家优先级

默认顺序为：

```json
{
  "country_priority": ["日本", "香港", "美国"],
  "country_fallback": "any"
}
```

国家名称按节点名进行不区分大小写的包含匹配。例如 `R5-4|日本-NF|IIJ` 会进入“日本”候选组。也可以直接使用订阅采用的英文标记，例如 `JP`、`HK`、`US`。

每次真正需要重新获取节点时，都会从顺序第一项重新开始。例如当前已经切换到香港，香港节点后来发生故障，程序仍会先重新测试日本；日本没有可用节点才测试其他香港节点，然后再测试美国。

线路健康时不会周期性重选，也不会仅仅为了回到第一国家而主动切换。程序重启后会继续使用 Clash 当前节点，直到该节点连续检测失败。

`country_fallback` 支持：

- `any`：优先国家全部不可用时，最后测试未匹配到国家列表的其他节点。
- `none`：只允许使用 `country_priority` 中列出的国家。

如果不需要国家限制，可以将 `country_priority` 设置为空数组 `[]`。

## 订阅刷新说明

Mihomo 标准 API 能刷新使用 `proxy-providers` 声明的 HTTP provider。部分桌面客户端把完整订阅保存在 GUI 自己的配置中，并未提供稳定的外部刷新 API；此时可以在配置中加入安全、明确的刷新命令：

```json
{
  "refresh_command": ["你的客户端命令", "参数一", "参数二"]
}
```

命令不会经过 Shell 解释，只有在所有候选节点均不可用时执行，最长运行 30 秒。Clash Verge Rev 当前的订阅更新属于客户端内部 Tauri 命令，因此默认不会调用未公开的本地接口。

## 与 Clash fallback 配合

如果可以维护 Clash 配置，优先让 Clash 自己用 `fallback` 做快速节点切换，再让 Clash Guardian 负责整体检测、通知和刷新：

```yaml
proxy-groups:
  - name: 自动故障转移
    type: fallback
    proxies:
      - 节点 A
      - 节点 B
      - 节点 C
    url: https://cp.cloudflare.com/generate_204
    interval: 60
```

然后把日常使用的 Selector 主策略组选择到 `自动故障转移`。Clash Guardian 不会强制固定 `Fallback` 或 `URLTest` 类型的组。

## 开发与验证

```bash
go test ./...
go test -race ./...
go vet ./...
make build
```

`make build` 会生成 macOS arm64/amd64、Linux arm64/amd64 和 Windows amd64 二进制文件。

OpenWrt 尚未加入安装器，但核心程序可以在 Linux/OpenWrt 上编译运行；后续只需增加 procd 服务安装适配。
