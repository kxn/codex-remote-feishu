# App-Server 第三批 Backlog 产品方向草案

> Type: `draft`
> Updated: `2026-04-26`
> Summary: 将 #233 中第三批非主路径 app-server 能力面翻译成产品语言，明确哪些更像账号/连接器能力，哪些更像原生客户端能力，哪些只是底层管理 RPC。

## 1. 文档定位

这份文档不回答“代码该怎么写”，而是回答：

1. `#233` 里那批能力如果以后真的进入产品面，用户会把它们理解成什么。
2. 哪些能力更像设置页、连接器页、状态页，而不是聊天主链。
3. 哪些能力本质上属于原生客户端路线，不适合直接按 Feishu 聊天卡片来想象。
4. 哪些只是底层 RPC，不一定值得直接暴露给最终用户。

本文是 backlog 方向草案，不代表当前代码已实现行为。

## 2. 背景

`#233` 记录的是一批“当前不是 turn 主链 correctness blocker，但以后不应忘掉”的 app-server 能力面。

按当前审计，它们不是一个统一产品功能，而是至少三类完全不同的东西：

1. 账号、连接器、能力包这类系统边缘能力
2. realtime / watch / fuzzy search 这类明显偏原生客户端的交互能力
3. memory / marketplace / inject_items 这类底层管理 RPC

如果不先把它们翻译成产品语言，后续非常容易误把它们都当成“聊天里再加几张卡片”。

## 3. 总体判断

### 3.1 这批能力大多不属于聊天主链

它们里的多数项都不是：

- 用户给 Codex 发一条消息
- Codex 在当前 turn 里回一个结果

更常见的产品形态反而是：

- 设置页
- 状态页
- 连接器管理页
- 授权跳转流
- 原生客户端专属控件
- 后台状态通知

### 3.2 不应按同一优先级排

从产品视角，这批能力至少应拆成三层优先级：

1. **像产品功能的**
   - `account/*`
   - `mcpServer/oauth/login`
   - `mcpServer/startupStatus/updated`
   - `app/list/updated`
2. **像另一条产品线的**
   - `thread/realtime/*`
   - `fuzzyFileSearch/*`
3. **像底层系统能力的**
   - `skills/changed`
   - `fs/watch`
   - `windowsSandbox/*`
   - 大部分 simple RPC

## 4. 产品分组

### 4.1 账号与身份能力

包含：

- `account/read`
- `account/login/start`
- `account/login/cancel`
- `account/login/completed`
- `account/logout`
- `account/updated`
- `account/chatgptAuthTokens/refresh`
- `account/rateLimits/read`
- `account/rateLimits/updated`

这组能力在产品上更像“账号中心”，而不是聊天动作。

用户视角下，它们对应的是：

- 我当前登录的是谁
- 我现在有没有登录
- 我要开始登录 / 取消登录 / 登出
- 登录完成后，系统要不要自动刷新状态
- 我的额度、rate limit、令牌状态是什么

如果以后产品化，比较合理的形态是：

- `/account` 页面或账号状态卡
- 登录引导页 / 授权结果提示
- 额度状态展示

而不是：

- 在普通聊天流里用一张 approval/request 卡硬承接

### 4.2 连接器、应用与外部服务状态

包含：

- `app/list -> app/list/updated`
- `mcpServer/oauth/login -> mcpServer/oauthLogin/completed`
- `mcpServer/startupStatus/updated`
- `mcpServerStatus/list`
- `mcpServer/resource/read`
- `config/mcpServer/reload`
- `skills/changed`

这组能力在产品上更像“应用与连接器管理”。

用户视角下，它们分别更接近：

- `app/list -> app/list/updated`
  - 有哪些 app / connector 现在可用
  - 列表变了时，UI 要刷新
- `mcpServer/oauth/login -> ...completed`
  - 某个外部服务需要 OAuth 授权
  - 用户去浏览器完成授权，再回到当前产品面
- `mcpServer/startupStatus/updated`
  - 某个 MCP server 正在启动、失败、恢复
  - 更像健康状态/运行状态，而不是聊天内容
- `mcpServerStatus/list`
  - 查看当前有哪些 MCP server，以及它们是不是在线
- `mcpServer/resource/read`
  - 从某个 MCP server 读取资源，更像管理或调试能力
- `config/mcpServer/reload`
  - 重载连接器配置，偏管理员动作
- `skills/changed`
  - 本地 skills / 能力包发生变化，菜单和能力列表应刷新

如果以后产品化，比较合理的形态是：

- 连接器页
- 授权页
- 状态页
- 管理页
- 自动刷新通知

而不是主聊天流里的常驻卡片。

### 4.3 原生客户端能力

包含：

- `thread/realtime/start -> thread/realtime/* -> thread/realtime/closed`
- `fs/watch -> fs/changed -> fs/unwatch`
- `windowsSandbox/setupStart -> windowsSandbox/setupCompleted`
- `fuzzyFileSearch/sessionUpdated -> fuzzyFileSearch/sessionCompleted`
- `externalAgentConfig/detect`
- `externalAgentConfig/import`

这组能力里，大部分都更像“原生客户端工作模式”。

#### `thread/realtime/*`

这不是普通消息流的轻量扩展，而更像一条新的交互产品线。

用户会感受到的是：

- 实时低延迟交互
- 语音/转录/流式双向会话
- 持续中的 live session，而不是一问一答的 turn

如果未来真的做，它更像“实时会话模式”，而不是普通聊天卡片。

#### `fs/watch -> fs/changed -> fs/unwatch`

这更像“持续观察文件/目录变化”。

用户视角：

- 让系统盯住某些文件
- 文件变化时收到通知或触发下一步
- 取消观察后停止事件

这本质上是后台订阅能力，不是一次性的聊天请求。

#### `windowsSandbox/*`

这更像平台/运行环境能力。

用户视角：

- Windows 上的隔离环境正在准备
- 准备完成后才能继续工作

这通常属于安装、环境、运行时状态，而不是聊天产品本身。

#### `fuzzyFileSearch/*`

这更像交互式搜索器。

用户视角：

- 输入几个关键字
- 系统快速筛出文件
- 用户继续选择目标

它很像原生客户端里的搜索控件，不像聊天正文。

#### `externalAgentConfig/detect/import`

这更像“迁移向导”。

用户视角：

- 探测机器上已有的外部 agent 配置
- 选择是否导入

更接近 setup / onboarding，而不是工作对话。

### 4.4 底层管理 RPC

包含：

- `thread/memoryMode/set`
- `memory/reset`
- `thread/inject_items`
- `marketplace/add`
- `mcpServer/tool/call`
- `configRequirements/read`

这组能力里，很多不天然对应一个独立用户功能，而是“后端有这个控制点”。

#### `thread/memoryMode/set`

产品上可能对应：

- 这个会话是否允许长期记忆
- 这个会话的记忆策略是什么

它比较像设置项，而不是普通聊天动作。

#### `memory/reset`

产品上就是：

- 清空记忆
- 让系统忘掉之前积累的长期记忆

这是一个明确用户动作，但更像设置页/安全页操作。

#### `thread/inject_items`

这更像系统能力，不一定适合直接暴露给最终用户。

用户能理解的产品解释通常是：

- 系统把某些上下文、素材、历史直接补写进会话

但它不一定需要一个用户直接点击的入口。

#### `marketplace/add`

产品上更像：

- 安装一个扩展
- 添加一个 marketplace 能力包

这通常属于插件/扩展管理，而不是主聊天流。

#### `mcpServer/tool/call`

如果暴露，产品上更像：

- 手动调用某个 MCP 工具

但这和“让模型自己按上下文选择调用工具”是两回事。

#### `configRequirements/read`

产品上更像：

- 检查当前配置还缺什么
- 某个功能为什么现在不能运行

比较像诊断/检查页。

## 5. 对当前产品面的启发

### 5.1 不要默认把它们都塞进聊天卡

`#233` 里这批能力，大多数都不应默认按：

- 一张卡
- 一个 slash command
- 一次 turn 中途交互

来设想。

更自然的落点往往是：

- 状态页
- 设置页
- 管理页
- 授权跳转页
- 原生客户端能力

### 5.2 只有少数项值得优先产品化

若只按产品价值与可理解性排序，建议优先考虑：

1. `account/*`
2. `mcpServer/oauth/login`
3. `mcpServer/startupStatus/updated`
4. `app/list/updated`

因为它们最容易转成用户能理解的页面或状态卡。

### 5.3 realtime / fuzzy search 属于另一条产品线

如果后面真要做：

- realtime
- fuzzy search

应该把它们视为新的交互范式，而不是当前聊天面的附属能力。

## 6. 对 #233 的建议用法

`#233` 继续保留为 umbrella backlog 是合理的，但后续不应直接拿它开工。

更合理的做法是：

1. 先按产品形态拆分
2. 再判断哪些值得真正进入产品面
3. 最后才决定各自走：
   - Feishu / headless 正式支持
   - 只保证 wrapper/native path 透传不破坏
   - 完全不产品化

建议未来至少拆成这些子方向：

1. 账号与连接器状态
2. MCP OAuth / startup status / app list / skills invalidation
3. realtime / watch / fuzzy search
4. simple RPC 暴露边界

## 7. 当前不回答的问题

本文刻意不回答这些实现问题：

1. 哪些能力一定要做进 Feishu
2. 哪些只做 headless
3. 哪些只保证 wrapper/native path 不破坏
4. 哪些 simple RPC 要不要直接做成命令

这些都属于后续真正拆分 issue 时的产品决策门。

## 8. 关联项

- GitHub issue: `#233`
- 上游审计基线：`docs/inprogress/codex-app-server-state-machine-audit.md`
