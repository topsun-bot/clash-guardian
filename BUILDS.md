# 自动构建

每次代码推送到 `main` 分支后，GitHub Actions 都会自动执行静态检查、竞态测试，并构建以下平台文件：

- `clash-guardian-darwin-arm64.tar.gz`：Apple 芯片 Mac
- `clash-guardian-darwin-amd64.tar.gz`：Intel Mac
- `clash-guardian-linux-arm64.tar.gz`：ARM64 Linux
- `clash-guardian-linux-amd64.tar.gz`：x86-64 Linux
- `clash-guardian-windows-amd64.zip`：64 位 Windows

构建完成后，打开仓库的 **Releases** 页面即可按提交编号下载五个平台的压缩包。自动构建会标记为 prerelease，名称类似 `Automatic build ff4b6e7`。

同一批文件也会保存在对应 GitHub Actions 任务的 **Artifacts** 区域，保留 90 天，作为构建记录和备用下载入口。

本地构建仍然使用：

```bash
make build
```

生成的原始文件位于 `dist/`，该目录不会提交进 Git 历史。
