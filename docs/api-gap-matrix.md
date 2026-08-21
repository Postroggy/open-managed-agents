# OMA vs 官方 Claude API：端点与契约对齐矩阵

> 核对基准：官方 API 参考文档镜像（`docs/api-reference/`，下载于 2026-08-21）
> 核对对象：OMA 当前代码（`internal/` 路由注册，截至 2026-08-21）
> 方法：官方 103 个唯一端点（方法+路径去重）逐条对照 OMA 路由注册

## 一、结论速览

| 项 | 数量 |
|---|---|
| 官方唯一端点（方法+路径去重） | 103 |
| 官方有、OMA 缺 | 23（整组 20 + 单端点 3） |
| OMA 有、官方无（OMA 扩展） | 20 |
| 完全对齐（官方 ∩ OMA） | 80 |
| 契约层面缺口（路径对上、语义缺） | 若干（见第四节） |

> 注：官方端点从 `docs/api-reference/` 各页正文 `**method** \`/path\`` 提取并去重；`api_beta-headers.md` 等通用页的示例端点不计入。

## 二、官方 103 端点分组

| 资源 | 端点数 | 说明 |
|---|---|---|
| Sessions | 19 | create/list/get/update/delete/archive + events(list/send/stream) + threads(list/get/archive/events/stream) + resources(add/get/list/update/delete) |
| Tunnels | 10 | create/get/list/archive/reveal_token/rotate_token + certificates(create/get/list/archive) |
| Deployments | 8 | create/list/get/update/archive/run/pause/unpause |
| Deployment Runs | 2 | list/get |
| Skills | 8 | create/list/get/delete + versions(create/list/get/delete) |
| Environments | 7 | create/list/get/update/delete/archive + work/retrieve |
| Batches | 6 | create/list/retrieve/cancel/delete/results |
| Vaults | 6 | create/list/get/update/delete/archive |
| Memory Stores | 6 | create/list/get/update/delete/archive |
| Agents | 6 | create/list/get/update/archive/versions |
| Dreams | 5 | create/list/get/cancel/archive |
| User Profiles | 5 | create/list/get/update/enrollment_url |
| Files | 5 | upload/list/get_metadata/download/delete |
| Messages | 2 | create/count_tokens |
| Models | 2 | list/retrieve |
| Completions | 1 | create（`POST /v1/complete`，旧端点） |
| Admin Tunnels（旧） | 5 | `/v1/organizations/tunnels`（Deprecated） |

## 三、官方有、OMA 缺（23 个端点）

### 3.1 整组缺失（20 个端点）

| 资源 | 缺失端点 | 官方文档 | 备注 |
|---|---|---|---|
| **Dreams** | `POST/GET /v1/dreams`、`GET /v1/dreams/{id}`、`POST .../cancel`、`POST .../archive`（5） | [Dreams create](https://platform.claude.com/docs/en/api/python/beta/dreams/create) | 官方较新能力面；OMA 有 issue #245（未实现） |
| **User Profiles** | `POST/GET /v1/user_profiles`、`GET/POST .../{id}`、`POST .../enrollment_url`（5） | [User Profiles](https://platform.claude.com/docs/en/api/python/beta/user_profiles) | **疑似 OMA 有意不做**（未确认，待核实） |
| **Tunnels（顶层）** | `POST/GET /v1/tunnels`、`GET/POST .../{id}`、`archive`、`reveal_token`、`rotate_token`、`certificates`×4（10） | [Tunnels](https://platform.claude.com/docs/en/api/python/beta/tunnels) | OMA 只实现旧版 admin `/v1/organizations/tunnels`（已 deprecated） |

### 3.2 单端点缺失（3 个端点）

| 端点 | 官方文档 | 备注 |
|---|---|---|
| `POST /v1/messages/count_tokens` | [count_tokens](https://platform.claude.com/docs/en/api/messages-count-tokens) | 官方 GA |
| `GET /v1/models/{model_id}` | [models retrieve](https://platform.claude.com/docs/en/api/models/retrieve) | OMA 只有 list |
| `POST /v1/complete` | [completions create](https://platform.claude.com/docs/en/api/python/completions/create) | 旧 Completions 端点，**时代弃子，不开 issue 不实现** |

## 四、契约层面缺口（路径对上、语义缺）

| 缺口 | 官方契约 | OMA 现状 | 关联 issue |
|---|---|---|---|
| `model.effort`/`inference_geo` 静默丢弃 | `agent.model` 对象含 `id/speed/effort/inference_geo`，响应回填默认值 | `normalizeModel`（`agents/handler.go:725-772`）只收 `id`+`speed` | **#250**（effort 调研中） |
| `session.usage` 事件缺失 | 官方事件类型表含 `session.usage` | `managedagentsevents/events.go` 的 `CategoryFor` 未收录，落入 unknown | **#249**（PR #268 在补） |
| `GET /v1/sessions` 缺 `prev_page` | 官方 Sessions 端点独有 `prev_page`（支持回退翻页） | `pageResponse` 只有 `Data`+`NextPage` | **#284**（已开） |
| `POST /v1/sessions` 缺 `initial_events`/`budget` | 官方 Create Session 支持 `initial_events`（仅 user.message/user.define_outcome，≤50）与 `budget` | `sessionMutationRequest` 不收这两个字段 | **#285**（已开） |
| beta header 用 `?beta=true` 代替 | 官方所有 managed-agents 端点要求 `anthropic-beta: managed-agents-2026-04-01` header | 仅 sessions 路由校验 `?beta=true`，agents/environments/files 无 beta 检查 | 未开 |
| `event_deltas[]` 缺非法值校验 | 官方要求仅 `agent.message`/`agent.thinking`，非法值 400 | `stream_hub.go` 只识别不校验 | 未开 |
| `system.message` 缺模型能力校验 | 官方仅部分模型支持，否则 400 | OMA 接受但不按模型拒绝 | 未开 |
| `redacted` 内容块拒绝规则 | 官方用户事件带 `redacted` 块返 400 | `validateContentBlocks` 未见相关逻辑 | 未开 |
| 上传文件 `downloadable` 语义 | 官方生成文件可下载（200），上传文件不可下载 | 对齐官方 ✅（`files/handler.go:152`） | 无 |

## 五、OMA 有、官方无（20 个扩展端点）

| 扩展组 | 端点 | 说明 |
|---|---|---|
| Environments Work（6） | `/v1/environments/{id}/work/{work_id}` + `poll/stats/ack/heartbeat/stop` | OMA 自研 E2B 沙箱工作队列协议（官方 self-hosted 是另一套） |
| Vaults Credentials（7） | `/v1/vaults/{id}/credentials/*`（create/list/get/update/archive/delete + mcp_oauth_validate） | OMA 的 vault 凭证管理，官方 vault 页未列 |
| Memory 文档与版本（8） | `/v1/memory_stores/{id}/memories/*` + `memory_versions/*`（含 redact） | OMA 的 memory 文档 CRUD + 版本红action |

> **标注**：这些扩展端点在 OMA 设计文档中**未明确标注「非官方实现」**——它们被当作正常功能文档化，读者无从知道哪些是官方有、哪些是 OMA 自研。建议后续在相关设计文档补标注。
>
> **注意**：`internal/codesessions/upstream_proxy_mitm.go` 的 "tunnel" 指 **CCRv2 的 WebSocket CONNECT 隧道**（内部网络机制），与本节 Tunnels API **无关**，不计入 gap。

## 六、完全对齐（80 个端点）

Agents（6）、Sessions（19，含 events/resources/threads 全子资源）、Environments（7，含 work/retrieve）、Deployments/Runs（10）、Vaults（6）、Memory Stores（6）、Files（5）、Skills（8）、Models list（1）、Messages create（1）、Batches（6）、Admin Tunnels 旧版（5）——路径与方法全部匹配。合计 6+19+7+10+6+6+5+8+1+1+6+5 = **80**。

## 七、已开 issue 索引

| # | Title | 类别 |
|---|---|---|
| #284 | `[Sessions] GET /v1/sessions 缺 prev_page 回退分页，与官方不一致` | Sessions |
| #285 | `[Sessions] POST /v1/sessions 缺 initial_events 与 budget 参数，与官方不一致` | Sessions |
| #286 | `[Messages] 实现 POST /v1/messages/count_tokens 端点（官方 GA）` | Messages |
| #287 | `[Models] 实现 GET /v1/models/{model_id} 单模型查询（官方 GA）` | Models |
| #288 | `[Tunnels] 实现官方顶层 /v1/tunnels API（当前只实现了 deprecated 的 admin 版）` | Tunnels |

> User Profiles 未开 issue（疑似有意不做）；Completions 未开（时代弃子）。

## 八、方法

- 官方端点：`docs/api-reference/` 各页正文提取 `**method** \`/path\``，按 (方法, 路径) 去重
- OMA 路由：`internal/{agents,sessions,environments,files,skills,batches,memory,vaults,webhooks,deployments,models}/handler.go` + `internal/api/server.go` 的 chi 路由注册
- 缺口 = 官方集合 − OMA 集合；扩展 = OMA 集合 − 官方集合
