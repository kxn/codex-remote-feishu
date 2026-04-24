# Turn Diff Frozen Preview Design

> Type: `inprogress`
> Updated: `2026-04-24`
> Summary: 收敛 authoritative turn diff snapshot 的 frozen preview 全流程、final 挂链合同、`/preview` 路由与授权复用、以及 renderer 的降级规则。

## 1. 背景

当前仓库已经打通了：

- 上游 `turn/diff/updated`
- orchestrator 侧 authoritative `TurnDiffSnapshot`
- turn 结束时把 latest aggregated diff 带到 final event

但前台现在仍只有“本次修改”摘要，没有一条真正可读的 turn 级 diff 查看页。

这张单不是在现有 preview 页面里临时塞一段 patch 文本，而是补一条独立能力：

- source of truth 是 authoritative `TurnDiffSnapshot`
- 页面语义是 `turn-scoped`、`frozen`、`read-only`
- 页面目标是“查看这一轮最终改了什么”
- 它与 `#215` / `#144` 的 live workspace reviewer 明确分开

当前已确认的最终用户可见 mock 见：

- `docs/draft/turn-diff-frozen-preview-mock.html`

本文是 `#307` 的实现主设计，不再只描述页面长相，而是把交付链路也一起收口。

## 2. 一句话定义

> `#307` 提供的是一个只读、冻结、按文件切换、带上下文折叠的 turn diff viewer；它不是 raw unified diff 文本页，也不是 live workspace reviewer。

## 3. 产品边界

### 3.1 属于本单的

- final turn diff summary 上的 `查看` 入口
- immutable turn diff snapshot artifact
- 专门的 turn diff viewer renderer
- portrait / landscape 自适应阅读
- hunk 上下文、gap 折叠展开、文件切换、阅读位置记忆
- parse 失败、binary、rename/copy、数据缺失时的明确降级

### 3.2 不属于本单的

- live workspace diff reviewer
- `accept` / `reject` / `revert` / `stage` / `unstage`
- frozen viewer 跳去 live reviewer
- 基于当前磁盘内容的再计算
- 预烘焙永久 HTML 成品

### 3.3 与 `#215` / `#144` 的边界

`#215` / `#144` 解决的是 live workspace 路线：

- 关注当前工作区状态
- 后续可能支持 reviewer 动作
- 语义是“现在仓库是什么样”

`#307` 解决的是 frozen snapshot 路线：

- 只看某一轮结束时的 authoritative diff
- 不依赖当前仓库是否已经继续变化
- 不给任何修改仓库状态的入口

两条路线必须分开，不共享 reviewer 语义，不互相预埋按钮。

## 4. 用户可见合同

### 4.1 入口

主入口跟随 final card 里的“本次修改”摘要头行。

V1 合同：

- 不额外发新消息
- 不在正文里插一段解释
- 只在摘要头行追加一个 `查看` 链接

这个链接的语义是：

- 查看本轮 diff 快照
- 不是打开当前工作区 diff

### 4.2 正常阅读态只保留必要信息

正常阅读态只应出现：

- 文件选择
- 文件名
- `+/-` 统计
- diff 阅读区

正常阅读态不应出现：

- 教学式 banner
- 解释设计意图的提示文案
- “这是 frozen 页面”的长说明
- reviewer 术语和 reviewer 动作

系统说明文案只留给真正异常的状态：

- 过期
- 不可用
- 数据缺失
- 无法渲染

### 4.3 响应式布局

竖屏：

- 顶部是一条轻量 selector bar
- bar 内只保留 preview 图标和文件选择
- 正文一次只显示一个文件

横屏：

- 左侧是文件列表
- 右侧是阅读区
- 正常阅读态不保留顶部 frame / 标题区

这条切换必须是运行时响应式的：

- 不能只在页面初次加载时判断一次
- 用户旋转设备或调整窗口后，布局应立即切换

### 4.4 Hunk / gap 合同

- 默认上下文行数取 `8`
- 大段未修改内容默认折叠
- gap 可展开，也可再次收起
- gap 展开后应与上下代码块无缝衔接
- 当下方 hunk 已与展开后的 gap 连成连续阅读流时，下方 hunk 头条不再保留独立分隔感
- 展开/收起应尽量保持当前阅读位置，不把页面跳回顶部

### 4.5 文件切换与阅读记忆

- 每个文件记住本页会话内的滚动位置
- 文件间来回切换时恢复该位置
- 首次进入文件时滚到第一个 hunk
- 这份状态只保存在页面内存中，不做持久化

## 5. 端到端工作流

## 5.1 触发条件

只有同时满足下面条件时，才生成 turn diff viewer：

- 当前事件是 final assistant block
- 当前 turn 带有非空 `TurnDiffSnapshot.Diff`
- `TurnDiffSnapshot` 对应本轮最终 authoritative diff

不会在这些场景触发：

- 中途流式更新
- 普通 `/help` `/status` 等单步结果
- 没有 diff 的 final

## 5.2 authoritative diff 的来源

当前 authoritative 来源保持不变：

- orchestrator 在 `service_turn_diff.go` 中按 `instance + thread + turn` 暂存上游 `event.TurnDiff`
- turn 终态时把该 snapshot 取出并挂到 final event 上
- 后续 viewer 只消费这个 final event 上携带的 snapshot

也就是说，`#307` 不改变上游 diff 的 capture 语义，只改变它在 final 投递阶段的使用方式。

## 5.3 冻结时机

冻结时机固定为：

- turn 刚结束时
- final event 正在投递之前或投递同一事务内

不允许把冻结时机延后到：

- 用户第一次点击 `查看`
- 页面首次打开时
- 服务端首次渲染时

只要延后冻结，就会引入“仓库后来又变了”的漂移风险，破坏 frozen 语义。

## 5.4 主路径

建议沿用现有 final preview rewrite 主路径，而不是单独再发一个“补链消息”：

1. `deliverUIEventWithContextMode(...)` 识别到 final block
2. 组装 `FinalBlockPreviewRequest`
3. preview 层在同一次调用里完成两件事：
   - 继续处理正文内已有 preview rewrite
   - 若存在 `TurnDiffSnapshot`，生成 frozen turn diff preview artifact 并返回 viewer 链接
4. app 把这条链接作为 delivery-only metadata 挂回 event
5. projector 渲染 final card：
   - 正文仍来自 block
   - “本次修改”摘要头行在有链接时追加 `查看`
6. gateway 发送最终卡片

这里的关键点是：

- viewer 链接是 final delivery artifact，不是新的业务数据
- 不把 URL 塞回 `TurnDiffSnapshot` 或 `FileChangeSummary`
- projector 只负责把已经拿到的链接渲染到摘要头行

## 5.5 second-chance patch

当前 final block 已有 second-chance patch 机制：

- 首次 preview rewrite 超时或失败时，先照常把 final 发出去
- 后台再重跑一次 preview rewrite，并尝试 update 同一张 final 卡

`#307` 的 viewer 链接应复用这条现有机制。

这意味着：

- 首次生成失败时，不额外 append 一条“查看 diff”消息
- second-chance 成功时，直接更新原 final 卡，让摘要头行补上 `查看`
- 用户最终仍只看到一张 final 卡

这条约束直接对应当前产品意见：

- 不要为了 `@`、提醒或补链语义再额外发一条消息
- 应该把真正需要用户点击的入口挂在原事件本身

## 6. final 卡挂链合同

## 6.1 挂载位置

链接只挂在 final card 的文件修改摘要头行。

推荐头行格式：

```markdown
**本次修改** 3 个文件  <font color='green'>+8</font> <font color='red'>-3</font>  [查看](https://...)
```

不改动：

- final 正文内容
- 文件列表各行
- 用时 / token / worktree footer

## 6.2 显示条件

V1 仅在以下条件同时满足时显示 `查看`：

- final card 有文件修改摘要头行
- turn diff preview artifact 成功生成并拿到了 URL

否则：

- 保持当前摘要头行原样
- 不出现空按钮、占位文案或死链

## 6.3 不新增新的卡片形态

不新增：

- 新 owner-card
- 新 request-card
- 新独立 notice

这只是 final card 上的派生 delivery artifact，不是新的前台业务对象。

## 7. 链接、路由与授权合同

## 7.1 路由形态

沿用当前 `/preview` 的公共路由前缀：

```text
/preview/s/<scopePublicID>/<previewID>
/preview/s/<scopePublicID>/<previewID>/download
```

其中：

- `scopePublicID` 复用现有 preview scope 的公开 ID
- `previewID` 在本单中表示“一次 frozen turn diff artifact”，不是 live 文件路径
- `/download` 返回 raw unified diff 下载

## 7.2 为什么继续复用 `/preview`

这里不需要新开一条公网暴露基座。

应直接复用已有：

- external-access listener / provider
- prefix grant
- path-scoped session
- `/preview/s/<scopePublicID>/` 路径授权模型

原因：

- turn diff viewer 本质上也是 preview delivery
- 它只是 artifact 类型和 renderer 变了，不需要重做公网授权层

## 7.3 外链形态

公开链接仍按现有 external-access foundation 发行：

```text
https://<public-base>/g/<grant-id>/?t=<exchange-token>
```

交换完成后建立的 cookie 仍应绑定到：

```text
/preview/s/<scopePublicID>/
```

这样用户在同一条 final message 里点击任意一个 preview 链接后：

- 同 scope 下的其它 preview 链接都可复用会话
- 不需要每个链接重新走一遍授权

## 7.4 grant 复用边界

grant 继续按“消息级 prefix grant”复用。

具体做法：

- 复用 `PreviewGrantKey`
- `TargetBasePath` 继续指向 `/preview/s/<scopePublicID>/`
- 同一条 final message 内：
  - 正文里被 rewrite 的 preview 链接
  - 文件摘要头行上的 turn diff `查看`
  应共享同一个 grant window

跨消息不复用同一个 grant window。

这保证：

- 同一条消息内体验一致
- 新消息不会继续借用旧消息即将过期的 grant

## 7.5 artifact TTL 与外链 TTL

语义上要区分两层寿命：

- artifact / record TTL：本地 frozen 数据寿命，继续复用 preview cache 的 record/blob 机制
- link / session TTL：外链授权寿命，继续复用现有 preview grant 默认值

当前实现基线：

- preview grant 默认 `24h`
- preview record 默认 `7d`
- preview blob 默认 `14d`

因此：

- 链接会过期
- frozen artifact 可以比链接活得更久
- 以后若同 scope 下重新签发新 grant，仍可访问尚未被 GC 的 artifact

## 8. 冻结 artifact 合同

## 8.1 核心原则

冻结的是“数据快照”，不是最终 HTML。

页面打开时由专用 renderer 读取 frozen artifact 渲染最终 HTML，这样可以同时满足：

- 数据语义冻结
- 页面表现可迭代
- 不依赖 live workspace

## 8.2 artifact 建议结构

建议新增独立的 turn diff artifact kind，例如：

- `artifactKind = "turn_diff_snapshot"`
- `rendererKind = "turn_diff"`

建议保存的最小数据结构：

```json
{
  "schemaVersion": 1,
  "threadID": "thread-1",
  "turnID": "turn-1",
  "generatedAt": "2026-04-24T12:00:00Z",
  "rawUnifiedDiff": "diff --git ...",
  "files": [
    {
      "fileID": "0",
      "oldPath": "internal/old.go",
      "newPath": "internal/new.go",
      "displayPath": "internal/new.go",
      "changeKind": "modify",
      "binary": false,
      "parseStatus": "ok",
      "rawHeader": "diff --git ...",
      "rawPatch": "@@ ...",
      "beforeText": "... optional ...",
      "afterText": "... optional ...",
      "hunks": [
        {
          "oldStart": 10,
          "oldLines": 5,
          "newStart": 10,
          "newLines": 6
        }
      ]
    }
  ]
}
```

其中：

- `rawUnifiedDiff` 始终保留 authoritative 原文
- `files[]` 是 viewer 用的结构化冻结数据
- `beforeText` / `afterText` 只在能稳定冻结时保存
- `hunks` 是解析结果，不替代 raw patch

## 8.3 为什么不读当前磁盘

打开页面时回读当前磁盘会直接破坏 frozen 语义：

- 文件可能已经被后续 turn 改过
- 文件可能被用户手动改过
- 文件可能被移动、删除、切支、rebase

因此 V1 明确禁止：

- 页面服务端按当前路径再去读 live 文件
- 用 live 文件内容反推缺失上下文

## 8.4 parser 策略

parser 采用保守策略：

- 只认 diff 文本里显式存在的 `rename from/to`
- 只认显式存在的 `copy from/to`
- 不根据 delete + add 自行推断 rename
- binary header、未知 header、无法匹配的 patch 一律进入降级

这条策略对齐此前已经确认的方向：

- authoritative source 是上游 diff 文本
- viewer 不擅自发明额外语义

## 9. renderer 合同

## 9.1 外层壳

外层壳继续跟现有 preview 页面保持风格一致：

- 基础留白
- 排版节奏
- 过期 / unavailable 系统态

但正文 renderer 不能复用当前普通 file preview 的正文语义。

本单需要的是：

- preview shell 一致
- turn diff renderer 专用

## 9.2 初始定位

页面首次打开时：

- 默认选中第一个有 hunk 的文件
- 若所有文件都无法结构化渲染，则打开 raw diff fallback

首次进入某个文件时：

- 默认滚到第一个 hunk

## 9.3 gap 展开行为

gap 展开后应达到的视觉结果：

- 展开内容直接填进上下两个块之间
- 不再保留一条额外断层
- 下方若已经接成连续阅读流，对应 hunk 的开头条应隐藏

再次收起时：

- 恢复成单条 gap bar
- 阅读位置尽量保持在当前附近

## 9.4 降级阅读态

当某个文件无法形成完整 viewer 时，降级顺序固定为：

1. 文件级 raw patch
2. 整体 raw unified diff

降级不应影响：

- 文件列表
- 基础文件元信息
- 原始 diff 下载

## 10. 组件职责

## 10.1 orchestrator

职责：

- 继续维护 authoritative `TurnDiffSnapshot`
- 在 turn 终态把 snapshot 带到 final event

不负责：

- 生成外链
- 生成 viewer 页面
- 拼接 markdown `查看` 链接

## 10.2 app / daemon

职责：

- 在 final 投递路径组装 `FinalBlockPreviewRequest`
- 继续负责 `PreviewGrantKey`
- 继续负责 second-chance final patch 调度
- 把 preview 层返回的 turn diff viewer metadata 挂回 event

不负责：

- 直接拼 summary markdown
- 在 daemon 层硬编码 Feishu 卡片内容

## 10.3 preview registry / publisher

职责：

- 把 `TurnDiffSnapshot` 冻结成 immutable artifact
- 给 artifact 分配 `previewID`
- 复用当前 preview cache 和 scope manifest
- 复用 `IssueScopePrefix(...)`
- 返回最终 viewer URL

## 10.4 projector

职责：

- 在 final 文件摘要头行追加 `查看`
- 无链接时保持现有 summary 头行不变

不负责：

- 自己签发 URL
- 自己推导 scope / grant

## 10.5 web preview route

职责：

- 复用当前 `/preview` 鉴权入口
- 读取 turn diff artifact
- 返回 turn diff viewer 页面
- 在 `/download` 返回 raw unified diff

## 11. 失败与降级矩阵

### 11.1 生成前

- 没有 `TurnDiffSnapshot`
  - 不生成 viewer
  - final 卡保持当前行为

- `TurnDiffSnapshot.Diff` 为空
  - 视为没有 viewer
  - 不生成空链接

### 11.2 生成中

- artifact publish 失败
  - 首次发送时不挂 `查看`
  - second-chance patch 可再次尝试
  - 若最终仍失败，则 final 卡保持无链接

- grant 签发失败
  - 同上，不出现空链接

- parse 失败
  - 仍可生成 viewer
  - 页面退回 raw patch / raw diff 阅读态

### 11.3 打开时

- preview record 丢失
  - 返回 unavailable 系统态

- preview record 过期
  - 返回 expired 系统态

- 外链授权过期
  - 走现有 external-access 失效语义
  - 不新增 turn diff 专属鉴权分支

- 文件是 binary
  - 不伪装成文本 viewer
  - 显示 binary / raw patch / 下载降级态

## 12. 实现切分建议

### 12.1 carrier 与接口

- 扩展 final preview 结果，让它能返回“turn diff viewer metadata”
- 这份 metadata 只包含 delivery 信息，例如：
  - `URL`
  - `DownloadURL`（若需要）
  - `ScopePublicID`
  - `PreviewID`
- 不把这类字段写回 authoritative `TurnDiffSnapshot`

### 12.2 artifact 与 registry

- 在现有 web preview registry 上新增 turn diff artifact kind
- 复用 scope manifest / blob / TTL / GC
- 为 turn diff artifact 提供 raw diff 下载

### 12.3 final card 渲染

- projector 的文件摘要头行支持追加 `查看`
- 保持无链接场景的输出与现在一致
- second-chance patch 路径也要能把这条链接补回同一张 final 卡

### 12.4 web renderer

- 新增 `turn_diff` renderer
- 复用 preview shell
- 正文渲染遵守当前已确认 mock

## 13. 验证面

- final card 有文件摘要且 turn diff preview 成功时，头行出现 `查看`
- `查看` 链接打开的是 frozen turn diff viewer，而不是 live workspace
- 链接与正文 preview 链接共享同一条消息级 grant
- second-chance patch 成功时，补的是原 final 卡，不是 append 新消息
- 页面横竖屏切换可在运行时响应，不需要刷新
- gap 展开后与上下代码块无缝衔接
- parse 失败、binary、rename/copy header、数据缺失时能明确降级
- `/download` 返回 raw unified diff

## 14. 实现完成标准

- final turn diff summary 可挂出 `查看`
- `查看` 直接落在原 final 卡的摘要头行
- 页面是只读 frozen turn diff viewer
- 打开页面时不依赖 live workspace 读盘
- 页面不出现 reviewer 语义与解释性文案
- portrait / landscape 都能正常阅读
- 页面能按文件切换、按 hunk 阅读、按 gap 展开收起
- 每个文件的滚动位置能在本页会话内记忆
- 解析失败和异常场景有明确降级
- 外层风格与现有 preview 页面保持基本一致

## 15. 当前结论

到这一步，`#307` 的设计已经不再只停留在页面外观，而是具备直接开工所需的核心合同：

- 什么时候生成
- 由谁生成
- 挂在哪里
- 链接长什么样
- 复用哪条授权链路
- 页面读什么数据
- 异常时如何降级

后续实现应直接以本文和已确认 mock 为准，不再回到“是否先做一个 raw diff 页面”这条临时路线。
