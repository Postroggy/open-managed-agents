# GET /demo_widgets API 文档

## 概述

此接口是一个用于 docs-sync 写入 E2E 测试的 smoke stub。它暴露一个简单的 GET /demo_widgets 端点，返回一个固定的 demo widget 列表响应。

**目的**: 为 docs-sync 设计文档同步流程提供一个未映射的 API 表面，验证审计和文档生成流程。

---

## 接口详情

### 请求

| 属性 | 值 |
|------|-----|
| **方法** | `GET` |
| **路径** | `/demo_widgets` |

### 请求头

无特殊要求。

---

## 响应

### 成功响应 (200 OK)

```typescript
interface DemoWidgetListResponse {
  type: "demo_widget_list"
  data: DemoWidget[]
  note: string
}

interface DemoWidget {
  id: string
  name: string
}
```

**示例响应:**

```json
{
  "type": "demo_widget_list",
  "data": [],
  "note": "docs-sync smoke stub"
}
```

### 错误响应

| 状态码 | 描述 |
|--------|------|
| **404 Not Found** | 路径或方法不匹配时返回 |

---

## 实现细节

### Handler 结构

```go
package demowidgets

type Handler struct{}

func New() *Handler { return &Handler{} }

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
    // 仅支持 GET /
    if r.Method != http.MethodGet || r.URL.Path != "/" && r.URL.Path != "" {
        http.NotFound(w, r)
        return
    }
    // 返回固定 JSON 响应
}
```

### 路由挂载

```go
// internal/api/server.go
r.Mount("/demo_widgets", demowidgets.New())
```

---

## 使用场景

### E2E 测试验证

```bash
# 验证端点可用
curl -X GET http://localhost:38080/demo_widgets

# 预期响应
{"type":"demo_widget_list","data":[],"note":"docs-sync smoke stub"}
```

---

## 注意事项

1. **非生产接口**: 此接口仅用于测试和验证 docs-sync 流程
2. **无状态**: 不维护任何会话或持久化数据
3. **固定响应**: 始终返回相同的静态 JSON 响应
4. **无鉴权**: 不执行任何身份验证或授权检查

---

*文档生成时间: 2026-07-10*
*用途: docs-sync E2E 测试*
