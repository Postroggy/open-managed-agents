# Claude API 官方文档本地镜像（API Reference）

> 来源: https://platform.claude.com/docs/en/api/overview

> 下载日期: 2026-08-21

> 下载方式: `curl <page>.md`（官方原始 Markdown）

> 与 `docs/managed-agents-reference/`（概念页镜像）互补；本目录为 **API 端点参考**。

> 内容版权归 Anthropic 所有；仅本地参考。


共 114 个文件。


## 索引


### 通用 / 平台约定

| 文件 | 官方 URL |
|---|---|
| [`README`](./README.md) |  |
| [`api/beta-headers`](./api_beta-headers.md) | https://platform.claude.com/docs/en/api/beta-headers |
| [`api/errors`](./api_errors.md) | https://platform.claude.com/docs/en/api/errors |
| [`api/ip-addresses`](./api_ip-addresses.md) | https://platform.claude.com/docs/en/api/ip-addresses |
| [`api/overview`](./api_overview.md) | https://platform.claude.com/docs/en/api/overview |
| [`api/rate-limits`](./api_rate-limits.md) | https://platform.claude.com/docs/en/api/rate-limits |
| [`api/service-tiers`](./api_service-tiers.md) | https://platform.claude.com/docs/en/api/service-tiers |
| [`api/supported-regions`](./api_supported-regions.md) | https://platform.claude.com/docs/en/api/supported-regions |
| [`api/versioning`](./api_versioning.md) | https://platform.claude.com/docs/en/api/versioning |
| [`manage-claude/admin-api`](./manage-claude_admin-api.md) | https://platform.claude.com/docs/en/manage-claude/admin-api |

### Messages / Completions

| 文件 | 官方 URL |
|---|---|
| [`api/python/beta/messages/create`](./api_python_beta_messages_create.md) | https://platform.claude.com/docs/en/api/python/beta/messages/create |
| [`api/python/completions/create`](./api_python_completions_create.md) | https://platform.claude.com/docs/en/api/python/completions/create |
| [`api/python/messages/count/tokens`](./api_python_messages_count_tokens.md) | https://platform.claude.com/docs/en/api/python/messages/count_tokens |
| [`api/python/messages/create`](./api_python_messages_create.md) | https://platform.claude.com/docs/en/api/python/messages/create |

### Message Batches

| 文件 | 官方 URL |
|---|---|
| [`api/python/messages/batches/cancel`](./api_python_messages_batches_cancel.md) | https://platform.claude.com/docs/en/api/python/messages/batches/cancel |
| [`api/python/messages/batches/create`](./api_python_messages_batches_create.md) | https://platform.claude.com/docs/en/api/python/messages/batches/create |
| [`api/python/messages/batches/delete`](./api_python_messages_batches_delete.md) | https://platform.claude.com/docs/en/api/python/messages/batches/delete |
| [`api/python/messages/batches/list`](./api_python_messages_batches_list.md) | https://platform.claude.com/docs/en/api/python/messages/batches/list |
| [`api/python/messages/batches/results`](./api_python_messages_batches_results.md) | https://platform.claude.com/docs/en/api/python/messages/batches/results |
| [`api/python/messages/batches/retrieve`](./api_python_messages_batches_retrieve.md) | https://platform.claude.com/docs/en/api/python/messages/batches/retrieve |

### Models

| 文件 | 官方 URL |
|---|---|
| [`api/python/beta/models/list`](./api_python_beta_models_list.md) | https://platform.claude.com/docs/en/api/python/beta/models/list |
| [`api/python/beta/models/retrieve`](./api_python_beta_models_retrieve.md) | https://platform.claude.com/docs/en/api/python/beta/models/retrieve |

### Files

| 文件 | 官方 URL |
|---|---|
| [`api/python/beta/files/delete`](./api_python_beta_files_delete.md) | https://platform.claude.com/docs/en/api/python/beta/files/delete |
| [`api/python/beta/files/download`](./api_python_beta_files_download.md) | https://platform.claude.com/docs/en/api/python/beta/files/download |
| [`api/python/beta/files/list`](./api_python_beta_files_list.md) | https://platform.claude.com/docs/en/api/python/beta/files/list |
| [`api/python/beta/files/retrieve/metadata`](./api_python_beta_files_retrieve_metadata.md) | https://platform.claude.com/docs/en/api/python/beta/files/retrieve_metadata |
| [`api/python/beta/files/upload`](./api_python_beta_files_upload.md) | https://platform.claude.com/docs/en/api/python/beta/files/upload |

### Skills

| 文件 | 官方 URL |
|---|---|
| [`api/python/beta/skills/create`](./api_python_beta_skills_create.md) | https://platform.claude.com/docs/en/api/python/beta/skills/create |
| [`api/python/beta/skills/delete`](./api_python_beta_skills_delete.md) | https://platform.claude.com/docs/en/api/python/beta/skills/delete |
| [`api/python/beta/skills/list`](./api_python_beta_skills_list.md) | https://platform.claude.com/docs/en/api/python/beta/skills/list |
| [`api/python/beta/skills/retrieve`](./api_python_beta_skills_retrieve.md) | https://platform.claude.com/docs/en/api/python/beta/skills/retrieve |
| [`api/python/beta/skills/versions/create`](./api_python_beta_skills_versions_create.md) | https://platform.claude.com/docs/en/api/python/beta/skills/versions/create |
| [`api/python/beta/skills/versions/delete`](./api_python_beta_skills_versions_delete.md) | https://platform.claude.com/docs/en/api/python/beta/skills/versions/delete |
| [`api/python/beta/skills/versions/list`](./api_python_beta_skills_versions_list.md) | https://platform.claude.com/docs/en/api/python/beta/skills/versions/list |
| [`api/python/beta/skills/versions/retrieve`](./api_python_beta_skills_versions_retrieve.md) | https://platform.claude.com/docs/en/api/python/beta/skills/versions/retrieve |

### Agents

| 文件 | 官方 URL |
|---|---|
| [`api/python/beta/agents/archive`](./api_python_beta_agents_archive.md) | https://platform.claude.com/docs/en/api/python/beta/agents/archive |
| [`api/python/beta/agents/create`](./api_python_beta_agents_create.md) | https://platform.claude.com/docs/en/api/python/beta/agents/create |
| [`api/python/beta/agents/list`](./api_python_beta_agents_list.md) | https://platform.claude.com/docs/en/api/python/beta/agents/list |
| [`api/python/beta/agents/retrieve`](./api_python_beta_agents_retrieve.md) | https://platform.claude.com/docs/en/api/python/beta/agents/retrieve |
| [`api/python/beta/agents/update`](./api_python_beta_agents_update.md) | https://platform.claude.com/docs/en/api/python/beta/agents/update |
| [`api/python/beta/agents/versions/list`](./api_python_beta_agents_versions_list.md) | https://platform.claude.com/docs/en/api/python/beta/agents/versions/list |

### Environments

| 文件 | 官方 URL |
|---|---|
| [`api/python/beta/environments/archive`](./api_python_beta_environments_archive.md) | https://platform.claude.com/docs/en/api/python/beta/environments/archive |
| [`api/python/beta/environments/create`](./api_python_beta_environments_create.md) | https://platform.claude.com/docs/en/api/python/beta/environments/create |
| [`api/python/beta/environments/delete`](./api_python_beta_environments_delete.md) | https://platform.claude.com/docs/en/api/python/beta/environments/delete |
| [`api/python/beta/environments/list`](./api_python_beta_environments_list.md) | https://platform.claude.com/docs/en/api/python/beta/environments/list |
| [`api/python/beta/environments/retrieve`](./api_python_beta_environments_retrieve.md) | https://platform.claude.com/docs/en/api/python/beta/environments/retrieve |
| [`api/python/beta/environments/update`](./api_python_beta_environments_update.md) | https://platform.claude.com/docs/en/api/python/beta/environments/update |
| [`api/python/beta/environments/work/retrieve`](./api_python_beta_environments_work_retrieve.md) | https://platform.claude.com/docs/en/api/python/beta/environments/work/retrieve |

### Sessions

| 文件 | 官方 URL |
|---|---|
| [`api/python/beta/sessions/archive`](./api_python_beta_sessions_archive.md) | https://platform.claude.com/docs/en/api/python/beta/sessions/archive |
| [`api/python/beta/sessions/create`](./api_python_beta_sessions_create.md) | https://platform.claude.com/docs/en/api/python/beta/sessions/create |
| [`api/python/beta/sessions/delete`](./api_python_beta_sessions_delete.md) | https://platform.claude.com/docs/en/api/python/beta/sessions/delete |
| [`api/python/beta/sessions/events/list`](./api_python_beta_sessions_events_list.md) | https://platform.claude.com/docs/en/api/python/beta/sessions/events/list |
| [`api/python/beta/sessions/events/send`](./api_python_beta_sessions_events_send.md) | https://platform.claude.com/docs/en/api/python/beta/sessions/events/send |
| [`api/python/beta/sessions/events/stream`](./api_python_beta_sessions_events_stream.md) | https://platform.claude.com/docs/en/api/python/beta/sessions/events/stream |
| [`api/python/beta/sessions/list`](./api_python_beta_sessions_list.md) | https://platform.claude.com/docs/en/api/python/beta/sessions/list |
| [`api/python/beta/sessions/resources/add`](./api_python_beta_sessions_resources_add.md) | https://platform.claude.com/docs/en/api/python/beta/sessions/resources/add |
| [`api/python/beta/sessions/resources/delete`](./api_python_beta_sessions_resources_delete.md) | https://platform.claude.com/docs/en/api/python/beta/sessions/resources/delete |
| [`api/python/beta/sessions/resources/list`](./api_python_beta_sessions_resources_list.md) | https://platform.claude.com/docs/en/api/python/beta/sessions/resources/list |
| [`api/python/beta/sessions/resources/retrieve`](./api_python_beta_sessions_resources_retrieve.md) | https://platform.claude.com/docs/en/api/python/beta/sessions/resources/retrieve |
| [`api/python/beta/sessions/resources/update`](./api_python_beta_sessions_resources_update.md) | https://platform.claude.com/docs/en/api/python/beta/sessions/resources/update |
| [`api/python/beta/sessions/retrieve`](./api_python_beta_sessions_retrieve.md) | https://platform.claude.com/docs/en/api/python/beta/sessions/retrieve |
| [`api/python/beta/sessions/threads/archive`](./api_python_beta_sessions_threads_archive.md) | https://platform.claude.com/docs/en/api/python/beta/sessions/threads/archive |
| [`api/python/beta/sessions/threads/events/list`](./api_python_beta_sessions_threads_events_list.md) | https://platform.claude.com/docs/en/api/python/beta/sessions/threads/events/list |
| [`api/python/beta/sessions/threads/events/stream`](./api_python_beta_sessions_threads_events_stream.md) | https://platform.claude.com/docs/en/api/python/beta/sessions/threads/events/stream |
| [`api/python/beta/sessions/threads/list`](./api_python_beta_sessions_threads_list.md) | https://platform.claude.com/docs/en/api/python/beta/sessions/threads/list |
| [`api/python/beta/sessions/threads/retrieve`](./api_python_beta_sessions_threads_retrieve.md) | https://platform.claude.com/docs/en/api/python/beta/sessions/threads/retrieve |
| [`api/python/beta/sessions/update`](./api_python_beta_sessions_update.md) | https://platform.claude.com/docs/en/api/python/beta/sessions/update |

### Deployments / Deployment Runs

| 文件 | 官方 URL |
|---|---|
| [`api/python/beta/deployment/runs/list`](./api_python_beta_deployment_runs_list.md) | https://platform.claude.com/docs/en/api/python/beta/deployment_runs/list |
| [`api/python/beta/deployment/runs/retrieve`](./api_python_beta_deployment_runs_retrieve.md) | https://platform.claude.com/docs/en/api/python/beta/deployment_runs/retrieve |
| [`api/python/beta/deployments/archive`](./api_python_beta_deployments_archive.md) | https://platform.claude.com/docs/en/api/python/beta/deployments/archive |
| [`api/python/beta/deployments/create`](./api_python_beta_deployments_create.md) | https://platform.claude.com/docs/en/api/python/beta/deployments/create |
| [`api/python/beta/deployments/list`](./api_python_beta_deployments_list.md) | https://platform.claude.com/docs/en/api/python/beta/deployments/list |
| [`api/python/beta/deployments/pause`](./api_python_beta_deployments_pause.md) | https://platform.claude.com/docs/en/api/python/beta/deployments/pause |
| [`api/python/beta/deployments/retrieve`](./api_python_beta_deployments_retrieve.md) | https://platform.claude.com/docs/en/api/python/beta/deployments/retrieve |
| [`api/python/beta/deployments/run`](./api_python_beta_deployments_run.md) | https://platform.claude.com/docs/en/api/python/beta/deployments/run |
| [`api/python/beta/deployments/unpause`](./api_python_beta_deployments_unpause.md) | https://platform.claude.com/docs/en/api/python/beta/deployments/unpause |
| [`api/python/beta/deployments/update`](./api_python_beta_deployments_update.md) | https://platform.claude.com/docs/en/api/python/beta/deployments/update |

### Vaults

| 文件 | 官方 URL |
|---|---|
| [`api/python/beta/vaults/archive`](./api_python_beta_vaults_archive.md) | https://platform.claude.com/docs/en/api/python/beta/vaults/archive |
| [`api/python/beta/vaults/create`](./api_python_beta_vaults_create.md) | https://platform.claude.com/docs/en/api/python/beta/vaults/create |
| [`api/python/beta/vaults/delete`](./api_python_beta_vaults_delete.md) | https://platform.claude.com/docs/en/api/python/beta/vaults/delete |
| [`api/python/beta/vaults/list`](./api_python_beta_vaults_list.md) | https://platform.claude.com/docs/en/api/python/beta/vaults/list |
| [`api/python/beta/vaults/retrieve`](./api_python_beta_vaults_retrieve.md) | https://platform.claude.com/docs/en/api/python/beta/vaults/retrieve |
| [`api/python/beta/vaults/update`](./api_python_beta_vaults_update.md) | https://platform.claude.com/docs/en/api/python/beta/vaults/update |

### Memory Stores

| 文件 | 官方 URL |
|---|---|
| [`api/python/beta/memory/stores/archive`](./api_python_beta_memory_stores_archive.md) | https://platform.claude.com/docs/en/api/python/beta/memory_stores/archive |
| [`api/python/beta/memory/stores/create`](./api_python_beta_memory_stores_create.md) | https://platform.claude.com/docs/en/api/python/beta/memory_stores/create |
| [`api/python/beta/memory/stores/delete`](./api_python_beta_memory_stores_delete.md) | https://platform.claude.com/docs/en/api/python/beta/memory_stores/delete |
| [`api/python/beta/memory/stores/list`](./api_python_beta_memory_stores_list.md) | https://platform.claude.com/docs/en/api/python/beta/memory_stores/list |
| [`api/python/beta/memory/stores/retrieve`](./api_python_beta_memory_stores_retrieve.md) | https://platform.claude.com/docs/en/api/python/beta/memory_stores/retrieve |
| [`api/python/beta/memory/stores/update`](./api_python_beta_memory_stores_update.md) | https://platform.claude.com/docs/en/api/python/beta/memory_stores/update |

### Dreams

| 文件 | 官方 URL |
|---|---|
| [`api/python/beta/dreams/archive`](./api_python_beta_dreams_archive.md) | https://platform.claude.com/docs/en/api/python/beta/dreams/archive |
| [`api/python/beta/dreams/cancel`](./api_python_beta_dreams_cancel.md) | https://platform.claude.com/docs/en/api/python/beta/dreams/cancel |
| [`api/python/beta/dreams/create`](./api_python_beta_dreams_create.md) | https://platform.claude.com/docs/en/api/python/beta/dreams/create |
| [`api/python/beta/dreams/list`](./api_python_beta_dreams_list.md) | https://platform.claude.com/docs/en/api/python/beta/dreams/list |
| [`api/python/beta/dreams/retrieve`](./api_python_beta_dreams_retrieve.md) | https://platform.claude.com/docs/en/api/python/beta/dreams/retrieve |

### Tunnels / MCP Tunnels

| 文件 | 官方 URL |
|---|---|
| [`api/admin/mcp/tunnels/archive`](./api_admin_mcp_tunnels_archive.md) | https://platform.claude.com/docs/en/api/admin/mcp_tunnels/archive |
| [`api/admin/mcp/tunnels/list`](./api_admin_mcp_tunnels_list.md) | https://platform.claude.com/docs/en/api/admin/mcp_tunnels/list |
| [`api/admin/mcp/tunnels/retrieve`](./api_admin_mcp_tunnels_retrieve.md) | https://platform.claude.com/docs/en/api/admin/mcp_tunnels/retrieve |
| [`api/admin/mcp/tunnels/reveal/token`](./api_admin_mcp_tunnels_reveal_token.md) | https://platform.claude.com/docs/en/api/admin/mcp_tunnels/reveal_token |
| [`api/admin/mcp/tunnels/rotate/token`](./api_admin_mcp_tunnels_rotate_token.md) | https://platform.claude.com/docs/en/api/admin/mcp_tunnels/rotate_token |
| [`api/python/beta/tunnels/archive`](./api_python_beta_tunnels_archive.md) | https://platform.claude.com/docs/en/api/python/beta/tunnels/archive |
| [`api/python/beta/tunnels/certificates/archive`](./api_python_beta_tunnels_certificates_archive.md) | https://platform.claude.com/docs/en/api/python/beta/tunnels/certificates/archive |
| [`api/python/beta/tunnels/certificates/create`](./api_python_beta_tunnels_certificates_create.md) | https://platform.claude.com/docs/en/api/python/beta/tunnels/certificates/create |
| [`api/python/beta/tunnels/certificates/list`](./api_python_beta_tunnels_certificates_list.md) | https://platform.claude.com/docs/en/api/python/beta/tunnels/certificates/list |
| [`api/python/beta/tunnels/certificates/retrieve`](./api_python_beta_tunnels_certificates_retrieve.md) | https://platform.claude.com/docs/en/api/python/beta/tunnels/certificates/retrieve |
| [`api/python/beta/tunnels/create`](./api_python_beta_tunnels_create.md) | https://platform.claude.com/docs/en/api/python/beta/tunnels/create |
| [`api/python/beta/tunnels/list`](./api_python_beta_tunnels_list.md) | https://platform.claude.com/docs/en/api/python/beta/tunnels/list |
| [`api/python/beta/tunnels/retrieve`](./api_python_beta_tunnels_retrieve.md) | https://platform.claude.com/docs/en/api/python/beta/tunnels/retrieve |
| [`api/python/beta/tunnels/reveal/token`](./api_python_beta_tunnels_reveal_token.md) | https://platform.claude.com/docs/en/api/python/beta/tunnels/reveal_token |
| [`api/python/beta/tunnels/rotate/token`](./api_python_beta_tunnels_rotate_token.md) | https://platform.claude.com/docs/en/api/python/beta/tunnels/rotate_token |

### User Profiles

| 文件 | 官方 URL |
|---|---|
| [`api/python/beta/user/profiles/create`](./api_python_beta_user_profiles_create.md) | https://platform.claude.com/docs/en/api/python/beta/user_profiles/create |
| [`api/python/beta/user/profiles/create/enrollment/url`](./api_python_beta_user_profiles_create_enrollment_url.md) | https://platform.claude.com/docs/en/api/python/beta/user_profiles/create_enrollment_url |
| [`api/python/beta/user/profiles/list`](./api_python_beta_user_profiles_list.md) | https://platform.claude.com/docs/en/api/python/beta/user_profiles/list |
| [`api/python/beta/user/profiles/retrieve`](./api_python_beta_user_profiles_retrieve.md) | https://platform.claude.com/docs/en/api/python/beta/user_profiles/retrieve |
| [`api/python/beta/user/profiles/update`](./api_python_beta_user_profiles_update.md) | https://platform.claude.com/docs/en/api/python/beta/user_profiles/update |
