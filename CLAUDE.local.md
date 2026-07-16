# Open Managed Agents — 本地规则（CLAUDE.local.md）

本文件覆盖 `CLAUDE.md` 中的部分规则，仅对当前仓库生效。

## PR 提交规范

- **语言**：所有 PR 标题和正文统一使用中文。
- **标题格式**：`[<模块>] <中文描述>`。模块参考仓库既有 PR，例如 `[Environment]`、`[Agent]`、`[Files]`、`[Prefactor]`、`[Sandbox]`。
- **关联 Issue**：每个 PR 必须关联至少一个 Issue（在 PR body 中使用 `Closes #N` 或 `Ref #N`）。
- **先讨论后编码**：Issue 下必须有充分的讨论和结论收敛，才能提 PR。不得在 Issue 无讨论或结论未明确的情况下直接提 PR。
- **PR 描述**：包含 Summary（问题背景 + 改动概要）、Changes（文件级改动清单）、Verification（验证方式与结果）。

## 提交前质量门禁

提交或推送前，必须依次执行以下检查，全部通过方可推送：

1. **gofmt** — `gofmt -l <改动的 Go 文件>`，确保无输出来格式化文件后重新检查。
2. **golngci-lint** — `golangci-lint run --config .golangci.yml ./<改动的包>/...`，确保 0 issues。
3. **go test** — `go test ./<改动的包>/... -count=1`，确保全部通过。
4. **line budget** — `./scripts/check-go-file-lines.sh`，确保无超限文件（修改 Go 文件时）。
5. **dead code** — `./scripts/go-dead-code.sh`，确保 0 issues（修改 Go 代码时）。
6. **duplicate code** — `./scripts/check-duplicates.sh`，确保无新增重复代码（修改 Go 或前端代码时）。

跳过检查必须在提交信息中注明原因。

## 工作流

- 提 PR 前先确认 branch name 符合 `fix/`、`feat/`、`chore/` 等惯例前缀。
- Draft PR 用于早期反馈，标记 ready 前需确保 CI 通过。
