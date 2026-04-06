# 工程可维护性与测试覆盖审查（2026-04）

> Type: `draft`
> Updated: `2026-04-07`
> Summary: 基于当前仓库做一次独立审查，记录维护性热点、测试覆盖、已修复问题和后续建议。

## 1. 范围与方法

这次审查覆盖：

- `internal/**`
- `web/src/**`
- `docs/**`

审查方式：

1. 通读核心模块与管理链路代码。
2. 扫描仓库大文件分布。
3. 运行当前测试与构建。
4. 记录明面上的维护性问题、测试薄弱点和显性逻辑风险。

本次已执行：

- `go test ./...`
- `go test -cover ./...`
- `cd web && npm run build`

## 2. 这轮已完成的维护动作

### 2.1 已完成的文件拆分

本轮已拆分：

- [internal/app/daemon/admin_feishu.go](../../internal/app/daemon/admin_feishu.go)
- [web/src/routes/AdminRoute.tsx](../../web/src/routes/AdminRoute.tsx)

拆分结果：

1. Feishu 管理后端改为“类型定义 / HTTP handlers / runtime helper / config helper”四段。
2. 管理页前端改为“数据编排 / 面板组件 / helper / types”四段。

### 2.2 已修复的明面问题

已修复一个用户可见问题：

1. 管理页中运行时接管的只读机器人此前仍允许输入和点击保存，最终才由后端报错；现在前端会直接进入只读态，避免无效操作路径。

## 3. 测试覆盖快照

本次 `go test -cover ./...` 的主要结果：

- `internal/core/orchestrator`: `70.9%`
- `internal/adapter/codex`: `77.3%`
- `internal/adapter/feishu`: `60.2%`
- `internal/app/daemon`: `63.4%`
- `internal/app/install`: `69.3%`
- `internal/app/wrapper`: `60.2%`
- `internal/config`: `75.4%`
- `internal/runtime`: `35.1%`
- `internal/core/renderer`: `83.3%`
- `internal/feishuapp`: `100.0%`

结论：

1. 关键状态机和协议翻译层覆盖率不差。
2. daemon 管理链路已有不错的接口级测试。
3. `internal/runtime` 是明显薄弱项。
4. 前端仍没有独立自动化测试，当前主要靠 `npm run build` 和后端集成测试兜底。

## 4. 当前最大的维护性热点

按当前仓库体量，最值得继续处理的大文件如下：

1. [internal/adapter/codex/translator.go](../../internal/adapter/codex/translator.go) `1673` 行
2. [internal/adapter/feishu/markdown_preview.go](../../internal/adapter/feishu/markdown_preview.go) `1322` 行
3. [web/src/routes/SetupRoute.tsx](../../web/src/routes/SetupRoute.tsx) `1066` 行
4. [internal/app/daemon/app.go](../../internal/app/daemon/app.go) `899` 行
5. [internal/app/wrapper/app.go](../../internal/app/wrapper/app.go) `860` 行
6. [internal/adapter/feishu/gateway.go](../../internal/adapter/feishu/gateway.go) `859` 行

### 4.1 建议优先顺序

建议优先拆分顺序：

1. `SetupRoute.tsx`
2. `translator.go`
3. `app.go`
4. `gateway.go`
5. `markdown_preview.go`

原因：

1. `SetupRoute.tsx` 和本轮已拆的 `AdminRoute.tsx` 在职责上已经明显不对称，继续单文件维护成本很高。
2. `translator.go` 是协议翻译核心，但过大后很难做局部修改与回归定位。
3. `app.go` 与 `gateway.go` 分别承载过多启动/路由职责，后续继续加功能会变脆。

## 5. 审查发现

### 5.1 中风险：管理型 Feishu 写接口是“先写配置，再尝试热应用”

相关文件：

- [internal/app/daemon/admin_feishu_handlers.go](../../internal/app/daemon/admin_feishu_handlers.go)

当前 `create / update / delete / enable / disable` 的基本流程是：

1. 先写本地配置文件
2. 再尝试把改动热应用到运行时

这样做的好处是配置一定落盘，但代价是：

1. 一旦热应用失败，页面会报错
2. 但本地配置其实已经变了
3. 运行时与持久化配置可能暂时不一致

这不是立即错误，但它是明显的运维一致性风险。

建议：

1. 至少在页面上明确区分“已保存但未生效”与“已保存且已生效”。
2. 后续可考虑把“保存成功但热应用失败”收敛成显式状态，而不是普通错误提示。

### 5.2 中风险：前端缺少行为级自动化测试

相关区域：

- [web/src/routes/AdminRoute.tsx](../../web/src/routes/AdminRoute.tsx)
- [web/src/routes/SetupRoute.tsx](../../web/src/routes/SetupRoute.tsx)

当前前端主要靠：

1. TypeScript 编译
2. Vite 构建
3. 后端集成测试间接兜底

这意味着：

1. 文案与按钮显隐逻辑缺少自动回归保护
2. 只读态、空态、失败态、确认弹窗等交互容易回归

建议：

1. 至少给 admin / setup 关键流程补最小组件测试或浏览器冒烟测试。

### 5.3 中风险：`internal/runtime` 覆盖率偏低

相关文件：

- [internal/runtime/manager.go](../../internal/runtime/manager.go)

当前覆盖率只有 `35.1%`。

而这层负责：

- 进程管理
- 状态持久化
- 生命周期控制

它一旦回归，影响面会比较大。

建议：

1. 补异常路径测试。
2. 补“已有状态文件 / 残留进程 / 删除失败”等边界测试。

### 5.4 低风险：setup 与 admin 仍有前端逻辑重复

相关文件：

- [web/src/routes/SetupRoute.tsx](../../web/src/routes/SetupRoute.tsx)
- [web/src/routes/AdminRoute.tsx](../../web/src/routes/AdminRoute.tsx)

虽然本轮已经把 admin 页拆开，但两个页面在这些方面仍明显重复：

1. 机器人基础字段
2. 验证结果处理
3. VS Code 检测加载
4. 部分状态 badge 与只读态语义

这不是 bug，但会持续放大维护成本。

建议：

1. 后续把“机器人配置表单 + 验证结果展示 + 只读态处理”抽成共享前端模块。

### 5.5 低风险：管理页仍夹带 setup 与调试视角

相关文件：

- [web/src/routes/admin/AdminPanels.tsx](../../web/src/routes/admin/AdminPanels.tsx)
- [docs/draft/web-admin-ui-redesign.md](./web-admin-ui-redesign.md)

当前页面虽然功能齐全，但仍保留不少 setup 和工程调试语义。

这更像产品设计问题，不是实现 bug。

建议：

1. 按新的管理页产品文档继续收敛展示层。

## 6. 当前代码结构评价

### 6.1 优点

1. relay / orchestrator / gateway / daemon 分层总体清晰。
2. 状态机相关测试比较扎实。
3. 管理 API 已能覆盖多飞书 App 的大部分生命周期。
4. docs 目录已经建立了比较清楚的生命周期分类。

### 6.2 问题

1. 若干核心文件仍偏大。
2. 前端共享层不足。
3. 少数“保存后热应用”的链路缺少清晰的一致性表达。
4. 前端缺少自动化回归测试。

## 7. 建议的后续动作

建议按这个顺序继续推进：

1. 按新的管理页产品文档收敛 `AdminRoute` 展示层。
2. 拆分 `SetupRoute.tsx`，并抽出与 admin 共用的机器人配置模块。
3. 拆分 `translator.go`，把协议映射按事件类别或响应类别收口。
4. 给前端补最小自动化测试。
5. 补 `internal/runtime` 的异常路径测试。

## 8. 总结

当前仓库已经具备不错的状态机测试基础和较完整的管理能力，但维护成本正在逐步向“大文件 + 页面重复逻辑 + 前端缺测试”三处集中。

本轮拆分后，管理链路的可读性已经明显改善；下一步最值得继续处理的是：

1. `SetupRoute.tsx`
2. `translator.go`
3. 管理页展示层收敛
