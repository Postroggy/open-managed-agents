# Open Managed Agents vs Claude Managed Agents 官方能力差距分析

> 分支: `docs/managed-agents-gap`（仅推送到 fork，不提 PR）
> 初始生成: 2026-07-17 ｜ 全量核查更新: 2026-07-17
> 对照来源: Claude Managed Agents 官方文档，本地镜像见 [`docs/managed-agents-reference/`](./managed-agents-reference/README.md)（26 个页面，已逐页核查）
> 目的: 识别 oma 相对官方 Managed Agents 的能力缺失与不完善之处，作为代码贡献着力点的排序依据。

## 1. 方法

对照官方 Managed Agents 文档定义的能力面，逐项核查 oma `internal/` 的实现。核查分四轮：

- **第一轮（整体实现度）**：核查每个能力面的主要能力点是否实现。覆盖 outcomes / memory / github / mcp / skills / vaults / session-operations / webhooks / multiagent。
- **第二轮（字段级穷尽）**：逐字段、逐参数、逐数值限制、逐枚举值、逐默认值核对代码精确实现，找出"数值边界不一致 / 枚举不全 / 默认值不同 / 校验缺失 / bug"这类精细 gap。覆盖 agent 定义 / session / events / environments / files / permissions / tools / deployments。
- **第三轮（反向 + 盲区）**：API 路由全集 + DB schema 全表对比，核查代码里有但文档没专门页面的包（batches / domain_tunnels / observability / 平台层），以及 reference.md 剩余章节、dreaming 确认。
- **第四轮（对照 origin issues）**：核对 superduck-ai/open-managed-agents 的 issue #62/#63/#64/#52/#23，补全 Models & AI Gateway、严格 Active Turn、凭证注入、公网回退等盲区。

**gap 分类框架**（判断某项差异时先归类，避免把有意设计误判为 bug）：

1. **遗漏**——官方有、oma 没有，属于真实 gap，应补。如公网回退未禁、凭证 sandbox 集成缺失、agent_with_overrides 缺失。
2. **有意偏离官方**——oma 主动选择与官方不同的行为，是产品决策而非 bug。如严格 Active Turn、Environment 无 Version/Snapshot、不重放有副作用的输入、Sandbox 销毁后不恢复。需确认设计意图后再决定是否对齐。
3. **平台特有**——oma 作为自建托管平台的内部需求，官方文档不涉及。如 #62 公司 AI 网关、Admin API、Console 层。不属于 Managed Agents 能力 gap。

标注规则：✅ 已实现且较完整（或字段级精确一致）｜⚠️ 部分实现 / 仅透传 / 需确认｜❌ 确认缺失。

## 2. 关键洞察：host 与 worker 的边界

`internal/managedagentsevents/events.go` 除官方事件类型外，还兼容一组非官方别名（`session.running`、`session.idled`、`session.requires_action`、`session.status_run_started`、`session.thread_idled` 等）；`internal/codesessions/` 处理 Claude Code transcript 事件（`assistant` / `user` / `system` / `result`）与 OTLP 日志回传；`internal/environments` 通过 `/work/poll` 让 worker 认领任务。

这说明 **oma 自身不运行 agent loop，而是启动一个 Claude Code 风格的 worker 进程在沙箱中执行，worker 经 `/v1/code/sessions/{id}/worker/*` 回传事件**。推论：

- prompt caching / compaction 的真正执行在 worker 侧（对应 `claude-code-research/` 的研究主题）；
- oma host 侧的 gap 多为"未控制 / 未暴露 / 未优化"，而非"从零实现"；
- 多 agent 委托、tool confirmation、skill 执行的**实际运行逻辑**在 code session（Claude Code 进程）层，Go 层只提供基础设施（表、API、事件注册）。这是架构分层，不是缺口。

这是理解后续所有 gap 的前提。

## 3. 能力面实现度全景（全 26 页核查）

| 能力面 | 真实实现度 | 判断 | 主要缺口 |
|---|---|---|---|
| **Skills** | ~98% | 最完整。CRUD + 版本 + prewarm + E2B volume 挂载全有 | 无 |
| **Webhooks（投递）** | ~98% | 异步 worker + lease + 二次退避 + HMAC + 20 次自动禁用 | 无独立缺口（deployment 事件未注册见 4.6） |
| **Environments & Sandboxes** | ~90% | 配置字段 + worker 协议（poll/ack/heartbeat/stop/stats）字段级几乎全过 | init_script/environment 泄露响应；workdir 默认值差异 |
| **Events** | ~92% | 36 官方事件全覆盖 + 别名映射 | `session.outcome_evaluation_ended` 未注册 CategoryFor；webhook 命名不一致 |
| **Agent 定义** | ~90%（不含 overrides） | 字段校验精确（20/128/2048/255 全对） | `agent_with_overrides` 完全缺失；agent name 无长度上限 |
| **Session operations** | ~90% | 状态机/CRUD/分页/update config 全有 | agent_with_overrides；list 缺 prev_page；状态转换无校验 |
| **Multiagent** | ~85% | Go 基础设施（threads 表/API/事件）完整 | 委托逻辑在 code session 层（架构性） |
| **MCP connector** | ~80% | 声明/tool filtering/注入沙箱完整 | 连接/认证失败事件 + 重试缺失 |
| **Vaults** | ~75% | 三类凭证 CRUD 完整 | injection_location / credential refresh / validate stub |
| **Files** | ~70% | 上传/list/delete/scope_id 完整 | **下载完全不可用（downloadable 硬编码 false）**；purpose 缺失 |
| **Memory（API 层）** | API 100% / **sandbox 0%** | 14 端点 + DB 三表全有 | agent 在沙箱里读不到 store；硬限制未强制 |
| **Scheduled Deployments** | **~40%** | CRUD + cron 校验 + upcomingRuns 完整 | **scheduler worker 完全不存在**；auto-pause/archive 缺失；webhook 事件未注册 |
| **GitHub** | ~65% | 数据模型/API 端点完整 | **authorization_token 传递链路断裂** |
| **Outcomes** | **~25%** | 只存了"待评分项" | **完全没有 grader 引擎**；status 永远 pending |
| **Models & AI Gateway** | ~30% | `/v1/models` 路由在、Messages 代理基础路径在 | 模型硬编码（两套，非网关同步）、无快照/stale、Usage/Cost 采集缺失、未禁公网回退（见 4.1/4.8） |

## 4. Gap 清单（按"工作量 × 价值"分梯队）

### 4.1 第一梯队 —— 小而高价值，强烈推荐首发

| Gap | 证据 | 工作量 | 说明 |
|---|---|---|---|
| **Files 下载完全不可用（downloadable 硬编码 false）** | `files/handler.go:152` 上传时固定 `Downloadable: false`；`:294` 下载时必检 → 所有文件永远返回 "File is not downloadable" | 需先确认是否有意（oma 可能改用 session 挂载提供内容）；若非有意则 ~10-20 行 | 文档有完整下载示例，代码却让下载端点不可用。疑似 bug，需确认设计意图后修复。 |
| **GitHub `authorization_token` 传递链路断裂** | `sessions/service_helpers.go:153-166`（create 分支只读 url/mount_path/checkout，不读 token）+ `environments/environment_manager.go:276-285`（`managedAgentSources` 只解 `Payload`，token 在 `SecretPayload` 里到不了沙箱） | ~15 行 | bug 级，影响私有仓库 git clone。update 路由（`service.go:711-725`）已支持 token，证明 create/sandbox 是遗漏。 |
| **`agent_with_overrides` 完全缺失** | `sessions/service_helpers.go:37-38`：`type != "agent"` 直接报错；全仓 grep 零命中。model/system/tools/mcp_servers/skills 五字段覆盖、null 清除、resolved snapshot 全缺 | 中（新增 resolve 分支 + snapshot 生成） | 已知 P0，字段级确认：连解析入口都没有。官方将其作为核心能力。 **⚠️ 实施约束（2026-07-17 核查 #54/#56/#57）**：非"加个 resolve 分支"即可——override 设值（model/system/tools/mcp_servers/skills）必须复用 agents 包配置校验（`normalizeModel/normalizeSkills/normalizeTools/normalizeMCPServers` + `validateMCPToolReferences`），但 #57 决策"不新建配置内核/Preflight"、#56 决策"不建 AgentConfig 合同"，复用方式存在三难：建独立配置包违 #57 / sessions→agents 横向依赖违依赖方向 / 复制一份违重复预算。且依赖 #54（配置校验强化进行中，assignee jh0904）。现状：未开 issue，待与 #54 负责人同步校验复用方式后再定方案。 |
| **Memory beta header 不兼容官方** | `memory/handler.go:150` 用 `?beta=true` query param，而非官方 `anthropic-beta: agent-memory-2026-07-22` header | 0.5 天 | 参照 `files/handler.go:74` 的 `files-api-2025-04-14` 现成模式。 |
| **Vault `injection_location` 缺失** | `vaults/handler.go:966` env_var 分支不解析 header/body | 60-80 行 | 文档定义了 environment_variable 凭证的 injection_location（header/body），含创建默认值与更新合并语义。 |
| **禁止 Anthropic 公网回退未实现**（安全） | `config/config.go:120` + `platformapi/platform_proxy.go:87-89` 默认 `https://api.anthropic.com`；未设 `ANTHROPIC_UPSTREAM_BASE_URL` 时 `/proxy/v1/messages` 静默转发公网 | 小（删默认值 + 启动校验非空且不指向公网） | **严重数据泄露风险**：内网部署泄漏到 Anthropic 公网。issue #62。详见 4.8。 |

### 4.2 第二梯队 —— 中工作量，中高价值

| Gap | 证据 | 工作量 |
|---|---|---|
| **MCP 连接/认证失败事件 + 重试缺失** | 无代码发 `mcp_connection_failed_error` / `mcp_authentication_failed_error` 类 `session.error`；无 idle→running 重连 | 中（~50 行，涉 codesessions/environment 层） |
| **Memory 硬限制未强制**（2000 条/store、8 store/session、30 天版本保留） | `db/memory.go` 无 count 检查；全仓无 memory_versions TTL 清理 | 中（1-2 天） |
| **Vault credential re-resolution/refresh 未实现** + `mcp_oauth_validate` 是 stub（恒返回 `unknown`） | `vaults/handler.go:634-661`；全仓无 refresh worker | 中大（新建 `internal/vaults/refresh.go`） |
| **Session list 缺 `prev_page` 向前翻页** | `sessions/handler.go:23-26` `pageResponse` 只有 `NextPage`；文档明确要求支持向前翻页 | 小-中 |
| **Session 状态转换无合法性校验** | `db/sessions.go:299` `SetSessionStatus` 无条件直接更新，可从 terminated 改回 idle | 小-中 |
| **API endpoint rate limiting 缺失**（300/1200 RPM） | `internal/api/`+`internal/httpapi/` 无 rate limiter middleware；`platformapi/rate_limits.go:24-43` 仅 model 级配额展示（console 用），非 endpoint 限流 | 中（middleware + token bucket/Redis）。`reference.md:111-117` 明确 300 RPM（create）/1200 RPM（read），完全未实现，属安全/稳定性 gap |

### 4.3 第三梯队 —— 大工作量，核心能力（高价值，需大投入）

| Gap | 证据 | 工作量 |
|---|---|---|
| **Memory sandbox 集成完全缺失** | `environment_manager.go:286` memory_store 仅透传 payload，无 `/mnt/memory/<slug>` 挂载、无 access 强制、无 system prompt 注入 | 大（5-8 天） |
| **Outcomes grader 引擎缺失（核心）** | 无 grader；status 永远 `pending`；`span.outcome_evaluation_*` 事件只透传不产生 | 大（全新模块 `internal/outcomes/`） |
| **Scheduled Deployments scheduler worker 缺失（核心）** | `deployments/handler.go` 有完整 schedule CRUD + `upcomingRuns()`，但全仓**无任何 worker 扫描 deployments 表触发 session 创建** | 大（新建 scheduler）。整个定时部署只有 CRUD 壳，没有运行时。 |
| **self-hosted worker 运行时缺失（核心）** | `runner.go:344-352` 只处理 cloud 环境；self_hosted 环境只存 `{type:self_hosted}`，work item 创建后**无消费逻辑**——只有 work queue 协议（poll/ack/heartbeat）的服务端，没有 client 侧消费者 | 大（实现 worker client）。oma 假设用户自部署官方 worker，但代码无集成方案，整个 self-hosted 沙箱不可用 |
| **凭证 sandbox 集成整体缺失（核心）** | oma 无运行时凭证注入层（grep `HTTP_PROXY`/`mitm`/`inject_credential` 零命中）；`environment_manager.go:50-52` 只传 `vault_ids` 列表，`codesessions/` 无 vault 明文拉取端点 → **所有 vault credential（mcp_oauth/static_bearer 明文）+ GitHub token 都到不了沙箱** | 大。比 memory sandbox 集成更广——MCP server 需 OAuth/bearer 时沙箱内 agent 拿不到 token。issue #52。详见 4.8 |
| **模型网关同步缺失（核心）** | `models/handler.go:56-131` 硬编码 8 模型 + `platformapi/platform_bootstrap_builders.go:137-149` 硬编码 9 模型；无网关同步/快照/stale；Usage/Cost 采集全缺（`platform_proxy.go:70-81` 零解析，cost 全仓零命中） | 大（新建 syncer + 采集层）。issue #62。详见 4.8 |

### 4.4 第四梯队 —— 依赖沙箱层 / 低优先

| Gap | 说明 |
|---|---|
| GitHub 仓库缓存 | 文档称"后续 session 启动更快"，需沙箱层支持 |
| Outcomes deliverables 与 session `scope_id` 关联 | Files API 已有 scope_id 过滤，但未与 session outputs 目录打通 |
| 工具输出 >100K token 自动写文件 | 文档 `tools.md` 声称此行为，代码无（属沙箱层） |
| DST wall-clock 语义（spring-forward skip / fall-back double fire） | `deployments/handler.go:1347` 逐分钟迭代无 DST 检测 |

### 4.5 已确认 bug（逻辑错误，非缺失）

1. **`outcome_evaluation_end` webhook 触发时机错误**：`sessions/service.go:618-619` 与 `deployments/handler.go:630-631` 在 `user.define_outcome` 刚入库、status 还是 `pending` 时就发出 "evaluation_ended"。
2. **`managedAgentSessionConfig` 硬编码传空 outcomes**：`environment_manager.go:37` 写死 `"outcomes": []any{}`，沙箱收不到 outcome 定义。
3. **`session.outcome_evaluation_ended` 事件类型未注册**：在 `webhooks/handler.go:46` 白名单和 `webhook_bridge.go:80` 中产生，但 `events.go` 的 `CategoryFor()` 不认识它 → `IsWorkerOutputEvent` 返回 false，会被 `codesessions/mapper.go:165` 拒绝丢弃。
4. **网络模式响应默认值不一致**：`environments/handler.go:929` 响应默认 `limited`，但 create 默认 `unrestricted`（`handler.go:1175`）。
5. **`init_script` / `config.environment` 泄露到公开 API 响应**：`environments/handler.go:890-905`，文档未提及的字段出现在 response。
6. **thread event stream 未拒绝 `event_deltas` 参数**：`sessions/stream_hub.go:127-149`，session 级与 thread 级共用 `acceptsStreamDeltas`，但文档明确 thread stream 应拒绝该参数（返回 400）。另：`stream_hub.go:169-170` 只检查参数存在、不校验值（文档要求仅接受 `agent.message`/`agent.thinking`）。

> 初版 P0/P1/P2 gap（Dreaming、stop_reason、prompt caching、compaction、MCP tunnels、worker CLI flag、事件别名清理、rate limiting）仍然有效。第二轮字段级核查补充了其精确实现现状（见 4.6）。

### 4.6 字段级精细 gap（第二轮 BFS 新发现，按能力面）

#### Files
- **`purpose` 字段完全缺失**：Anthropic Files API 标准字段，`db/files.go` FileRecord 与 files 表均无（grep 零命中）。
- **`managed-agents-2026-04-01` beta header 在 files 端点不校验**：`files/handler.go:73` 只检查 `files-api-2025-04-14`，但文档 Managed Agents 上下文示例要求同时带前者。
- **100 files/session 限制未强制**：`files.md:327` 声明上限，sessions 资源挂载处无检查。
- **custom tool `input_schema` 只校验顶层 `type=object`**：`agents/handler.go:1074-1076`，不校验 properties/required 有效性、JSON Schema 合规性。
- **`scope` 响应键名差异**：代码返回 `{id, type}`，官方 SDK 通常用 `{scope_id, scope_type}`。

#### Scheduled Deployments
- **无 cron scheduler worker**（核心，见 4.3）：CRUD 壳完整，无运行时。
- **无 auto-pause / auto-archive**：文档明确 agent archived → auto-archive deployment、subagent archived → auto-pause，代码均无。
- **deployment webhook 事件未注册**：`webhooks/handler.go:31-54` 的 `supportedEndpointEventTypes` 中 0 个 deployment 事件（缺 `deployment.created/paused/unpaused/archived/updated`、`deployment_run.succeeded/failed`）。
- **无 jitter**（文档允许 up to 10s）、无 1000 部署/org 上限、无 `session_rate_limited_error` 错误类型、`paused_reason` 仅支持 manual。

#### Agent 定义
- **agent `name` 无最大长度**：MCP server name 有 255、custom tool name 有 128，但 agent name 无任何上限（`agents/handler.go:460`）。
- **tools 总数 128 上限未文档化**：代码 `handler.go:940` 限制 128，比公开文档严格，需确认。
- **`model.speed: "fast"` 仍接受**：文档说 Opus fast 已弃用，代码仍接受（有意向后兼容，可加 deprecation 日志）。
- **skills 上限语义偏差（per-session vs per-agent）**：文档说 "Each session supports up to 20 skills total, counted across every agent"，代码 `agents/handler.go:851` 只做 per-agent ≤20；multiagent 场景下 coordinator + 各子 agent 的 skills 累计可突破 20。
- **search `limit=0` 边界 bug**：`agents/handler.go:678` 用 `< 0` 判断，允许 `limit=0` 通过，但错误消息说 "between 1 and 100"。
- **去重缺失**：tools configs 内同 name 可重复（`handler.go:966-1013`）、skills 内同 skill_id 可重复（`handler.go:843-876`）。

#### Session
- **agent_with_overrides 五字段覆盖全缺**（见 4.1）。
- **list 缺 `prev_page`**、**order cursor 一致性校验缺失**（文档要求重用不同 order 的 cursor 返回 400）、**状态转换无校验**。
- **`include_archived` 未在文档提及**（实现扩展，需确认是否应保留）。

#### Events
- **`session.outcome_evaluation_ended` 未注册**（见 4.5.3）。
- **webhook 命名不一致**：webhook 用过去式别名（`session.status_run_started/idled`），session events 用现在式。
- **`span.model_request_end` 的 usage 无结构校验**：`codesessions/mapper.go:419-421` 直接透传 worker usage。
- **thread stream 未拒绝 `event_deltas`**（严重，见 4.5.6）：`stream_hub.go:127-149`。
- **`event_deltas` 参数值不校验**：`stream_hub.go:169-170` 只检查参数存在，不验证值（文档要求仅接受 `agent.message`/`agent.thinking`）。
- **`session.status_idle` 不含 usage 字段**：`codesessions/mapper.go:427` result→idle 映射不携带 usage。

#### Environments
- **workdir 默认值差异**：`environment_manager.go:19` 默认 `/home/user`，文档 self-hosted 系统默认 `/workspace`（可能是 cloud vs self-hosted 刻意差异，需确认）。
- **`init_script`/`config.environment` 泄露响应**（见 4.5.5）、**网络默认值不一致**（见 4.5.4）。
- **`reclaim_older_than_ms` 默认值偏差**：`handler.go:1448` 默认 5000ms，官方 SDK/文档示例为 2000ms。
- **self_hosted + memory store 约束缺失**：文档说 self-hosted 不支持 memory，但 session 创建时不校验（`sessions/service.go:42-56`）。
- **`allowed_hosts` 正则支持端口号**：`handler.go:29` 正则含 `(:[0-9]{1,5})?`，但文档明确 "Do not include a URL scheme, port, or path"。
- **self-hosted worker 运行时缺失**（核心，见 4.3）。

#### Memory
- beta header 用错（见 4.1）、硬限制未强制（见 4.2）、sandbox 集成缺失（见 4.3）。

### 4.7 API 面归属与 Admin API 范围说明（第三轮反向核查）

经反向核查（API 路由全集 + DB schema 全表 + 代码包归属），oma 实际实现了**多个 API 面**。此前归为"oma 扩展端点"的组织/用户/工作区管理等能力，实际对应 [Admin API](https://platform.claude.com/docs/en/manage-claude/admin-api) 文档（本仓库未镜像），是独立 API 面，**不是 Managed Agents gap**：

| API 面 | oma 端点 | 归属文档 | 核查状态 |
|---|---|---|---|
| **Managed Agents API** | `/v1/agents`、`/sessions`、`/deployments`、`/deployment_runs`、`/environments`、`/files`、`/memory_stores`、`/vaults`、`/skills`、`/webhooks`、`/models` | `managed-agents-reference/` | ✅ 逐页核查完成 |
| **Admin API** | `/v1/organizations/{id}/`（me、invites、users、workspaces、workspace_members、api_keys、external_keys、rate_limits） | admin-api 文档（**未镜像**） | ⏳ 未核查 |
| **标准 Anthropic API 兼容** | `/v1/messages/batches`、`/v1/files`、`/v1/models` | Messages API / Files API | batches 已确认完整 |
| **oma 内部基础设施** | `/v1/code/sessions/*`、`/v1/session_ingress/*`、`/v2/*` | oma 独有 worker 桥接 | 非 API 面，是 host↔worker 协议 |

**关键澄清**：

- **`internal/batches/`（1116 行）非任何 gap**：是 Anthropic **Messages API Batches** 的完整代理（beta `message-batches-2024-09-24`，路由 `/v1/messages/batches`，upstream 转发到 `{AnthropicUpstreamBaseURL}/v1/messages`）。生产级实现：状态机 + 异步 worker + S3 JSONL 结果 + 心跳续租 + stale 恢复 + 指数退避。
- **`internal/platform*/` + `internal/workbench/` + `console_api_keys`** 是 **Console 控制台层**（部分属 Admin API，部分是 oma 独有的 Prompt IDE / workbench）。
- **`internal/observability/`**（247 行）只是本地 console slog handler，非能力面；真正的可观测性通过 event stream + span events 实现（已有）。
- **`internal/agentsnapshot/`** 是成熟基础设施，snapshot 生成入口本身不缺；但 `agent_with_overrides` 的真正难点是 override 设值需复用 agents 包配置校验，受 #56/#57 决策约束、复用方式待定（见 4.1）。
- **DB schema 无孤儿表/字段**：48 张表全部有读写路径，未发现被遗忘的半成品功能。

**Dreaming 确认**：后端零实现（`grep dream` 零命中），仅前端有 `/dreams` placeholder。官方 `dreams.md` 有完整 API 定义（状态机 `pending→running→succeeded/failed/canceled` + 5 端点 + beta `dreaming-2026-04-21` + 输入 memory store/sessions、输出新 memory store），属 research preview，全新模块。

**MCP tunnels 精确化**（升级自初版 P1）：管理面完整（`admin/handler.go:77-84` + `mcp_tunnels`/`mcp_tunnel_certificates` 表 + token reveal/rotate + certificate CRUD + beta `mcp-tunnels-2026-05-19`），但**运行时 tunnel client 缺失**——`internal/environments/` 零命中 tunnel 连接逻辑，整个功能无法实际工作。

**Rate limiting**：见 4.2（endpoint 级 300/1200 RPM 完全缺失，安全/稳定性 gap）。

> **范围边界**：Admin API（organizations / users / invites / workspaces / api_keys / external_keys / rate_limits 管理端点）是独立 API 面，本分析未镜像其文档、未核查 oma 对 Admin API 的实现完整度。如需覆盖，需先下载 admin-api 文档。

### 4.8 issue 揭示的盲区与"有意偏离官方"的产品决策（第四轮，对照 origin issues）

对照 origin（superduck-ai/open-managed-agents）issue #62 / #63 / #64 / #52 / #23 后，发现前三轮漏掉的方向。其中最重要的一类是 **OMA 有意偏离官方的产品决策**——前三轮只找"缺失"，漏了"故意不同"。已确认 #11（outcome）/ #25（skill）/ #65（MCP OAuth）/ #66（Files）/ #69（Networking）等已被前三轮覆盖。

#### Models & AI Gateway（#62）—— 前三轮完全盲区

| 能力点 | 代码现状 | 状态 |
|---|---|---|
| 模型目录数据源 | `models/handler.go:56-131` `buildPlatformModels()` 硬编码 8 个；`platformapi/platform_bootstrap_builders.go:137-149` 另硬编码 9 个（Console）；前端 `web/.../model.ts:105-120` 还有 4 个 fallback | ❌ 无网关同步 |
| 从公司 AI 网关同步模型 | 无 HTTP client / 分页游标 / 原子替换 / 定时同步 | ❌ |
| 模型快照 + stale 状态 | 无 DB 持久化、无 sync 时间、无 stale 标记 | ❌ |
| Gateway Request ID / Usage / Cost 采集 | `platform_proxy.go:70-81` 代理层零解析；cost 全仓零命中；Console 报告 API 是 stub（空 map） | ❌ |
| 禁止 Anthropic 公网回退 | `config.go:120` + `platform_proxy.go:87-89` 默认 `https://api.anthropic.com`，未设环境变量时静默转发公网 | ❌（安全 gap，已提升到 4.1） |

#### 凭证请求拦截注入（#52）—— 强化既有判断

oma **无任何运行时 HTTP 拦截 / 凭证注入层**（grep `HTTP_PROXY`/`mitm`/`inject_credential` 零命中）。凭证全靠启动时 stdin payload + 环境变量注入。后果（比既有认知更广）：

- 不仅 GitHub `authorization_token` 到不了沙箱，**所有 vault credential（mcp_oauth / static_bearer 明文）都无法到达沙箱**——`environment_manager.go:50-52` 只传 `vault_ids` 列表，`codesessions/` 无 vault 明文拉取端点。
- MCP server 需 OAuth/bearer token 时，沙箱内 agent 拿不到。
- 这把"GitHub token 链路断裂"升级为"**凭证 sandbox 集成整体缺失**"（已提升到 4.3）。

#### 严格 Active Turn（#64）—— 有意偏离官方，但代码尚未落地

OMA 设计上要比官方严格（一个 session 同时只一个 Active Turn、运行中拒绝 user.message、不支持 `processed_at:null` 排队、interrupt 后必须等 idle），但**代码未实现**：

- `sessions/service.go:554-626` `sendEventsRoute` 不检查 session.Status，运行中可发 `user.message`（对比同文件 archive/delete/update 都有状态检查）。
- `processed_at` 在 HTTP 接受时立即赋值（`service_helpers.go:223,271`），不符合官方"null=已排队"语义（`events-and-streaming.md:22`）。
- interrupt+message 同批未拒绝（官方 `events-and-streaming.md:179-201` 明确允许）。

#### Retry Session / Work 恢复 / 孤儿清理（#64 / #63）

- **Retry Session 完全未实现**：grep `retry_of`/`RetrySession` 零命中，无端点、无 `retry_of_session_id` 字段。
- **无 stale Work 扫描**：`runner.go:68-90` 只 poll `queued`，对 `starting`/`active` 超时 work 无恢复。
- **无孤儿 Sandbox 清理**：`db/environments.go` 只有正常 Stop，无孤儿扫描。
- Worker Epoch 机制已在 code_session 内部事件层实现（`db/code_sessions.go:85,184`），但未与 Environment Work 恢复衔接。

#### "有意偏离官方"产品决策清单（前三轮漏掉的类别）

| 决策 | 官方行为 | OMA 决策 | 代码状态 |
|---|---|---|---|
| 严格 Active Turn / 不排队 | 运行中可发 event + `processed_at:null` 排队 | 单 Active Turn、不支持排队 | 设计有，**代码未实现**（见上） |
| Environment 无 Version/Snapshot | Agent 有 Version | Environment 只按 environment_id 引用当前配置 | ✅ 已实现（`runner.go:100`） |
| 不透明重放用户消息 | — | Retry 不自动重放有副作用的输入 | 设计决策 |
| Sandbox 销毁后不恢复 | — | 不恢复文件系统/进程/内存 | ✅ 已实现（`runner.go:109-118`） |

> 这些 issue 多数是 oma 作为"自建托管平台"的内部验证需求（#62 公司 AI 网关、#63/#64 内部 E2B 验证），部分属于 Console/平台层而非 Managed Agents API 能力面。但其中**公网回退安全 gap、凭证 sandbox 集成缺失、严格 Active Turn 未落地、模型网关同步缺失**是真实的 Managed Agents 范围 gap，已提升到第 4 节各梯队。

## 5. 已实现较完整的能力面（基线，字段级已验证精确）

以下能力面字段级核查未发现值得贡献的 gap，记录供交叉验证：

| 能力 | 落点 | 字段级结论 |
|---|---|---|
| Agent 版本化 + CRUD + archive + update 语义 | `agents/handler.go` | ✅ version 必传/409 冲突/省略保留/null 清除/数组全量替换/metadata 合并 全精确 |
| tools 三类型 + configs + default_config | `agents/handler.go` | ✅ 8 内置工具名全对；20 servers/128 tools/2048 url/255 name 边界全精确 |
| permission_policy | `agents/handler.go:1044` | ✅ 枚举全集 `always_allow`/`always_ask` 一致；默认值（toolset=allow, mcp=ask）一致 |
| multiagent coordinator | `agents/handler.go` | ✅ 1–20 + max 1 self + distinct + version snapshot |
| Skills 全链路 | `skills/` + `skillprewarm/` + `runtime/e2bruntime/` | ✅ CRUD + 版本 + prewarm + E2B volume 挂载 |
| Webhooks 投递 | `webhooks/` | ✅ 异步 worker + lease + 二次退避 + HMAC + 自动禁用 |
| Memory API（14 端点） | `memory/handler.go` | ✅ stores/memories/versions CRUD + redact + precondition |
| self_hosted work 协议 | `environments/handler.go` | ✅ poll/ack/heartbeat/stop/stats 字段级全过；block_ms 1-999、ttl 5-300、expected_last_heartbeat 412 全精确 |
| cron 表达式校验 | `deployments/handler.go:1231-1321` | ✅ 5-field/范围/步进/范围/逗号/通配/Sunday 归一化 全精确 |

## 6. 贡献着力点建议（优先级排序）

1. **禁止 Anthropic 公网回退**（第一梯队，小工作量 × 严重安全）—— **最优先**。改动最小（删 `config/config.go:120` + `platformapi/platform_proxy.go:87-89` 的公网默认值 + 启动校验非空），风险最高（内网部署静默泄漏到 Anthropic 公网），最容易说清楚。issue #62。
2. **Files downloadable 修复**（第一梯队）—— 先确认设计意图，若为 bug 则最快胜利，影响下载核心功能。
3. **GitHub `authorization_token` 链路修复**（第一梯队，~15 行）—— bug 级、影响私有仓库 clone、可写测试。
4. **`agent_with_overrides`**（第一梯队，中）—— 官方核心能力，字段级已确认完全缺失；但实施受 #56/#57 决策约束（配置校验复用方式待定，见 4.1），非"可直接上手"。
5. **Memory beta header 对齐**（第一梯队，0.5 天）—— 最快兼容性胜利。
6. **Vault `injection_location`**（第一梯队，60-80 行）—— 字段补全。
7. **Session list `prev_page` + 状态转换校验 + 严格 Active Turn 落地**（第二梯队）—— 防御性增强 + 把有意偏离的产品决策真正实现。
8. **Memory sandbox 集成 + 凭证 sandbox 集成**（第三梯队）—— 两个 sandbox 集成缺失是同类大投入；凭证集成更广（影响所有 MCP OAuth/bearer），memory 集成 5-8 天。
9. **Deployments scheduler worker + self-hosted worker 运行时**（第三梯队）—— 都是"有 CRUD 壳、无运行时消费者"。
10. **Models & AI Gateway 网关同步**（第三梯队）—— 模型目录动态同步 + 快照/stale + Usage/Cost 采集。
11. **Outcomes grader 引擎**（第三梯队，最大）—— 风险高（依赖评估模型调用）。

## 7. 参考资源

- 官方文档镜像: [`docs/managed-agents-reference/`](./managed-agents-reference/README.md)（26 个页面，2026-07-17 下载）
- Claude Code 逆向研究: `../claude-code-research/`
  - [`UNIFIED-INDEX.md`](../../../claude-code-research/UNIFIED-INDEX.md) — 总索引
  - `reports/prompt-cache-architecture/` — prompt caching 8 篇报告
  - `source-code-analysis/phase-05-memory-context/03-context-compaction.md` — compaction 流程
  - `source-code-analysis/phase-09-harness-engineering/01-agent-loop-analysis.md` — agent loop

## 8. 调研进度（Managed Agents API 范围内全量完成）

三轮 BFS 核查已覆盖 Managed Agents API 全部 26 个官方文档页面、字段级实现，以及反向的 API 路由全集与 DB schema 全表对比：

- [x] **第一轮（整体实现度）**：outcomes / memory / github / mcp / skills / vaults / session-operations / webhooks / multiagent
- [x] **第二轮（字段级穷尽）**：agent 定义 / session + agent_with_overrides / events 全集 / environments + 沙箱 / files + permissions + tools / deployments + migration + onboarding
- [x] **第三轮（反向 + 盲区）**：batches（=Messages API）/ domain_tunnels（MCP tunnels）/ observability / 平台层包归属（=Admin API/Console）/ agentsnapshot / reference.md 剩余章节（rate limits / branding / worker CLI / MCP server types）/ dreaming 确认 / API 路由全集对比 / DB schema 全表孤儿检查
- [x] **第四轮（对照 origin issues）**：核对 superduck-ai/open-managed-agents 的 #62（Models & AI Gateway）、#63（Work 恢复/孤儿清理）、#64（严格 Active Turn/Retry/observability）、#52（凭证注入）、#23——发现 Models & AI Gateway、严格 Active Turn、凭证注入、公网回退、Retry Session 等**前三轮盲区**（见 4.8）。已确认 #11/#25/#65/#66/#69 等已被前三轮覆盖；#19（shadcn）/#24（超管）/#28（文档同步 hook）属前端/Admin API/工程治理，不在 Managed Agents 能力范围。

**未覆盖范围（明确边界）**：

- **Admin API**（`platform.claude.com/docs/en/manage-claude/admin-api`）：organizations / users / invites / workspaces / api_keys / external_keys / rate_limits 等管理端点属此 API 面。本仓库未镜像其文档，未核查 oma 对 Admin API 的实现完整度——这是下一个可选的调研方向。
- migration.md 反推出的托管能力清单（18 项）已全部在代码中定位，无遗漏。plan mode / output styles / slash commands / PreToolUse hooks / max_turns 经 migration.md 确认属 client 责任，非 gap。

**结论**：在 Managed Agents API 范围内，"文档→代码"正向核查与"代码→文档"反向核查均已完成，无遗漏的能力面引用，DB 48 张表无孤儿结构。剩余可深挖方向：

1. **Admin API** 独立核查（需先镜像 admin-api 文档）；
2. **Dreaming**（research preview，全新模块，风险高）；
3. **prompt caching / compaction** 的 host 侧控制策略（依赖 `claude-code-research` 结论）；
4. 第 4 节各梯队 gap 的逐个确认与修复。
