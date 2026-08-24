# PR #289（llmproviders）BYOK 实现评审

> 分支: `docs/byok-review`（基于 `origin/main` 7ebca56）
> 评审对象: PR #289「支持工作区自定义 LLM 模型配置」（jh0904，2026-08-22 合并）
> 对比基准: PR #155「实现持久化 BYOK 模型目录与动态模型选择」（OPEN）
> 评审方法: 基于 origin/main 实际代码（commit `7ebca56`）逐文件核查，所有结论附 `文件:行号` 证据；关键发现经独立验证（grep/codegraph 交叉确认）。
> 整理日期: 2026-08-24

---

## 一、评审对象

### 1.1 #289 做了什么

将 BYOK 实现为「工作区级 LLM Provider 手动配置」：

- **数据模型**：`llm_providers` 表（migration 00053）——provider（base_url + 信封加密 api_key）+ 模型目录（`model_ids jsonb` 数组）压在一张工作区级表里
- **配置方式**：管理员为每个工作区手动创建 provider（填 base_url + api_key + model_ids），可点「获取模型列表」从上游 `/v1/models` 拉取填充，可点「同步」合并新模型
- **请求路由**：`/v1/messages` 等 4 个调用点按请求体 model_id 匹配 provider（`llmproviders.Resolve`），未配置则报错
- **设计文档**：`docs/design/fe/llm-models-page.md`——"LLM 模型页是工作区的模型目录"（从前端 UX 倒推的设计）

### 1.2 评审范围

- 功能正确性、API 合同兼容性（Anthropic Models/Messages API）、安全、数据完整性、企业级治理、与仓库既有惯例的符合度
- 对照基准：Anthropic Managed Agents 官方设计 + 仓库既有先例（`internal/vaults`/`networkpolicy`/`secrets`）

---

## 二、总体评价

### 2.1 一句话结论

**#289 解决了"能不能配置 BYOK"（能用），但把模型目录从"系统级动态发现"退化成了"每工作区手动维护的 ID 清单"**——能力字段、自动同步、分页、stale 全部丢失，且存在 SSRF、并发数据完整性、batch TOCTOU 等严重问题。**#62（AI Gateway）的核心目标（同步/快照/stale/能力合同）并未实现**，是"能用"而非"用得好"。

### 2.2 问题统计

| 严重度 | 数量 | 主题 |
|--------|------|------|
| 🔴 严重 | 5 | SSRF、并发 sync 无版本、batch TOCTOU、capabilities 空、凭据非引用 |
| 🟡 中 | 13 | 手动同步、无分页、硬截断、跨 provider 冲突、key 明文、错误合同、双 header、双代理、零审计、请求层选模型、成本缺位、删除路径、目录三形态 |
| 🟢 低 | 7 | modelmapping 删除、无超时、body 无上限、lastFour、preview key、migration 混入、整块缓冲 |

> 注：评审过程中有两处曾被误判为问题、经核实属于设计行为的条目（"未配置即不可用"符合 OMA 定位、"每请求解密"是贯穿性安全设计），已在问题清单中剔除，详见 3.4 与第七章修正记录。

---

## 三、问题清单

### 3.1 🔴 严重（5 项）

#### S1. SSRF：出站地址无网络层校验（安全）

- **代码**：仓库已有 `networkpolicy.PublicAddress`（拒绝 loopback/私网/链路本地/CGNAT），但 grep 全仓仅 2 处使用（都在 `internal/codesessions/`）——**llmproviders 新链路零引用**：
  - `ValidateBaseURL`（`llmproviders/providers.go:105-112`）只查 scheme/hostname/userinfo，**接受 `http://127.0.0.1:5432`、`http://169.254.169.254`**
  - `NewHTTPClient`（`providers.go:128-142`）`CheckRedirect` 只 `ErrUseLastResponse`，不校验重定向目标
  - `ListUpstreamModels`（`upstream_models.go:27-53`）直接 `client.Do`，无 DNS 预检/拨号后复核（**DNS rebinding 敞口**）
- **更尖锐**：`providers_test.go:17-19` **主动把 `127.0.0.1`/`localhost`/`10.0.0.1` 锁定为合法用例**——接受内网是测试固化的明确意图
- **后果**：任何有控制台访问权限的账号可把 `base_url` 指向内网/元数据服务（`169.254.169.254`），preview/sync/messages 变成 **SSRF 中继**，api_key 明文发送到攻击者控制的地址
- **修复**：`NewHTTPClient` 自定义 `DialContext`（拨号后校验 netip + 锁定连接 IP），`CheckRedirect` 对 Location 跑同一校验；复用 `networkpolicy.PublicAddress`

#### S2. 并发 sync 读取-归并-写入无版本比较（数据完整性）

- **代码**：`syncConsoleLLMProviderModels`（`console_llm_provider_models.go:77-126`）时序为：`GetLLMProvider`（无锁读）→ `excludeConfiguredModels`（无锁读）→ `MergeModelIDs` → `UpdateLLMProvider`。`UpdateLLMProvider`（`db/llm_providers.go:81-105`）虽有 `LockWorkspace`，但**无 `updated_at` 版本条件**（`llm_provider_mapper.xml` `UpdateByExternalID` WHERE 无 updated_at）
- **后果**：两次 sync/编辑交错时基于旧快照归并——**上游删掉的模型永不清除（只增不减）**，并发窗口静默丢失新模型
- **修复**：乐观锁（读 `updated_at`，写入 `WHERE updated_at = ?`）或把"读当前快照 + 归并 + 写"整体放进同一事务

#### S3. batch 创建校验与执行 Resolve 存在 TOCTOU（正确性）

- **代码**：`batches/handler.go:206-230` `validateConfiguredModels` 创建时校验模型存在；但 `ExpiresAt = now + 24h`（`:183`），worker 执行时 `batches/upstream.go:50-66` 再次 `Resolve`——**创建时模型存在 ≠ 24h 后执行时存在**
- **错误分类退化**：同一个「模型不存在/未配置」错误，messages 路径是 400/503（`messages/errors.go:39-48`），batches 路径折叠成单一 `api_error`（`erroredResult`）——**用户无法区分「模型没配（4xx 可自行修复）」与「provider 挂了（5xx）」**
- **修复**：batch 执行期错误按类型拆分；或创建时校验与执行期绑定（钉住 provider 快照）

#### S4. capabilities 空对象（API 合同）

- **代码**：`models/catalog.go:15-28` `modelResponses()`：`Capabilities *struct{}`（**空结构体指针**）、`MaxInputTokens/MaxTokens *int`（nil）；`parseUpstreamModelIDs`（`upstream_models.go:70-94`）只提取 `.ID`，capability 全部丢弃
- **后果**：`/v1/models` 返回 `capabilities: {}`——SDK 客户端以为"模型没有任何能力"而非"能力未知"（**对合同撒谎**，修复要动 API 面无法悄悄改）；前端无法展示能力
- **能力缺口**：应支持 `Capabilities map[string]CapabilityValue` 开放字段无损透传 + 类型化读取

#### S5. 凭据是值不是引用（无 Vault 对应物，企业治理）

- **代码**：`api_key` 是 provider 行内字段（`db/llm_providers.go:24-38` 含 SecretEnvelope）；无凭据独立实体、无轮换版本、无 create-only 语义；`api_key_last4` 暴露给所有能看列表的人（`console_llm_providers.go:33-38`）
- **对照**：仓库已有 `internal/vaults`（凭据资源化 + create-only + webhook 审计的完整实现）+ `internal/secrets` 信封加密——#289 **用了 secrets 的形，绕开了 vaults 的魂**
- **后果**：轮换 = 全量重写 provider；审计 = 无；企业 BYOK 场景（"这个 key 被谁用过、什么时候轮换的"）无法表达
- **修复**：凭据独立实体（create-only、只进不出、可轮换版本化），provider 只存 `credential_id` 引用

### 3.2 🟡 中（13 项）

#### M1. 模型无法自动获取，必须手动同步（可观察性）

- **代码**：`ListUpstreamModels` 仅 2 处调用（`console_llm_provider_models.go:51` preview、`:95` sync）——无定时刷新、无启动同步、无后台 worker
- **补充**：手动同步是**"表单思维"的产品定位**（非技术妥协）——设计文档明说"获取结果与表单内已有的非空模型 ID **合并、去重，不覆盖手工输入**"，即把模型目录当"管理员维护的配置表单"而非"上游模型的投影"。隐含好处是管理员审核；企业级正确做法是自动同步 + 变更审计
- **能力缺口**：应有 5 分钟定时刷新 + 启动自动刷新 + 手动触发三通道
- **修正记录**：原判 🔴 严重，2026-08-24 降为 🟡 中（表单思维的产品定位，非技术妥协）

#### M2. 无 stale / 不可用状态（可观察性）

- **代码**：`llm_providers` 表只有 `updated_at`，无 sync 状态、无 stale 标记、无最后成功时间（migration 00053）
- **后果**：无法判断目录新鲜度；同步失败不落库（直接 502）——"目录新鲜度"概念不存在
- **能力缺口**：应有 `SaveSuccess` + stale 标记 + 首次失败明确不可用

#### M3. 无分页（API 合同）

- **代码**：`parseUpstreamModelIDs`（`upstream_models.go:70-94`）只读单页 `data`，**无 `has_more`/`after_id` 字段**；`/v1/models` `has_more` 恒 false（`models/handler.go:38-57`）、无 limit/before_id/after_id
- **后果**：模型超过单页上限时**静默丢失**（管理员以为同步成功实际只拿一半）；SDK 分页合同不完整
- **能力缺口**：应有 `Page{Models, HasMore, LastID}` 分页 + 游标 + 页上限保护

#### M4. 模型上限硬截断，无提示（产品诚实性）

- **代码**：`MergeModelIDs(existing, incoming, maxLLMProviderModels)`（`upstream_models.go:96-118`）到 max（100）即停静默丢弃；**preview 也截断**（`console_llm_provider_models.go:55-60`）——创建时预览就不完整
- **后果**：模型超上限时无任何提示；管理员以为同步成功其实丢了
- **能力缺口**：分页不完整时应直接报错不发布（`errIncompletePage` 语义）

#### M5. 多 provider 无法共享同一模型（领域建模）

- **代码**：冲突在**写入层**即被拦截——`validateLLMProviderModelOwnership`（`db/llm_providers.go:128-153`）Create/Update 事务内发现模型被其他 provider 占用 → 409 `model_conflict`（`console_llm_provider_errors.go:54-64`）；sync 用 `excludeConfiguredModels` 静默跳过；运行时 `ErrAmbiguousModel`（`providers.go:19,53,87`）因此几乎不可达（防御性残留）
- **后果**：两个 provider 配置相同模型 → **第二个根本建不出来**（409）——系统强制模型全局唯一，模型"属于"第一个占用者
- **修复方向**：模型独立实体 + provider_models 多对多（见第五章）
- **修正记录**：原判"运行时全链路报错"，对抗性自检修正为"写入层 409 拦截 + 运行时几乎不可达"

#### M6. 密钥明文过 HTTP 多次往返（密钥哲学）

- **代码**：`llmProviderRequest.APIKey *string`（`console_llm_providers.go:33,84-88`）创建/更新每次明文传；preview 用前端明文 key 打上游（`previewLLMProviderModelsRequest{BaseURL, APIKey}`，`console_llm_provider_models.go:13-15,34`）；无 TLS 要求（compose caddy 80 端口）
- **后果**：密钥在浏览器↔后端明文传输、前端内存持有明文；preview 的 key 不落库（语义不透明：用户以为保存了其实没有）
- **能力缺口**：key 应只在服务端 config，前端永远不碰

#### M7. 错误合同混乱（API 哲学）

- **三套错误映射**：`providerResolveError`（messages，httpapi 结构）/ `writeProxyProviderError`（proxy，`proxy_error`）/ `erroredResult`（batches，"api_error"）（`messages/errors.go:39-48`；`platform_proxy.go:149-169`；`batches/upstream.go:54-96,160`）
- **错误码是临时键**：13 个 `llmProviderCode*` 常量（`console_llm_provider_errors.go:11-23`）既是 API 合同又是前端 i18n 字典键（`catalog.ts:8-38`）；`Error` 字段两套取值（`invalid_request` vs `permission_denied`），违反仓库自身 `httpapi/app_error.go:87` 的 403→`permission_error` 合同；`model_conflict` 走特例（直接 writeJSON 带额外字段）；**13 个码零测试覆盖**
- **workbench 字符串匹配**：`writeWorkbenchInferenceError`（`console_platform_workbench.go:706-719`）用 `strings.Contains(message, ...)` 匹配错误**文案**而非 sentinel，default 落 500——任何文案修改都会静默改变分类
- **修复**：统一到 sentinel + 单一错误映射层，管理面错误码与 `/v1/*` 同族

#### M8. 双 header 注入 + header 黑名单透传面（最小透传面）

- **代码**：`ApplyAPIKey`（`request.go:26-29`）同时注入 `X-Api-Key` + `Authorization: Bearer`；`sanitizedRequestHeaders`（`messages/handler.go:160-166`）黑名单删除（16 项），**黑名单外自定义 header 原样透传**；同 PR 的 `/proxy/v1/messages` 用白名单（4 项，`platform_proxy.go:124-133`）——**同 PR 内不一致**
- **后果**：客户端可注入任意自定义 header 到达第三方网关；双注入在严格网关上行为不确定、日志泄露面加倍
- **修复**：白名单化（只放行 Content-Type/Anthropic-Version/Accept 等）

#### M9. 两套 messages 代理并存（一致性）

- **代码**：`/v1/messages`（`internal/messages/handler.go`：黑名单 header + httpapi 错误 + Timeout:0）vs `/proxy/v1/messages`（`internal/platformapi/platform_proxy.go:30`：白名单 header + proxy_error + Timeout:0）vs batches（`internal/batches/upstream.go`：erroredResult + 10min 超时）——**同一 Anthropic 兼容面三条代理路径**
- **后果**：同一合同的行为（header 策略、错误结构、超时语义）互不一致

#### M10. 配置变更零审计 + 不可回滚（可追溯性）

- **代码**：CRUD/sync 无 audit 事件（grep 零命中）；表无 created_by/updated_by/last_synced_at（migration 00053）；`UpdateByExternalID` 无版本条件
- **后果**：密钥轮换、base_url 指向变更（悄悄换供应商）无法追溯；无回滚 → 事故后无法止血回切
- **对照**：仓库 `vault_credentials` 表（migration 00001:683-715）有 `created_by_api_key_id`/`archived_at`/`deleted_at`——#289 抄了加密列没抄审计列

#### M11. 模型选择在请求层而非配置层（治理）

- **代码**：4 个 `Resolve` 调用点全部按请求体 model_id 每请求解析（messages/proxy/batches/workbench）；agent 的 model 字段"创建时校验、执行时不绑定"（`agents/handler.go:773-793` 校验 vs `providers.go:41` 执行）——"配置层想固定、执行层不强制"半实现
- **后果**：客户端可每请求切换 model 绕过任何管理意图；"这个 agent 固定用 X 模型"无法表达
- **对照 Anthropic**：模型是 Agent 配置属性，override 后固定；修复方向是模型选择上移到配置层

#### M12. 成本治理与用量可观测缺位（企业计费前提）

- **代码**：messages 转发不解析 usage、不记账；PR 无 usage/billing/webhook 变更；仓库有 `internal/webhooks` 基础设施未接
- **后果**：无"这个 workspace 调了网关多少次/多少 token"、无成本封顶、无 provider 用量分布——**messages 代理是唯一算得清账的地方，但它不记账**
- **对照 Anthropic**：budget 硬上限 → `budget_reached` 暂停

#### M13. 删除路径的完整性缺失（数据完整性）

- **删除零引用检查**：`deleteConsoleLLMProvider`（`console_llm_providers.go:181-203`）→ `DeleteByExternalID` 硬 DELETE，**无任何"该 provider 的模型是否仍被 agent/batch 引用"检查**——创建时严格（`normalizeConfiguredModel`）、删除时放任，已创建 agent 的 model 字段成为悬空引用，界面无任何提示
- **Delete 不走锁**：Create/Update 都有 `LockWorkspace`（`db/llm_providers.go:47,98`），`DeleteLLMProvider`（`:117`）直接删无锁——并发下"A 删 provider X"与"B 创建引用 X 模型"交错时，模型唯一性校验被绕过
- **修复**：删除前引用检查（有依赖则提示/阻止）；Delete 纳入锁协议

#### M14. "模型目录"一个概念三种形态（单一事实来源）

- **代码**：DB `model_ids jsonb`（`llm_providers` 表）/ 前端 `catalogModels` 归组（`catalog.ts:37-43`）/ `/v1/models` `ListModelIDs` 全扫（`providers.go:82-100`）——**三个都叫"目录"，互不保证一致**；sync 有自己的"目录"（上游拉取）、执行有自己的"目录"（Resolve 匹配）、展示有自己的"目录"（归组）
- **哲学**：目录应该在服务端是唯一权威，前端和 /v1 只是投影

### 3.3 🟢 低（7 项）

#### L1. modelmapping 被删除（设计取舍，非缺陷）

- **代码**：`internal/modelmapping` 包整体删除（`modelmapping.go` -43 + 测试 -48）+ 前端 `modelMappings.ts` 删除（-12）；`create-dialog-api.ts` displayName 从 `display_name || id` 退化为 `id`
- **判定**：modelmapping 是 #152 由**同一作者**添加、#289 删除——主动设计演进。官方 Managed Agents 文档**没有 model_mappings 概念**（OMA 自己的扩展），删除不影响兼容性；"模型目录 = 上游真实 ID"（设计文档明说"页面不映射、不改写"）设计下确实无存在意义
- **代价**：丢失"展示名 ≠ 路由名"灵活性；若将来需要映射需重新引入
- **修正记录**：原判 🟡 中（缺陷），2026-08-24 改为 🟢 低（设计取舍）

#### L2. messages 转发 client 无任何超时（运行时健壮性）

- **代码**：`NewHandler`（`messages/handler.go:66-68`）`NewHTTPClient(0)`——`Timeout: 0`；preview/sync 15s（`console_llm_providers.go:44`）；batches 10min（`batches/upstream.go:39-46`）——**同函数三种语义无注释**
- **后果**：上游挂死（不响应不关连接）时客户端无限等待；应设 `ResponseHeaderTimeout`/`IdleConnTimeout`

#### L3. llm_providers CRUD 请求体无大小上限（资源上限）

- **代码**：`readRequiredJSON`（`platformapi/support.go:31-37`）无 `MaxBytesReader`（messages 有 32MB）
- **后果**：恶意/异常大 body 整体解析进内存

#### L4. `lastFour` 短 key 返回完整 key + `HasAPIKey` 恒 true（防御性）

- **代码**：`lastFour`（`console_llm_providers.go:348-352`）`len(runes) <= 4` 返回**完整字符串**；`formatLLMProvider`（`:334`）`HasAPIKey: true` 硬编码
- **后果**：配置 ≤4 位"key"时 `api_key_last4` 完整暴露；`has_api_key` 语义不准确

#### L5. preview key 不落库仅即时使用（产品诚实性）

- **代码**：`previewConsoleLLMProviderModels`（`console_llm_provider_models.go:34-41`）——用户以为保存了其实没有

#### L6. migration 00053 混入跨资源 schema 变更（单职责）

- **代码**：`message_batches.organization_uuid` 变更（00053:32-42）混进 llm_providers migration；backfill 依赖"每个 batch 有 workspace"，孤儿行导致**生产升级直接失败**（有 `raise exception` 保护）
- **对照**：仓库其他 migration 单表单职责（如 `00049` 只动 vault_credentials）

#### L7. 请求体整块缓冲，流式只在响应侧（流式语义）

- **代码**：`readRequestModel`（`messages/handler.go:100-104`）`io.ReadAll` 完整读入再 `bytes.NewReader` 转发——客户端必须完整上传后才开始转发，首 token 延迟 = 上传时间；"流式"只在响应侧成立
- **权衡**：整块读是为了"转发前校验顶层 model 且不重写 body"——正确性换性能的取舍，无安全风险

### 3.4 已排除的误判项（经核实属设计行为，非缺陷）

> 评审过程中以下两条曾被误判为问题，经代码与官方文档核实属于设计行为，不计入问题清单。记录在此以避免后续重复误判。

#### D1. 未配置 provider 即不可用（符合 OMA 定位）

- **代码**：`normalizeConfiguredModel`（`agents/handler.go:773-793`）→ 未配置 → `apperr.Unavailable`；`messages/errors.go:43-44` → `providerNotConfiguredError()`
- **判定**：**不是缺陷**。`docs/en/compatibility.mdx` 明确："Model IDs are sent unchanged. OMA does not provide Claude-name aliases, mappings, **or a default upstream**."——OMA 定位是"self-hosted agent control plane"（自托管控制面），BYOK 是核心而非例外，"默认 Claude 模型"本来就不存在。README.cn 也确认"本地优先 Managed Agents API 服务 + 兼容 SDK 的 /v1 API 表面"
- **唯一遗留改进点**：升级路径——旧版（有 `anthropic_upstream` 默认）升到新版（无默认）的部署需要显式配置 provider，**升级迁移提示是否充分**（可辩护的改进，非缺陷）
- **修正记录**：原判 🔴 严重（破坏性变更），2026-08-24 改为设计行为

#### D2. 每请求解密 secrets（贯穿性安全设计）

- **代码**：`Resolve`（`providers.go:41-74`）每请求 `ListLLMProviders` + `secretService.Open`（`defer clear(plaintext)`）
- **判定**：**不是缺陷**。与 `internal/vaults` 的"明文 token 绝不缓存（Plaintext tokens are never cached）"一致——明文生命周期 = 单个请求，用完即清，安全优先于性能
- **修正记录**：原判 🟢 低（缺陷），2026-08-24 移除

---

## 四、根源分析（为什么会出现这些问题）

### 4.1 核心建模错误：模型归属 provider

**他的模型**：模型 = provider 的属性（`llm_providers.model_ids` 内嵌 jsonb）
**正确模型**：模型 = 独立实体，provider 只是"模型的可选路由目标"

- **Agent 引用的是 model_id，不是 provider**——把模型挂在 provider 下 = 让"模型身份"依赖"供应商身份"
- **重名 model_id 是必然的**（多供应商都提供同一模型），#289 的处理是写入层 409 拒绝 + sync 静默跳过 + 运行时 ErrAmbiguousModel（几乎不可达）——**同一个问题堵了三处，三处都没解决根本问题**
- **最有力的证据**：`validateLLMProviderModelOwnership` + `excludeConfiguredModels` 专门处理"模型已被别的 provider 占用"——如果模型是独立实体，根本不需要这些函数

### 4.2 表设计错误：一张表扛三个职责

`llm_providers` 同时是：凭据存储（ciphertext/nonce/wrapped_dek 信封列）+ 模型目录（model_ids jsonb）+ 路由配置（base_url）——三个生命周期完全不同的实体被压进一张工作区级表。

问题分层：
1. **模型无法独立存在**（身份）：删 provider → 模型全消失；迁移 → 重配
2. **jsonb 数组无法承载元数据**（能力）：无 capabilities/max_tokens/版本/顺序语义
3. **查询被迫全表拉取 + 内存匹配**（性能/正确性）：`Resolve` 遍历 provider + `containsModel`；无法 SQL 索引；无 DB 层约束

### 4.3 项目惯例违背

| 惯例 | 项目标准 | llm_providers |
|------|---------|---------------|
| `id bigint identity` | ✅ 所有表 | ✅ |
| `uuid uuid` 业务标识 | ✅ 所有表 | ✅ |
| `external_id` | ⚠️ 仅对外 API 暴露的资源表需要 | ✅ 有（但不需要：内部配置，走 `/api/console/` 管理面） |
| `deleted_at` 软删除 | ✅ 多数资源表 | ❌ 无 |
| 资源独立成表 | ✅（skills + skill_versions） | ❌ model_ids 内嵌 |
| 多对多用关联表 | ✅（workspace_members） | ❌ 无关联表 |
| 加密列的审计列 | ✅（vault_credentials 有 created_by/archived_at/deleted_at） | ❌ 只抄加密列不抄审计列 |

### 4.4 过程性原因（为什么质量低）

> 不是能力归因，而是**过程归因**——从代码形态可还原出四个可复现的决策模式：

1. **产品先行、建模滞后（最根本）**：设计文档第一句是页面文案（"LLM 模型页是工作区的模型目录"），不是系统设计；先画页面再倒推表结构，`model_ids jsonb` 的存在理由就是"前端好按 provider 分组展示"——**实体建模跟着界面走，而不是跟着领域走**
2. **防御性编程替代正确建模**：冲突处理在三个层各堵一次（409 + 跳过 + ErrAmbiguousModel），**防御堆得越多越说明建模错了**——正确建模让冲突在概念上不存在
3. **选择性复用：捡便宜的，不捡贵的**：抄了 `internal/secrets`（现成，便宜）；绕开 `internal/vaults`/`networkpolicy`/`webhooks`（要理解再接线，费事）——**"借鉴了加密层的形，没借鉴资源层的魂"**
4. **没吃透 Anthropic API 合同**：capabilities 空对象、has_more 恒 false、13 个自造错误码——"读了接口文档的参数表，没读合同的语义"；**对合同撒谎是评审里最不能忍的一类**（修复要动 API 面）

---

## 五、修复建议

### 5.1 优雅方案：模型独立实体 + 多对多关联

```sql
create table models (
    id bigint generated always as identity,
    uuid uuid not null default gen_random_uuid(),
    model_id text not null,              -- 业务 ID，唯一
    capabilities jsonb,                  -- 能力字段
    max_input_tokens int, max_tokens int,
    created_at timestamptz not null default now(),
    updated_at timestamptz not null default now(),
    deleted_at timestamptz,
    constraint models_model_id_key unique (model_id)
);
create table model_providers (
    id bigint generated always as identity,
    uuid uuid not null default gen_random_uuid(),
    name text not null, base_url text not null,
    -- secrets 信封加密列
    ...
);
create table provider_models (           -- 多对多，带路由优先级
    id bigint generated always as identity,
    uuid uuid not null default gen_random_uuid(),
    provider_uuid uuid not null, model_uuid uuid not null,
    priority int default 0,
    constraint provider_models_provider_model_key unique (provider_uuid, model_uuid)
);
```

- 模型有独立身份（可被多个 provider 提供、可承载能力/元数据）
- 重名冲突从"报错"变"多对多天然支持"
- priority 列优雅解决"同一模型多 provider 选谁"

### 5.2 企业级分层：配置在组织级，选择在工作区级

```
🏢 Organization（组织级，一次配好）
   ├── model_providers（供应商 + 密钥 + 模型同步）
   └── 📁 Workspace（只做选择）
         ├── enabled_provider_ids（可空 = 组织默认）
         ├── default_model_id（可空 = 组织默认）
         └── 可覆盖的模型白名单
```

| 维度 | #289（workspace 放 provider） | 企业级（组织配置 + 工作区选择） |
|------|------------------------------|------------------------------|
| 配置次数 | N 工作区 × N provider | 1 次（组织级） |
| 密钥管理 | N 份副本 | 1 份 |
| 多供应商 | 冲突 | 组织级多 provider，工作区选择 |
| 默认模型 | 无概念 | 组织级默认 + 工作区覆盖 |
| 权限 | 工作区管理员各自配 | 组织管理员统一管，工作区只选 |

### 5.3 优先级与工作量

| 档位 | 内容 | 额外工作量 |
|------|------|-----------|
| **止血**（能合进主干） | S1 SSRF 接线 networkpolicy 2-3d；S2 乐观锁 2d；S4 capabilities 透传 3d；M 系列低成本项约 5d | **约 12-15 人日**（2-3 周） |
| **质量**（符合仓库哲学） | 止血档 + M5 模型独立实体 5-8d；M10 审计事件 2-3d；M1 自动同步 worker + 分页 + stale 3-5d；M6 密钥流程重构 2d | **再加 12-18 人日**（累计 5-7 周） |
| **架构**（企业级） | 质量档 + S5 凭据引用化 5-10d；M11 模型选择上移 5-8d；M12 usage 记账 + budget 5-10d | **累计 35-50 人日**（2-3 个月单人） |

**工作量参照**：架构档里的"目录快照 + stale + advisory lock + 分页 + 能力透传 + 错误分级"这些能力，单人即可在数周内完成——**架构档的增量成本主要是"想清楚"，不是"写出来"**。表设计对了，M3/M5/M14/S4 一大半会消失。

**建错表的纯额外成本**：`validateLLMProviderModelOwnership`（31 行）+ `excludeConfiguredModels`（21 行）+ 前端 `skipped_model_ids` toast——**同一件事在三个层各实现一遍（50+ 行），是"建错表"的纯额外成本**；正常 ER 建模只多 1-2 天工作量，却能消掉至少 8-10 条意见。

---

## 六、对照参考

### 6.1 对照 Anthropic Managed Agents

| 概念 | Anthropic Managed Agents | OMA #289 现状 |
|------|--------------------------|-------------|
| 模型 | **Agent 配置的属性**；会话可 override，override 后固定 | agent 自由文本 model，messages 时按 model_id 匹配（M11） |
| 凭据 | **Vault：create-only，引用式注入**（vault_ids → 环境变量，自动刷新） | api_key 明文过 HTTP + 服务端信封加密（S5/M6） |
| 配置管理 | 控制面 CLI + **版本化 YAML**（不可变版本、可回滚） | DB 行直接改（无版本、无回滚、无审计，M10） |
| 成本治理 | **预算即一等公民**（budget 硬上限 → budget_reached 暂停） | 无（M12） |
| 可观测性 | 每次会话可追踪（trace URL + usage/timing） | 无 trace、无 usage 聚合（M12） |
| 错误 | **错误即事件**（session.error 事件） | 上游错误裸转发（M7） |
| 生命周期 | rescheduling → running ↔ idle → terminated | work 状态机（无预算暂停） |

**7 项缺口**：⑤-1 凭据是值不是引用（🔴）、⑤-2 模型选择在请求层（🔴）、⑤-3 成本治理缺位（🟡）、⑤-4 配置不可回滚（🟡）、⑤-5 错误即响应非事件（🟢）、⑤-6 目录与路由职责混用（🟡）、⑤-7 无配额/继承治理（🟢）。

**总评**：OMA #289 最大的结构性差异是**"模型/凭据/成本"三个企业级维度全在"值"层而不在"资源"层**——凭据是行内字段、模型是字符串数组、成本是零。企业版 BYOK 必然要求：**凭据资源化（Vault 式）、模型配置化（Agent 属性式）、用量记账化（budget/trace 式）**。

**最扎心的发现**：仓库里已经有一个 Vault 了（`internal/vaults`，凭据资源化 + create-only + webhook 审计的完整实现），BYOK 却没用它，而是把 api_key 塞进 provider 行里重造了半个轮子——**"借鉴了加密层的形，没借鉴资源层的魂"**。

### 6.2 #289 模型目录的 8 项缺失能力

**按企业级模型目录应有的能力逐项核查，以下 8 项 #289 全部不具备**：

| # | 应有的能力 | #289 现状 |
|---|-----------|----------|
| 1 | 持久化模型目录快照 + stale 状态 | 静态数组，无快照/stale（`llm_providers.model_ids jsonb`，migration 00053） |
| 2 | 自动定时刷新 + 启动刷新 + 手动三通道 | 仅手动 sync（`console_llm_provider_models.go:65`） |
| 3 | PostgreSQL advisory lock 多实例协调 | 无锁（`UpdateLLMProvider` 无版本条件，S2） |
| 4 | 分页拉取（游标 + 页上限） | 单页解析，无 `has_more`/`after_id`（`upstream_models.go:70-94`，M3） |
| 5 | capability 完整透传 + 类型化读取 | `capabilities: {}` 空对象（`models/catalog.go:15-28`，S4） |
| 6 | `GET /v1/models/{model_id}` + limit/before_id/after_id | 只有 `GET /`（`models/handler.go:38-57`） |
| 7 | 明确的错误语义分级 | 有 sentinel 但无 stale 语义（`providers.go:16-22`） |
| 8 | 前端展示目录新鲜度 + 手动刷新 | 无新鲜度概念（页面仅"获取/同步"按钮） |

**结论**：#289 是"手动配置模型清单"，不是"自动发现的模型目录"——上表 8 项缺失正是 #62（AI Gateway）的核心目标（同步/快照/stale/能力合同），**#289 合并 ≠ #62 完成**。

---

## 七、验证记录

### 7.1 关键发现的独立验证（2026-08-24）

对 `origin/main`（7ebca56）实际代码逐条独立验证，8 项关键发现全部确认存在：

| # | 发现 | 验证 | 证据锚点 |
|---|------|------|---------|
| B1 | batch TOCTOU | ✅ | `batches/handler.go:212` 校验 + `:182` 24h 过期 vs `upstream.go:56` 执行 Resolve |
| ① | SSRF | ✅ | `ValidateBaseURL`（`providers.go:105-113`）无私有 IP 校验 |
| ② | 并发 sync 无版本 | ✅ | `UpdateLLMProvider`（`db/llm_providers.go:90`）无版本条件 |
| ⑥ | 双 header 注入 | ✅ | `ApplyAPIKey`（`request.go:22-25`）双 Set |
| ⑬ | lastFour 短 key | ✅ | `console_llm_providers.go:342-348` len≤4 返回完整串 |
| B3 | Delete 不走锁 | ✅ | Create/Update 有 LockWorkspace（`:47,98`），Delete（`:117`）无 |
| B5 | migration 混入 | ✅ | `message_batches` 变更（00053:32-42） |
| ⑧ | 两套代理 | ✅ | `batches/upstream.go` + `messages/handler.go` 各有 v1/messages |

### 7.2 严重度修正记录

| 条目 | 原判定 | 修正后 | 原因 |
|------|--------|--------|------|
| #10 每请求解密 | 🟢 低（缺陷） | **移除**（设计行为） | 与 vaults"明文不缓存"一致，OMA 贯穿性安全设计 |
| #9 modelmapping | 🟡 中（缺陷） | 🟢 低（设计取舍） | 同作者 #152 添加 #289 删除；官方无此概念 |
| #2 手动同步 | 🔴 严重 | 🟡 中 | 表单思维的产品定位，非技术妥协；隐含审核好处 |
| #11 未配置即不可用 | 🔴 严重（缺陷） | **设计行为** | `compatibility.mdx` 明确 "no default upstream"；OMA 定位就是 BYOK |
| #8 跨 provider 冲突 | 运行时报错 | 写入层 409 拦截 | 对抗性自检修正：冲突在写入层即被拒绝，运行时 ErrAmbiguousModel 几乎不可达 |

### 7.3 核查结论

- 全部意见经独立强校验（对抗性逐条反驳验证）成立，证据锚点以 commit `7ebca56` 为准
- 关键发现经 codegraph 检索 + grep 交叉验证（定义 → 调用 → 行为）
- 清单可作为向维护者提交 review 的定稿依据

### 7.4 问题归属验证（2026-08-24）

> 验证方法：对 `7ebca56^`（#289 父提交）与 `7ebca56` 做 diff，确认每条问题对应的代码是 #289 新增还是存量已有。

**结论：25 条问题全部由 #289 的 diff 引入，无一条是存量代码的既有问题。**

依据：
- **新增文件**（33 个，问题主体所在）：`internal/llmproviders/`（providers/request/upstream_models）、`internal/platformapi/console_llm_providers*.go`、`internal/models/catalog.go`、`internal/db/llm_providers*.go`、`migration 00053`、`internal/messages/errors.go`（**整个文件是 #289 新增**，旧版 `7ebca56^` 无此文件）
- **存量文件的新增代码行**：`messages/handler.go`（llmproviders.Resolve/ApplyAPIKey/readRequestModel 全为 `+` 行）、`agents/handler.go`（normalizeConfiguredModel/ListModelIDs 为 `+`）、`batches/handler.go` + `batches/upstream.go`（validateConfiguredModels/erroredResult 为 `+`）、`platform_proxy.go`（/proxy/v1/messages 改造为 `+`）
- **L1 modelmapping 删除**：`modelmapping.go` -43 行确为 #289 的删除

**唯一需要说明的**：M7 的"三套错误映射"中，`/proxy/v1/messages` 和 batches 代理路径本身是存量路由，但**被 #289 改造成 llmproviders 版时没有统一错误结构**——"三套并存"这一事实是 #289 引入的（改造时的不一致），不是存量问题。
