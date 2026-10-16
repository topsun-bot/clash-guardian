# SECURITY_CHECK_2026-10-15 — clash-guardian

静态扫描(正则,非完整审计)。严重度 H/M/L。真实密钥值未写入本报告,仅列位置。

## 高危 (1)

| 文件:行 | 类别 | 问题 | 建议修法 |
|---|---|---|---|
| `internal/config/config_test.go:15` | secret | Hardcoded secret assignment | 删除并轮换该凭据,改用环境变量/GitHub Secrets,必要时清理 git 历史 |

## 通用建议
- pull_request_target:不要 checkout PR head 代码并执行;或改用 pull_request。
- 工作流中不要把 `${{ github.event.* }}` 直接插入 run:,改用 env 变量。
- 第三方 action 固定到 commit SHA;工作流加 `permissions: contents: read`。

本 PR 仅添加此报告,未改动代码。