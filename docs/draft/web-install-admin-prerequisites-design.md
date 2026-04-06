# Web 安装与管理界面前置改造设计

> Type: `draft`
> Updated: `2026-04-06`
> Summary: 细化统一 JSON 配置、多 App 在线、gateway controller、预览管理、启动鉴权与 SPA 接入的前置改造。

## 1. 文档定位

这份文档只讨论“开始做 Web 安装/管理界面之前，必须先完成哪些基础改造”。

它的目标不是描述最终页面，而是把前置工作拆到可以直接开始实现的粒度：

- 改哪些结构
- 涉及哪些文件
- 需要补哪些 API
- 需要补哪些测试
- 推荐按什么顺序落地

产品层设计见：

- [web-install-admin-ui-design.md](./web-install-admin-ui-design.md)

## 2. 目标与非目标

### 2.1 目标

这批前置改造完成后，应满足：

- runtime 统一使用 `config.json`
- 支持 legacy env 自动迁移
- 多个飞书 App 同时在线
- 飞书凭证可热替换、热重连、实时观测
- Web setup 链路有最小可用鉴权
- relay 和 Web admin 有独立绑定策略
- Markdown 预览云盘可被管理和对账
- 本地图片暂存区可统计和清理
- 管理页可以独立创建/删除 headless 实例
- SPA 可以被 embed 到 Go 二进制

### 2.2 非目标

本阶段不要求：

- 先做完整前端
- 一次性把所有现有文档改成新配置语义
- 立刻实现完整多租户/多用户权限系统
- 让所有管理操作都支持跨机器公开访问

## 3. 代码调研后的硬阻塞

当前仓库里，开始做 Web UI 前至少有这些硬阻塞：

1. 配置体系仍基于 `config.env`
2. daemon 当前只支持一个 `LiveGateway`
3. surface 与 preview scope 没有 App 维度
4. relay 和 admin HTTP 默认都监听全网卡
5. preview API 只有上传路径，没有 list / delete / reconcile
6. 本地图片暂存没有专用目录和回收器
7. 手动新建实例没有 daemon 级 API，只存在 surface 语义
8. setup/admin 页面没有认证模型
9. 前端工具链当前不在主机 PATH
10. 飞书安装清单没有统一 manifest，现有模板已与代码漂移

## 4. 推荐实现顺序

按依赖关系，推荐顺序是：

1. 配置与绑定策略
2. surface/gateway 身份模型
3. MultiGateway controller 与热重连
4. admin/setup 鉴权
5. 图片暂存与 preview drive 管理
6. daemon 级实例管理
7. admin API
8. SPA 构建与 embed

不要跳过 1 到 4 直接做页面，否则接口和状态模型会很快返工。

## 5. 前置改造明细

### 5.1 统一配置文件与 legacy 迁移

#### 5.1.1 目标结果

runtime 统一配置切换为：

- `~/.config/codex-remote/config.json`

legacy 文件保留为只读输入：

- `config.env`
- `wrapper.env`
- `services.env`

安装状态继续保留：

- `~/.local/share/codex-remote/install-state.json`

#### 5.1.2 建议 schema

推荐 schema v1：

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
        "id": "legacy-default",
        "name": "Legacy Default",
        "appId": "cli_xxx",
        "appSecret": "secret_xxx",
        "enabled": true,
        "verifiedAt": null,
        "wizard": {
          "credentialsSavedAt": null,
          "connectionVerifiedAt": null,
          "scopesExportedAt": null,
          "eventsConfirmedAt": null,
          "callbacksConfirmedAt": null,
          "menusConfirmedAt": null,
          "publishedAt": null
        }
      }
    ]
  },
  "debug": {
    "relayFlow": false,
    "relayRaw": false
  },
  "storage": {
    "imageStagingDir": "~/.local/state/codex-remote/image-staging",
    "previewStatePath": "~/.local/state/codex-remote/feishu-preview-drive.json",
    "previewRootFolderName": "Codex Remote Previews"
  }
}
```

#### 5.1.3 legacy 映射规则

建议映射如下：

- `RELAY_SERVER_URL` -> `relay.serverURL`
- `CODEX_REAL_BINARY` -> `wrapper.codexRealBinary`
- `CODEX_REMOTE_WRAPPER_NAME_MODE` -> `wrapper.nameMode`
- `CODEX_REMOTE_WRAPPER_INTEGRATION_MODE` -> `wrapper.integrationMode`
- `RELAY_PORT` -> `relay.listenPort`
- `RELAY_API_PORT` -> `admin.listenPort`
- `FEISHU_USE_SYSTEM_PROXY` -> `feishu.useSystemProxy`
- `FEISHU_APP_ID` + `FEISHU_APP_SECRET` -> `feishu.apps[0]`
- `CODEX_REMOTE_DEBUG_RELAY_FLOW` -> `debug.relayFlow`
- `CODEX_REMOTE_DEBUG_RELAY_RAW` -> `debug.relayRaw`

legacy 迁移出的默认 App：

- `id = "legacy-default"`
- `name = "Legacy Default"`

#### 5.1.4 读写规则

必须满足：

- `CODEX_REMOTE_CONFIG` 继续保留为“配置文件路径 override”
- 写入使用临时文件 + 原子 rename
- 权限固定 `0600`
- 未提供的新值不能清空已有 secret
- `config.json` 存在时，完全忽略 legacy env
- 迁移成功后，把旧文件备份为 `*.migrated-<timestamp>.bak`

#### 5.1.5 需要改动的文件

- [internal/config/envfile.go](../../internal/config/envfile.go)
- [internal/runtime/paths.go](../../internal/runtime/paths.go)
- [internal/app/install/service.go](../../internal/app/install/service.go)
- [internal/app/install/wizard.go](../../internal/app/install/wizard.go)
- [internal/app/daemon/entry.go](../../internal/app/daemon/entry.go)
- [internal/app/wrapper/app.go](../../internal/app/wrapper/app.go)
- [internal/runtime/daemon_process.go](../../internal/runtime/daemon_process.go)
- [internal/runtime/headless_process.go](../../internal/runtime/headless_process.go)

#### 5.1.6 额外要求

迁移时如果发现 `install-state.json` 里的 `configPath` 仍指向 `config.env`，应同步更新到 `config.json`，避免管理页展示旧路径。

### 5.2 显式网络绑定与 setup 访问模型

#### 5.2.1 当前问题

daemon 现在是：

- relay server `:9500`
- api server `:9501`

也就是默认全网卡监听。

对于将要录入 App Secret 的 Web setup 来说，这个默认值不可接受。

#### 5.2.2 目标行为

- relay WebSocket 默认只监听 `127.0.0.1`
- admin HTTP 默认只监听 `127.0.0.1`
- SSH 且未完成配置时，允许临时把 admin HTTP 扩大到 `0.0.0.0`
- 即使 setup 暴露，relay WebSocket 也仍只监听 loopback

#### 5.2.3 setup token 规则

实现一套最小认证：

- `setupToken` 只在 setup 模式下生成
- 只用于 `/setup` 和 `/api/setup/*`
- 只保存哈希
- TTL 默认 20 分钟
- 完成配置后立刻失效

管理页 session 可以采用：

- loopback 下 trusted local session
- 非 loopback 下必须通过 startup 打印的带 token 链接换取 session cookie

#### 5.2.4 推荐新增模块

建议新增轻量模块，例如：

- `internal/app/adminauth`

提供：

- token mint / validate
- session cookie encode / decode
- route guard middleware

#### 5.2.5 需要改动的文件

- [internal/app/daemon/app.go](../../internal/app/daemon/app.go)
- [internal/app/daemon/entry.go](../../internal/app/daemon/entry.go)
- [internal/app/launcher/launcher.go](../../internal/app/launcher/launcher.go)
- [internal/app/launcher/role.go](../../internal/app/launcher/role.go)

#### 5.2.6 当前阶段拍板

本轮实现进一步固定为：

- `setup required` 的判断口径是：
  - runtime 生效的飞书 App 中，没有任何一个同时具备 `appId + appSecret`
  - 这里包含 legacy env/runtime override，不只看 `config.json`
- loopback 请求在当前阶段默认视为 trusted local session：
  - 可直接访问 `/setup`
  - 可直接访问 `/api/setup/*`
  - 可直接访问 `/api/admin/*`
- 非 loopback 请求在当前阶段只开放 setup 会话：
  - 通过 startup 打印的 `/setup?token=...` 链接换取 session cookie
  - 配置完成后的正式 admin 远程 session 仍留到后续阶段
- 兼容状态接口 `/v1/status` 也跟随 admin 鉴权：
  - loopback 继续可直接访问
  - setup 暴露到 `0.0.0.0` 时不会把状态接口匿名暴露出去

### 5.3 Surface 与 Gateway 身份模型

#### 5.3.1 当前问题

当前 surface 仍是：

- `feishu:user:<userId>`
- `feishu:chat:<chatId>`

在多 App 在线时会直接碰撞。

另外当前代码里还存在多处硬编码前缀判断，例如：

- `strings.HasPrefix(surfaceSessionID, "feishu:chat:")`

这种写法在新命名格式下会全部失效。

#### 5.3.2 推荐结构

引入显式 identity helper，而不是继续靠字符串猜：

```go
type SurfaceRef struct {
    Platform  string
    GatewayID string
    ScopeKind string
    ScopeID   string
}
```

推荐 surface ID 格式：

- `feishu:<gatewayID>:user:<userId>`
- `feishu:<gatewayID>:chat:<chatId>`

#### 5.3.3 结构字段调整

建议新增或调整这些字段：

- `control.Action.GatewayID`
- `control.UIEvent.GatewayID`
- `control.DaemonCommand.GatewayID`
- `state.SurfaceConsoleRecord.GatewayID`
- `feishu.Operation.GatewayID`
- `feishu.MarkdownPreviewRequest.GatewayID`

这样无需在每一层重复解析 surface string。

#### 5.3.4 需要改动的文件

- [internal/core/control/types.go](../../internal/core/control/types.go)
- [internal/core/state/types.go](../../internal/core/state/types.go)
- [internal/adapter/feishu/gateway.go](../../internal/adapter/feishu/gateway.go)
- [internal/adapter/feishu/projector.go](../../internal/adapter/feishu/projector.go)
- [internal/adapter/feishu/markdown_preview.go](../../internal/adapter/feishu/markdown_preview.go)
- [internal/app/daemon/app.go](../../internal/app/daemon/app.go)
- [internal/core/orchestrator/service.go](../../internal/core/orchestrator/service.go)

#### 5.3.5 回归要求

必须新增回归测试覆盖：

- 两个 App 下同一 `chatId`
- 两个 App 下同一 `userId`
- 相同 surface 文本/图片/recall 不串线

### 5.4 MultiGateway Controller 与热替换

#### 5.4.1 当前问题

daemon 当前只接受一个 `feishu.Gateway`。

这不满足：

- 多 App 同时在线
- 凭证热替换
- 单 App 重连/停用/状态查询

另外按当前仓库实际依赖的 `github.com/larksuite/oapi-sdk-go/v3/ws` 实现来看：

- `Client.Start(ctx)` 内部不会在 `ctx.Done()` 时主动返回
- SDK 没有公开的 `Stop` / `Close` API

因此 2B 不能简单按“worker 持有一个 ctx，取消即完成热替换”实现，必须额外设计连接关闭 workaround 或自管长连接生命周期。

#### 5.4.2 推荐接口

建议把当前 `Gateway` 提升为 controller：

```go
type GatewayController interface {
    Start(context.Context, ActionHandler) error
    Apply(context.Context, []Operation) error
    UpsertApp(context.Context, GatewayAppConfig) error
    RemoveApp(context.Context, string) error
    Verify(context.Context, GatewayAppConfig) (VerifyResult, error)
    Status() []GatewayStatus
}
```

运行模型：

- 一个 enabled App 对应一个 worker
- 一个 worker 内部维护一个 `LiveGateway`
- `Apply` 先按 `GatewayID` 分组，再发到对应 worker
- `UpsertApp` 支持凭证更新后的热替换
- `RemoveApp` 负责关闭 ws client 并清理状态

这里的 `worker` 不应直接把 SDK client 暴露到 controller 外层，否则后续 stop/reconnect workaround 很容易散落到 daemon 代码里。

#### 5.4.3 验证语义

`verify` 不能只做字符串校验，也不能只拿 tenant token。

建议验证定义为：

1. 用提供的 App ID / Secret 创建临时连接
2. 在限定时间内确认长连接建立成功
3. 返回：
   - 连接是否成功
   - 错误码/错误文本
   - 耗时

临时验证通过后，保存动作再交给 controller 执行真正热接入。

#### 5.4.4 需要改动的文件

- [internal/adapter/feishu/gateway.go](../../internal/adapter/feishu/gateway.go)
- [internal/app/daemon/entry.go](../../internal/app/daemon/entry.go)
- [internal/app/daemon/app.go](../../internal/app/daemon/app.go)

#### 5.4.5 状态字段建议

每个 App 至少暴露：

- `disabled`
- `connecting`
- `connected`
- `degraded`
- `auth_failed`
- `stopped`
- `lastError`
- `lastConnectedAt`
- `lastVerifiedAt`

### 5.5 Markdown 预览云盘管理模型

#### 5.5.1 当前问题

当前 preview 只负责：

- 创建根目录
- 创建 scope 目录
- 上传文件
- 查询 URL
- 加权限

没有：

- App 维度隔离
- 远端 list
- delete
- reconcile
- cleanup policy

#### 5.5.2 推荐状态文件

建议新状态文件：

- `~/.local/state/codex-remote/feishu-preview-drive.json`

推荐 schema：

```json
{
  "version": 1,
  "apps": {
    "main-bot": {
      "root": {
        "token": "fld_root",
        "url": "https://...",
        "lastReconciledAt": "2026-04-06T10:00:00Z"
      },
      "scopes": {
        "chat:oc_xxx": {
          "folder": {
            "token": "fld_scope",
            "url": "https://..."
          },
          "lastUsedAt": "2026-04-06T10:00:00Z"
        }
      },
      "files": {
        "file_xxx": {
          "token": "file_xxx",
          "url": "https://...",
          "path": "/repo/docs/README.md",
          "sha256": "abc",
          "scopeKey": "chat:oc_xxx",
          "sizeBytes": 1234,
          "createdAt": "2026-04-06T10:00:00Z",
          "lastUsedAt": "2026-04-06T10:00:00Z"
        }
      }
    }
  }
}
```

#### 5.5.3 API 能力补齐

基于官方 Drive API，至少补这些方法：

- `ListFiles(folderToken)`
- `DeleteFile(token, type)`
- `TaskCheck(taskID)`
- `ListPermissionMembers(token, type)`
- `CreatePermissionMember(...)`
- `DeletePermissionMember(...)`

当前已有：

- `CreateFolder`
- `UploadFile`
- `QueryMetaURL`
- `GrantPermission`

#### 5.5.4 cleanup 语义

推荐两个操作：

1. `cleanup before <days>`
   - 删除 `lastUsedAt` 早于阈值的 preview file
   - 再尝试删除空 scope folder
2. `purge app preview root contents`
   - 清空该 App root 下所有已知内容
   - 但保留 root 自身，避免额外异步重建

这里推荐“逐文件删除 + 逐目录清空”，不要默认直接删根文件夹，因为：

- 文件夹删除是异步任务
- root 复建后需要重新做权限/状态对齐

#### 5.5.5 reconcile 语义

`reconcile` 至少做：

1. 列出 app root 下所有一级节点
2. 与本地 state 的 root/scope/file token 对账
3. 标记：
   - `remote_missing`
   - `local_missing`
   - `permission_drift`
4. 生成摘要给管理页

#### 5.5.6 容量统计边界

由于官方 `list` / `meta` 不直接给出可靠总字节数：

- 页面显示“估算占用”
- 口径为本地 state 记录的 `sizeBytes` 之和
- 对远端孤儿文件只给“未知大小的额外文件数”

#### 5.5.7 需要改动的文件

- [internal/adapter/feishu/markdown_preview.go](../../internal/adapter/feishu/markdown_preview.go)
- [internal/app/daemon/entry.go](../../internal/app/daemon/entry.go)
- [internal/app/daemon/app.go](../../internal/app/daemon/app.go)

### 5.6 本地图片暂存管理

#### 5.6.1 当前问题

图片下载当前落在：

- `os.TempDir()`
- 文件名 `codex-remote-image-*`

这样没法稳定统计，也没法安全清理。

#### 5.6.2 推荐目录

改为：

- `~/.local/state/codex-remote/image-staging/<gatewayID>/`

#### 5.6.3 cleanup 规则

管理页清理不能只按文件时间删。

至少要排除：

- 当前 `ImageStaged`
- 当前仍被 queue item 引用的本地文件

建议规则：

- “清理一天前文件” = 只删未被当前 orchestrator 状态引用、且 mtime 超过阈值的文件
- “全部清空” = 只清空未引用文件，并返回 `skippedActiveCount`

#### 5.6.4 需要改动的文件

- [internal/adapter/feishu/gateway.go](../../internal/adapter/feishu/gateway.go)
- [internal/app/daemon/entry.go](../../internal/app/daemon/entry.go)
- [internal/app/daemon/app.go](../../internal/app/daemon/app.go)
- [internal/core/orchestrator/service.go](../../internal/core/orchestrator/service.go)

### 5.7 daemon 级实例管理

#### 5.7.1 当前问题

当前新建 headless 只存在 surface 路径：

- `ActionNewInstance`
- `/newinstance`
- 先选 thread 再恢复

这不等于管理页要的：

- 手动输入 `workspaceRoot`
- 手动输入 `displayName`
- 直接新建后台实例

#### 5.7.2 推荐新增服务

建议在 daemon 侧增加独立的 managed instance API，不经过 surface 状态机：

- `CreateManagedHeadless(workspaceRoot, displayName)`
- `KillManagedHeadless(instanceID)`

它们只服务 Web admin。

#### 5.7.3 运行时记录

新增 daemon 级 pending launch 记录，例如：

```go
type ManagedInstanceLaunch struct {
    InstanceID    string
    WorkspaceRoot string
    DisplayName   string
    RequestedAt   time.Time
    PID           int
    Status        string
}
```

这样管理页能看到：

- `starting`
- `online`
- `failed`
- `stopping`

#### 5.7.4 启动参数

新增实例时传入：

- `CODEX_REMOTE_INSTANCE_ID`
- `CODEX_REMOTE_INSTANCE_SOURCE=headless`
- `CODEX_REMOTE_INSTANCE_MANAGED=1`
- `CODEX_REMOTE_INSTANCE_DISPLAY_NAME=<displayName>`
- `WorkDir=<workspaceRoot>`

#### 5.7.5 需要改动的文件

- [internal/app/daemon/app.go](../../internal/app/daemon/app.go)
- [internal/runtime/headless_process.go](../../internal/runtime/headless_process.go)

### 5.8 飞书安装 manifest 统一 source of truth

#### 5.8.1 当前问题

现在要求分散在：

- gateway 代码
- `deploy/feishu/README.md`
- `deploy/feishu/app-template.json`
- 未来前端页面文案

已经发生了一次真实漂移：

- 漏掉 `im.message.recalled_v1`

#### 5.8.2 推荐方案

新增一个后端 manifest 源，例如：

- `internal/feishuapp/manifest.go`

内容包括：

- 必需 scopes
- 必需事件
- 必需菜单 key
- 手工步骤 checklist
- scopes JSON 导出

推荐事件清单至少包含：

- `im.message.receive_v1`
- `im.message.recalled_v1`
- `im.message.reaction.created_v1`
- `application.bot.menu_v6`
- `card.action.trigger`

scopes JSON 以当前已确认样例为基线：

```json
{
  "scopes": {
    "tenant": [
      "drive:drive",
      "im:message",
      "im:message.group_at_msg:readonly",
      "im:message.group_msg",
      "im:message.p2p_msg:readonly",
      "im:message.reactions:read",
      "im:message.reactions:write_only",
      "im:message:send_as_bot",
      "im:resource"
    ],
    "user": []
  }
}
```

#### 5.8.3 需要改动的文件

- 新增后端 manifest 包
- [deploy/feishu/README.md](../../deploy/feishu/README.md)
- [deploy/feishu/app-template.json](../../deploy/feishu/app-template.json)

#### 5.8.4 当前阶段拍板

当前阶段已经把 manifest 收口到后端包：

- `internal/feishuapp/manifest.go`

并补了回归约束：

- `deploy/feishu/app-template.json` 中的
  - `scopes_import`
  - 事件列表
  - 菜单 key
- 必须与后端 manifest 保持同步

这样后续页面和文档都可以复用同一份 source of truth。

### 5.9 Admin API 骨架

#### 5.9.1 推荐最小接口集

建议第一批就定义稳定 contract：

- `GET /api/admin/bootstrap-state`
- `GET /api/admin/runtime-status`
- `GET /api/admin/config`
- `PUT /api/admin/config`
- `GET /api/admin/feishu/apps`
- `POST /api/admin/feishu/apps`
- `PUT /api/admin/feishu/apps/:id`
- `DELETE /api/admin/feishu/apps/:id`
- `POST /api/admin/feishu/apps/:id/verify`
- `POST /api/admin/feishu/apps/:id/reconnect`
- `POST /api/admin/feishu/apps/:id/enable`
- `POST /api/admin/feishu/apps/:id/disable`
- `GET /api/admin/feishu/apps/:id/scopes-json`
- `GET /api/admin/instances`
- `POST /api/admin/instances`
- `DELETE /api/admin/instances/:id`
- `GET /api/admin/storage/image-staging`
- `POST /api/admin/storage/image-staging/cleanup`
- `GET /api/admin/storage/preview-drive/:appId`
- `POST /api/admin/storage/preview-drive/:appId/reconcile`
- `POST /api/admin/storage/preview-drive/:appId/cleanup`
- `GET /api/admin/vscode/detect`
- `POST /api/admin/vscode/apply`
- `POST /api/admin/vscode/reinstall-shim`

#### 5.9.1 当前阶段落地约束

当前阶段先把访问模型和 contract 骨架打通，实际落地分成两类：

- 已实现：
  - `GET /api/setup/bootstrap-state`
  - `GET /api/admin/bootstrap-state`
  - `GET /api/admin/runtime-status`
  - `GET /api/admin/config`
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
  - `GET /setup`
  - `GET /`
- 已注册但暂时返回结构化 `501 not_implemented`：
  - `instances`
  - `storage/*`
  - `vscode/*`
  - `PUT /api/admin/config`

这样后续阶段可以在不改路径 contract 的前提下逐步把能力填实。

额外约束：

- 如果当前 runtime 仍由 `FEISHU_APP_ID` / `FEISHU_APP_SECRET` override 驱动：
  - 对应 gateway 在管理页按只读展示
  - 允许查看状态、导出 manifest、执行 verify / reconnect
  - 不允许通过 Web 直接改写该 gateway 的持久化配置

#### 5.9.2 返回值要求

这些接口应尽量满足：

- secret 默认不回显，只回 `hasSecret`
- 状态字段稳定，不让前端拼日志猜
- 错误对象统一：
  - `code`
  - `message`
  - `retryable`
  - `details`

### 5.10 SPA 构建与 embed

#### 5.10.1 当前现实

当前仓库没有前端目录，也没有 Node 工具链。

当前主机上实际观察到：

- `node` 不在 `PATH`
- `npm` 不在 `PATH`

#### 5.10.2 推荐落地方式

建议：

- 新增 `web/`
- `Vite + React + TypeScript`
- 使用 `npm`，降低额外工具前置
- 构建产物输出到 `web/dist`
- 用 `//go:embed` 集成到 daemon 二进制

#### 5.10.3 dev / prod 模式

建议区分：

- dev：
  - Vite dev server
  - 反向代理到本地 daemon API
- prod：
  - embed 静态资源
  - 单二进制发布

#### 5.10.4 当前环境前提

开始做前端前，需要先满足其一：

- 安装 Node LTS
- 或修正当前机器 PATH，让现有 Node 可用

## 6. 建议的代码提交切分

推荐按以下提交或 PR 颗粒度推进：

1. `config.json` + legacy migration + bind host 拆分 + launcher 默认启动行为
2. surface/gateway identity 升级 + MultiGateway controller
3. setup/auth middleware + admin API 骨架
4. image staging manager + preview drive manager
5. daemon 级 managed instance API
6. VS Code detect/reinstall shim API
7. SPA 工程接入与 embed
8. deploy/feishu manifest 与安装文档同步

## 7. 测试矩阵

### 7.1 配置与迁移

- 只有 `config.env` 时自动迁移成功
- 同时存在 `config.json` 与 `config.env` 时优先 JSON
- 保存新配置时不清空旧 secret
- `install-state.json` 的 `configPath` 同步更新

### 7.2 网络与鉴权

- relay 只监听 loopback
- admin 默认只监听 loopback
- setup 模式下仅 admin 可临时暴露到 `0.0.0.0`
- 无 token 不能访问 setup API

### 7.3 多 App 在线

- 两个 App 可同时连上
- 同一 `chatId` 在两个 App 下不串 surface
- outbound `Apply` 正确路由到对应 gateway
- 更新某个 App secret 后无需重启 daemon

### 7.4 Preview 与图片存储

- 图片下载落到专用目录
- cleanup 不删除当前 active staged image
- preview cleanup 可按 `lastUsedAt` 删除
- preview reconcile 能发现本地/远端漂移
- 删除 preview 后历史链接失效的提示存在

### 7.5 实例管理

- 管理页新建实例可输入 `workspaceRoot` / `displayName`
- 仅能删除 managed headless 实例
- pending launch 与 online 状态可观测

### 7.6 安装 manifest

- 事件清单包含 `im.message.recalled_v1`
- scopes JSON 与后端 manifest 一致

## 8. 开工前仍需拍板的实现细节

只剩这些实现级细节还需要最后决定：

1. `config.json` 中 `relay.serverURL` 是显式存储，还是由 bind host/port 推导
2. preview purge 是否默认逐文件删除，还是提供“删文件夹 + task_check”的高级选项
3. SPA 首期是否只支持 Linux 开发环境

## 9. 参考资料

- [web-install-admin-ui-design.md](./web-install-admin-ui-design.md)
- [internal/config/envfile.go](../../internal/config/envfile.go)
- [internal/app/daemon/entry.go](../../internal/app/daemon/entry.go)
- [internal/app/daemon/app.go](../../internal/app/daemon/app.go)
- [internal/adapter/feishu/gateway.go](../../internal/adapter/feishu/gateway.go)
- [internal/adapter/feishu/markdown_preview.go](../../internal/adapter/feishu/markdown_preview.go)
- [internal/app/install/service.go](../../internal/app/install/service.go)
- [internal/runtime/paths.go](../../internal/runtime/paths.go)
- Feishu Drive List
  - https://open.feishu.cn/document/uAjLw4CM/ukTMukTMukTM/reference/drive-v1/file/list
- Feishu Drive Delete
  - https://open.feishu.cn/document/uAjLw4CM/ukTMukTMukTM/reference/drive-v1/file/delete
- Feishu Drive Task Check
  - https://open.feishu.cn/document/uAjLw4CM/ukTMukTMukTM/reference/drive-v1/file/task_check
- Feishu Permission Member Create
  - https://open.feishu.cn/document/uAjLw4CM/ukTMukTMukTM/reference/drive-v1/permission-member/create
- VS Code Remote SSH
  - https://code.visualstudio.com/docs/remote/ssh
- VS Code Command Line
  - https://code.visualstudio.com/docs/editor/command-line
