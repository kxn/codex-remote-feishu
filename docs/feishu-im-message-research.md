# 飞书机器人接收消息研究

## 目标

确认两个问题：

1. 用户发送“图文混合消息”时，机器人在 `im.message.receive_v1` 中实际看到的结构。
2. 用户使用客户端“引用/回复消息”时，机器人在 `im.message.receive_v1` 中实际看到的结构。

## 基于官方文档的结论

### 1. 图文混合消息

文档层面，最接近“图文混合消息”的消息类型是 `post`（富文本）。

在接收事件里：

- `event.message.message_type` 会是 `post`
- `event.message.content` 是一个 JSON 字符串
- 解析后通常是一个对象，核心字段为：
  - `title`
  - `content`
- `content` 是二维数组：
  - 最外层每个元素代表一个段落
  - 每个段落里是若干富文本节点

常见节点：

- `{"tag":"text","text":"..."}`
- `{"tag":"a","text":"...","href":"..."}`
- `{"tag":"at","user_id":"@_user_1"}`
- `{"tag":"img","image_key":"img_xxx"}`
- `{"tag":"media","file_key":"file_xxx","image_key":"img_xxx"}`
- `{"tag":"emotion","emoji_type":"SMILE"}`
- `{"tag":"code_block","language":"GO","text":"..."}`
- `{"tag":"hr"}`

一个接收侧的典型结构可以概括成：

```json
{
  "message_type": "post",
  "content": "{\"title\":\"标题\",\"content\":[[{\"tag\":\"text\",\"text\":\"第一行\"}],[{\"tag\":\"img\",\"image_key\":\"img_xxx\"}],[{\"tag\":\"text\",\"text\":\"第二行\"}]]}"
}
```

### 2. 富文本接收内容和发送内容不完全一致

官方文档明确提到，接收富文本时，内容可能与发送时不完全一致：

- 发送时可用的 `md` 标签，接收时不会原样返回
- `md` 会被系统转换成其他标签
- 引用、无序列表、有序列表在接收侧会被简化成 `text`

这意味着：

- 即使客户端编辑框里看起来是“图文混排 + Markdown 引用”
- 机器人端拿到的也不一定是原始编辑结构
- 接收侧应以实际推送中的 `content` 为准，而不要假设能还原客户端输入

### 3. 引用/回复消息

文档没有直接写“客户端点引用按钮后 payload 长什么样”，但消息事件和消息管理概述对“回复场景”给了明确字段定义：

- `event.message.root_id`
- `event.message.parent_id`

这两个字段只在回复消息场景返回。

按文档定义：

- `root_id` 指整棵回复树的根消息
- `parent_id` 指当前消息直接回复的上一层消息

因此，文档层面可以先做出如下判断：

- 用户通过客户端进行“引用/回复”时，机器人收到的新消息主体仍然在 `event.message.content`
- 被引用的旧消息内容不会直接嵌进新的 `content` 里
- 要拿到被引用的原消息，需要再通过 `root_id` 或 `parent_id` 调用消息查询接口获取

一个典型结构可以概括成：

```json
{
  "message_type": "text",
  "content": "{\"text\":\"这是回复内容\"}",
  "root_id": "om_root_xxx",
  "parent_id": "om_parent_xxx"
}
```

### 4. `upper_message_id` 不是普通引用字段

文档说明 `upper_message_id` 用于“合并转发消息”里的子消息层级，不是普通引用/回复场景的主字段。

因此普通“引用/回复”研究重点应放在：

- `content`
- `root_id`
- `parent_id`

而不是 `upper_message_id`

## 当前仍需实测确认的点

虽然文档已经给出大致结构，但仍有两个非常值得实测的细节：

1. 飞书客户端里用户发出的“图文混发”是否总是落成单条 `post`，还是某些客户端操作会拆成 `text + image` 多条消息。
2. 客户端界面里的“引用”是否 100% 对应开放平台里的“回复场景”，以及 `content` 中是否完全不带被引用摘要。

## 2026-04-07 实测结果

测试环境：

- 会话类型：机器人单聊（`chat_type = "p2p"`）
- 事件类型：`im.message.receive_v1`
- 触发顺序：
  1. 纯文本
  2. 单张图片
  3. 图文混合
  4. 引用纯文本
  5. 引用图文混合

### 1. 纯文本

实测结果：

- `message_type = "text"`
- `content = {"text":"这是一条文本消息"}`
- 没有 `root_id`
- 没有 `parent_id`

### 2. 单张图片

实测结果：

- `message_type = "image"`
- `content = {"image_key":"img_v3_..."}`
- 没有 `root_id`
- 没有 `parent_id`

### 3. 图文混合

实测输入为一条“图片 + 文本”的混合消息。

实测结果：

- 机器人收到的是 **单条消息**
- `message_type = "post"`
- 没有被拆成一条 `image` 加一条 `text`
- `content` 解析后结构如下：

```json
{
  "title": "",
  "content": [
    [
      {
        "tag": "img",
        "image_key": "img_v3_0210h_08546add-5511-470c-9407-9c3ff5229a8g",
        "width": 893,
        "height": 423
      }
    ],
    [
      {
        "tag": "text",
        "text": " 这是图文混合消息",
        "style": []
      }
    ]
  ]
}
```

结论：

- 在本次实测里，飞书客户端“图文混合消息”在机器人侧表现为单条 `post`
- 图片节点和文本节点分别位于不同段落
- `img` 节点除了 `image_key` 之外，还带了 `width` 和 `height`

### 4. 引用纯文本

客户端表现上是“引用一条纯文本后发送新内容”。

机器人实测收到：

- 新消息本身仍然是 `text`
- `content = {"text":"这是引用的纯文本"}`
- 同时返回：
  - `root_id = 被引用消息ID`
  - `parent_id = 被引用消息ID`

通过补查 `parent_id` 对应消息，拿到原消息：

```json
{
  "message_id": "om_x100b527f012604acb3fb6421adc9aa6",
  "msg_type": "text",
  "content": "{\"text\":\"这是一条文本消息\"}"
}
```

结论：

- 客户端“引用纯文本”在机器人侧就是一条新的回复消息
- 新消息 `content` 里 **不包含** 被引用内容的摘要
- 被引用内容需要通过 `parent_id` / `root_id` 再查原消息

### 5. 引用图文混合

客户端表现上是“引用一条图文混合消息后发送新内容”。

机器人实测收到：

- 新消息本身仍然是 `text`
- `content = {"text":"这是引用了图文混合"}`
- 同时返回：
  - `root_id = 被引用的 post 消息ID`
  - `parent_id = 被引用的 post 消息ID`

通过补查 `parent_id` 对应消息，拿到原消息：

```json
{
  "message_id": "om_x100b527f1f1ca8ecb29502287f4aeb0",
  "msg_type": "post",
  "content": "{\"title\":\"\",\"content\":[[{\"tag\":\"img\",\"image_key\":\"img_v3_0210h_08546add-5511-470c-9407-9c3ff5229a8g\",\"width\":893,\"height\":423}],[{\"tag\":\"text\",\"text\":\" 这是图文混合消息\",\"style\":[]}]]}"
}
```

结论：

- 客户端“引用图文混合”在机器人侧同样是新的 `text` 回复消息
- 新消息 `content` 中 **不包含** 被引用图文的预览信息
- 被引用的图文内容需要通过 `parent_id` / `root_id` 查询原始 `post`

## 最终结论

结合文档和本次实测，可以得出以下结论：

1. 飞书客户端发送“图文混合消息”时，机器人端在 `im.message.receive_v1` 里看到的是单条 `post` 消息，而不是独立的多条消息。
2. 这条 `post` 的 `content` 是 JSON 字符串，解析后是富文本结构；图片通常表现为 `img` 节点，文字表现为 `text` 节点。
3. 飞书客户端做“引用消息”时，机器人端收到的是一条新的消息；本次实测中两种引用场景收到的都是 `text`。
4. “引用关系”不体现在新消息的 `content` 里，而体现在 `root_id` 和 `parent_id`。
5. 如果业务要还原“用户引用了哪条消息、那条消息原文是什么”，必须在收到事件后再用 `root_id` / `parent_id` 查询原消息。

## 已准备好的实测程序

仓库里已经准备了一个 Go 监听器：

- 入口：`cmd/lark-msg-probe/main.go`
- 运行方式：通过飞书 Go SDK 长连接 websocket 收消息
- 输出内容：
  - 原始事件 JSON
  - 解析后的 `message.content`
  - 如果命中 `root_id` / `parent_id`，会自动再查一次原消息并一起落盘

事件产物默认输出到：

- `artifacts/events/`

## 实测建议

拿到 `app_id` / `app_secret` 后，建议按下面顺序发消息：

1. 纯文本
2. 单张图片
3. 一条图文混合消息
4. 对纯文本做一次引用/回复
5. 对图文混合消息做一次引用/回复

这样就能很快确认：

- `text`
- `image`
- `post`
- reply with `root_id` / `parent_id`

这四类场景的真实 payload。
