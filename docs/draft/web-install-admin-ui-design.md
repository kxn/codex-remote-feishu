# Web 安装与管理界面设计草案

> Type: `draft`
> Updated: `2026-04-06`
> Summary: 根据最新反馈收敛到统一 JSON 配置、多飞书 App 同时在线、setup token 和前置改造拆分方案。

## 1. 文档定位

这份文档描述的是 Web 安装向导和本地管理界面的产品设计方向。

它回答三件事：

- 最终产品形态应该长什么样
- 现有仓库哪些部分能直接复用
- 哪些基础改造必须先做，才能开始实现

底层改造细节已经单独拆到：

- [web-install-admin-prerequisites-design.md](./web-install-admin-prerequisites-design.md)

## 2. 已确认结论

本轮讨论已经明确这些原则，不再回退：

- 配置体系直接迁移到统一 `config.json`，不再继续扩展 `config.env`
- 启动时若只有旧配置，则自动做一次 legacy env -> JSON 迁移
- 飞书未来不是“多配置单 active”，而是“多 App 同时在线”
- 飞书凭证更新后需要热替换、热重连和状态更新能力
- 未配置场景采用 `setupToken` 保护 setup 链路
- 安装/配置流程采用显式状态机
- 本地图片暂存与 Markdown 预览飞书云盘要分开管理
- 前端采用嵌入式 SPA，构建产物 embed 到 Go 二进制

## 3. 目标产品形态

### 3.1 启动体验

- `codex-remote` 无参数时默认进入 `daemon + web`
- console 打印当前运行状态和 Web 地址
- 未完成配置时：
  - 本机桌面优先尝试自动打开浏览器
  - SSH 场景给出带 `setupToken` 的访问链接
- 已完成配置时：
  - 只打印简洁工作状态
  - 保留管理页入口

### 3.2 安装向导

向导不是“填两个输入框”。

它需要覆盖完整链路：

1. 检测当前环境与安装状态
2. 添加或编辑飞书 App
3. 指导用户在飞书开放平台完成对应步骤
4. 录入 App ID / App Secret
5. 执行真实连接验证
6. 导出权限 scopes JSON
7. 提示用户手动完成事件、回调、菜单、发布
8. 完成 VS Code 集成

页面结构必须天然支持多 App，因此 V1 就要用“App 列表 + 当前编辑项”的方式组织，而不是写死“唯一一组凭证”。

### 3.3 本地管理页

管理页至少包含这些功能分区：

- 系统状态
- 飞书 App 管理
- 实例管理
- 本地图片暂存区管理
- Markdown 预览飞书云盘管理
- VS Code 集成与 shim 健康检查

### 3.4 页面风格

风格方向保持：

- 现代
- 清晰
- 专业
- 不花哨
- 不简陋

## 4. 当前仓库现实情况

### 4.1 已有基础

现有代码并不是从零开始，已有这些可复用基础：

- daemon 已有 HTTP server 与状态 API
- wrapper 已有 relay daemon 自动拉起
- VS Code 已支持 `editor_settings` 与 `managed_shim`
- 已有 headless 实例运行时
- 已有飞书长连接 gateway
- 已有 Markdown 预览上传与 URL 重写
- 已有 `install-state.json` 记录安装落点

相关代码：

- [internal/app/daemon/app.go](../../internal/app/daemon/app.go)
- [internal/app/daemon/entry.go](../../internal/app/daemon/entry.go)
- [internal/app/wrapper/app.go](../../internal/app/wrapper/app.go)
- [internal/app/install/service.go](../../internal/app/install/service.go)
- [internal/adapter/editor/shim.go](../../internal/adapter/editor/shim.go)
- [internal/adapter/editor/settings.go](../../internal/adapter/editor/settings.go)
- [internal/adapter/feishu/gateway.go](../../internal/adapter/feishu/gateway.go)
- [internal/adapter/feishu/markdown_preview.go](../../internal/adapter/feishu/markdown_preview.go)

### 4.2 当前最重要的现实约束

代码调研后，当前实现存在这些关键限制：

- 配置仍是 `config.env` / `wrapper.env` / `services.env` 模型
- daemon 当前只支持一个 `LiveGateway`
- surface ID 仍是单 App 命名空间：
  - `feishu:user:<userId>`
  - `feishu:chat:<chatId>`
- Markdown 预览状态没有 App 维度，也没有远端 list / delete / reconcile
- 图片暂存仍写到 `os.TempDir()`，没有专用目录和管理模型
- `/newinstance` 走的是“surface 先选 thread 再恢复”的 Feishu 产品语义，不等于“后台手动输入 cwd 新建实例”
- `/v1/status` 当前无鉴权
- daemon 现在实际用 `:9500` / `:9501` 监听，默认是全网卡绑定，不是 loopback

这意味着 Web UI 不能直接套在现有实现上，必须先做底层改造。

### 4.3 当前文档和模板也有一处真实漂移

当前代码已经处理：

- `im.message.recalled_v1`

但 `deploy/feishu/README.md` 和 `deploy/feishu/app-template.json` 之前没有把它列进事件清单。

如果向导直接照当前模板输出，会漏掉一个真实在用的事件。因此“飞书安装清单”必须改成后端统一 source of truth，而不是散落在文档和前端字符串里。

## 5. 可行性调研结论

### 5.1 飞书长连接路径可行

官方 Go SDK 文档已经覆盖：

- 事件订阅长连接
- 回调长连接
- 企业自建应用要求
- 单 App 最多 50 个连接
- 回调/事件处理需在 3 秒内完成

这与当前仓库使用 `ws.NewClient(appID, appSecret, ...)` 的方向一致。

### 5.2 飞书 Drive 管理 API 可行，但边界要说清

官方 Go SDK 生成代码和官方文档链接已经明确存在这些能力：

- 新建文件夹
- 上传文件
- 获取文件夹清单
- 删除文件/文件夹
- 查询异步删除任务状态
- 查询文档元数据
- 增加/删除/列出协作者权限

因此“预览云盘管理”在 API 能力上是可做的。

但要注意两个边界：

- `List` 是“文件夹下清单”，默认不递归
- 删除文件夹是异步任务，需要 `task_check`

另外，从当前 SDK 暴露的数据看：

- `list` / `meta` 不能直接给出可靠的“总占用字节数”

所以管理页里的“总占用”更适合定义成：

- 本地已知预览文件的估算值
- 加上远端对账结果

而不是承诺绝对精确的云盘容量统计。

### 5.3 VS Code 路径现实可行

官方文档与本机实际环境都支持当前路线：

- Remote SSH 场景下，扩展主要装在远端主机
- CLI 支持 `--list-extensions` / `--show-versions`
- 当前主机实际存在：
  - `~/.vscode-server/extensions/openai.chatgpt-26.325.31654-linux-x64`
  - `bin/linux-x86_64/codex`
  - `bin/linux-x86_64/codex.real`

同时，当前主机也有两个需要写进实现假设的现实情况：

- `code` 不在 `PATH`
- `node` / `npm` 也不在 `PATH`

因此：

- shim 检测不能依赖 `code` CLI 才能工作，必须以文件系统扫描为主
- 前端开发前需要先安装 Node 或修正 PATH

## 6. 推荐的 V1 产品设计

### 6.1 网络与启动策略

推荐把 relay 与 Web admin 的绑定策略拆开：

- relay WebSocket：
  - 默认 `127.0.0.1:9500`
  - 仅供 wrapper / 本地 headless 使用
- Web admin：
  - 默认 `127.0.0.1:9501`
  - 未配置且 SSH 场景下，允许临时扩大到 `0.0.0.0`

关键原则：

- 只有 Web setup 链路在需要时才允许对外暴露
- relay WebSocket 不应跟着 setup 模式一起暴露到 `0.0.0.0`

### 6.2 setup token 与访问控制

`setupToken` 已经不是可选项，而是 setup 模式的默认基线。

推荐策略：

- 仅未完成配置时生成
- 只授予 `/setup` 与 setup API
- TTL 15 到 30 分钟
- 服务端只存哈希
- 首次完成配置后立即失效

管理页则采用普通 session/cookie。

当前阶段进一步固定为：

- loopback 直接信任本机访问
- 非 loopback 先通过 `/setup?token=...` 换取 setup session cookie
- 这一步只开放 setup 远程访问，正式 admin 远程 session 留到后续阶段

### 6.3 多 App 同时在线

V1 就按“多飞书 App 同时在线”设计，不再保留“唯一 active app”假设。

这意味着：

- 所有启用中的 App 都要各自建立长连接
- 每个 App 都有独立连接状态
- 同一个用户或群在不同 App 下必须映射成不同 surface

推荐 surface 命名升级为：

- `feishu:<appRuntimeId>:user:<userId>`
- `feishu:<appRuntimeId>:chat:<chatId>`

### 6.4 安装状态机

推荐状态至少包含：

- `legacy_config_detected`
- `legacy_config_migrating`
- `uninitialized`
- `feishu_apps_saved`
- `feishu_apps_partial_online`
- `feishu_platform_config_pending`
- `vscode_integration_pending`
- `ready`
- `ready_degraded`

这样 console、setup 页面和管理页都能用同一套语言描述当前阶段。

### 6.5 向导步骤

推荐向导步骤如下：

1. 欢迎页
   - 展示本机/SSH、已有安装状态、检测到的 VS Code 扩展路径
2. 飞书应用列表
   - 新增、编辑、删除、启停 App
3. 凭证录入
   - App ID / Secret / 备注名
4. 连接验证
   - 验证凭证有效
   - 验证 daemon 已建立长连接
   - 展示最近错误
5. 飞书平台配置引导
   - 导出 scopes JSON
   - 展示事件、回调、菜单、发布 checklist
   - 明确哪些步骤必须手工完成
6. VS Code 集成
   - SSH 场景优先 `managed_shim`
   - 本机场景优先 `editor_settings`
7. 完成页
   - 展示最终状态与管理页入口

### 6.6 管理页分区

#### A. 系统状态

- daemon 状态
- relay / web 监听地址
- 安装状态机状态
- 当前管理 session 状态

#### B. 飞书 App

- App 列表
- 各自连接状态
- 最近验证时间
- 最近错误
- 重新验证
- 热重连
- 修改凭证
- 启用/停用

这里的语义是“多个 App 同时在线”，不是“切换一个 active app”。

#### C. 实例管理

- 查看实例及状态
- 新建 headless 实例
- 删除 headless 实例

V1 的 Web 新建实例定义为：

- 手动输入 `workspaceRoot`
- 手动输入 `displayName`
- 直接启动一个新的 managed headless instance

它不复用 Feishu `/newinstance` 的“先选历史 thread 再恢复”语义。

#### D. 本地图片暂存区

- 当前占用
- 文件数
- 删除一天前文件
- 全部清理未引用旧文件

#### E. Markdown 预览飞书云盘

- 按 App 展示
- 当前预览根目录链接
- 文件数
- 估算占用
- 清理 N 天前文件
- 全量清理当前 App 预览根目录下的内容
- 对账 / reconcile

需要明确告知用户：

- 清理预览文件会让旧消息里的预览链接失效

#### F. VS Code 集成

- 当前集成模式
- `settings.json` 路径
- 当前记录的 `bundleEntrypoint`
- 检测到的最新 OpenAI 扩展路径
- 是否已 shim
- 一键重新安装 shim

### 6.7 统一配置方向

V1 推荐正式切换到：

- `~/.config/codex-remote/config.json`

并把旧 `config.env` 视为 legacy input。

示意结构：

```json
{
  "version": 1,
  "relay": {
    "serverURL": "ws://127.0.0.1:9500/ws/agent",
    "listenHost": "127.0.0.1",
    "listenPort": 9500
  },
  "admin": {
    "listenHost": "127.0.0.1",
    "listenPort": 9501,
    "autoOpenBrowser": true
  },
  "wrapper": {
    "codexRealBinary": "codex",
    "nameMode": "workspace_basename",
    "integrationMode": "managed_shim"
  },
  "feishu": {
    "useSystemProxy": false,
    "apps": [
      {
        "id": "main-bot",
        "name": "Main Bot",
        "appId": "cli_xxx",
        "appSecret": "secret_xxx",
        "enabled": true,
        "verifiedAt": "2026-04-06T10:00:00Z"
      }
    ]
  },
  "debug": {
    "relayFlow": false,
    "relayRaw": false
  },
  "storage": {
    "imageStagingDir": "",
    "previewRootFolderName": "Codex Remote Previews"
  }
}
```

旧配置迁移规则：

1. 优先读 `config.json`
2. 若 `config.json` 不存在但 `config.env` 存在，则自动迁移
3. 迁移写入必须原子化并保持 `0600`
4. 旧文件保留备份
5. 新旧同时存在时，只认 JSON
6. 空输入不能清空已有 secret

### 6.8 后端接口方向

V1 建议按 App 作用域提供接口，而不是“单 App 全局接口”：

- `GET /api/admin/bootstrap-state`
- `GET /api/admin/runtime-status`
- `GET /api/admin/config`
- `PUT /api/admin/config`
- `GET /api/admin/feishu/manifest`
- `GET /api/admin/feishu/apps`
- `POST /api/admin/feishu/apps`
- `PUT /api/admin/feishu/apps/:id`
- `DELETE /api/admin/feishu/apps/:id`
- `POST /api/admin/feishu/apps/:id/verify`
- `POST /api/admin/feishu/apps/:id/reconnect`
- `POST /api/admin/feishu/apps/:id/enable`
- `POST /api/admin/feishu/apps/:id/disable`
- `GET /api/admin/feishu/apps/:id/scopes-json`
- `GET /api/admin/vscode/detect`
- `POST /api/admin/vscode/apply`
- `POST /api/admin/vscode/reinstall-shim`
- `GET /api/admin/instances`
- `POST /api/admin/instances`
- `DELETE /api/admin/instances/:id`
- `GET /api/admin/storage/image-staging`
- `POST /api/admin/storage/image-staging/cleanup`
- `GET /api/admin/storage/preview-drive/:appId`
- `POST /api/admin/storage/preview-drive/:appId/reconcile`
- `POST /api/admin/storage/preview-drive/:appId/cleanup`

当前阶段额外约束：

- 如果运行时仍由 `FEISHU_APP_ID` / `FEISHU_APP_SECRET` env override 驱动，该 App 在页面上应按只读展示

## 7. 风险与 workaround

### 7.1 默认绑定所有网卡

当前实现默认监听 `:9500` / `:9501`。

workaround：

- 前置改造里必须先拆成显式 host + port
- relay 默认回到 loopback
- Web setup 暴露单独受 token 保护

### 7.2 保存凭证后无法立即生效

workaround：

- 先补 `gateway controller`
- 保存后执行热重连
- 页面展示真实连接状态而不是只看文件是否已保存

### 7.3 多 App 命名空间碰撞

workaround：

- 统一改成 app-scoped surface / preview scope
- 不再依赖 `strings.HasPrefix(surfaceID, "feishu:chat:")` 这种旧规则

### 7.4 预览云盘清理影响历史链接

workaround：

- 清理前明确告警
- 默认按 `lastUsedAt` 清理
- 保留 reconcile 入口

### 7.5 云盘容量无法做精确统计

workaround：

- 页面展示“估算占用”
- 统计口径以本地已知文件和远端对账结果为准

### 7.6 当前模板和代码要求漂移

workaround：

- 把事件、菜单、scopes 改成后端统一 manifest
- 不再让前端或 README 手工维护多份真相

## 8. 实施顺序

不要先画页面，建议顺序如下：

1. 做前置基础改造
2. 再补 admin API
3. 最后接 SPA 与 startup 体验

基础改造清单见：

- [web-install-admin-prerequisites-design.md](./web-install-admin-prerequisites-design.md)

## 9. 仍需实现前确认的点

开始编码前，只剩这些还需要最终拍板：

1. `config.json` 最终字段命名是否按文中示意固定为 `version: 1`
2. `setupToken` 是否允许同一 TTL 内开多个浏览器标签页
3. 预览清理默认保留天数是多少
4. 第一阶段是否明确先只支持 Linux 主机

## 10. 参考资料

### 10.1 仓库内资料

- [docs/general/install-deploy-design.md](../general/install-deploy-design.md)
- [docs/general/feishu-product-design.md](../general/feishu-product-design.md)
- [docs/inprogress/relay-daemon-autostart-design.md](../inprogress/relay-daemon-autostart-design.md)
- [docs/inprogress/feishu-headless-instance-design.md](../inprogress/feishu-headless-instance-design.md)
- [deploy/feishu/README.md](../../deploy/feishu/README.md)
- [deploy/feishu/app-template.json](../../deploy/feishu/app-template.json)

### 10.2 外部资料

- Feishu Go SDK 开发前准备
  - https://open.feishu.cn/document/uAjLw4CM/ukTMukTMukTM/server-side-sdk/golang-sdk-guide/preparations.md
- Feishu Go SDK 处理事件
  - https://open.feishu.cn/document/uAjLw4CM/ukTMukTMukTM/server-side-sdk/golang-sdk-guide/handle-events.md
- Feishu Go SDK 处理回调
  - https://open.feishu.cn/document/uAjLw4CM/ukTMukTMukTM/server-side-sdk/golang-sdk-guide/handle-callback.md
- Feishu Drive 创建文件夹
  - https://open.feishu.cn/document/uAjLw4CM/ukTMukTMukTM/reference/drive-v1/file/create_folder
- Feishu Drive 获取文件夹下清单
  - https://open.feishu.cn/document/uAjLw4CM/ukTMukTMukTM/reference/drive-v1/file/list
- Feishu Drive 删除文件/文件夹
  - https://open.feishu.cn/document/uAjLw4CM/ukTMukTMukTM/reference/drive-v1/file/delete
- Feishu Drive 查询异步任务状态
  - https://open.feishu.cn/document/uAjLw4CM/ukTMukTMukTM/reference/drive-v1/file/task_check
- Feishu Drive 增加协作者权限
  - https://open.feishu.cn/document/uAjLw4CM/ukTMukTMukTM/reference/drive-v1/permission-member/create
- Feishu Drive 获取协作者列表
  - https://open.feishu.cn/document/uAjLw4CM/ukTMukTMukTM/reference/drive-v1/permission-member/list
- VS Code Remote SSH
  - https://code.visualstudio.com/docs/remote/ssh
- VS Code Command Line
  - https://code.visualstudio.com/docs/editor/command-line
