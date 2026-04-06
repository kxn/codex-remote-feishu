# Feishu 配置模板

`app-template.json` 是这个项目的飞书应用配置模板，不是飞书控制台的官方导入格式。

它的作用是把当前实现依赖的事项固定下来，方便你在飞书开放平台里逐项完成配置：

1. 打开“凭证与基础信息”，记录 `App ID` 和 `App Secret`。
2. 打开“添加能力”或“机器人”，确保机器人可接收文本和图片消息，并可发送文本、卡片与 reaction。
3. 打开“事件与回调”，订阅：
   - `im.message.receive_v1`
   - `im.message.recalled_v1`
   - `im.message.reaction.created_v1`
   - `application.bot.menu_v6`
   - `card.action.trigger`
4. 打开“权限管理”，补齐模板里列出的消息、P2P 和 reaction 相关权限。
   - 如果控制台支持权限 JSON 导入，可直接使用模板中的 `scopes_import`
5. 打开“机器人菜单”，创建以下菜单 key：
   - `list`
   - `status`
   - `threads`（展示“切换会话”即可）
   - `stop`
   - `reasonlow`
   - `reasonmedium`
   - `reasonhigh`
   - `reasonxhigh`
   - `access_full`
   - `access_confirm`

`card.action.trigger` 现在不仅用于 attach / 切换会话，也用于 approval request 卡片按钮回调；如果这个事件没配，飞书里的确认卡片会点了没反应。

文本命令不需要在飞书控制台单独注册，直接给机器人发消息即可。当前建议保留这些命令：

- `/list`
- `/status`
- `/threads`
- `/use`
- `/follow`
- `/detach`
- `/stop`
- `/model`
- `/reasoning`
- `/access`
- `/approval`

## 当前实现必需能力

## 权限导入 JSON

`app-template.json` 里的 `scopes_import` 字段就是当前后端 manifest 使用的导入样例。

如果你的飞书控制台支持权限 JSON 导入，优先使用这段内容，再补手工确认：

- `drive:drive`
- `im:message`
- `im:message.group_at_msg:readonly`
- `im:message.group_msg`
- `im:message.p2p_msg:readonly`
- `im:message.reactions:read`
- `im:message.reactions:write_only`
- `im:message:send_as_bot`
- `im:resource`

### 1. 基础机器人收发

至少要确保机器人具备：

- 接收文本消息
- 接收图片消息
- 发送文本消息
- 发送卡片消息
- 添加和移除消息 reaction

### 2. 事件订阅

当前实现依赖这 5 个事件：

- `im.message.receive_v1`
- `im.message.recalled_v1`
- `im.message.reaction.created_v1`
- `application.bot.menu_v6`
- `card.action.trigger`

其中：

- `im.message.recalled_v1` 负责撤回尚未发送的排队输入，或取消 staged image
- `application.bot.menu_v6` 负责实例列表、状态、推理强度和执行权限快捷菜单
- `card.action.trigger` 负责 selection prompt 和 approval request 两类卡片交互

### 3. 单聊额外权限

如果你主要通过单聊与机器人交互，还需要额外开通 P2P 消息接收权限。

## `.md` 预览额外权限

如果你希望 assistant 最终回复里的本地 `.md` Markdown 链接自动变成“飞书内可点击预览链接”，推荐直接给应用开通：

- `drive:drive`

这是当前实现里最省事、最不容易漏项的配置，因为预览链路会实际调用这些能力：

- 在应用云空间中自动创建目录
- 上传 Markdown 文件
- 查询文件访问 URL
- 给目录和文件增加协作者权限

如果不开通这部分权限：

- 机器人基础对话仍然可用
- 但本地 `.md` 链接会保留原样，不会被替换成飞书预览链接

## `.md` 预览的可见性与授权要求

当前实现不是“只上传文件”，而是“上传 + 授权”一起完成。

默认授权策略：

- 单聊：授权给当前对话用户
- 群聊：同时授权给当前对话用户和当前群

这样做是为了避免“机器人创建成功，但当前用户点开是死文件”。

因此在群聊里还要注意：

- 应用需要已经在目标群中可见
- 如果你用群聊测试 `.md` 预览，机器人本身必须已经被加入该群

## 运行时可观察行为

当前 `.md` 预览实现只会处理：

- assistant 最终回复
- Markdown 链接格式，例如 ``[README](docs/README.md)``

当前不会处理：

- 纯文本里的裸路径
- 代码块里的路径
- 用户输入里的路径
