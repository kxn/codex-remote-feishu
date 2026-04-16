# Web Preview Snapshot Design

> Type: `draft`
> Updated: `2026-04-16`
> Summary: `/preview` snapshot/cache 主设计，收束 prefix grant、blob 落盘、lineage、大文件 diff-first，并补记当前已实现的极简 shell + renderer 分层与 external-access prefix 复用前置。

## 1. 文档定位

这份文档是 `#220` 当前设计收敛后的主设计文档。

它解决的不是单一页面样式问题，而是 `/preview` 这条链路里几个彼此强耦合的问题：

- final message 里的本地文件链接如何统一改写成可访问预览
- external access 应该如何复用授权，不把 grant 粒度做得过细
- 文件预览应该以 live path 还是 snapshot 为 source of truth
- snapshot 应该存在哪里，如何复用、清理、跨重启保留
- 大文件是否应直接预览本体，还是优先看与上一版的 diff

当前建议把这些问题先收在一份主文档里，而不是拆成多份并列草案。原因是：

- 这些设计点现在还在同时变化
- 一个地方改动，往往会牵动另一个地方的边界
- 如果现在就拆成“external grant 一篇”“snapshot cache 一篇”“diff preview 一篇”，source of truth 会变得过于分散

因此当前的文档组织建议是：

- 继续保留已有文档作为上下文输入
  - `docs/implemented/authenticated-external-access-foundation-design.md`
  - `docs/implemented/feishu-md-preview-design.md`
  - `docs/draft/feishu-file-preview-handler-design.md`
- 由本文作为当前 `/preview` snapshot/cache 方向的主设计
- 等实现阶段把契约真正固化后，再考虑把其中稳定部分拆成更窄的长期文档

## 2. 设计目标

### 2.1 目标

当前 `/preview` 方向的目标可以收束为：

1. 给 final message 中的本地文件链接提供统一 web 兜底预览。
2. 预览语义上始终是“单文件”，但访问授权应支持“同一批文件共享同一外链前缀会话”。
3. 预览 source of truth 使用 snapshot，而不是 live path。
4. snapshot 可以跨 turn、跨 daemon 重启复用。
5. 对小文件给 full preview；对大文本文件，在存在上一版时优先考虑 diff-first 预览。
6. 不把 repo/workspace 暴露成文件浏览器，也不把 preview 退化成随意读取本地路径的裸 handler。

### 2.2 非目标

当前文档不直接承诺：

- 通用文件管理器或目录浏览器
- 仓库级 git diff viewer 与 hunk revert/accept 闭环
- 任意 HTML 原样执行
- 第一阶段就完成完整的 cache budget 后台与派生 renderer 生态
- 把所有 preview 场景都泛化成一个完全抽象的平台

## 3. 为什么当前不拆成多份文档

当前这条链路里至少有四个子问题：

1. `/preview` web route 与渲染策略
2. external-access prefix grant 与 session 复用
3. snapshot cache 的磁盘布局、record/blob/TTL
4. 大文件 diff-first 与 lineage 模型

看起来可以拆，但当前不建议这么做。原因是：

- route 设计依赖 grant prefix 形态
- grant prefix 形态依赖 scope 设计
- scope 设计又会影响 snapshot cache key
- diff-first 是否可做，取决于 snapshot 是否预埋 lineage

也就是说，现在拆文档会把一个还没稳定的“耦合设计”人为切成很多孤岛。

当前更合理的方式是：

- 用一份主设计文档收敛这些耦合边界
- 在文档内按章节拆层，而不是拆文件
- 等真正进入实现并稳定后，再按稳定契约拆文档

建议将来的拆分条件是：

- external-access 因 preview 需求新增了通用契约，值得抽独立 general/inprogress 文档
- snapshot cache 预算/GC 已经成熟，值得抽成单独 data/cache 文档
- diff-first renderer 已经从“策略”变成“真实可实施子系统”

在那之前，本文就是主 source of truth。

## 4. 总体分层

当前推荐把 `/preview` 看成四层：

1. `final preview rewrite` 层
   - 从 final assistant 内容里发现本地文件引用
   - 决定该文件是否走 web preview
2. `preview delivery` 层
   - 负责生成或复用 preview record
   - 负责确保存在可用的 external prefix grant
   - 返回可点击的外链
3. `snapshot cache` 层
   - 负责 record/blob/derived artifact 的落盘、复用、TTL 和清理
4. `preview route / renderer` 层
   - 根据 `scopePublicID + previewID` 查 record/blob
   - 根据 artifact 类型、大小、是否存在上一版决定 viewer
   - 当前用户可见页面已经收敛成“固定顶部极简 shell + 下方独立滚动 renderer”的结构：顶部只保留文件名与下载按钮，图片/PDF/文本类 renderer 共享同一套壳层

这四层里，当前最关键的结论是：

- 外网授权和本地 snapshot cache 是两层完全不同的东西
- 外网 URL 是短时凭证
- snapshot cache 是本地可复用数据层
- 单文件边界由 snapshot record 保证，不由 external-access grant 粒度直接保证

## 5. 路由与授权模型

### 5.1 路由形态

推荐 v1 路由形态：

```text
/preview/s/<scopePublicID>/
/preview/s/<scopePublicID>/<previewID>
/preview/s/<scopePublicID>/<previewID>/download
```

语义：

- `scopePublicID` 表示一个可复用 preview scope
- `previewID` 表示该 scope 下的一个单文件预览对象
- `/download` 是显式下载落点，不和默认预览渲染混用

### 5.2 为什么授权要按 scope/prefix，而不是每个文件一个 grant

external-access 基座已经能稳定复用：

- listener
- tunnel/provider
- token -> cookie -> reverse proxy 主链路

真正不适合 preview 场景的，是“如果每个文件都单独 issue grant，就会得到每个文件一个 `/g/<grant>/` cookie path”。

而 preview 的真实使用形态是：

- 一次 final message 常常会产出一批文件
- 下一轮 turn 可能又产出另一批文件
- 两批之间通常有高重合度

因此推荐：

- 每个 preview scope 只维护一个 external prefix grant
- 该 grant 的 `TargetBasePath` 指向 `/preview/s/<scopePublicID>/`
- 浏览器第一次点击这个 scope 下任一链接时，完成一次 token -> cookie 交换
- 同一 scope 下其余文件链接直接复用同一 cookie path

这里有一个实现层约束也已经收敛：

- prefix grant 虽然按 scope 复用，但它依赖的 external-access provider 不能盲目复用旧 tunnel。
- 当前实现要求 provider 在复用前同时满足：当前 tunnel `/ready` 仍健康、且它仍指向这次 listener 的 target。
- external-access idle timeout 当前会回收 listener、grant、session 和 preview scope grant；provider 只在 full shutdown、健康探测失败或 target 漂移时关闭/重启。
- 因此 preview scope 可以稳定复用授权语义，但不会因为 listener 重建而继续发出指向旧 origin 的 stale tunnel。

### 5.3 scope 的定义

当前推荐直接复用现有 drive preview 的 scope 归类逻辑：

- `previewScopeKey(req.GatewayID, req.SurfaceSessionID, req.ChatID, req.ActorUserID)`

原因：

- 现有 `.md` preview 已经按这套维度做去重与授权归组
- 它天然贴近“同一飞书 surface/chat/actor 的连续交互”
- 不需要为 web preview 额外发明第二套 scope 语义

但对外不暴露原始 scope key，而是为它生成稳定的 `scopePublicID`。

## 6. Snapshot，不是 live path

### 6.1 为什么不能直接读 live path

如果 preview 直接回读当前文件路径，会立即出现两个问题：

1. 文件被后续 turn 覆盖时，旧消息链接会悄悄指向新内容
2. 文件被删除时，旧链接会直接失效，用户无法判断是“链接过期”还是“文件没了”

因此当前推荐：

- `/preview` 的 source of truth 是 snapshot
- 链接一旦生成，对应的是一个特定版本的内容
- 后续文件变化不应让旧链接漂移到新内容

### 6.2 snapshot 的生成时机

对于当前 final preview pipeline，这是合理的：

- rewrite 阶段本来就要读本地文件
- 可以在那时计算内容 hash
- 可以直接把 bytes 存成 preview blob

这样 preview route 不再需要依赖原始文件路径来取数。

## 7. 数据模型

### 7.1 preview scope record

每个 scope 至少需要承载：

- `scopeKey`
- `scopePublicID`
- `externalPrefixURL`
- `grantExpiresAt`
- `lastUsedAt`
- 该 scope 下的 preview record 索引

注意：

- `externalPrefixURL` / `grantExpiresAt` 是可以不落盘的短时授权态
- `scopePublicID` 与 record 索引是应当持久化的

### 7.2 preview record

每个单文件预览对象至少需要：

- `previewID`
- `scopePublicID`
- `sourcePath`
- `displayName`
- `artifactKind`
- `mimeType`
- `rendererKind`
- `contentHash`
- `blobKey`
- `lineageKey`
- `previousPreviewID` 或 `previousBlobKey`
- `createdAt`
- `lastUsedAt`
- `expiresAt`

### 7.3 preview blob

每个不可变内容快照至少需要：

- `blobKey`，推荐直接等于 `contentHash`
- `sizeBytes`
- `contentHash`
- `lastUsedAt`
- 可选 `refCount`

推荐按内容寻址，避免同内容被重复写多份。

### 7.4 derived artifact

当前 v1 不强制实现，但数据模型应预留：

- diff html
- rendered markdown/html safe output
- 其他以后可能新增的只读派生物

这类对象本质上是“从 blob 派生出的可重建内容”，不应和原始 snapshot blob 混在一起。

## 8. Snapshot 到底存在哪里

当前建议明确：

- external prefix grant / session：只放内存
- preview record / blob / derived artifact：落盘
- 落盘目录：当前实例的 `DataDir/preview-cache/`

不建议的落点：

- `install-state.json`：这是安装/升级事务状态，不适合塞 feature-specific preview 元数据
- `config.json`：preview snapshot 不是配置
- `StateDir`：更适合 PID / lock / identity / 运行态小文件
- repo/workspace 目录：会污染用户工作区

推荐 v1 目录布局：

```text
<DataDir>/preview-cache/
  scopes/
    <scopePublicID>.json
  blobs/
    sha256/ab/<hash>.bin
  derived/
    diff/<leftHash>__<rightHash>.html
```

其中：

- `scopes/` 保存 scope manifest 与 preview record 元数据
- `blobs/` 保存不可变原始快照
- `derived/` 预留给 diff 或其他 render 派生结果

## 9. 为什么 metadata / blob 要分层

不建议把所有状态塞进一个大 JSON，也不建议只保存路径回源读取。

推荐 metadata / blob 分层，原因是：

1. metadata 改动频繁，blob 一旦写入就是不可变的
2. 同内容可跨 record 复用 blob
3. scope manifest 能保持小而可原子写回
4. 后续要做 diff/render cache 时，有自然的 `derived/` 落点

换句话说：

- scope/record 是索引层
- blob 是数据层
- derived 是可重建产物层

## 10. 重启语义

当前推荐语义：

- daemon 重启后，external grant/session 失效
- daemon 重启后，preview cache 仍然保留

因此：

- 旧外链不承诺在 daemon 重启后继续可用
- 但同一 scope / 同一文件快照再次出现时，应能快速复用已有 cache，并重新签发新外链

这能把“访问授权的短时性”和“数据缓存的可复用性”分离开。

## 11. 上限、预算与清理

既然 snapshot 走落盘路线，v1 就必须有显式边界。

### 11.1 单文件上限

推荐沿用或扩展当前 `MaxFileBytes` 概念：

- 超过上限的文件，不进入普通 full snapshot preview
- 是否允许进入 diff-only 路径，可由后续策略决定

### 11.2 总缓存预算

推荐后续至少支持：

- TTL 清理
- 总字节预算
- 基于 `lastUsedAt` 的 LRU 式淘汰

推荐清理顺序：

1. 删除过期 record
2. 删除无引用 blob
3. 若仍超预算，再按 `lastUsedAt` 继续淘汰

即使 v1 不马上实现完整预算控制，目录结构和 metadata 也应该为这件事预留位置。

## 12. Lineage：为 diff-first 预埋版本血缘

如果以后要支持“看和上一版的 diff”，当前数据模型里就必须有版本血缘。

### 12.1 为什么需要 lineage

问题不是所有文件都适合预览本体：

- 小文件：full preview 很合适
- 大文本文件：full preview 往往不合适
- 但大文本文件和上一版的 diff，常常仍然是可读、可用的

所以 preview model 不能只知道“这是一个文件快照”，还要知道“它属于哪条版本演进线”。

### 12.2 推荐最小 lineage 模型

推荐每个 record 至少持有：

- `lineageKey`
- `previousPreviewID` 或 `previousBlobKey`

v1 最小生成方式：

- `lineageKey = scope + normalized source path`
- 同一 lineage 下出现新的 content hash 时：
  - 生成新 record
  - 把它挂到上一版 record 后面

这样即使 v1 先不真正实现 diff 页面，也不会把后续能力堵死。

## 13. Diff-first 策略

### 13.1 默认策略建议

当前更推荐的产品策略是：

- 小文件：默认 full preview
- 大文本文件：若存在上一版，则优先 diff preview
- 大二进制文件：默认不做 full preview，只给元信息和下载

也就是说，`/preview` 最终应该是“为 artifact 选择合适 viewer”，而不是“一律原样吐文件”。

### 13.2 和 `#144` 的关系

这里要明确区分两类 diff：

1. 本文的 diff preview
   - artifact snapshot 对 artifact snapshot
   - 关注“这个产物相对上一版改了什么”
2. `#144` 的 git diff viewer
   - repo 级、git-backed、可能带 hunk 操作
   - 关注 working tree / commit / revert / accept

二者可以共享：

- external access 基座
- prefix grant 经验
- 某些 renderer/HTML 承载经验

但 source of truth 和产品语义不同，不应混成同一张单。

## 14. 为什么这次先出一份主文档，不拆多份

当前推荐的组织方式是：

- 一份主文档：本文
- 其余文档继续作为相邻上下文
  - external-access 基座文档
  - Feishu md preview 已实现文档
  - file preview handler 抽象文档

不建议现在再拆成：

- grant 设计一份
- cache 设计一份
- diff preview 一份
- renderer 一份

原因是这些边界还未稳定，拆散后更难同步。

## 15. 建议实施顺序

### 阶段 1：数据与授权底座

- 新增 `/preview/s/<scopePublicID>/<previewID>` 路由
- 落 preview scope / prefix grant 复用
- 落 `DataDir/preview-cache/` 目录结构
- 落 scope manifest + blob 持久化
- 预埋 lineage 字段

### 阶段 2：web preview fallback 接入

- `md/html` 云盘失败时回退 web preview
- 非 `md/html` 本地链接进入 web preview
- 同一批文件复用同一 prefix grant
- path+hash 未变时复用已有 record/blob

### 阶段 3：基础 renderer

- 继续沿当前统一 shell 扩展 renderer，不再回到按文件类型各自复制整页壳层的做法
- markdown
- 文本/code/json/log 等文本类
- image
- pdf
- download-only
- html 先以源码或安全模式处理，不原样执行

### 阶段 4：diff-first 与预算控制

- 大文本文件 diff-first 视图
- lineage 驱动的 artifact-to-artifact diff
- cache budget / GC
- 更复杂 renderer 的后续扩展

## 16. 何时再拆文档

如果下一轮继续细化后，已经需要明确：

- preview cache 的 GC / budget 契约
- diff renderer 的格式与来源约束
- external-access 为 preview 新增通用 helper 或 API

那时再拆独立文档更合理。

在那之前，本文就是这条链路的主设计文档。
