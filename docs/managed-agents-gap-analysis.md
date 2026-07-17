# Open Managed Agents vs Claude Managed Agents 官方能力差距分析

> 分支: `docs/managed-agents-gap`（仅推送到 fork，不提 PR）
> 生成日期: 2026-07-17
> 对照来源: Claude Managed Agents 官方文档，本地镜像见 [`docs/managed-agents-reference/`](./managed-agents-reference/README.md)
> 目的: 识别 oma 相对官方 Managed Agents 的能力缺失与不完善之处，作为代码贡献着力点的排序依据。

## 1. 方法

对照官方 Managed Agents 文档（overview / quickstart / sessions / reference / tools 等，完整镜像见 `docs/managed-agents-reference/`）定义的能力面，逐项核查 oma `internal/` 的实现：关键词 grep + 关键文件精读（`internal/agents/handler.go`、`internal/managedagentsevents/events.go`、`internal/deployments/handler.go`、`internal/environments/handler.go`、`internal/sessions/`、`internal/codesessions/`）。

标注规则：

- ✅ 已实现且较完整
- ❌ 确认缺失（有 grep 零命中佐证）
- ⚠️ 部分实现 / 需进一步确认

## 2. 关键洞察：host 与 worker 的边界

`internal/managedagentsevents/events.go` 除官方事件类型外，还兼容一组非官方别名（`session.running`、`session.idled`、`session.requires_action`、`session.status_run_started`、`session.thread_idled` 等）；`internal/codesessions/` 处理 Claude Code transcript 事件（`assistant` / `user` / `system` / `result`）与 OTLP 日志回传；`internal/environments` 通过 `/work/poll` 让 worker 认领任务。

这说明 **oma 自身不运行 agent loop，而是启动一个 Claude Code 风格的 worker 进程在沙箱中执行，worker 经 `/v1/code/sessions/{id}/worker/*` 回传事件**。推论：

- prompt caching / compaction 的真正执行在 worker 侧（对应 `claude-code-research/` 的研究主题）；
- oma host 侧的 gap 多为“未控制 / 未暴露 / 未优化”，而非“从零实现”。

这是理解后续所有 gap 的前提。

## 3. 已实现且较完整（基线）

| 能力 | 落点 | 状态 |
|---|---|---|
| Agent 版本化 + CRUD + archive | `internal/agents/handler.go` | ✅ version + `agentver_` |
| tools 三类型 + configs + default_config | `agents/handler.go` normalizeTools | ✅ `agent_toolset_20260401` / `mcp_toolset` / `custom` |
| permission_policy | `agents/handler.go` normalizePermissionPolicy | ✅ `always_allow` / `always_ask` |
| multiagent coordinator | `agents/handler.go` normalizeMultiagent | ✅ 1–20 agents + self 引用 |
| 事件类型全覆盖 | `managedagentsevents/events.go` | ✅ user/agent/session/span/system/deltas |
| Scheduled deployments (cron) | `deployments/handler.go` normalizeOptionalSchedule | ✅ cron 表达式 + IANA timezone |
| self_hosted work polling 协议 | `environments/handler.go` `/work/*` 路由 | ✅ poll/ack/heartbeat/stop |
| vaults (mcp_oauth/static_bearer/env_var) | `vaults/handler.go` | ✅ 含 has_refresh_token |
| event deltas / outcomes / threads | events.go + stream_hub | ✅ |

## 4. Gap 清单（按贡献价值排序）

### P0 — 明确缺失，官方核心能力

| Gap | 证据 | 官方要求 | 贡献切入点 |
|---|---|---|---|
| **Dreaming / Dreams** | `grep -ril dream internal/` 零命中 | research preview，见 [`managed-agents-reference/dreams.md`](./managed-agents-reference/dreams.md) | 全新模块，需先据官方文档定义事件 / 状态机 |
| **Session 级 `agent_with_overrides`** | `grep -rn override internal/sessions/` 零命中 | session 创建时可覆盖 model/system/tools/mcp_servers/skills（null=清除），三种 agent 引用形式（string / pinned / overrides） | `sessions/handler.go` 创建逻辑 + agent 解析；支持 `agent_with_overrides` 并生成 resolved snapshot |
| **`stop_reason` 暴露** | `grep -rn stop_reason internal/sessions/` 零命中（仅 `codesessions/mapper` 有） | `session.status_idle` 必须带 `stop_reason` | session idle 事件构造处补字段，从 worker 输出映射 |

### P1 — 部分实现 / 依赖 worker，需补 host 侧控制

| Gap | 证据 | 现状 | 切入点 |
|---|---|---|---|
| **Prompt caching** | `grep -rn cache_control internal/sessions,codesessions,runtime/` 仅 `codesessions/ingress.go` 一处 `ephemeral` | 官方称“harness supports built-in prompt caching”；oma host 侧未显式设置 / 透传 cache_control | 先确认 worker 是否自带 caching；host 侧可能需透传 cache 策略。参考 `claude-code-research/reports/prompt-cache-architecture/` |
| **Compaction 主动策略** | 有 `agent.thread_context_compacted` 事件 + `is_compaction` 字段，未见 oma 主动触发 | 官方有 compaction 优化；oma 可能完全依赖 worker 自压 | 确认 compaction 触发方；若在 worker，host 是否需配置阈值。参考 `claude-code-research` compaction 流程分析 |
| **MCP tunnels** | `admin/domain_tunnels.go` 存在 | 可能是平台目录服务 tunnel，未必对齐 Managed Agents 的 private MCP tunnels（research preview） | 确认 `domain_tunnels` 语义是否对齐官方 MCP tunnels |

### P2 — 对齐性增强

| Gap | 说明 |
|---|---|
| self_hosted worker CLI flag 语义 | 官方 `ant beta:worker` 有 `--on-work` / `--unrestricted-paths` / `--max-idle`；oma 用自己的 runner，需确认 work 协议字段是否覆盖 |
| 事件别名清理 | oma 兼容非官方 worker 别名（`session.running` 等），需确认是否应作为内部映射而非公开持久化事件 |
| rate limiting | 官方 reference 定义 300/1200 RPM；oma 是否有组织级 rate limit 中间件待确认 |

## 5. 贡献着力点建议

1. **`agent_with_overrides`**（P0，中等规模，边界清晰）— 最适合首发贡献。纯 host 侧逻辑，官方契约明确，可写完备测试，不依赖沙箱。
2. **`stop_reason` 暴露**（P0，小规模）— 快速胜利，需先理解 worker→host 事件映射。
3. **Prompt caching**（P1，高价值需调研）— 性能核心，`claude-code-research` 已有现成分析可复用；需先确认执行链路。
4. **Dreaming**（P0，但 research preview）— 全新模块，官方文档可能不稳定，风险较高。

## 6. 参考资源

- 官方文档镜像: [`docs/managed-agents-reference/`](./managed-agents-reference/README.md)（25 个页面，2026-07-17 下载）
- Claude Code 逆向研究: `../claude-code-research/`
  - [`UNIFIED-INDEX.md`](../../../claude-code-research/UNIFIED-INDEX.md) — 总索引
  - `reports/prompt-cache-architecture/` — prompt caching 8 篇报告
  - `source-code-analysis/phase-05-memory-context/03-context-compaction.md` — compaction 流程
  - `source-code-analysis/phase-09-harness-engineering/01-agent-loop-analysis.md` — agent loop

## 7. 待细化

本分析基于 overview / quickstart / sessions / reference / tools 五个页面的代码级核查。后续应结合 `docs/managed-agents-reference/` 中其余页面细化，重点关注：

- `github.md` — 可能对应未实现的 GitHub 集成能力
- `memory.md` — memory stores（独立 beta header `agent-memory-2026-07-22`）
- `skills.md` / `webhooks.md` — 已有资源的完整契约对齐
- `migration.md` / `onboarding.md` — 迁移与引导流程
- `cloud-sandboxes-reference.md` / `self-hosted-sandboxes-security.md` — 沙箱配置细节对齐
