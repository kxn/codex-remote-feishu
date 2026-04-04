# Feishu 配置模板

`app-template.json` 是这个项目的飞书应用配置模板，不是飞书控制台的官方导入格式。

它的作用是把当前实现依赖的事项固定下来，方便你在飞书开放平台里逐项完成配置：

1. 打开“凭证与基础信息”，记录 `App ID` 和 `App Secret`。
2. 打开“添加能力”或“机器人”，确保机器人可接收文本和图片消息，并可发送文本、卡片与 reaction。
3. 打开“事件与回调”，订阅：
   - `im.message.receive_v1`
   - `im.message.reaction.created_v1`
   - `application.bot.menu_v6`
4. 打开“权限管理”，补齐模板里列出的消息、P2P 和 reaction 相关权限。
5. 打开“机器人菜单”，创建以下菜单 key：
   - `list`
   - `status`
   - `threads`（展示“切换会话”即可）
   - `stop`
   - `reasonlow`
   - `reasonmedium`
   - `reasonhigh`
   - `reasonxhigh`

如果你主要通过单聊与机器人交互，还需要额外开通 P2P 消息接收权限。
