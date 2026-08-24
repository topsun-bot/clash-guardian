# 自动构建

每次代码推送到 `main` 分支后，GitHub Actions 都会自动执行静态检查、竞态测试，并构建以下平台文件：

- `clash-guardian-darwin-arm64.tar.gz`：Apple 芯片 Mac
- `clash-guardian-darwin-amd64.tar.gz`：Intel Mac
- `clash-guardian-linux-arm64.tar.gz`：ARM64 Linux
- `clash-guardian-linux-amd64.tar.gz`：x86-64 Linux
- `clash-guardian-windows-amd64.zip`：64 位 Windows

构建完成后，打开仓库的 **Actions** 页面，进入对应的 `Build cross-platform binaries` 任务，在 **Artifacts** 区域下载以提交编号命名的构建包。构建包保留 90 天。

本地构建仍然使用：

```bash
make build
```

生成的原始文件位于 `dist/`，该目录不会提交进 Git 历史。

