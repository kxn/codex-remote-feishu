# Feishu 配置模板

`app-template.json` 是这个项目的飞书应用配置模板，不是飞书控制台的官方导入格式。

它的作用是把当前实现依赖的事项固定下来，方便你在飞书开放平台里逐项完成配置：

1. 打开“凭证与基础信息”，记录 `App ID` 和 `App Secret`。
2. 打开“添加能力”或“机器人”，确保机器人可接收文本和图片消息，并可发送文本、卡片与 reaction。
3. 打开“事件与回调”，先完成事件订阅：
   - 点击“订阅方式”，默认就是“长连接”，点击保存
   - `im.message.receive_v1`
   - `im.message.recalled_v1`
   - `im.message.reaction.created_v1`
   - `im.chat.member.bot.added_v1`
   - `im.chat.member.bot.deleted_v1`
   - `im.chat.disbanded_v1`
   - `application.bot.menu_v6`
4. 在同一个“事件与回调”页面，继续完成回调配置：
   - 点击“回调订阅方式”
   - 选择“长连接”，点击保存
   - 配置 `card.action.trigger`
   - 当前版本不需要额外填写 HTTP 回调地址
5. 打开“权限管理”，补齐模板里列出的消息、P2P 和 reaction 相关权限。
   - 点击“批量导入/导出权限”
   - 粘贴模板中的 `scopes_import`
   - 点击“保存并申请开通”
6. 打开“机器人菜单”，创建以下菜单 key：
   - `menu`
   - `status`
   - `steerall`
   - `projects`
   - `resources`
   - `history`
   - `settings`

按 WebSetup 的推荐菜单配置机器人。私聊中 `/menu` 和 `/help` 会打开工作入口；项目群由负责人协调工作。文本命令无需在飞书控制台注册，详见 [飞书使用说明书](../../docs/general/user-guide.md)。

`card.action.trigger` 用来接收工作导航、设置和业务卡片上的按钮操作。

## 当前实现必需能力

## 权限导入 JSON

`app-template.json` 里的 `scopes_import` 字段就是当前后端 manifest 使用的导入样例。

飞书后台这里的入口名是“批量导入/导出权限”。

如果你的飞书控制台支持权限 JSON 导入，优先在这个入口里粘贴这段内容，再补手工确认：

- `application:application:self_manage`
- `drive:drive`
- `bitable:app`
- `im:message:readonly`
- `im:message.group_at_msg:readonly`
- `im:message.group_at_msg.include_bot:readonly`
- `im:message.group_msg`
- `im:message.p2p_msg:readonly`
- `im:message.reactions:read`
- `im:message.reactions:write_only`
- `im:message:send_as_bot`
- `im:resource:upload`

### 1. 基础机器人收发

至少要确保机器人具备：

- 接收文本消息
- 接收图片消息
- 发送文本消息
- 发送卡片消息
- 读取群信息
- 接收群聊普通消息和 @ 消息
- 添加和移除消息 reaction

读取项目群普通消息是基础安装的一部分，负责人通过该权限接收群里的需求和跟进。飞书多维表格用作项目资料时，需要对应的 API 权限和资料访问权限。

### 2. 事件订阅

为飞书团队启用以下事件：

- `im.message.receive_v1`
- `im.message.recalled_v1`
- `im.message.reaction.created_v1`
- `im.chat.member.bot.added_v1`
- `im.chat.member.bot.deleted_v1`
- `im.chat.disbanded_v1`
- `application.bot.menu_v6`

进入事件列表前，先点击“订阅方式”，默认就是“长连接”，点击保存。

其中：

- `im.message.reaction.created_v1` 接收支持的消息反馈。
- `im.message.recalled_v1` 接收消息撤回。
- `im.chat.member.bot.added_v1` 记录机器人加入项目群。
- `im.chat.member.bot.deleted_v1` 和 `im.chat.disbanded_v1` 记录机器人被移出或群解散，封存受影响的群接入。
- `application.bot.menu_v6` 接收上方配置的静态机器人菜单操作。

### 3. 回调配置

在同一个“事件与回调”页面里继续完成：

- 点击“回调订阅方式”
- 选择“长连接”
- 点击保存
- 配置 `card.action.trigger`
- 不需要填写 HTTP 回调 URL

其中：

- `card.action.trigger` 负责 selection prompt、`/menu` 导航、参数卡和 approval request 四类卡片交互

### 4. 单聊额外权限

如果你主要通过单聊与机器人交互，还需要额外开通 P2P 消息接收权限。

## 文件发送与团队资料

私聊使用 `/sendfile` 发送当前工作允许读取的文件。将成果发布为团队资料时，机器人还需要对应飞书资料 API 的权限以及目标资料的访问权限。
