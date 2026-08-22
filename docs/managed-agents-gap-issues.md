# OMA vs 官方 Managed Agents —— 待开 issue 探索结果（2026-08-15）

> 状态：**待逐条核对**（由 Postroggy 决定开哪些）
> 来源：官方 25 页文档全文拉取 + 3 个深挖 agent（事件 schema / 字段校验 / 行为错误码，2000+ 次代码核对）+ 自验，main @ 5004194
> 已开 issue：#244 budget / #245 dreams / #246 outcomes / #247 状态转换 / #248 files 下载 / #249 session.usage

---

## 🔴 P0 —— 核心能力缺失/断裂（官方契约明确，OMA 静默失效）

| # | 建议标题 | 核心判断 | 证据 | 决策 |
|---|---|---|---|---|
| 1 | `[Agents] model 对象支持 effort / inference_geo，speed 传递到运行时` | 官方 SDK 传 effort/inference_geo 被**静默丢弃**；speed 只回显不生效 | agents/handler.go:725-772 + environment_manager.go 零命中 | ⚠️ **部分做**：inference_geo 不做（开源版不做地域限制）；**effort 做**（模型推理强度）；**speed 透传做** |
| 2 | `[Events] 产生 session.error 事件（类型化错误对象 type+retry_status）` | 全仓**无构造点**，错误只能靠 status_terminated 推断 | events.go:32 / mapper.go:580-582 | 待定 |
| 3 | `[Memory] 强制沙箱限额与 access 校验（8 store/session、2000/store、30 天保留、read_only 文件级）` | 官方 4 项限额全缺；access 只在 deployments 路径校验，session 路径透传 | service_helpers.go:193-213 + memory_mapper.xml 无计数 | 待定 |
| 4 | `[GitHub] authorization_token 运行时注入沙箱` | token 写 SecretPayload 落库但**运行时从不读取**——私有仓库 clone 必失败 | service.go:686-690 vs environment_manager.go | 待定 |
| 5 | `[MCP] 连接/认证失败产生 session.error 事件（mcp_server_name + retry_status）` | 官方明确失败要发事件；OMA 只有 502 无事件 | mcp_proxy.go:108-113 | 待定 |
| 6 | `[Webhooks] 修复 session.archived webhook 断链` | 默认订阅有 + archive 时 enqueue，但 API 允许列表和事件映射桥没有 → 端点订阅 400 | defaults.go:99-104 vs webhooks/handler.go:33-56 | 待定 |

## 🟠 P1 —— 能力面缺失（官方有、OMA 无，工作量中等）

| # | 建议标题 | 核心判断 | 证据 | 决策 |
|---|---|---|---|---|
| 7 | `[Sessions] 支持 initial_events（创建即 running）` | 官方核心；OMA 创建后固定 idle | sessions/handler.go:81-88 | 待定 |
| 8 | `[Webhooks] 补齐 agent/deployment/deployment_run/environment/memory_store 事件` | 官方 `beta/webhooks.md` 定义 **45 种**事件域类型（agent/deployment/deployment_run/environment/memory_store/session/vault/vault_credential），OMA 只有 session/vault 两类，且**无发射点**（2026-08-22 镜像核实） | webhooks/handler.go:33-56 | 待定 |
| 9 | `[Environments] 支持 init_script（云沙箱定制核心能力）` | 官方核心；OMA 请求侧丢弃不报错 | environments/handler.go:1082-1107 | 待定 |
| 10 | `[Vaults] mcp_oauth 运行时自动 refresh + refresh_failed 事件` | 有 schema 无执行；token 过期静默失效 | vaults schema 有、执行无 | 待定 |
| 11 | `[Events] model_usage 结构规整（cache_read/cache_creation 嵌套，模型名不靠猜）` | 原样透传 worker 结构，与官方 schema 不对齐 | mapper.go:404-421,713-727 | 待定 |
| 12 | `[Rate Limits] endpoint 级限流（300/1200 RPM）` | 官方明确；OMA 无本地 429 防护，靠上游兜底 | go.mod 无限流依赖 | 待定 |

## 🟡 P2 —— 协议细节（中-小工作量，SDK 兼容性）

| # | 建议标题 | 核心判断 | 证据 | 决策 |
|---|---|---|---|---|
| 13 | `[Events] event_deltas 值集合/数量校验（仅 agent.message/agent.thinking，>100 个 400）` | 官方明确 400 场景；OMA 不校验 | stream_hub.go:170-172 | 待定 |
| 14 | `[Sessions] stop_reason 合法值集合校验` | 任意透传，task_notification 产生非官方值 | mapper.go:303-338,587-590 | 待定 |
| 15 | `[Events] session.updated 事件只含变更字段` | 全量快照非变更集，diff 客户端会重复写 | event_effects.go:126-134 | 待定 |
| 16 | `[Agents] update 允许省略 version（官方无条件更新）` | OMA 强制必填 400，与官方语义不符 | agents/handler.go:310-312 | 待定 |
| 17 | `[Sessions] resources 支持 mcp_server / attachment 类型` | 官方 5 种类型 OMA 只有 3 种 | service_helpers.go:151-213 | 待定 |
| 18 | `[Vaults] mcp_oauth_validate 真实实现` | 桩实现恒 unknown，UI 显示"已验证"实际没验证 | vaults/handler.go:628-666 | 待定（或并入 #121） |
| 19 | `[Beta] 统一 anthropic-beta header 门禁（sessions/memory 弃用 ?beta=true）` | 官方统一 header；OMA 三种不一致 | transport.go:69 / memory/handler.go:170 | 待定 |
| 20 | `[Self-hosted] 提供 worker CLI（对齐官方 ant beta:worker flags）` | 服务端协议齐，无客户端工具 | cmd/ 只有 3 个入口 | 待定 |
| 21 | `[Deployments] wire 形状对齐（memory_stores/vaults/files 顶层字段 + budget）` | 用 resources/vault_ids 替代，官方 SDK 解析差异 | deployments/handler.go | 待定 |
| 22 | `[Environments] 响应不泄露 init_script/environment 空字段` | 响应比请求多两个官方没有的字段 | environments/handler.go:852-867 | 待定 |

## 🟢 明确不推荐开（深挖后排除）

| 项 | 排除原因 |
|---|---|
| 100K 工具输出落盘 | 官方是 sandbox 层行为（Claude Code worker 处理），OMA 不该做 |
| env_var injection_location | #236 已覆盖，PR#242 在做 |
| agent_with_overrides | 依赖 #54 配置校验复用方案，需先与负责人同步 |
| advisor | 依赖运行时，工作量极大，暂缓 |
| **inference_geo** | **开源版不做地域限制（有意设计）**——P0 #1 中排除此项 |

## 补充说明

- 每条证据均为 main @ 5004194 实际代码核对，非文档推测
- P0 #1 已按决策拆分：inference_geo 排除、effort/speed 保留
- 开 issue 时按项目 SOP：`[模块] 中文描述（英文术语）` 标题、讲人话正文（背景/为什么做/官方行为/现状/要做的事/关联/验收）、assign、挂 project Todo
