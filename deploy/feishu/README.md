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
5. 打开“权限管理”，补齐模板里的应用身份权限。
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

## 权限导入

已接入的机器人可以在 Codex Remote 管理页打开「权限检查」，点「重新检查配置」，再用「复制导入 JSON」拿到该应用缺少的权限。新应用可复制 `app-template.json` 中整个 `scopes_import` 对象。两种方式都在飞书开放平台的「权限管理 → 批量导入/导出权限」中粘贴并申请开通。

| 能力 | 应用身份权限 | 配置说明 |
| --- | --- | --- |
| 项目群消息、身份核对和群解散 | `im:message.group_msg`、`im:chat:readonly`、`im:chat:read` | 负责人可接收群里未 @ 机器人的后续消息，并核对项目群和成员。 |
| 机器人进退群 | `im:chat.members:bot_access` | 开通后订阅两个机器人进退群事件。 |
| 自动邀请队员 | `im:chat.members:write_only` | 可稍后开通，期间可以在群里手动添加队员机器人。 |
| 群标签页 | `im:chat.tabs:read`、`im:chat.tabs:write_only` | 可稍后开通，群里的项目导航卡仍可作为入口。 |
| 消息、卡片、Pin 和附件 | `im:message:send_as_bot`、`im:message:readonly`、`im:resource:upload` | 已有收发权限覆盖消息编辑、卡片更新、读取消息附件及 Pin 的创建和核对。 |
| 云空间 | `drive:drive` | 用于目录、资料元信息、协作者、评论读取、预览和现有电子表格读写。 |
| Docx、知识库、多维表格 | `docx:document`、`wiki:wiki`、`bitable:app` | 按需开通，用于项目首页及 Docx 成果、Wiki 节点或多维表格。 |

每个执行角色使用对应 Bot 的应用身份。开通 API 权限后，还要在飞书为需要读写现有资料的 Bot 授予相应的目录、文档或知识库访问权限。配置并发布应用版本后，回到 Codex Remote 重新检查。

参考 [飞书消息 API](https://open.feishu.cn/document/uAjLw4CM/ukTMukTMukTM/reference/im-v1)、[群组权限列表](https://open.feishu.cn/document/develop-robots/add-bot-to-external-group)、[飞书 Wiki API](https://open.feishu.cn/document/server-docs/docs/wiki-v2/wiki-overview)、[电子表格 API](https://open.larkenterprise.com/document/server-docs/docs/sheets-v3/overview) 和 [云文档授权](https://open.feishu.cn/document/server-docs/docs/docs-overview)。

## 事件订阅

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

## 回调配置

在同一个“事件与回调”页面里继续完成：

- 点击“回调订阅方式”
- 选择“长连接”
- 点击保存
- 配置 `card.action.trigger`
- 不需要填写 HTTP 回调 URL

`card.action.trigger` 处理负责人选择、项目导航、资料选择、执行设置和待处理事项等交互。

## 文件发送与团队资料

私聊使用 `/sendfile` 发送当前工作允许读取的文件。将成果发布为团队资料时，机器人还需要对应飞书资料 API 的权限以及目标资料的访问权限。
