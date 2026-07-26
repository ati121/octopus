# Octopus 中文维护上下文

最后更新：2026-07-26

本文档用于跨对话、跨维护者保存项目状态。开始工作前先阅读本文档；完成重要修改或发布后同步更新。

## 仓库与身份

- 当前主仓库：`https://github.com/ati121/octopus`
- 可见性：私有仓库
- 默认分支：`dev`
- 当前 GitHub 用户：`ati121`
- 当前本地 `origin`：`https://github.com/ati121/octopus.git`
- 旧仓库保留为本地 `previous-origin`：`https://github.com/tianxia3111/octopus.git`
- Go 模块路径仍为 `github.com/bestruirui/octopus`。这是上游模块标识，不能仅因仓库迁移而全局替换。

## 当前发布状态

- 新私有仓库已经包含当前完整提交历史和最新代码，但尚未创建 Release，也尚未在新用户名下发布 GHCR 镜像。
- 旧仓库最后发布的正式版本为 `v0.9.1`，对应旧仓库 Release 和旧用户名下的镜像。
- 新仓库的下一正式版本应从 `v0.9.2` 开始，避免复用已经发布过的 `v0.9.1`。
- 新镜像地址为 `ghcr.io/ati121/octopus`。
- 不要批量推送本地旧标签到新仓库；`.github/workflows/release.yaml` 会对每个 `v*` 标签触发完整发布。

## 更新检查与私有仓库鉴权

- 内置更新 API：`https://api.github.com/repos/ati121/octopus/releases/latest`
- 更新文件下载地址：`https://github.com/ati121/octopus/releases/latest/download`
- 前端项目链接：`https://github.com/ati121/octopus`
- 私有仓库无法匿名检查或下载 Release。运行环境必须提供 `OCTOPUS_GITHUB_PAT`，代码会用它设置 GitHub Bearer 鉴权。
- Token 只允许通过运行环境或安全的 Secret 管理注入，不能提交到仓库。

## 已完成的近期功能

- 请求日志新增缓存命中率百分比，只显示百分比，悬停提示“缓存命中率”。
- 调整请求日志布局，费用固定在最右侧，耗时和缓存命中率使用中间空间。
- 运行状态区域新增“清空”按钮和确认流程。
- 新增 `DELETE /api/v1/runtime/clear`，只清理熔断状态和近期渠道健康状态，不清理粘性会话。
- 修复 New API 快捷创建站点时延迟同步 Key 的兼容问题。
- 项目默认链接、构建作者、Docker 镜像地址和更新源已从 `tianxia3111` 迁移到 `ati121`。

## 相关实现位置

- 缓存命中率与日志布局：`web/src/components/modules/log/Item.tsx`
- 运行状态清空按钮：`web/src/components/modules/home/runtime-circuit-strip.tsx`
- 运行状态清空接口：`internal/server/handlers/runtime.go`
- 熔断状态清理：`internal/relay/balancer/circuit.go`
- 近期渠道健康状态清理：`internal/op/channel_recent_health.go`
- 更新检查：`internal/update/update.go`
- 默认仓库信息：`internal/conf/version.go`、`web/src/lib/info.ts`
- Docker 手动发布：`.github/workflows/publish-image.yml`
- 正式版本发布：`.github/workflows/release.yaml`

## 最近验证记录

仓库迁移与更新地址修改后已完成：

```bash
go test ./internal/update ./internal/conf
cd web && npm exec eslint -- src/lib/info.ts
git diff --check
```

缓存命中率和运行状态清理功能此前已完成：

```bash
go test ./internal/relay/balancer ./internal/op ./internal/server/handlers
cd web && npm run build
```

## 发布流程

### 手动发布 Docker 镜像

使用 `.github/workflows/publish-image.yml`，必须明确传入：

- `tag`：镜像标签，例如 `latest`
- `version`：应用版本，例如 `v0.9.2`
- `ref`：必须指向新仓库中包含目标修改的分支或提交

该工作流发布 Debian 镜像到当前仓库对应的 GHCR，并构建 `linux/amd64`、`linux/arm64`。

### 正式发布

在新仓库的目标提交上创建新的 `v*` 标签会触发 `.github/workflows/release.yaml`，生成：

- GitHub Release 和跨平台下载文件
- `latest` 与版本号 Debian 镜像
- `latest-alpine` 与版本号 Alpine 镜像

正式发布后必须核验 Release 被标记为 Latest，并读取 GHCR manifest 确认镜像标签和架构。

## 后续维护规则

- 新增提交统一使用中文主题。
- 每次发布记录版本、提交、Actions 地址、镜像 digest 和支持架构。
- 更新仓库归属或版本源时，同时检查后端更新地址、前端仓库链接、构建参数、Docker Compose 和使用文档。
- 遇到新私有仓库没有 Release 时，更新检查返回 404 属于预期状态；发布首个新版本后恢复正常。
