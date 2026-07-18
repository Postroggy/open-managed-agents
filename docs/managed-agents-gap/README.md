# Managed Agents Gap — Postroggy 个人本地配置备份

本目录备份 Postroggy 本机用于 `open-managed-agents` 开发的个人配置，便于多机同步。**仅限个人 fork，不回流上游 `superduck-ai/open-managed-agents`**——这些文件在上游仓库中要么被 `.gitignore` 忽略（`CLAUDE.local.md`），要么通过 `.git/info/exclude` 排除（`.claude/skills/`）。

## 内容

### `skills/`

cc-skills-golang（[samber/cc-skills-golang](https://github.com/samber/cc-skills-golang)，MIT）的 Tier1 Go skill 集合，共 29 个：

```
golang-benchmark          golang-error-handling      golang-observability       golang-structs-interfaces
golang-code-style         golang-gopls               golang-performance         golang-testing
golang-concurrency        golang-how-to              golang-pkg-go-dev          golang-troubleshooting
golang-context            golang-lint                golang-popular-libraries   golang-database
golang-continuous-        golang-modernize           golang-project-layout      golang-data-structures
  integration             golang-naming              golang-refactoring
golang-dependency-        golang-documentation       golang-safety
  injection / -management golang-design-patterns     golang-security
                                                    golang-stay-updated
```

每个 skill 含 `SKILL.md`（主指南）以及 `references/`、`evals/`、`assets/`（部分）。本机安装位置：仓库 `.claude/skills/`（已加入 `.git/info/exclude`，不进入工作区提交）。

### `CLAUDE.local.md`

覆盖项目 `CLAUDE.md` 的个人本地约束，仅本机生效。核心内容：

- GitHub issue/PR 协作规范（中文 title/body、ready for review、讨论先行、提案≠共识）
- 一修复/一 issue/一 PR 铁律
- 分支必须基于最新 `origin/main`
- 提交前 skill 自检（只审本次 diff，不审存量）
- Postroggy 本机权限限制（缺 Label/Projects 权限）

## 新机同步步骤

```bash
# 1. skills → 仓库 .claude/skills/（并加入 .git/info/exclude 避免 push）
cp -r docs/managed-agents-gap/skills/* .claude/skills/
echo ".claude/skills/" >> .git/info/exclude

# 2. CLAUDE.local.md → 仓库根（.gitignore 已忽略，不会污染提交）
cp docs/managed-agents-gap/CLAUDE.local.md ./CLAUDE.local.md
```

全局 `~/.claude/CLAUDE.md`（语言规范等）另行管理，不在本目录范围。

## 注意

- 本分支（`docs/managed-agents-gap`）同时承载 Managed Agents 能力差距分析和个人本地配置备份，**不要合并到 fork 的 main 或开 PR 到上游**。`personal/docs-managed-agents-gap` 是早期备份分支，已废弃，内容已归并到本分支。
- skill 内容来自上游 cc-skills-golang（MIT），如需更新以那边为准。
- `CLAUDE.local.md` 中的 Postroggy 权限说明会随账号权限变化而过时，同步后请核对。
