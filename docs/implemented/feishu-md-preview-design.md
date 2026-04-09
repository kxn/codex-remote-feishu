# Feishu Markdown 预览设计

> Type: `implemented`
> Updated: `2026-04-09`
> Summary: 文档已同步到 fixed-root remote-first inventory 语义，移除旧的 marker / 文件名前缀 / reconcile 依赖描述。

## 1. 文档定位

这份文档描述的是 **Feishu relay 新增“本地 Markdown 文件在飞书端可点击预览”能力** 的产品设计与可行性结论。

文档范围覆盖：

- 用户问题与目标
- 方案比较与推荐决策
- 飞书开放平台能力边界
- 权限与可见性模型
- 版本管理与垃圾回收
- 结合当前仓库分层的实现边界

截至 **2026-04-05**，本文中的飞书能力判断已基于官方文档做过调研验证；对应的 V1 链路也已经在本仓库实现并接入当前 `relayd -> Feishu` 发送流程。本文因此同时描述产品设计与当前落地边界。

## 2. 背景与问题

当前 relay 在 Feishu 侧会投影 Codex 的文本与卡片消息。

在真实使用中，Codex 经常会在输出里引用本地文件路径，例如：

- `README.md`
- `docs/general/architecture.md`
- `/workspace/docs/general/feishu-product-design.md`

这些路径在 VS Code 或本地终端里是可打开的，但在 Feishu 中只是普通文本。用户点击后无法看到文件内容。

用户当前主要诉求是：

- 当回复中出现本地 Markdown 文件引用时
- 在 Feishu 里能直接点击
- 打开后看到文件内容预览
- 不依赖用户再回到服务器或 VS Code

附加约束：

- 当前主要关注 `.md`
- 该能力在需求提出时尚未支持，当前仓库已完成 V1 落地
- 使用场景要求 Feishu 端可用，但 **不希望为了这个功能额外建设公网预览网关**
- 历史调试中已经发现：**应用创建的飞书文件默认用户未必可见**，如果不显式处理权限，就会产生“死文件”

## 3. 产品目标

### 3.1 目标

新增一个 Feishu 侧能力：

- 当 assistant 最终回复中出现可识别的本地 `.md` 文件引用时
- relay 自动把该文件上传到飞书云文档/云空间
- 自动完成当前会话用户或群聊的访问授权
- 在 Feishu 输出中提供可点击的飞书内链
- 用户点击后直接在飞书侧预览内容

### 3.2 非目标

本期不做：

- 任意二进制文件预览
- 任意代码文件预览
- 外网自建 HTML 预览页
- 完整的 Markdown AST 高保真转 Docx 渲染
- 所有历史消息的回填改写
- 跨租户公开分享策略

## 4. 核心结论

### 4.1 推荐方案

**推荐采用“预上传到飞书 + 自动授权 + 在 Feishu 消息中投放飞书链接”的方案。**

这是在“不建设公网预览网关”的约束下，唯一能稳定实现 **单击即可预览** 的方案。

### 4.2 不推荐方案

**不推荐把“点击后按需上传”作为主路径。**

原因不是做不到，而是做不到“单击即开”：

- 如果没有公网预览网关，点击动作只能先打到 Feishu 卡片回调
- 卡片回调更适合返回 toast 或更新卡片
- 不适合在一次点击里动态生成新飞书文件并立刻完成稳定跳转

因此，“点击后按需上传”最多只能做成：

1. 用户点一次“生成预览”
2. 服务端上传并授权
3. 卡片更新为“打开预览”
4. 用户再点一次

这不是理想体验，只能作为备选降级路径。

### 4.3 关键权限结论

应用使用 `tenant_access_token` 创建的文件/文档属于 **应用云空间**。

用户默认 **没有** 访问权限，即使用户本身是管理员，也可能无法直接打开。

因此这个功能若要可用，必须把“文件上传”与“权限开放”视为一个原子产品动作：

- 只上传不授权 = 无效
- 只授权文件夹不校验文件 = 风险过高
- 推荐做法：**文件夹授权 + 文件级补授权**

## 5. 方案比较

### 5.1 方案 A：预上传飞书 Drive 文件并插入链接

流程：

1. assistant 最终回复中识别出 `.md` 本地文件引用
2. relay 读取文件内容
3. 上传到飞书云空间
4. 给当前用户或当前群授予访问权限
5. 获取飞书文件 URL
6. 在 Feishu 输出中展示可点击链接

优点：

- 不需要公网预览网关
- 用户体验最好，接近“单击即看”
- 可直接复用飞书原生文件预览
- 与当前 Feishu Markdown / 卡片投影兼容
- 文件所有权仍在应用侧，便于版本管理和 GC

缺点：

- 发送最终回复前要多一次上传与授权开销
- 需要处理权限失败和限频
- 会在飞书云空间沉淀版本文件，需要做清理

结论：

- **主方案**

### 5.2 方案 B：直接发飞书 `file` 消息

流程：

1. 上传 `.md` 到 IM 文件或云空间
2. 直接作为文件消息发到聊天

优点：

- 简单直接
- 聊天内天然有可点文件实体

缺点：

- 会额外制造消息噪音
- 与原始 assistant 回复割裂
- 不适合一个回复里出现多个引用文件
- 版本管理与去重展示较差

结论：

- 可作为降级方案
- 不适合作为默认主路径

### 5.3 方案 C：点击后按需上传

流程：

1. 先发一个“生成预览”按钮
2. 用户点击按钮
3. relay 收到卡片回调后上传文件并授权
4. 再更新卡片为“打开预览”

优点：

- 只有用户真正需要看时才上传
- 可减少无效文件沉淀

缺点：

- 无公网网关时做不到“单击即开”
- 只能两步交互
- 卡片回调需要额外稳定性保障
- 当前仓库使用的卡片结构仍偏旧，接入复杂度更高

结论：

- 可做备选
- **不作为默认产品路径**

### 5.4 方案 D：自建公网预览网关

流程：

1. 消息里放自有公网 URL
2. 用户点击后由网关按需读取本地文件并渲染

优点：

- 可做真正按需生成
- 可完全自定义预览效果

缺点：

- 需要公网地址
- 需要单独维护权限、鉴权、缓存、日志与攻击面
- 与当前约束相冲突

结论：

- 当前场景下直接排除

## 6. 推荐产品形态

### 6.1 默认体验

当最终回复中出现被识别的本地 `.md` 文件引用时：

- 保留原有正文
- 对可解析且可上传成功的路径，替换为飞书可点击链接
- 链接目标为飞书文件预览页

用户看到的效果应接近：

```text
你可以先看这两个文档：
- [README.md](https://...)
- [docs/general/architecture.md](https://...)
```

### 6.2 失败时体验

若某个文件未能成功物化为飞书预览链接：

- 不阻塞整条 assistant 回复
- 保留原始 Markdown 链接目标，不额外插入用户可见失败脚注
- 在 daemon 日志中记录失败原因，便于排障

当前实现不会额外发送“预览失败”系统提示或卡片。

### 6.3 支持范围

V1 只处理：

- 后缀为 `.md` 的文件
- 文件真实存在
- 文件位于允许的工作区范围内
- 文件大小低于上传阈值

V1 不处理：

- `.go`、`.ts`、`.json`
- 目录路径
- 不存在的路径
- 工作区外路径
- 超大文件

## 7. 路径识别规则

V1 当前实际只识别 **assistant final block** 里的 Markdown 链接目标，不扫描用户输入，也不扫描内部 helper 流量。

当前支持的引用形态：

- ``[README](README.md)``
- ``[设计文档](docs/general/architecture.md)``
- ``[README](README.md:12)``
- ``[README](README.md#L12)``
- ``[README](README.md#L12C3)``
- ``[绝对路径](/workspace/README.md)``

当前不处理：

- 反引号包裹的路径
- 纯文本裸路径 `foo/bar.md`
- 代码块里的路径
- 非 Markdown 链接目标里的路径

解析规则：

- 只解析 Markdown 链接中的 `(...)` 目标
- 绝对路径：直接校验
- 相对路径：优先以当前 thread 的 `cwd` 解析
- 若 `cwd` 不可用，则回退到 instance `workspaceRoot`
- 若 `workspaceRoot` 仍不可用，则回退到 daemon 进程当前工作目录
- `:line`、`:line:column`、`#L12`、`#L12C3` 这类后缀当前只用于容错解析源文件，不会保留为飞书侧行号跳转语义

安全规则：

- 路径必须落在当前允许工作区内
- 拒绝路径穿越
- 拒绝符号链接逃逸到工作区外
- 拒绝上传不存在文件

## 8. 权限模型

### 8.1 为什么权限必须单独处理

应用创建的云文档/文件属于应用。

飞书官方权限 FAQ 明确说明：

- 应用创建的云文档不会自动对个人开放
- 需要通过接口授权，或开放链接访问，或转移所有者

因此，“上传成功”不等于“用户可访问”。

### 8.2 推荐权限策略

默认采用 **会话级最小授权**：

- `P2P` 会话：授权给当前用户
- `群聊` 会话：同时授权给当前用户和当前群

对应原则：

- 单聊只让当前用户看
- 群聊优先保证当前触发用户可看，同时把当前群作为协作者加入
- 不默认开放到整个租户

当前实现按“至少一项授权成功即可使用”的原则工作：

- 单聊下要求当前用户授权成功
- 群聊下会依次尝试“当前用户 + 当前群”
- 如果二者都失败，则保留原始本地链接，不替换为飞书预览链接

### 8.3 授权对象

根据 surface 类型区分：

- `feishu:user:<user>` -> 授权 `user`
- `feishu:chat:<chat>` -> 授权 `chat`

授权标识优先级：

- 用户：根据当前 `actorUserID` 自动推断为 `openid` / `unionid` / `userid`
- 群：当前实现使用 `openchat`

当前代码里的判定规则是：

- `ou_` 前缀 -> `openid`
- `on_` 前缀 -> `unionid`
- 其他 -> `userid`

因此若上游状态里保存的不是上述 ID，或群聊侧拿到的不是 `openchat`，则需要先在状态链路里补齐，不能把别的标识直接拿去做协作者授权。

### 8.4 文件夹与文件双授权

尽管产品层面通常可认为“文件夹协作者可访问文件夹中的文档内容”，但官方文档没有对“应用创建的新文件在共享文件夹内一定无条件继承可见性”给出足够强的接口级保证。

因此 V1 不依赖隐式继承，采用更稳妥的策略：

1. 先对会话专属文件夹授权
2. 再对新上传文件做一次显式授权

这样可以避免再次出现“目录可见但文件死链”或“新文件未继承成功”的问题。

### 8.5 可选宽松模式

可提供一个配置开关，允许将预览文件设置为：

- `tenant_readable`

即组织内拿到链接的人可阅读。

这个模式的优点是简单，但默认不启用，原因是：

- 权限范围明显变大
- 与“当前对话用户/群”最小暴露原则不一致

### 8.6 落地所需飞书应用能力

除了基础收发消息能力外，当前落地还需要明确开通以下能力：

- 事件订阅：`im.message.receive_v1`
- 事件订阅：`im.message.reaction.created_v1`
- 事件订阅：`application.bot.menu_v6`
- 事件订阅：`card.action.trigger`
- 若需要和机器人单聊：P2P 消息接收权限
- 若要启用 `.md` 预览：推荐直接开通 `drive:drive`

`drive:drive` 在当前实现中覆盖了这些实际调用：

- 自动创建根目录和会话目录
- 上传 Markdown 文件
- 查询文件访问 URL
- 给目录和文件增加协作者

如果不开通这部分 Drive 权限：

- 机器人主对话能力仍可使用
- 但 `.md` 本地链接会保持原样，不会被替换成飞书预览链接

具体开通项已同步写入：

- `deploy/feishu/README.md`
- `deploy/feishu/app-template.json`

## 9. 文件组织与缓存

### 9.1 当前目录策略

当前实现把托管 inventory 根目录固定为：

```text
Codex Remote Previews/
```

这个目录是当前唯一的远端托管 boundary：

- 只要位于这个根目录内，就视为托管 preview inventory
- 根目录之外的飞书云盘内容不会被 cleanup 触碰
- 不再通过 gateway marker 或文件名前缀判断 ownership

根目录下当前仍会按 surface 建子目录，主要用于权限组织和缓存复用：

```text
Codex Remote Previews/
  feishu-user-<id-short>/
  feishu-chat-<id-short>/
```

但这些子目录不再承担“是否托管”的判定语义；真正的产品边界只有固定 inventory 根目录本身。

### 9.2 缓存键

推荐缓存键：

```text
surface_session_id + absolute_path + content_sha256
```

原因：

- 同一路径在不同会话下权限不同，不能直接复用
- 同一路径文件内容变化时，应视为新版本
- 同内容重复引用时应复用已有飞书文件，避免重复上传

### 9.3 索引数据

当前实现会把每个 gateway 的本地缓存状态分别持久化到运行时 state 目录，例如：

```text
~/.local/state/codex-remote/feishu-md-preview-<gateway>.json
```

持久化结构不是“平铺索引表”，而是三层：

- `root`
  - 根目录 `Codex Remote Previews/` 的 `token` / `url`
- `scopes[scope_key]`
  - 每个会话目录的 `token` / `url`
  - 已授权主体集合 `shared`
- `files[scope_key|absolute_path|content_sha256]`
  - `path`
  - `sha256`
  - `token`
  - `url`
  - 已授权主体集合 `shared`

其中：

- `scope_key` 默认取 `surface_session_id`
- 文件唯一键等价于 `surface_session_id + absolute_path + content_sha256`
- 本地状态现在只作为缓存和辅助元数据使用，不再是 preview inventory 的治理真源
- 远端 status / cleanup 会以固定 inventory 根目录做 remote-first 发现和统计
- 旧状态里的 marker 信息或旧文件名前缀，不再是继续治理的前提

当前还**没有**持久化以下治理字段：

- `created_at`
- `last_referenced_at`
- `deleted_at`
- 独立的 GC 标记

## 10. 版本策略

### 10.1 推荐：不可变版本

同一 Markdown 文件发生变化时，**新建新文件，不覆盖旧文件**。

当前实现已经按这个策略落地：同一路径但内容哈希变化时，会生成新的飞书文件；同一路径且内容哈希不变时，则复用已存在文件。

例如：

- `README--a1b2c3d4.md`
- `README--f6e7d8c9.md`

原因：

- 老消息点开后仍应看到当时引用的版本
- 避免“历史回复语义漂移”
- 便于排查和回滚

### 10.2 不推荐：覆盖旧文件

覆盖模式会带来：

- 老消息打开的是新内容
- 历史语义不稳定
- 并发写入更复杂

因此不作为默认策略。

## 11. Garbage Collection

### 11.1 当前已上线的 cleanup 边界

当前实现已经上线 preview inventory cleanup，但它的语义是：

- 以固定根目录 `Codex Remote Previews/` 为唯一托管 boundary
- 只治理这个目录里的 preview 文件
- 根目录外任何飞书云盘内容都不触碰
- 本地状态不是 cleanup 真源，只是缓存

### 11.2 当前删除规则

当前 cleanup 分两步执行：

1. 先删除本地缓存里已知、且 `lastUsedAt/createdAt` 早于 cutoff 的文件记录
2. 再 remote-first 扫描固定 inventory 根目录下的全部托管子目录，删除未被当前本地缓存跟踪、但已早于 cutoff 的远端文件

这意味着：

- state 丢失时，仍可围绕远端 inventory 继续清理
- 旧格式 `__crp__` 文件和旧 gateway marker 目录不会被特殊排除
- 只要内容还在 fixed inventory 根目录内，就继续按托管规则治理

### 11.3 当前触发方式

当前已经落地两条 cleanup 路径：

- 惰性清理：上传新 preview 前，按节流间隔做一次 cutoff 清理
- 管理页手动清理：管理员可按时间阈值主动触发 preview cleanup

二者都会调用飞书删除文件接口；删除后文件进入飞书回收站，而不是立即硬删除。

### 11.4 当前仍未做的治理项

当前尚未落地的部分包括：

- 每个 `absolute_path + surface` 的 `N` 版本上限策略
- 更积极的定时批量清理
- 空目录 / orphan scope folder 的进一步回收策略

## 12. 单击即开与按需上传的结论

### 12.1 在无公网网关前提下，单击即开如何实现

要实现“用户点一下就看到内容”，链接在消息发出时就必须已经存在。

因此无公网网关条件下，唯一稳定方案是：

- **发送前预上传**

### 12.2 点击后按需上传能不能做

能做，但只能两步，不适合作为默认主体验。

原因：

- Feishu 卡片回调适合服务端处理交互并返回 toast / 更新卡片
- 不适合在回调里通过 HTTP 3xx 完成动态跳转
- 没有公网预览网关时，用户第一次点击只能触发“生成”，不能直接看到内容

因此：

- **点击后按需上传：技术上可行**
- **单击即看：在当前约束下不现实**

## 13. 与当前仓库分层的关系

### 13.1 wrapper

不承担该功能的产品逻辑。

wrapper 不应：

- 解析 Feishu 可见性策略
- 决定哪些本地路径要上传
- 决定哪些链接对谁可见

### 13.2 server/orchestrator

当前 V1 实现并没有把这条链路放在独立 orchestrator 阶段，而是挂在 daemon 的 **pre-projection rewrite** 阶段：

- 触发点：`UIEventBlockCommitted`
- 位置：`internal/app/daemon/app.go`
- 时机：projector 投影到 Feishu 之前

daemon 在这里负责：

- 只拦截最终 assistant Markdown block
- 收集 `surfaceSessionID`、`chatID`、`actorUserID`
- 收集 `workspaceRoot` 与 thread `cwd`
- 调用 Markdown preview service 对 block 文本做原位改写
- 失败时只记日志，不阻塞后续投影

更重的索引、上传、授权、状态落盘逻辑当前封装在 Feishu preview service 内，而不是散落在 projector 中。

### 13.3 Feishu adapter

Feishu 适配层负责：

- 调用上传文件接口
- 调用协作者权限接口
- 调用权限设置接口
- 取得文件 URL
- 维护本地预览状态缓存
- 将已物化好的飞书链接回写给 daemon

### 13.4 projector

projector 只负责渲染最终可见内容，不直接做上传与权限决策。

更合适的职责是：

- 消费“已带飞书预览链接”的 block 或 UIEvent
- 输出 markdown 链接或预览提示卡片

## 14. 建议的实现边界

### 14.1 V1 最小实现

V1 当前实际已实现：

- 仅处理 final assistant Markdown block
- 仅处理 Markdown 链接目标中的 `.md`
- 仅处理真实存在且位于允许工作区内的文件
- 发送前预上传到飞书云空间
- 自动创建固定 inventory 根目录 `Codex Remote Previews/`
- 自动创建 surface 级目录
- 文件键按 `surface_session_id + absolute_path + content_sha256` 去重
- 新文件名不再依赖托管前缀；旧前缀文件仍会被纳入固定 inventory 治理
- 给目录和文件都补 `view` 协作者权限
- 单聊授权当前用户；群聊尝试同时授权当前用户和当前群
- 获取飞书 URL 后，直接替换 Markdown 链接目标
- 父目录或远端资源丢失时会自动清缓存并重建一次
- admin status / cleanup 已切到 remote-first inventory 语义
- 管理面已不再暴露本地 reconcile 能力
- 任一文件失败都不阻塞主回复

### 14.2 V1 不做

- 富文本 AST 转换
- 代码块高保真渲染
- 点击后现生成
- 多文件聚合预览卡片
- 跨 surface 共享同一文件实体
- 裸路径识别

## 15. 风险与失败模式

### 15.1 权限失败

可能原因：

- 应用自身没有文档分享权限
- 应用对目标文件夹没有足够权限
- 群协作者不可见
- 用户与调用身份不满足可见性约束

产品处理：

- 不阻塞主回复
- 保留原始路径
- 记录日志

### 15.2 上传限频或飞书 API 波动

上传、授权、删除都有限频。

产品处理：

- 单文件失败不影响整条回复
- 命中“父目录/资源不存在”这类错误时，清理对应缓存后重建一次
- 其余错误记录日志并回退原始链接
- 同路径同内容命中本地状态时避免重复上传

### 15.3 文件内容敏感

一旦上传到飞书，即进入企业文档系统。

产品处理：

- 仅上传 `.md`
- 仅上传 assistant 明确引用的路径
- 限制工作区范围
- 可加总开关允许用户在配置中禁用该能力

## 16. 可行性结论

### 16.1 已验证可行

以下能力官方明确支持，因此该需求 **可实现**：

- 上传文件到飞书云空间
- 获取文件元数据和 URL
- 给文件或文件夹增加协作者
- 更新公共权限设置
- 删除旧文件
- 在消息文本或卡片 Markdown 中放可点击链接

### 16.2 需要额外注意但不构成阻塞

- 应用创建文件默认不对用户开放
- 群授权依赖机器人在群内且对群可见
- 单击即开要求预上传，不能依赖点击后现生成

### 16.3 最终判断

**该需求在当前产品约束下是可行的。**

并且 V1 已经完成了这条最小可用链路：

1. 识别 final assistant Markdown 链接里的本地 `.md`
2. 发送前预上传到飞书云空间
3. 自动创建目录并补目录/文件协作者权限
4. 获取飞书文件 URL
5. 在 Feishu 输出中替换为可点击链接

当前仍未完成的部分包括：

- 裸路径识别
- 代码块路径识别
- 用户可见失败提示
- 自动 GC

## 17. 当前实现状态与后续缺口

截至 2026-04-09，当前仓库里已经实现：

- final assistant Markdown block 改写
- 只改写 Markdown 链接目标，不改写裸路径
- 固定 inventory 根目录与会话目录自动创建
- 不可变版本上传与本地状态缓存
- 目录与文件双授权
- 单聊按用户授权，群聊按“用户 + 当前群”授权
- 远端目录或文件被删除后按需自动重建
- cleanup 只以 fixed inventory root 为边界，已不再依赖 gateway marker 或文件名前缀
- admin status / cleanup 已切到 remote-first inventory 发现
- 本地 reconcile 产品能力已移除
- 任一预览失败不阻塞主回复

当前仍待补完：

- 裸路径识别
- 代码块路径识别
- 用户可见失败提示
- 自动 GC 与版本回收
- 更积极的空目录治理
- 更细粒度的监控与运营指标

## 18. 官方文档参考

以下为本设计直接依赖的官方文档：

- 发送消息: https://open.feishu.cn/document/server-docs/im-v1/message/create
- 发送消息内容结构: https://open.feishu.cn/document/server-docs/im-v1/message-content-description/create_json
- 消息卡片 Markdown: https://open.feishu.cn/document/common-capabilities/message-card/message-cards-content/using-markdown-tags
- 卡片交互配置: https://open.feishu.cn/document/feishu-cards/configuring-card-interactions
- 卡片回调结构: https://open.feishu.cn/document/feishu-cards/card-callback-communication
- 上传云空间文件: https://open.feishu.cn/document/server-docs/docs/drive-v1/upload/upload_all
- 获取文件元数据: https://open.feishu.cn/document/server-docs/docs/drive-v1/file/batch_query
- 删除文件: https://open.feishu.cn/document/server-docs/docs/drive-v1/file/delete
- 权限概述: https://open.feishu.cn/document/server-docs/docs/permission/overview
- 权限 FAQ: https://open.feishu.cn/document/server-docs/docs/permission/faq
- 增加协作者权限: https://open.feishu.cn/document/server-docs/docs/permission/permission-member/create
- 获取协作者列表: https://open.feishu.cn/document/server-docs/docs/permission/permission-member/list
- 更新云文档权限设置: https://open.feishu.cn/document/server-docs/docs/permission/permission-public/patch-2
