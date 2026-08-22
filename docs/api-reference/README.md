# Claude API 官方文档本地镜像（API Reference）

> 来源: https://platform.claude.com/docs/en/api/overview

> 下载日期: 2026-08-22

> 下载方式: `curl <page>.md`（官方原始 Markdown，目录源自官方 `llms.txt` 的 `### API reference` 与 `### API Reference` 两个分类，排除 java/python 语言变体——本站以官方默认语言页为准）

> 与 `docs/managed-agents-reference/`（Managed Agents 概念指南镜像）互补；本目录为 **API 端点参考**。

> 内容版权归 Anthropic 所有；仅本地参考。


共 384 个文件（2026-08-22 版本，覆盖 2026-08-21 旧镜像 114 个文件）。


## 索引


### 通用 / 平台约定（顶层）

| 文件 | 官方 URL |
|---|---|
| [`overview`](./overview.md) | https://platform.claude.com/docs/en/api/overview |
| [`beta-headers`](./beta-headers.md) | https://platform.claude.com/docs/en/api/beta-headers |
| [`errors`](./errors.md) | https://platform.claude.com/docs/en/api/errors |
| [`ip-addresses`](./ip-addresses.md) | https://platform.claude.com/docs/en/api/ip-addresses |
| [`rate-limits`](./rate-limits.md) | https://platform.claude.com/docs/en/api/rate-limits |
| [`service-tiers`](./service-tiers.md) | https://platform.claude.com/docs/en/api/service-tiers |
| [`supported-regions`](./supported-regions.md) | https://platform.claude.com/docs/en/api/supported-regions |
| [`versioning`](./versioning.md) | https://platform.claude.com/docs/en/api/versioning |
| [`claude-code/routines-fire`](./claude-code/routines-fire.md) | https://platform.claude.com/docs/en/api/claude-code/routines-fire |
| [`claude-platform-on-aws-iam-actions`](./claude-platform-on-aws-iam-actions.md) | https://platform.claude.com/docs/en/api/claude-platform-on-aws-iam-actions |

### Messages / Completions / Models / Files / Skills（非 beta）

> 每个资源目录含 landing 页（如 `messages.md`）；顶层另有同名 landing 页副本（如 `docs/api-reference/messages.md`），两者内容一致。

| 目录 | 文件数 | 官方 URL 前缀 |
|---|---|---|
| [`messages/`](./messages/) | 9 | https://platform.claude.com/docs/en/api/messages/ |
| [`completions/`](./completions/) | 1 | https://platform.claude.com/docs/en/api/completions/ |
| [`models/`](./models/) | 2 | https://platform.claude.com/docs/en/api/models/ |
| [`files/`](./files/) | 5 | https://platform.claude.com/docs/en/api/files/ |
| [`skills/`](./skills/) | 9 | https://platform.claude.com/docs/en/api/skills/ |

### 资源 landing 页（顶层）

> 各资源分类的入口页，位于顶层，与上述目录页内容一致。

| 文件 | 官方 URL |
|---|---|
| [`messages`](./messages.md) | https://platform.claude.com/docs/en/api/messages |
| [`completions`](./completions.md) | https://platform.claude.com/docs/en/api/completions |
| [`models`](./models.md) | https://platform.claude.com/docs/en/api/models |
| [`files`](./files.md) | https://platform.claude.com/docs/en/api/files |
| [`skills`](./skills.md) | https://platform.claude.com/docs/en/api/skills |
| [`beta`](./beta.md) | https://platform.claude.com/docs/en/api/beta |
| [`admin`](./admin.md) | https://platform.claude.com/docs/en/api/admin |
| [`compliance`](./compliance.md) | https://platform.claude.com/docs/en/api/compliance |

### Beta API（Managed Agents 等新资源）

> 目录对应官方侧边栏 "API reference → Beta" 分组；Managed Agents 相关端点均需
> `managed-agents-2026-04-01`（或 `agent-memory-2026-07-22` 用于 memory store 端点）beta header。
> 每个子目录含同名 landing 页（如 `beta/agents.md`，已计入各目录文件数，不在下表单独链接）。

| 目录 | 文件数 | 说明 |
|---|---|---|
| [`beta/agents/`](./beta/agents/) | 8 | 含 landing 页 + Agent CRUD + versions |
| [`beta/sessions/`](./beta/sessions/) | 24 | 含 landing 页 + events、resources、threads 子资源 |
| [`beta/environments/`](./beta/environments/) | 16 | 含 landing 页 + work 子资源（poll/ack/heartbeat/stop…） |
| [`beta/deployments/`](./beta/deployments/) | 9 | 含 landing 页 + run/pause/unpause |
| [`beta/deployment_runs/`](./beta/deployment_runs/) | 3 | 含 landing 页 |
| [`beta/vaults/`](./beta/vaults/) | 15 | 含 landing 页 + credentials（含 mcp_oauth_validate） |
| [`beta/memory_stores/`](./beta/memory_stores/) | 17 | 含 landing 页 + memories、memory_versions |
| [`beta/dreams/`](./beta/dreams/) | 6 | 含 landing 页 |
| [`beta/skills/`](./beta/skills/) | 11 | 含 landing 页 + versions |
| [`beta/tunnels/`](./beta/tunnels/) | 12 | 含 landing 页 + certificates |
| [`beta/user_profiles/`](./beta/user_profiles/) | 6 | 含 landing 页 |
| [`beta/files/`](./beta/files/) | 6 | 含 landing 页 |
| [`beta/messages/`](./beta/messages/) | 10 | 含 landing 页 + batches |
| [`beta/models/`](./beta/models/) | 3 | 含 landing 页 |
| [`beta/webhooks.md`](./beta/webhooks.md) | 1 | Webhooks 订阅（事件域类型定义） |

### Admin API（组织管理）

> 每个子目录含同名 landing 页（如 `admin/workspaces.md`，已计入各目录文件数，不在下表单独链接）。

| 目录 | 文件数 | 说明 |
|---|---|---|
| [`admin/workspaces/`](./admin/workspaces/) | 20 | 含 landing 页 |
| [`admin/analytics/`](./admin/analytics/) | 20 | 含 landing 页 |
| [`admin/mcp_tunnels/`](./admin/mcp_tunnels/) | 11 | 含 landing 页 |
| [`admin/rbac_groups/`](./admin/rbac_groups/) | 10 | 含 landing 页 |
| [`admin/service_accounts/`](./admin/service_accounts/) | 10 | 含 landing 页 |
| [`admin/federation_rules/`](./admin/federation_rules/) | 10 | 含 landing 页 |
| [`admin/spend_limits/`](./admin/spend_limits/) | 10 | 含 landing 页 |
| [`admin/external_keys/`](./admin/external_keys/) | 7 | 含 landing 页 |
| [`admin/federation_issuers/`](./admin/federation_issuers/) | 6 | 含 landing 页 |
| [`admin/api_keys/`](./admin/api_keys/) | 4 | 含 landing 页 |
| [`admin/invites/`](./admin/invites/) | 5 | 含 landing 页 |
| [`admin/rbac_roles/`](./admin/rbac_roles/) | 5 | 含 landing 页 |
| [`admin/users/`](./admin/users/) | 5 | 含 landing 页 |
| [`admin/usage_report/`](./admin/usage_report/) | 3 | 含 landing 页 |
| [`admin/cost_report/`](./admin/cost_report/) | 2 | 含 landing 页 |
| [`admin/organizations/`](./admin/organizations/) | 2 | 含 landing 页 |
| [`admin/rate_limits/`](./admin/rate_limits/) | 2 | 含 landing 页 |

### Compliance API（合规审计）

> 每个子目录含同名 landing 页（如 `compliance/apps.md`，已计入各目录文件数，不在下表单独链接）。

| 目录 | 文件数 | 说明 |
|---|---|---|
| [`compliance/apps/`](./compliance/apps/) | 38 | 含 landing 页 |
| [`compliance/organizations/`](./compliance/organizations/) | 11 | 含 landing 页 |
| [`compliance/code/`](./compliance/code/) | 5 | 含 landing 页 |
| [`compliance/groups/`](./compliance/groups/) | 5 | 含 landing 页 |
| [`compliance/activities.md`](./compliance/activities.md) | 1 | |
| [`compliance/activities/list.md`](./compliance/activities/list.md) | 1 | |


## 版本管理

- 本目录是 **官方原始 Markdown 快照**，随官方文档更新重新下载即可。
- 更新步骤：`curl https://platform.claude.com/docs/llms.txt` 提取 `### API reference` 与 `### API Reference` 两个分类的全部 `.md` URL（排除 `java/`、`python/` 变体），逐个 `curl <url>` 落到对应子目录，最后更新本 README 的日期与文件数。
