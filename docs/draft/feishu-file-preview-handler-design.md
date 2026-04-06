# Feishu File Preview Handler 重构设计

> Type: `draft`
> Updated: `2026-04-06`
> Summary: 收束 preview handler 抽象，明确失败文件动作区交互，并纳入 `docs/draft` 分类。

## 1. 文档定位

这份文档针对一个独立重构需求：

- 把当前“仅处理 `.md` 链接并直接上传飞书 Drive 文件”的实现
- 重构成一个**可扩展的文件预览处理流水线**
- 允许后续为不同文件类型注册独立 handler
- 由框架统一完成上传、授权、链接回写与缓存

本文基于 **2026-04-06** 的仓库状态和官方能力调研输出，目标是先把产品边界、技术抽象和可行性说清楚，不直接进入实现。

## 2. 需求重述

目标需求可以收束为四点：

1. 从 final message 中提取本地文件引用。
2. 对可识别文件走可扩展的 preview handler。
3. handler 只负责把本地文件“物化”为某种预览产物，不直接操心飞书上传细节。
4. 框架统一根据 handler 给出的交付方案，把结果变成：
   - 飞书链接改写
   - 或仅对失败文件生成紧凑动作区
   - 并完成上传、授权、缓存与状态回写。

当前第一期主要关注：

- `.md`

后续预留：

- 代码文件渲染成彩色 HTML diff
- 其他格式的自定义预览

## 3. 当前实现现状

当前仓库已经有一个可工作的 `.md` 预览能力，但抽象很窄：

- 设计文档：`docs/implemented/feishu-md-preview-design.md`
- 代码入口：`internal/app/daemon/app.go`
- 具体实现：`internal/adapter/feishu/markdown_preview.go`
- 装配位置：`internal/app/daemon/entry.go`

当前行为是：

1. 仅在 `UIEventBlockCommitted` 且 block 为 final assistant markdown 时触发。
2. 用正则只扫描 markdown 链接目标。
3. 仅识别 `.md`。
4. 直接读取本地文件。
5. 直接上传成飞书 Drive 文件。
6. 直接做权限开放。
7. 直接改写消息中的链接。

这版能用，但有四个明显问题：

### 3.1 抽象名已经把未来锁死

当前接口叫 `MarkdownPreviewService`，方法叫 `RewriteFinalBlock`，实现叫 `DriveMarkdownPreviewer`。

这等于把“文件识别”“本地预处理”“远端发布目标”“消息改写”全部耦合成了“改 markdown 链接并上传 markdown 文件”这一件事。

### 3.2 本地处理和远端发布耦合过深

`.md` 处理逻辑里直接做了这些动作：

- 路径解析
- 内容读取
- 内容 hash
- 飞书目录创建
- 飞书上传
- 飞书授权
- URL 查询
- 状态持久化

未来如果要支持“代码文件 -> 先渲染 HTML -> 再上传”，会不得不在同一坨逻辑里继续塞分支。

### 3.3 发布目标被硬编码成 Drive 文件

当前 `.md` 预览实际上传的是 **Drive file**，不是飞书原生 docx 文档。

用户这次的新目标更偏向：

- `.md` 生成飞书原生文档
- 在飞书里以文档方式打开

现有抽象没有为“同一种本地文件，不同远端发布方式”留出位置。

### 3.4 缓存键不够表达“handler + publisher”的组合

当前状态文件是 `feishu-md-preview.json`，缓存键实质上是：

- scope
- source path
- content sha

以后如果同一个源文件既可能生成：

- 飞书 docx
- Drive file
- HTML diff

那缓存模型必须显式纳入：

- handler id
- artifact kind
- publisher id

## 4. 重构目标

## 4.1 目标

本次重构建议达成以下目标：

1. 保持现有“只在 final assistant message 中做链接改写”的产品时机不变。
2. 引入可注册的 `file preview handler`。
3. 把“本地物化预览产物”和“发布到飞书”拆成两层。
4. `.md` 默认升级为“飞书 docx 文档优先，Drive file 兜底”。
5. 继续保留自动建目录、自动授权、按 scope 去重、按内容 hash 去重。
6. 为未来的 HTML diff、代码预览、按需上传留好扩展点。

## 4.2 非目标

本次重构不建议同时做：

- 所有消息类型的全量抽取
- 历史消息回填
- 代码 diff 真实渲染实现
- 垃圾回收完整策略实现
- 点击后按需上传的产品闭环

## 5. 推荐抽象

## 5.1 总体结构

推荐把当前单体实现拆成四层：

1. `ReferenceExtractor`
2. `PreviewHandler`
3. `PreviewPublisher`
4. `PreviewRewriteService`

它们的职责边界应当是：

- `ReferenceExtractor`
  - 从 final block 里提取候选文件引用
  - 只做语法层抽取，不负责读文件

- `PreviewHandler`
  - 判断自己是否处理某个本地文件
  - 负责把本地文件转成“预览产物”
  - 同时给出一组按优先级排序的交付方案
  - 不直接调用飞书 API

- `PreviewPublisher`
  - 接收 handler 产物
  - 执行某一种交付方案
  - 负责目录、创建、授权、缓存、URL 返回

- `PreviewRewriteService`
  - 协调整个流程
  - 决定是回写正文链接，还是生成失败文件动作区

## 5.2 为什么不是 `bool`，也不建议只是单个 `action`

`bool` 只能表达：

- 能处理
- 不能处理

但这个需求实际需要表达的是：

- 我能处理 `.md`
- 我优先想发布成飞书原生文档
- 如果失败，再回退到 Drive file
- 如果还是失败，再给用户一个“发送文件”的兜底动作

这已经不是一个布尔值能表达的事。

但我也不建议 handler 只返回单个 `action enum`，因为真实需求更像：

- 一个 handler 产出一个 artifact
- 同时产出一组**有优先级的 delivery plan**

也就是：

- 不是“这次只能 native doc”
- 而是“先 native doc，不行就 drive，再不行就 file-send-action”

## 5.3 推荐接口

下面是建议的 Go 抽象轮廓：

```go
type FilePreviewRewriteService interface {
    RewriteFinalBlock(ctx context.Context, req RewriteRequest) (*PreviewApplyResult, error)
}

type ReferenceExtractor interface {
    Extract(block render.Block) []FileReference
}

type PreviewHandler interface {
    ID() string
    Match(ctx context.Context, req HandleRequest) bool
    Plan(ctx context.Context, req HandleRequest) (*PreviewPlan, error)
}

type PreviewPublisher interface {
    ID() string
    Supports(plan DeliveryPlan, artifact *PreparedArtifact) bool
    Publish(ctx context.Context, req PublishRequest) (*PublishResult, error)
}
```

关键数据结构建议长成这样：

```go
type PreviewPlan struct {
    HandlerID  string
    Artifact   PreparedArtifact
    Deliveries []DeliveryPlan // 按优先级排序
}

type PreparedArtifact struct {
    SourcePath   string
    DisplayName  string
    ContentHash  string
    ArtifactKind string
    MIMEType     string
    Text         string
    Bytes        []byte
    FilePath     string
}

type DeliveryKind string

const (
    DeliveryNativeDocLink DeliveryKind = "native_doc_link"
    DeliveryDriveFileLink DeliveryKind = "drive_file_link"
    DeliveryFileSendAction DeliveryKind = "file_send_action"
)

type DeliveryPlan struct {
    Kind     DeliveryKind
    Priority int
    Metadata map[string]any
}

type PublishResult struct {
    PublisherID string
    RemoteToken string
    RemoteType  string
    URL         string
    Mode        string // inline_link | action_row
}

type PreviewApplyResult struct {
    Block        render.Block
    Supplements  []PreviewSupplement
}

type PreviewSupplement struct {
    Kind string // preview_file_action_row
    Data map[string]any
}
```

## 5.4 为什么一定要拆成 handler + publisher

如果只引入 handler，但上传仍然写死在框架里，后面会马上遇到两个问题：

1. `.md` 想发布成 docx，HTML diff 想发布成 Drive file，这已经不是一个统一上传逻辑。
2. 权限、目录、缓存虽然共性很强，但“创建 docx”“上传 file”“导入文件”“更新 docx blocks”完全不是一套 API。

所以合理的抽象不是“只有 handler”，而是：

- handler 决定“我产出了什么”
- handler 决定“我希望按什么顺序交付”
- publisher 决定“我怎么发到飞书”

## 5.5 结果不一定都是“改链接”

当前实现默认假设：

- 最终结果一定是正文里的 URL 被替换

但这个假设并不总成立，因为飞书普通正文里的 markdown/text 链接本身不能触发 bot callback。

这意味着：

- 能做到“一点就看”的，必须提前发布成可打开的飞书链接
- 如果预览发布失败，后续动作必须挂在交互式卡片按钮上，而不是普通链接上

所以总控层应该返回的不是单纯一个 `render.Block`，而是：

- 改写后的 block
- 再加一组 supplements

不过 supplements 的使用要收紧：

- 不是每条 final message 都补一张卡片
- 只有当某些文件无法被改写为可打开链接时，才为这些失败文件生成紧凑动作区

这样后续 projector / gateway 才有空间把“正文输出”和“失败文件动作区”组合投影出来，同时避免把正常消息都变成卡片流。

## 6. `.md` 的推荐落地路径

## 6.1 结论

`.md` handler 的推荐交付策略不是只选一种动作，而是：

1. 优先：飞书原生 docx 文档
2. 回退：Drive file 链接
3. 再回退：`file_send_action` 兜底动作

其中主路径仍然不是继续上传 Drive 文件，而是：

1. 新建飞书 docx 文档
2. 把 markdown 转成 docx blocks
3. 写入文档
4. 授权
5. 返回 docx URL

这样更符合这次产品诉求里的“创建飞书文档上传”。

## 6.2 为什么不用 import task 作为主路径

飞书 Drive 有导入任务能力，但从官方 SDK 暴露的信息看：

- import task 明确支持导入为 `doc`、`docx`、`sheet`、`bitable`
- 但没有看到对 `md` 作为显式导入源格式的稳定承诺

而 docx API 侧有一条更直接、也更可控的能力链：

1. `docx/v1/documents` 新建文档
2. `docx/v1/documents/blocks/convert` 把 markdown 文本转成 blocks
3. `docx/v1/documents/:document_id/blocks/:block_id/descendant` 写入 block 树

因此对 `.md` 来说，推荐主路径是：

- **docx create + markdown convert + descendant create**

而不是：

- 先上传文件再走导入任务

## 6.3 `.md` handler 的职责

`MarkdownFilePreviewHandler` 只做本地侧工作：

- 识别后缀 `.md`
- 解析相对路径/绝对路径
- 做 workspace/thread cwd 安全限制
- 读取内容
- 计算 hash
- 产出：
  - `ArtifactKind = "markdown"`
  - `MIMEType = "text/markdown"`
  - `Text = 文件原始 markdown 内容`

也就是你说的“对 md 文件就直接给出原路径”的本质升级版：

- 不是直接上传原路径
- 而是把“原始 markdown 内容”作为标准化 artifact
- 再同时返回交付计划，例如：
  - `native_doc_link`
  - `drive_file_link`
  - `file_send_action`

## 6.4 `.md` publisher 的职责

`FeishuDocxMarkdownPublisher` 负责：

1. 确保预览根目录存在
2. 确保当前 scope 子目录存在
3. 创建 docx 文档
4. 调用 markdown convert
5. 写入 block tree
6. 给当前 actor + 当前 chat/group 授权
7. 缓存 token/url/share 状态

如果 docx 发布失败，再由统一框架尝试：

- `FeishuDriveFilePublisher`

作为第二优先级回退。

如果 Drive file 也失败，则进入：

- `FeishuFileSendActionPublisher`

它不直接改正文链接，而是返回一个失败文件动作区计划，让用户后续点击“发送文件”。

## 6.5 发送文件兜底的自然交互

这里需要先明确一个产品边界：

- 飞书普通正文里的链接不能直接触发 bot callback
- 可回调的点击动作必须落在交互式卡片按钮上

因此下面两件事里，只有第二件是可行的：

1. 用户点击正文里的普通链接，然后 bot 收到回调并发送文件
2. 用户点击卡片按钮，然后 bot 收到回调并发送文件

所以我不建议把兜底交互设计成：

- 每个正文链接后都硬塞一个按钮
- 或每条 final message 后都追加一张较大的补充卡片

更自然的首版交互是：

1. 正常可预览的文件，仍然只改写正文链接
2. 只有预览失败的文件，才进入一个紧凑的失败文件动作区
3. 动作区按文件列出：
   - 文件名
   - `发送文件` 按钮
4. 用户点击按钮后，bot 把该文件作为飞书文件消息发到当前会话
5. 动作区局部更新状态：
   - `已发送`
   - 或 `发送失败，可重试`

这样比“整条消息变成按钮卡片”自然得多，原因是：

- 不破坏 assistant 正文的阅读体验
- 不会让所有带文件引用的消息都变得很吵
- 多文件时能自然扩展
- 失败重试也更自然

如果未来消息渲染层升级为更细粒度的 card body 组合能力，同一个 `file_send_action` 也可以被渲染成更贴近链接位置的局部按钮，但这不应该成为首版前提。

## 7. 缓存、版本与垃圾回收

## 7.1 版本策略

建议采用：

- **按内容 hash 创建新文档**
- **同 scope + 同 hash 命中复用**

也就是：

- 文件内容没变，不重复创建
- 文件内容变了，创建新飞书文档并把本次消息链接指向新文档

这比“原地覆盖旧文档”更稳妥，原因有三点：

1. 飞书 docx 的原地覆盖不是单一 API，更新失败更难恢复。
2. 消息一旦发出，旧消息中的历史链接最好继续可读。
3. 基于 hash 的幂等更容易测、也更容易排障。

## 7.2 状态文件

建议把当前 `feishu-md-preview.json` 升级成更泛化的：

- `feishu-file-preview.json`

建议缓存键包含：

- scope key
- source path
- content hash
- handler id
- publisher id

建议状态值包含：

- 远端 token
- 远端类型
- URL
- 已授权 principal 集合
- 最近使用时间
- 创建时间

如果引入 `file_send_action`，还建议额外记录：

- preview ticket
- ticket 过期时间
- ticket 对应的 source path
- ticket 对应的 content hash

这样点击“发送文件”时才能稳定落到正确的源文件版本。

## 7.3 垃圾回收建议

这次不建议在首版里把 GC 做复杂。

推荐第一版只做：

- 状态记录 `lastAccessAt`
- 预留一个离线 GC 命令或后台慢速清理任务

清理策略建议后续再加：

- 只清理超过 TTL 的未访问预览
- 每次最多删有限数量
- 只删除本应用创建、且在状态文件中可追踪的资源

不建议第一版做：

- 引用计数
- 跨消息反向索引
- 覆盖式复用旧 token

## 8. 权限模型

这是这个需求里必须强制保留的部分。

应用创建的飞书文件/文档默认不保证当前会话用户可见，所以预览发布必须是一个原子动作：

1. 创建目录或文档
2. 对当前 actor 授权
3. 如当前是群会话，再对 chat / 群作用域补授权
4. 记录已授权 principal，避免重复打权限 API

推荐继续沿用现有 scope 设计：

- `surfaceSessionID + chatID + actorUserID`

这样权限边界最清晰，也最不容易出现“文档被发到别的会话还能看见”的串读问题。

同时建议保留现有做法：

- 文件夹授权
- 文件/文档级补授权

不要只做目录授权。

## 9. 失败预览文件的动作区与按需动作

这里需要区分两类“点击后的动作”：

1. `file_send_action`
2. “点击后按需上传生成预览”

### 9.1 普通正文链接不能承担回调职责

这点需要在设计上写死：

- 普通 markdown/text 链接只能打开已有 URL
- 不能指望用户点正文链接以后，再回调 bot 去做“上传并发送文件”

所以任何“点击后做事”的能力，都必须经由卡片按钮承载。

### 9.2 `file_send_action` 是值得做的，但只该出现在失败文件上

因为它不是为了实现“单击即开预览”，而是为了在预览失败时提供一个自然兜底。

它的产品语义很简单：

- 你现在看不了内联预览
- 那我把文件本体发给你

这条链路是自然的，也是符合飞书用户习惯的。

但它不应该被设计成：

- 每条 final message 后都追加一个大卡片
- 或每个文件链接后无差别附带发送按钮

更合适的策略是：

- 能改成 docx / drive 链接的，直接改链接
- 只有失败文件才进入失败文件动作区
- 多个失败文件聚合在一块紧凑区域里输出

### 9.3 “点击后按需上传生成预览”仍然不建议做主路径

结论还是和之前一致：

- **技术上能做**
- **但不适合当主路径**

如果没有公网预览网关，点击后按需上传只能是：

1. 用户点“生成预览”
2. 服务端上传并授权
3. 卡片更新成“打开预览”
4. 用户再点一次

这做不到“点一下就开”。

因此推荐策略是：

- 默认仍然走发送前预发布
- 默认兜底走 `file_send_action`
- 新流水线只在架构上保留“按需生成预览”的 future hook

也就是：

- handler/publisher 都可以被复用到“按需生成”场景
- 但本期不把“按需生成预览”作为默认产品路径

## 10. 推荐代码落点

建议保留当前 daemon hook 时机不变，只替换内部抽象。

推荐落点：

- `internal/adapter/feishu/preview/service.go`
  - 重写总控
- `internal/adapter/feishu/preview/extractor_markdown_link.go`
  - 当前链接抽取器
- `internal/adapter/feishu/preview/handler_markdown.go`
  - `.md` handler
- `internal/adapter/feishu/preview/publisher_docx_markdown.go`
  - docx markdown publisher
- `internal/adapter/feishu/preview/publisher_drive_file.go`
  - Drive file fallback publisher
- `internal/adapter/feishu/preview/publisher_file_send_action.go`
  - 失败文件动作区 publisher
- `internal/adapter/feishu/preview/state.go`
  - 泛化状态缓存
- `internal/adapter/feishu/preview/ticket.go`
  - file-send action ticket 持久化与校验

现有 `internal/adapter/feishu/markdown_preview.go` 建议拆解后逐步下线，不继续扩展。

## 11. 测试建议

这次重构的测试必须从一开始就按流水线拆开写。

建议至少覆盖：

1. 链接抽取
   - 相对路径
   - 绝对路径
   - 带 `:line` 后缀
   - 非文件 URL

2. 路径安全
   - thread cwd 内
   - workspace root 内
   - workspace 外拒绝

3. handler 分发
   - `.md` 命中 markdown handler
   - 未识别文件走 pass-through

4. markdown publisher
   - 根目录不存在时自动创建
   - scope 目录不存在时自动创建
   - 文档创建成功
   - markdown convert 成功
   - block 写入成功
   - 权限成功

5. 缓存幂等
   - 同 scope + 同 hash 复用
   - 同路径内容变化创建新文档

6. 失败回退
   - docx publisher 失败时回退 Drive file publisher
   - Drive file 失败时回退 `file_send_action`
   - 全部失败时保留原始链接并记录错误

7. file-send action ticket
   - ticket 生成
   - ticket 过期
   - 点击后发送正确文件版本
   - 源文件缺失时提示失败

8. 交互回归
   - 正文链接改写成功时不生成动作区
   - 进入 `file_send_action` 时保留正文并仅为失败文件追加动作区
   - 多文件场景下动作区只包含失败文件
   - 不会对所有带文件引用的消息都追加统一大卡片

9. 权限回归
   - actor 授权
   - group/chat 授权
   - 已授权成员不重复授权

## 12. 可行性结论

结论是：

- **这个重构完全可做**
- **而且值得做**

原因是当前仓库已经有一条跑通的 preview 链路，真正要做的是：

- 从“单功能实现”升级为“可扩展流水线”

技术风险主要不在抽象，而在飞书 docx 细节：

- markdown convert 的格式保真度
- 超长文档或复杂 markdown 的限制
- 权限 API 的失败重试

另一个需要明确接受的边界是：

- 普通正文链接无法承担 bot 回调
- 所以“点击链接直接把文件发回来”不是可行路径
- 失败兜底必须依赖交互式卡片按钮

但这些都属于可控工程问题，不构成方向性阻碍。

所以推荐决策是：

- 立项
- 先做架构抽象 + `.md` docx publisher
- Drive file 继续作为兜底

## 13. 参考资料

外部资料：

- Agent Client Protocol 官方文档（无直接依赖，但调研时用于对比抽象思路）：https://agentclientprotocol.com/protocol/overview
- 飞书 docx 创建文档 API：https://open.feishu.cn/document/ukTMukTMukTM/uUDN04SN0QjL1QDN/document-docx/docx-v1/document/create
- 飞书 docx markdown 转 blocks API：https://open.feishu.cn/api-explorer?from=op_doc_tab&apiName=convert&project=docx&resource=document&version=v1
- 飞书 docx 创建 block descendant API：https://open.feishu.cn/api-explorer?from=op_doc_tab&apiName=create&project=docx&resource=document.block.descendant&version=v1
- 飞书 Drive 创建文件夹 API：https://open.feishu.cn/document/uAjLw4CM/ukTMukTMukTM/reference/drive-v1/file/create_folder
- 飞书 Drive 增加权限 API：https://open.feishu.cn/document/uAjLw4CM/ukTMukTMukTM/reference/drive-v1/permission-member/create
- 飞书消息卡片概览：https://open.feishu.cn/document/uAjLw4CM/ukTMukTMukTM/reference/im-v1/message-card/overview
- 飞书卡片回调通信：https://open.feishu.cn/document/uAjLw4CM/ukzMukzMukzM/feishu-cards/card-callback-communication
- 飞书 IM 文件上传 API：https://open.feishu.cn/document/server-docs/im-v1/file/create?lang=zh-CN
- 飞书 IM 发送消息 API：https://open.feishu.cn/document/server-docs/im-v1/message/create?lang=zh-CN

本地实现参考：

- 当前预览设计：`docs/implemented/feishu-md-preview-design.md`
- 当前实现：`internal/adapter/feishu/markdown_preview.go`
- 当前接线：`internal/app/daemon/app.go`
- 当前入口装配：`internal/app/daemon/entry.go`
