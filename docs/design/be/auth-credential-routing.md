# `/v1/*` 认证路由：基于凭证而非 Host 头

> 目标：让 `/v1/*` 入口路由根据客户端实际携带的凭证类型（API key / session cookie）做分发，而不是依赖 Host 头猜测调用方身份，从而让反向代理和任意端口部署都能正确工作。

---

## 1. 问题

### 1.1 原有路由逻辑

`apiEntrypointRouter.ServeHTTP` 在 `internal/api/server.go` 中决定 `/v1/*` 请求走 service 路由还是 platform 路由：

```go
// 原有实现
func (r apiEntrypointRouter) ServeHTTP(w http.ResponseWriter, req *http.Request) {
    if isPlatformHost(req.Host) && auth.ExtractAPIKey(req) == "" {
        r.platform.ServeHTTP(w, req)  // session cookie 鉴权
        return
    }
    r.service.ServeHTTP(w, req)        // x-api-key 鉴权
}
```

`isPlatformHost` 只识别以下 host：

- `localhost:5173` / `127.0.0.1:5173` / `[::1]:5173` — Vite 前端开发服务器
- `oma.duck.ai` — 生产域名

### 1.2 触发场景

当通过以下方式访问时，Host 头不在白名单内，`/v1/*` 请求被错误路由到 service 路径（要求 `x-api-key`），返回 401：

| 访问方式 | Host 头 | 路由结果 | 预期 |
|----------|---------|----------|------|
| `http://localhost` (Caddy :80) | `localhost` | → service (401) | platform |
| `http://localhost:38080` (直连) | `localhost:38080` | → service (401) | platform |
| 任意反向代理后 | 代理域名 | → service (401) | platform |

这个问题在 docker-compose 部署中尤其突出：Caddy 监听 `:80`，前端通过 `http://localhost` 访问，所有 `/v1/*` 请求都带 session cookie 但被路由到 service auth middleware，直接返回 401。

---

## 2. 方案

### 2.1 核心思路

**不看 Host，看凭证。** 客户端带什么凭证，就进什么路由：

```go
// 修复后
func (r apiEntrypointRouter) ServeHTTP(w http.ResponseWriter, req *http.Request) {
    if auth.ExtractAPIKey(req) != "" {
        r.service.ServeHTTP(w, req)
        return
    }
    if auth.ExtractPlatformSessionKey(req) != "" {
        r.platform.ServeHTTP(w, req)
        return
    }
    r.platform.ServeHTTP(w, req)
}
```

### 2.2 凭证提取

两个核心函数均在 `internal/auth/auth.go` 中：

```go
// ExtractAPIKey — 从 X-Api-Key header 或 Authorization: Bearer <token> 提取
func ExtractAPIKey(r *http.Request) string

// ExtractPlatformSessionKey — 从 sessionKey cookie 提取
func ExtractPlatformSessionKey(r *http.Request) string
```

### 2.3 路由决策表

| API Key | Session Cookie | 路由 | 原因 |
|---------|---------------|------|------|
| ✓ | — | service | SDK/CLI 调用，token 鉴权 |
| ✓ | ✓ | **service** | API key 优先，明确的服务调用意图 |
| — | ✓ | platform | 浏览器控制台，session 鉴权 |
| — | — | platform | 默认走 platform，保留 `/v1/privacy-consents` 等无需鉴权的开放路由 |

### 2.4 向后兼容分析

| 场景 | 修复前 | 修复后 | 变化 |
|------|--------|--------|------|
| `curl -H 'x-api-key: ...' localhost:38080/v1/models` | service | service | 无 |
| 浏览器 `localhost:5173` 带 session cookie | platform | platform | 无 |
| 浏览器 `localhost:38080` 带 session cookie | service (401) | **platform** | ✅ 修复 |
| 浏览器 `localhost` (Caddy) 带 session cookie | service (401) | **platform** | ✅ 修复 |
| 无凭证请求 | host 猜测 | platform | 无开放路由影响 |

唯一的语义变化是：**session cookie 现在在任意端口/域名上都生效**，这正是本次修复的目标。

### 2.5 为什么 API key + session cookie 同时存在时选 service

当两个凭证都存在时（例如开发者用 curl 带 API key 调试，但浏览器也留下了 cookie），API key 是更强的调用意图信号 — 客户端明确选择了 service 调用方式。选择 service 路由也符合最小惊讶原则。

---

## 3. 不影响的范围

1. **`isPlatformHost` 函数保留** — 其他路径（`/api/*`、`/auth/*` 等）仍依赖 host 白名单做平台鉴权和登录流程，这些路径有自己的路由入口，不经过 `apiEntrypointRouter`，本次改动不涉及。
2. **`/v1/*` 以外的路由** — 不受影响。
3. **service auth middleware 逻辑** — 不变。API key 验证、权限、scope 均无变化。
4. **platform auth middleware 逻辑** — 不变。session 解析、组织上下文注入均无变化。

---

## 4. 测试

### 4.1 单元测试

`internal/api/auth_test.go` — `TestAPIEntrypointRouterDispatchesByAuth`：

覆盖：

- API key 在任何 host 上都进 service
- Bearer token 在任何 host 上都进 service
- session cookie 在 `localhost:5173`、`localhost:38080`、`oma.duck.ai`、`api.anthropic.com` 上都进 platform
- API key + session cookie 同时存在 → API key 胜出，进 service
- 无凭证时默认进 platform（保留开放路由）

### 4.2 集成验证

```bash
# 通过 Caddy :80 访问（docker-compose 部署）
curl http://localhost/v1/models -H 'Cookie: sessionKey=...'
# 预期：200（platform 路由），而非 401

# 直连服务端口
curl http://localhost:38080/v1/models -H 'Cookie: sessionKey=...'
# 预期：200（platform 路由），而非 401
```

---

## 5. 与 docker-compose 部署的关系

本次修复是 docker-compose 一键部署的前置条件。Caddy 反向代理在 `:80` 提供服务，Host 头为 `localhost`（不带端口），原路由逻辑会将其误判为 service 调用。修复后，前端控制台通过 Caddy 访问时，session cookie 被正确识别，platform 路由生效。

参见：`docs/design/docker-compose-deployment.md` 第 5 节。

---

## 6. 实现文件

| 文件 | 变更 |
|------|------|
| `internal/api/server.go` | `apiEntrypointRouter.ServeHTTP` 改为凭证驱动路由 |
| `internal/api/auth_test.go` | 测试用例从 host 驱动改为凭证驱动，新增 session cookie 和混合场景 |

PR: https://github.com/superduck-ai/open-managed-agents/pull/6