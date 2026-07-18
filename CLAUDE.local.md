# CLAUDE.local.md — 个人本地约束（gitignored）

> 本文件覆盖项目 `CLAUDE.md` 中与之冲突的条目，仅本机生效，不提交仓库。
> 对应 Codex 的覆盖文件是 `AGENTS.override.md`。

## GitHub Issue / PR 协作规范（superduck-ai/open-managed-agents）

### 语言
- Issue、PR 的 title、body、comment **一律使用中文**；代码标识符、文件路径、字段名、命令可保留原文。
- 不得出现纯英文 title（如 `[Web] Fix Create Workspace ...` 是错的）。

### 一修复 / 一功能 = 一 issue = 一 PR（铁律）
- 一个 issue、一个 PR 只能描述并修复**一个**功能或缺陷；绝对禁止把多个不相关的 fix 或 feature 混在同一分支、同一 issue 或同一 PR。
- 修 A 时顺手发现的不相关 B 问题，必须另开分支、另开 issue、另开 PR。
- 提 PR 前确认它的 commits、files、title、body 只服务一个 issue。

### 分支起点（铁律）
- 开功能/修复分支前，必须先 `git fetch origin`，再从**最新的 `origin/main`** 开分支（如 `git checkout -b feat/xxx origin/main`）。
- 禁止在其它 feature 分支、本地未合并的 commit 或过时的 `main` 之上直接开新分支——否则会把不相关的工作（例如某次顺手修复）夹带进新分支，违反"一修复一 PR"。
- 开分支后用 `git merge-base HEAD origin/main` 核对起点；如果发现分支带了不属于本功能的 commit（例如基点落在某个未合并的修复 commit 上），立即基于最新 `origin/main` 重建，只保留本功能的改动。

### 提交前 skill 自检（边界 + 规范）

`git commit` 前必须对**本次 staged diff**做一次 skill 自检，**只审本次改动行，不审存量代码、不审全仓库**：

1. **职责范围（克制，呼应"一修复一 PR"）**：逐文件过 `git diff --cached`，确认每一处改动都只服务本次 issue/PR 的目标。夹带的不相关重构、改名、去重、格式化必须剥离（stash 或另开分支），不得带进本次提交。
2. **改动行规范**：按 diff 涉及的主题，调用对应 `golang-*` skill **只评价本次 +/- 行**（不扩到整文件/全仓）。常见映射：
   - 风格 / 命名 → `/golang-code-style` + `/golang-naming`
   - 错误处理 → `/golang-error-handling`；DB / SQL → `/golang-database`
   - 并发 → `/golang-concurrency`；测试 → `/golang-testing`（已加 OMA override，不用 testify）
   - 行为不变的重构 → `/golang-refactoring`
   - 只挑真正相关的 skill，不全跑。
3. **边界**：skill 若对**存量代码**给出建议，只记录、不得在本次 diff 顺手改（那是另一件事，另开 PR）。
4. 顺序：skill 自检 → 修正 diff → 跑下方"提交前硬门禁" → 才 `git commit`。

skill 自检是**软约束**（依赖自觉执行），硬门禁见下节；两者都要过。若要 100% 强制防止跳过，可另配 Claude Code `PreToolUse` hook 拦截 `git commit`（当前未配，按需再说）。

### 提交前硬门禁（just 命令清单）

skill 自检是软约束，以下硬门禁必须**全部通过**方可 `git push`（对应 `CLAUDE.md` 的提交前质量门禁）：

1. **gofmt** — `gofmt -l <改动的 Go 文件>`，无输出即合格；有输出则格式化后重查。
2. **golangci-lint** — `golangci-lint run --config .golangci.yml ./<改动的包>/...`，确保 0 issues（等价 `just lint`）。
3. **go test** — `go test ./<改动的包>/... -count=1`，全部通过。
4. **line budget** — `./scripts/check-go-file-lines.sh`，确保无超限文件（修改 Go 文件时）。
5. **dead code** — `./scripts/go-dead-code.sh`，确保 0 issues（修改 Go 代码时；等价 `just dead-code`）。
6. **duplicate code** — `./scripts/check-duplicates.sh`，确保无新增重复代码（修改 Go 或前端代码时；等价 `just duplicates`）。
7. **complexity** — 修改 Go 或前端生产代码时跑 `just complexity`，不得超限（见 `CLAUDE.md` 复杂度预算）。

跳过任一项必须在提交信息中注明原因。

### Issue 流程（顺序不可跳过）
1. 开 issue：中文 title + 中文 body，说明问题背景、证据、修复方向。
2. 在 issue 内发**讨论 comment**，给出方案、影响范围、验证计划。
3. **与维护者沟通达成一致后**，才同步提 PR；PR 描述中引用并 `Closes #issue`。

> **先讨论后编码（可与编码并行）**：Issue 发出后即可从最新 `origin/main` 开 `fix/`/`feat/` 分支开始写代码，不必等讨论完全定论；但 PR 标记 ready 前，Issue 必须已收敛且 CI 通过。Issue 未定论时不得直接请求合并。

### 执笔立场：代表用户意志，不暴露助手
- 写 issue / PR 的 title、body、comment 时，**我代表用户的意志**执笔——其中的方案、立场、决策都是**用户（issue / PR 作者本人）**的，不是"AI 助手"的。
- 决策在内部与用户对话中共同达成；但**对外（issue / PR 的公开内容）绝不提及自己**：不出现 "AI / 助手 / Claude / 模型" 等字样，也不把作者说成第三人称"贡献者"（那会把作者与执笔者拆成两个主体）。
- 措辞以作者立场写：用"建议 / 拟 / 计划 / 待维护者确认"，主体就是作者本人。
- "待维护者确认"属事实陈述（维护者确实尚未同意），保留；但**不得**写成"已与维护者达成一致 / 已定 / 已开始实现"等夸大共识或进度的措辞。

### PR 状态与描述
- PR 创建后必须是 **ready for review**（非 draft）。即便用 `gh pr create --draft`，也要紧接着 `gh pr ready <N>` 转 ready。此条**覆盖** `CLAUDE.md` 中"创建 Draft PR"的默认。
- **标题格式**：`[<模块>] <中文描述>`，模块参考仓库既有 PR，例如 `[Environment]`、`[Agent]`、`[Files]`、`[Prefactor]`、`[Sandbox]`；不得出现纯英文 title。
- **关联 Issue**：PR body 必须关联至少一个 Issue，用 `Closes #N`（合并即关闭）或 `Ref #N`（仅引用）。
- **body 结构**（中文）：Summary（问题背景 + 改动概要）、Changes（文件级改动清单）、Verification（验证方式与结果），并附 `Closes/Ref #issue`。

### Issue / PR 元数据治理（当前因权限不足暂缓）
理想做法是给 issue/PR 打 Label（`bug` / `enhancement` / `documentation`）、纳入 superduck-ai 的 GitHub Projects 并设置 Priority / Effort 字段。但本机 `gh`（Postroggy）**当前没有这些操作的权限**（见下"本机权限限制"），因此在权限补齐前，**暂不强制**写 Labels / Projects / Priority / Effort。

**当前必须保证的（不依赖那些权限）：中文 title/body、讨论先行、一修复一 PR、PR ready。** 待 Postroggy 获得 Triage+ 角色并补 `read:project` / `project` scope 后，再恢复元数据治理。

### 本机权限限制
`gh` 当前以 **Postroggy** 登录，对 `superduck-ai/open-managed-agents`：
- token scope 缺 `read:project` / `project` → **无法**经 API 读写 GitHub Projects 或 Priority/Effort 字段。补 scope：`gh auth refresh -s read:project,project`（交互式，需用户执行）。
- **无 Label 写入权限**（`AddLabelsToLabelable` 被拒）→ 需 org/repo 管理员授予 Triage+ 角色，或维护者在 Web UI 打 Label；补 token scope 不能解决。
- 可用：创建 issue / PR / comment、修改 title + body、`gh pr ready`。

## 个人配置同步

项目级个人配置（仅本机生效，不进入仓库提交，别人看不到）：

- **`.claude/skills/`** — 29 个 cc-skills-golang（[samber/cc-skills-golang](https://github.com/samber/cc-skills-golang)，MIT）Tier1 Go skill，通过 `.git/info/exclude` 排除（非项目 `.gitignore`，不污染上游）。
- **`CLAUDE.local.md`** — 本文件，通过项目 `.gitignore` 第 67 行忽略。

**同步分支：`docs/managed-agents-gap`**（fork：`Postroggy/open-managed-agents`）。

更新 skill 或 `CLAUDE.local.md` 后，提交到该分支并 `git push fork docs/managed-agents-gap`；新机拉取后按 `docs/managed-agents-gap/README.md` 的步骤落地。全局 `~/.claude/CLAUDE.md`（语言规范等）不在本范围。

> 注：`personal/docs-managed-agents-gap` 是早期备份分支，内容已归并到 `docs/managed-agents-gap`，后续不再使用；新同步一律走 `docs/managed-agents-gap`。

## 本地开发工具与启动

### 工具安装原则
- 遇到命令找不到（`command not found`）时，直接安装，不要让用户动手。
- `just`：用 `brew install just` 安装；安装后优先用 `just <recipe>` 替代手动调 scripts。
- `bun`：路径在 `~/.bun/bin/bun`，不在系统 PATH，需显式指定路径或 `PATH="$PATH:/Users/yueqi/.bun/bin"`。
- `go`/`goproxy`：已配置 `GOPROXY=https://goproxy.cn,direct`，启动后端时确保环境继承该变量（`GOPROXY=https://goproxy.cn,direct bash scripts/restart-server.sh`）。

### 启动前后端
- 前提：`just` 已安装时用 `just restart-server` / `just restart-web`。
- `just` 不可用时直接调脚本，并注意：
  - 后端：`GOPROXY=https://goproxy.cn,direct bash scripts/restart-server.sh`
  - 前端：`PATH="$PATH:/Users/yueqi/.bun/bin" bash scripts/restart-web.sh`
- 两个服务均以后台方式启动（`&`），等待端口 38080 / 5173 出现 LISTEN 状态再报告成功。
