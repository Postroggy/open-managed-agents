# OMA vs 官方 Claude API：端点与契约对齐矩阵

> 更新日期：2026-08-22
> 文档来源：官方 API 参考文档镜像 `docs/api-reference/`（2026-08-22 全面镜像，384 个文件）；对比旧镜像（2026-08-21，114 个文件）的 103 端点口径，本次升级为 255 端点口径
> 核对对象：OMA 当前代码（`internal/` 路由注册，截至 2026-08-21）
> 依据：官方端点从 `docs/api-reference/` 各页正文 `**method** \`/path\`` 提取并按 (方法, 路径) 去重，共 **255** 个；OMA 端点从 `internal/{agents,sessions,environments,files,memory,batches,models,admin,skills,vaults,webhooks,deployments}/handler.go` 的 chi 路由注册重建完整路径（含 `internal/api/server.go` 挂载前缀），共 **149** 个；参数名归一化为 `{}` 后比对
> 方法：缺口 = 官方集合 − OMA 集合；扩展 = OMA 集合 − 官方集合

## 一、结论速览

| 项 | 数量 |
|---|---|
| 官方唯一端点（方法+路径去重） | 255 |
| 官方有、OMA 缺 | 116（整组 112 + 单端点 4） |
| OMA 有、官方无（OMA 扩展） | 10 |
| 完全对齐（官方 ∩ OMA） | 139 |
| 契约层面缺口（路径对上、语义缺） | 若干（见第四节） |

> 注：官方端点从 `docs/api-reference/` 各页正文 `**method** \`/path\`` 提取并去重；`beta-headers.md` 等通用页的示例端点不计入。

## 二、官方 255 端点分组

| 资源 | 端点数 | 对齐 | 说明 |
|---|---|---|---|
| Admin RBAC/Federation/SA/Spend | 45 | 0 | rbac_groups、rbac_roles、federation_rules、federation_issuers、service_accounts、spend_limits、spend_limit_increase_requests（含 workspaces/service_accounts、rbac_groups/members 等子资源） |
| Compliance | 36 | 0 | activities、apps（chats/projects/sessions/code）、groups、organizations/roles |
| Organizations 基础管理 | 42 | 42 | api_keys、workspaces、users、invites、external_keys、rate_limits、me、usage_report、cost_report、tunnels（deprecated admin 版） |
| Admin Analytics | 11 | 0 | analytics/*（usage_report、cost_report、user_usage、users 等） |
| Tunnels（顶层） | 10 | 0 | 官方新版顶层 `/v1/tunnels`（含 certificates/reveal_token/rotate_token） |
| Sessions | 19 | 19 | create/list/get/update/delete/archive + events(list/send/stream) + resources + threads |
| Environments | 14 | 14 | 含 work 子资源（poll/stats/ack/heartbeat/stop） |
| Memory Stores | 14 | 14 | 含 memories、memory_versions（含 redact） |
| Vaults | 13 | 13 | 含 credentials（含 mcp_oauth_validate） |
| Skills | 9 | 9 | 含 versions（含 content） |
| Messages | 8 | 6 | create、count_tokens + batches 6 个 |
| Deployments | 8 | 8 | 含 run/pause/unpause |
| Agents | 6 | 6 | 含 versions |
| Files | 5 | 5 | upload/list/retrieve_metadata/content/delete |
| Dreams | 5 | 0 | create/list/get/cancel/archive |
| User Profiles | 5 | 0 | create/list/get/update/enrollment_url |
| Deployment Runs | 2 | 2 | list/get |
| Models | 2 | 1 | list + retrieve（缺） |
| Completions | 1 | 0 | `POST /v1/complete`（旧端点） |

## 三、官方有、OMA 缺（116 个端点）

### 3.1 整组缺失（112 个端点）

| 资源 | 缺失端点 | 端点数 | 备注 |
|---|---|---|---|
| **Admin RBAC/Federation/SA/Spend** | `rbac_groups`(CRUD+members)、`rbac_roles`(list/get/permissions)、`federation_issuers`(CRUD+archive)、`federation_rules`(CRUD+archive+workspaces)、`service_accounts`(CRUD+archive+workspaces)、`spend_limits`(CRUD+effective)、`spend_limit_increase_requests`(list/get/approve/deny)、`workspaces/{id}/service_accounts` | 45 | **未开 issue** |
| **Compliance** | `compliance/activities`、`compliance/apps/*`（chats 含 files/generated-files、projects 含 documents/attachments/collaborators、sessions local/remote、code artifacts）、`compliance/groups`、`compliance/organizations`（roles/users/settings） | 36 | **未开 issue** |
| **Admin Analytics** | `organizations/analytics/*`（usage_report、cost_report、user_usage_report、user_cost_report、users、artifacts、connectors、plugins、skills、apps/chat/projects、summaries） | 11 | **未开 issue** |
| **Tunnels（顶层）** | `POST/GET /v1/tunnels`、`GET/POST .../{id}`、`archive`、`reveal_token`、`rotate_token`、`certificates`×4 | 10 | **#288**；OMA 只实现旧版 admin `/v1/organizations/tunnels`（官方 deprecated） |
| **Dreams** | `POST/GET /v1/dreams`、`GET /v1/dreams/{id}`、`POST .../cancel`、`POST .../archive` | 5 | **#245** |
| **User Profiles** | `POST/GET /v1/user_profiles`、`GET/POST .../{id}`、`POST .../enrollment_url` | 5 | **疑似 OMA 有意不做**（未确认，待核实） |

### 3.2 单端点缺失（4 个端点）

| 端点 | 官方文档 | 备注 |
|---|---|---|
| `POST /v1/messages` | [messages create](https://platform.claude.com/docs/en/api/messages/create) | 官方 GA；OMA 未注册（batches 已实现） |
| `POST /v1/messages/count_tokens` | [count_tokens](https://platform.claude.com/docs/en/api/messages/count_tokens) | 官方 GA；**#286** |
| `GET /v1/models/{model_id}` | [models retrieve](https://platform.claude.com/docs/en/api/models/retrieve) | OMA 只有 list；**#287** |
| `POST /v1/complete` | [completions create](https://platform.claude.com/docs/en/api/completions/create) | 旧 Completions 端点，**时代弃子，不开 issue 不实现** |

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
| Webhook 事件域类型 | 官方 `beta/webhooks.md` 定义 **45 种**事件类型（agent×4、deployment×9、deployment_run×3、environment×4、memory_store×3、session×16、vault×5、vault_credential×4），OMA 只有 session/vault 两类 | `webhooks/handler.go` 事件映射仅两类，无发射点 | P1 #8（清单"待定"，未开 issue） |

### 4.1 按最新 API reference 复核 `docs/managed-agents-gap-issues.md`（2026-08-22）

复核基准：`docs/api-reference/`（2026-08-22 全面镜像，384 个文件）为唯一文档依据，`docs/managed-agents-reference/`（2026-08-22 同步更新，27 页）不作为 API 依据。仅记录与 gap 清单不一致的复核结果。

| 原清单项 | 复核结果 |
|---|---|
| P1 #9 `[Environments] init_script` | 最新 `environments/create` 字段仅 `name/config/description/metadata`，无 `init_script`；`deployments/create` 亦无。**依据不成立，不开 issue** |
| P1 #10 `[Vaults] mcp_oauth 运行时自动 refresh` | 最新 `vaults/create` 字段仅 `display_name/metadata`，无 oauth/refresh 相关契约。**依据不成立，不开 issue** |
| P1 #12 `[Rate Limits] endpoint 级限流` | 依据成立：`rate-limits.md` "Managed Agents" 章节明确 Create 端点 300 RPM、Read 端点 1,200 RPM（org 级，与 Messages API 限额独立） |
| P2 #17 `[Sessions] resources 支持 mcp_server / attachment 类型` | 最新 `sessions/resources/retrieve` 的 `type` 仅 `github_repository`/`file`/`memory_store` 三种，无 `mcp_server`/`attachment`。**依据不成立，不开 issue** |
| P2 #19 `[Beta] 统一 anthropic-beta header 门禁` | 依据成立：`beta-headers.md` 明确 `/v1/agents`、`/v1/sessions`、`/v1/environments` 需 `managed-agents-2026-04-01`；memory store 端点用 `agent-memory-2026-07-22` 替换，同时发送两者返回 400 |
| P1 #8 `[Webhooks] 补齐 agent/deployment/deployment_run/environment/memory_store 事件` | 2026-08-22 镜像新增 `beta/webhooks.md`：官方 webhook 事件域类型共 **45 种**（agent ×4、deployment ×9、deployment_run ×3、environment ×4、memory_store ×3、session ×16、vault ×5、vault_credential ×4 等），**远超原清单 5 大类**。依据成立，清单项仍为"待定"（未开 issue），需按新版 45 种事件全量对表 |

其余 P0 已开 issue（#250-#255）及 P1/P2 未开项（P1 #7/#11，P2 #13-#16/#18/#20-#22）与最新 API reference 一致，未另行列出。

## 五、OMA 有、官方无（10 个扩展端点）

| 扩展组 | 端点 | 说明 |
|---|---|---|
| Webhooks CRUD（6） | `/v1/webhooks`（create/list/get/update/delete + regenerate_signing_secret） | OMA 自研；官方 `beta/webhooks.md` 仅定义事件域类型（45 种），未公开订阅端点 |
| Files 扩展（3） | `/v1/files/upload_b64`、`/v1/files/files/{id}/preview`、`/v1/files/files/{id}/thumbnail` | OMA 自研 base64 上传与预览/缩略图 |
| Agents 搜索（1） | `POST /v1/agents:search` | OMA 自研 |

> **口径变化（相对 2026-08-21 旧镜像）**：旧口径下的扩展组——Environments Work（6）、Vaults Credentials（7）、Memory 文档与版本（8）——在新镜像中均有对应官方端点文档（`environments/{id}/work/*`、`vaults/{id}/credentials/*`、`memory_stores/{id}/memories/*`、`memory_versions/*`），已转为**对齐**（见第六节）。
>
> **注意**：`internal/codesessions/upstream_proxy_mitm.go` 的 "tunnel" 指 **CCRv2 的 WebSocket CONNECT 隧道**（内部网络机制），与 Tunnels API **无关**，不计入 gap。

## 六、完全对齐（139 个端点）

Agents（6）、Sessions（19，含 events/resources/threads 全子资源）、Environments（14，含 work 全子资源）、Deployments（8）+ Deployment Runs（2）、Vaults（13，含 credentials 全子资源）、Memory Stores（14，含 memories/memory_versions）、Files（5）、Skills（9，含 versions/content）、Messages batches（6）、Models list（1）、Organizations 基础管理（42）——路径与方法全部匹配。合计 6+19+14+8+2+13+14+5+9+6+1+42 = **139**。

## 七、已开 issue 索引

| # | Title | 类别 |
|---|---|---|
| #284 | `[Sessions] GET /v1/sessions 缺 prev_page 回退分页，与官方不一致` | Sessions |
| #285 | `[Sessions] POST /v1/sessions 缺 initial_events 与 budget 参数，与官方不一致` | Sessions |
| #286 | `[Messages] 实现 POST /v1/messages/count_tokens 端点（官方 GA）` | Messages |
| #287 | `[Models] 实现 GET /v1/models/{model_id} 单模型查询（官方 GA）` | Models |
| #288 | `[Tunnels] 实现官方顶层 /v1/tunnels API（当前只实现了 deprecated 的 admin 版）` | Tunnels |
| #245 | `[Dreams] 实现 Dreams 能力面后端（memory store 精炼 job，dreaming-2026-04-21）` | Dreams |

> User Profiles 未开 issue（疑似有意不做）；Completions 未开（时代弃子）；Admin RBAC/Federation/SA/Spend（45）、Compliance（36）、Admin Analytics（11）未开 issue，为 org 管理面最大缺口。

## 八、方法

- 官方端点：`docs/api-reference/` 各页正文提取 `**method** \`/path\``，按 (方法, 路径) 去重
- OMA 路由：`internal/{agents,sessions,environments,files,skills,batches,memory,vaults,webhooks,deployments,models}/handler.go` 的 chi 路由注册 + `internal/api/server.go` 的挂载前缀，重建完整路径
- 参数名归一化（`{agent_id}` → `{}`）后比对；尾斜杠不参与比对
- 缺口 = 官方集合 − OMA 集合；扩展 = OMA 集合 − 官方集合
