# Outcomes Grader 位置与前置条件调研

> Issue: #246 ｜ 关联: #11（grader 架构决策）
> 日期: 2026-09-16
> 分支: `fix/outcomes-link-and-grader`
> 状态: 调研结论，待拍板（见第 7 节）

本文固化「grader 应该跑在哪里」这一决策所需的全部证据，避免重复调研与失忆。
**每条结论都标注来源；官方原文逐字引用，不做意译。**

---

## 0. 来源说明（重要）

本仓库有两份来源不同、**不可混用**的官方文档：

| 来源 | 位置 | 性质 | 引用时 |
|---|---|---|---|
| **官方原始镜像** | `docs/api-gap` 分支 → `docs/managed-agents-reference/` | 官方站点原始 Markdown，**未经改写**。写入 "Anthropic-managed sandboxes" | ✅ **引用官方原文以本文档为准** |
| OMA 改写版 | 本分支 → `docs/en/`、`docs/zh/` | 由官方文档改写而来，把 "Anthropic" 替换为 "OMA" | ⚠️ 仅作对照，不可当官方原文 |

官方页面 URL 映射见 `docs/managed-agents-reference/README.md`（27 页清单，下载日期 2026-08-22）。

> **已发现的改写差异**：`docs/managed-agents-reference/tools.md:642` 有一句关于 grader 工具集的关键契约，
> 而 OMA 改写版 `docs/en/tools.mdx` 中 **`grader` 命中次数为 0** —— 该契约在改写过程中丢失。
> 这本身就是「必须以原始镜像为准」的理由。

---

## 1. 官方原文摘录

### 1.1 grader 是什么

来源: `docs/managed-agents-reference/define-outcomes.md:9`
官方页面: https://platform.claude.com/docs/en/managed-agents/define-outcomes

> When you define an outcome, the harness automatically provisions a *grader* to evaluate the artifact against a rubric. The grader uses a separate context window to avoid being influenced by the main agent's implementation choices.

> The grader returns an explanation summarizing which criteria passed or failed, or confirming that the artifact satisfies the rubric. That feedback is handed back to the agent for the next iteration.

（同段，`define-outcomes.md:11`）

**可提取的硬约束**：
1. grader 由 **harness** provision（不是 agent 自己发起）
2. 评估对象是 **"the artifact"（单数，官方未定义其物理位置）**
3. grader 使用**独立的 context window**
4. grader 产出 explanation，并**回灌给 agent 进入下一轮迭代**

### 1.2 grader 的工具集

来源: `docs/managed-agents-reference/tools.md:642`
官方页面: https://platform.claude.com/docs/en/managed-agents/tools

> The grader in outcome-driven sessions runs without `web_search` and `web_fetch`, regardless of these settings.

**可提取的硬约束**：
- grader **有**工具（否则无需特别声明"不含"这两个）
- grader 的工具策略是**相对该 session 的工具设置做减法**（"regardless of these settings" 指的是该 session 的 web 工具设置），即 grader 的工具环境**派生自 session**，而非独立一套
- **减集规则明确**：减去 `web_search`、`web_fetch`

### 1.3 交付物位置（两种环境类型不同）

**Anthropic-managed 沙箱** —— 来源: `docs/managed-agents-reference/define-outcomes.md:713`

> The agent writes output files to `/mnt/session/outputs/` inside the sandbox.

**self-hosted 沙箱** —— 来源: `docs/managed-agents-reference/self-hosted-sandboxes.md:48`（Sandbox filesystem 节）

> **Outputs:** on self-hosted environments the session's system prompt omits the `/mnt/session/outputs` instruction used on Anthropic-managed sandboxes, so final deliverables land wherever the agent writes them in your sandbox filesystem, typically under the working directory.

**这条是本文档最重要的证据**：self-hosted 环境下，交付物**没有固定路径、没有可枚举的位置**，"落在 agent 写它的任何地方"。

### 1.4 多 agent 共享沙箱（官方唯一的「一沙箱多上下文」模型）

来源: `docs/managed-agents-reference/multiagent-orchestration.md:17`
官方页面: https://platform.claude.com/docs/en/managed-agents/multiagent-orchestration

> All agents share the same sandbox, filesystem, and vault credentials, but each agent runs in its own **session thread**, a context-isolated event stream with its own conversation history. The coordinator reports activity in the **primary thread** (which is the same as the session-level event stream); additional threads are spawned at runtime when the coordinator delegates work.

同文档 `:21`：

> Each agent uses its own configuration: model, system prompt, tools, MCP servers, and skills. Session-level agent configuration overrides are the exception; they apply to the coordinator and its `self` copies. Tools, MCP servers, and context are not shared.

**⚠️ 范围警告**：**这一节通篇在描述 multiagent orchestration，全文未提及 grader。**
不能据此推断 grader 也在同一沙箱 —— 这是两个独立特性。该节的用途是证明
**"官方基础设施支持『一个沙箱 + N 个上下文隔离的执行'这一形态"**，仅此而已。

### 1.5 沙箱在 idle 时被 checkpoint

来源: `docs/managed-agents-reference/events-and-streaming.md:2379`

> When a session goes idle, its sandbox is checkpointed, preserving the full sandbox state, including the filesystem, installed packages, and any files the agent created.

同页 `:2382`：沙箱状态自创建起只保留 **30 天**，活动不延长该窗口。

### 1.6 事件与结果语义

来源: `docs/managed-agents-reference/define-outcomes.md:565-627`、`webhooks.md:27`
OpenAPI: `docs/api-gap` 分支 → `openapi/oma.en.json`

| 事件 | required 字段 |
|---|---|
| `span.outcome_evaluation_start` | `type, id, processed_at, iteration, outcome_id` |
| `span.outcome_evaluation_ongoing` | `type, id, processed_at, iteration, outcome_id` |
| `span.outcome_evaluation_end` | `type, id, processed_at, outcome_evaluation_start_id, iteration, result, explanation, usage, outcome_id` |

`span.outcome_evaluation_end.result` 取值（`:597-608` 表格 + OpenAPI）：
`satisfied` / `needs_revision` / `max_iterations_reached` / `failed` / `interrupted`

`usage` 定义（OpenAPI）：**"Aggregate token usage for this evaluation cycle. Sums across all grader model requests within the cycle."**

**⚡ 两套 result 不要混淆**（这是实现时最容易踩的坑）：

| | 取值 |
|---|---|
| **span 事件**的 `result` | `satisfied` / `needs_revision` / `max_iterations_reached` / `failed` / `interrupted` |
| **`outcome_evaluations[].result`** | `pending` / `running` / `evaluating` + 终态 `satisfied` / `max_iterations_reached` / `failed` / `interrupted` |

`needs_revision` **只存在于 span 事件**，不是资源的取值 —— 该轮结束后资源回到 `running`。

`outcome_evaluations[]` 资源 required 字段（OpenAPI `BetaManagedAgentsOutcomeEvaluationResource`）：
`type, outcome_id, description, result, iteration, completed_at, explanation`

Webhook 语义（`webhooks.md:27`）：

> `session.outcome_evaluation_ended` | Outcome evaluation for **a single iteration** completed.

---

## 2. 官方文档**没有**说什么（显式声明）

以下四点经**全量检索**（`docs/api-gap` 全部 670 个文件 + `main` 分支 `docs/` 全树）确认**不存在**：

1. ❌ **没有**任何一句规定 grader 的**物理执行位置**（同沙箱 / 独立沙箱）
2. ❌ **没有**说明 grader 能否读 session 沙箱的文件系统
3. ❌ **没有**说明 grader 是否是一个 thread
4. ❌ **没有**规定 artifact 在 self-hosted 环境下如何交付给 grader

检索方式：对全部 `.md`/`.mdx` 逐文件 `grep -c "grader"`。命中文件仅 7 个：
`define-outcomes.md`(7)、`tools.md`(1)、`managed-agents-gap-analysis.md`(6)、
若干 API reference（均为 rubric schema 的重复描述）。

**结论：「grader 在哪」必须靠推断，官方文档没有答案。**

---

## 3. 推断：grader 在 session 自己的沙箱内

### 3.1 推理链

```
P1  官方要求 grader「evaluate the artifact」(§1.1)
       │
P2  self-hosted 环境下交付物没有固定路径，
    「land wherever the agent writes them in your sandbox filesystem」(§1.3)
       │
P3  outcomes 特性对两种环境类型都可用
    （define-outcomes.md 全文未按环境类型做任何限制）
       │
P4  一个运行在**另一个文件系统**里的 grader，无法定位 P2 描述的交付物
       │
    ⇒  grader 必须能访问 session 自己的文件系统
       ⇒  grader 运行在 session 的沙箱内
```

### 3.2 佐证

- **工具集派生关系**（§1.2）：grader 的工具是「session 的工具配置减掉 web 两项」，说明它是
  **同一个工具执行环境**里的一个派生执行，而不是另起一套执行环境
- **官方基础设施已支持该形态**（§1.4）：`一沙箱 + N 个 context-isolated 执行` 是官方已有的能力
  （用于 multiagent thread），说明 grader 采用同样形态在工程上不需要新概念
- **`span.outcome_evaluation_end.usage` 是 grader 自己的 token 计量**（§1.6）：说明 grader 的模型调用
  是**独立于 writer 记账**的，与「独立 context window」一致

### 3.3 反证 / 未决

- 若 grader 在**独立沙箱**，则必须在 idle 后把产物搬运过去。而 §1.3 表明 self-hosted 环境的产物
  **不可枚举**（"wherever the agent writes them"），搬运不可行。**故排除独立沙箱。**
- 官方对 Anthropic-managed 沙箱有 `/mnt/session/outputs` 这一固定路径，理论上那里可以做搬运。
  但官方**同一套 outcomes 契约**必须同时适用于 self-hosted（否则文档会写明限制），
  因此不能采用「仅 Anthropic-managed 可用」的设计。

### 3.4 置信度

- 「grader 与 writer 在同一沙箱」：**高**（P1–P4 推理链闭合，无反例）
- 「grader 的具体实现形态（进程 / 上下文隔离方式）」：**未确定**，官方未描述

---

## 4. OMA 现状与缺口

### 4.1 当前链路

```
user.define_outcome 到达 OMA
  └─ ❌ IsPublicWorkerInputEvent 白名单不含它（events.go:57-68）
       → 事件到不了沙箱 worker，且不重置 idle
       → 【writer 收到 outcome 后完全不会开始工作】

writer 一轮结束
  └─ ⚠️ 信号来源：worker 自报状态（codesessions/status.go:29-35）
       running            → session.status_running
       idle / requires_action → session.status_idle
       → 【requires_action（等权限确认）也被映射成 idle，朴素用它触发会误判】

grader
  └─ ❌ 完全不存在
```

### 4.2 与官方的契约差距

| 项 | 官方 | OMA 现状 |
|---|---|---|
| `outcome_evaluations[].result` | `pending/running/evaluating`+4 终态 | 只有 `status: "pending"`，字段名都不对 |
| 资源字段 | `type,outcome_id,description,result,iteration,completed_at,explanation` | `{id,outcome_id,max_iterations,status,type,updated_at}` |
| `span.outcome_evaluation_*` | harness 产生 | 只注册了分类，无产生方 |
| `session.outcome_evaluation_ended` webhook | "for a single iteration completed" | 收到 `define_outcome` 就发 |
| grader 引擎 | 有 | 无 |

### 4.3 沙箱与 environment-manager 的数量关系（现状）

| 关系 | 现状 |
|---|---|
| OMA 沙箱 : work item | 1 : 1（`environment_sandboxes` 按 `work_id` 建，`schema.go:1258`） |
| 沙箱 : `task-run` 进程 | 1 : 1 |
| `task-run` : Claude Code | 1 : 1（`EM/src/cmd/cmd_task_run.rs:817`） |

即 **1 : 1 : 1**。官方模型是 **1 沙箱 : N 上下文**（§1.4）。

### 4.4 environment-manager-rs 的阻碍（前置任务清单）

仓库: `/home/gyq/Coding/Agent/environment-manager-rs`（superduck-ai 自己的 Rust 仓库，**可改**）

要支持「同沙箱内第二个 Claude Code」，必须先解决以下**共享路径冲突**（均无锁保护）：

| 路径 | 问题 | 位置 |
|---|---|---|
| `/home/claude/.claude/remote/.session_ingress_token` | 每次覆盖；**`destroy` 会删除它** → 先结束的进程破坏仍在运行的另一个的 bash token | `claude_code_executor.rs:68`、写 `:1205-1211`、删 `:1126-1134` |
| `claude mcp add/remove --scope user` | 改 Claude Code **用户级**配置；remove-before-add 有竞态 | `mcp/registry.rs:181-227` |
| `/tmp/claude-code.log` | 所有实例共用，只能追加、会交错 | `claude_code_executor.rs:65`、`:1214-1221` |
| `/tmp/claude-command` | truncate 覆盖写 | `claude_code_executor.rs:1274-1292` |
| `~/.gitconfig`（`git config --global`） | 跨 session 共享的全局配置 | `manager.rs:617-632` |

**已有的隔离**（无需改动）：
- session 锁按 session ID 分（`lockfile.rs:246-268`），**不同 session ID 的两进程不会互斥**
- 无 pid 文件、无固定端口（agent proxy / MCP 都 bind `127.0.0.1:0`）
- 取消只杀 direct child（`claude_code_executor.rs:9-10`）

**服务端侧的硬约束**：worker epoch 是 **per-code-session** 的，第二个 worker 对同一 code session
重新 register 会 bump epoch，旧 worker 写入返回 409
（`docs/design/be/ccrv2/ccr-v2-epoch-design.md:11-15`）。
→ **grader 不能复用 writer 的 code session 身份。**

---

## 5. 候选方案

| 方案 | 内容 | 与官方的贴近度 | 风险 |
|---|---|---|---|
| **A** | OMA 用 `Provider.RunCommand` 直接在沙箱 exec。**已否决** | 低 | 需在 OMA 重造 EM 的认证装配 / agent proxy / token fd 注入 / session URL 构造四件事；且「工具集与 writer 相同」会碎成两份实现 |
| **B** | EM 增加「在已有沙箱内再跑一个 Claude Code」的能力，OMA 侧用新的 code session 身份承载 | 高 | 需改 EM（前置任务见 §4.4） |
| **H** | 同 B 的 EM 改动；OMA 侧改用 **thread 形态** 承载第二个上下文 | 最高 | 同 B + 对外必须屏蔽 thread 语义（§6.3） |
| **G** | grader 跑在独立沙箱，产物拷进去。**已否决** | 低 | 与 §3.1 推理链的 P2/P4 直接冲突：self-hosted 产物**不可枚举**，无法搬运 |

**B 与 H 在 EM 侧的工作量完全相同**，差异仅在 OMA 侧用什么模型承载第二个上下文：

- 选 **B**：第二个上下文是一段新的 code session 身份 —— 复用现有 session 机制，但与官方的
  「一沙箱多上下文」抽象不平行，未来若要实现 multiagent 需要再抽象一层
- 选 **H**：第二个上下文是内部 thread —— 形态上最接近官方，但必须做 §6.3 的语义屏蔽，
  且依赖 OMA 的 thread 是否真能承载独立执行（见 §7 Q3）

---

## 6. 官方 grader ≠ 官方 thread

必须区分，避免设计时混淆。

### 6.1 事件与语义对照

| | 官方 thread | 官方 grader |
|---|---|---|
| 面向 | multiagent orchestration（协调者委派） | outcome 评估 |
| 事件族 | `session.thread_created` / `session.thread_status_*` 等 | `span.outcome_evaluation_*` |
| 生命周期事件 | 有（created/idle/terminated） | 无 thread 事件 |
| 由谁发起 | 协调者在运行时 spawn | **harness** provision |
| 文档是否提及对方 | ❌ 未提及 grader | ❌ 未提及 thread |

**官方文档中两者没有任何交叉引用。**

### 6.2 API 级反证（更硬）

来源: `docs/managed-agents-reference/webhooks.md:24`

> `session.thread_created` ｜ New multiagent thread opened: **an additional agent called by the coordinator is starting work, or the session's advisor is being consulted.**

官方 thread 的创建**只有两个触发源**：

1. 协调者委派给 roster 里的 agent
2. advisor 咨询

**二者都不包含 grader。** 即：outcome 评估**不会**产生 `session.thread_created`。

再加上 thread 是**可枚举的公开资源**（`GET /v1/sessions/{session_id}/threads`，见
`docs/api-reference/beta/sessions/threads.md`）—— 如果 grader 是一个 thread，它就会出现在这个列表里。
官方文档没有任何这类描述。

### 6.3 由此推出的设计约束

官方存在**第三类执行上下文**：既不是主 agent，也不是 multiagent thread，而是
**harness 私下 provision 的评估执行**（对外只暴露 `span.outcome_evaluation_*`）。

这给 OMA 的设计带来一条硬约束：

> **若用 OMA 的 thread 机制承载 grader，必须防止它泄漏到对外的 `GET /threads` 列表与
> `session.thread_*` 事件族中 —— 否则会产出官方契约里不存在的可观测行为。**

即：thread 只能作为**内部机制**借用，对外必须完全呈现为 outcome 语义。

---

## 7. 待决问题

| # | 问题 | 状态 |
|---|---|---|
| Q1 | 承载第二个上下文的模型：**B（新 code session 身份）** vs **H（thread 形态）** | 待拍板 |
| Q2 | 接受「EM 的 5 处共享路径必须做 session 隔离」为前置任务吗 | 待拍板 |
| Q3 | OMA 的 thread 是「真能承载独立执行」还是「只有 DB 表 + 事件归因」 | **核查中** |
| Q4 | 触发信号：`session.status_idle` 会把 `requires_action` 误判为「一轮结束」，如何区分 | 待设计 |
| Q5 | 基础设施故障（grader 超时 / 沙箱被回收）映射到哪个 result（官方 5 态无对应值） | 待设计 |

---

## 8. 引用索引

| 编号 | 文件 | 行 | 官方页面 |
|---|---|---|---|
| §1.1 | `docs/managed-agents-reference/define-outcomes.md` | 9, 11 | https://platform.claude.com/docs/en/managed-agents/define-outcomes |
| §1.2 | `docs/managed-agents-reference/tools.md` | 642 | https://platform.claude.com/docs/en/managed-agents/tools |
| §1.3 | `docs/managed-agents-reference/define-outcomes.md` | 713 | 同上 |
| §1.3 | `docs/managed-agents-reference/self-hosted-sandboxes.md` | 48 | https://platform.claude.com/docs/en/managed-agents/self-hosted-sandboxes |
| §1.4 | `docs/managed-agents-reference/multiagent-orchestration.md` | 17, 21 | https://platform.claude.com/docs/en/managed-agents/multiagent-orchestration |
| §1.5 | `docs/managed-agents-reference/events-and-streaming.md` | 2379, 2382 | https://platform.claude.com/docs/en/managed-agents/events-and-streaming |
| §1.6 | `docs/managed-agents-reference/define-outcomes.md` | 565-627 | 同上 |
| §1.6 | `docs/managed-agents-reference/webhooks.md` | 27 | https://platform.claude.com/docs/en/managed-agents/webhooks |
| §6.2 | `docs/managed-agents-reference/webhooks.md` | 24 | https://platform.claude.com/docs/en/managed-agents/webhooks |
| §6.2 | `docs/api-reference/beta/sessions/threads.md` | — | https://platform.claude.com/docs/en/api/beta/sessions/threads |
| §1.6 | `openapi/oma.en.json` | schema | `BetaManagedAgentsOutcomeEvaluationResource` 等 7 个 |
| §4.4 | `environment-manager-rs` | 见表格 | 仓库: https://github.com/superduck-ai/environment-manager-rs |

> 除 `openapi/oma.en.json` 与本仓库代码外，上表 `docs/managed-agents-reference/*` 均位于
> **`docs/api-gap` 分支**，不在本分支。
