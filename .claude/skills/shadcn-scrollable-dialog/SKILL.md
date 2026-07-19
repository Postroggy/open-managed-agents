---
name: shadcn-scrollable-dialog
description: "Fix or implement scrollable shadcn/ui Dialog content in this project. Use when a Dialog has too much content to fit on screen and needs a scrollable body with a pinned header and pinned footer/action buttons — without the scrollbar obscuring content or the header/footer scrolling away."
user-invocable: true
metadata:
  author: Postroggy
  version: "1.0.0"
allowed-tools: Read Edit Bash(bun:*) Bash(git:*)
---

## 问题背景

本项目的 `DialogContent`（`src/shared/ui/dialog.tsx`）基础样式是 `grid gap-4 p-4`，使用 Base UI `@base-ui-components/react`。

在内容过多时，直接给 `DialogContent` 加 `overflow-y-auto` 会导致：

- 滚动条出现在整个 dialog 容器右边缘（含 header），视觉上压着内容
- Header 标题和底部操作按钮随内容一起滚出视口
- 关闭按钮（`absolute top-2 right-2`）被滚动区域遮住

在 `DialogContent` 上加 `flex flex-col` 也不可行，因为它会与基础 `grid gap-4` 冲突，导致子项高度计算错乱。

## 正确做法（已在本项目验证）

参考 `src/features/workbench/dialogs.tsx` 的成熟写法：

```tsx
<DialogContent
  className="max-h-[min(720px,calc(100vh-48px))] gap-0 overflow-hidden p-0 sm:max-w-[540px]"
>
  {/* 1. 固定 Header：无下边框，保持简洁 */}
  <DialogHeader className="px-4 py-4">
    <DialogTitle>标题</DialogTitle>
  </DialogHeader>

  {/* 2. 表单或内容容器：不加 flex，让 grid 保持原始行为 */}
  <form onSubmit={handleSubmit}>
    {/* 3. 可滚动内容区：用绝对 max-h 而非 flex-1 */}
    <div className="subtle-scrollbar-auto max-h-[min(588px,calc(100vh-192px))] overflow-y-auto pl-4 pr-2 py-4 space-y-4">
      {/* 表单字段 */}
    </div>

    {/* 4. 固定底部操作区：无上边框，保持简洁 */}
    <div className="flex justify-end px-4 py-4">
      <Button type="submit">确认</Button>
    </div>
  </form>
</DialogContent>
```

## 关键要点

| 要点 | 说明 |
|---|---|
| `gap-0 overflow-hidden p-0` | 覆盖 `DialogContent` 基础的 `gap-4` 和 `p-4`，交给子层控制间距 |
| 绝对 `max-h` on 内容区 | 内容区 max-h = dialog max-h − header高度 − footer高度，避免依赖 `flex-1` |
| `subtle-scrollbar-auto` | 项目已有工具类（`src/styles/utilities.css`），平时透明、悬停时显示细条，背景透明 |
| `pl-4 pr-2` | 右侧 padding 缩小为 8px，滚动条与内容之间间距适中 |
| Header/Footer 无分隔线 | 去掉 `border-b` / `border-t`，视觉更干净 |
| 不用 `flex flex-col` on DialogContent | 会与基础 `grid` 冲突，导致高度计算失效 |

## max-h 计算参考

Dialog 总高 720px 时，内容区 max-h 取值：

```
内容区 max-h = 720px − header(~68px) − footer(~64px) = ~588px
响应式写法：max-h-[min(588px,calc(100vh-192px))]
```

如果 dialog 总高不同，按相同方式减去 header + footer 的实测高度。

## 验证步骤

1. `cd web && bun run build` 确认无编译错误
2. 启动开发服务器 `just restart-web`
3. 打开对应 Dialog，内容超出时滚动，确认：
   - Header 标题固定可见
   - Create/确认按钮固定可见
   - 滚动条仅出现在内容区右侧，背景透明
   - 鼠标悬停时滚动条细条出现，离开后消失
