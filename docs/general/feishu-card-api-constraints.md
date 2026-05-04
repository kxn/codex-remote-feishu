# Feishu Card API Constraints

> Type: `general`
> Updated: `2026-05-04`
> Summary: 固化当前仓库进行飞书卡片、消息卡片 patch、CardKit 流式更新设计时必须先考虑的平台硬约束、频控和降级基线。

## 1. 文档定位

这份文档不是某一个 issue 的设计草案，而是当前仓库在做飞书卡片相关产品设计、协议设计和实现时必须先看的平台约束清单。

它主要回答四个问题：

1. 飞书消息和卡片的体积上限大致在哪里。
2. 常见的发送、更新、流式更新、交互回调有哪些频控和时序限制。
3. 哪些限制会直接影响“把一个 turn 收进一张卡片”这类产品方案。
4. 后续设计如果逼近这些限制，当前仓库默认应该怎样降级。

## 2. 使用范围

以下场景默认都应先对照本文件，再做交互或实现决策：

- 飞书消息发送、patch、延时更新
- CardKit 卡片实体创建、更新、组件增删改、流式文本更新
- 将多条过程消息收敛为单张卡片
- approval / request_user_input / MCP 授权等交互卡片
- plan / progress / final answer 在卡片中的聚合展示
- 任何会明显改变卡片体积、更新频率、交互模式的设计

## 3. 当前确认的硬约束

### 3.1 消息体积限制

消息发送接口当前明确给出了两档大小上限：

- 文本消息请求体最大 `150 KB`
- 卡片消息、富文本消息请求体最大 `30 KB`

这意味着：

1. 纯文本在容量上明显宽松于卡片。
2. 如果把长输出全部改塞进卡片，容量上反而更容易先撞线。
3. 如果消息中包含大量样式标签，实际长度会比肉眼看到的正文更大。

来源：

- `https://open.feishu.cn/document/server-docs/im-v1/message/create`

### 3.2 已发送消息卡片 patch 的限制

普通消息卡片 patch 当前有几个直接影响产品设计的限制：

- 仅支持更新 `14` 天内发送的消息
- 仅支持共享卡片；更新前后都必须显式声明 `update_multi=true`
- 单条消息更新频控为 `5 QPS`

这意味着如果一个功能依赖“持续改同一张消息卡片”，它本身就有单消息级别的更新频率上限，不能把模型 delta 原样无节流地刷上去。

当前共享“工作中”过程卡的降级基线：

1. 多行过程超过卡片预算时，优先在 projector 层停止复用当前 active progress segment，并直接新开下一张 progress segment card；旧卡保留为历史快照，不再裁掉前文。
2. 新 segment 默认只承接后续较晚的可见过程行；不会把旧段已 seal 的历史内容再搬运到新段开头。
3. 对仍处于活动态的可变过程项，owner 会把当前快照接到新 active segment，避免后续状态更新继续回写 sealed 旧卡。
4. 单条可见过程行本身超过 30KB transport 预算时，才允许对这一行做预算裁剪；这不是业务级固定长度摘要规则。
5. reasoning/thinking 行不再有单独的短截断策略，仍受上述统一卡片预算策略约束。
6. reasoning/thinking delta 不逐条触发 `message.patch`；同一张工作中卡因 reasoning/thinking 主动更新时按约 1 秒窗口合并，普通高价值过程更新会顺手携带最新 reasoning/thinking 内容，正文开始、reasoning item 结束或 turn 结束前会强制 flush 最后一段内容。

来源：

- `https://open.feishu.cn/document/server-docs/im-v1/message-card/patch`

### 3.3 Table 数量限制

除了 transport bytes 与组件数，飞书对单卡中的 table 数量也存在单独上限。

当前仓库把这条约束视为必须单独预算的一维，而不是默认把它并入“30 KB / 200 elements”里一起猜。

当前已确认的证据有两类：

- 运行态已观察到真实失败：`ErrCode: 11310; ErrMsg: card table number over limit; ErrorValue: table`
- 飞书公开内容文档中也明确写到：一张卡片最多 `5` 个 Table，超出的表格以文本方式展示

这里需要特别说明：

1. 上述 `5 个 Table` 来自飞书公开内容产品文档，不是当前 message create OpenAPI 文档里的正式字段契约。
2. 但它与当前仓库在运行时遇到的 `table number over limit` 现象一致，足以作为当前实现的保守预算基线。
3. 因此当前仓库的实现纪律是：**不要把 table 数量预算寄托给 transport-size split**。

当前仓库基线：

1. final assistant markdown 默认按 **单卡最多 5 个 table** 做保守预算。
2. 超额 table 应在 projector / adapter 层先改写成非 table 形态，例如 fenced text block。
3. 只有完成这一步后，才继续进入 transport-size split / trim 判断。

来源：

- `https://www.feishu.cn/content/7gprunv5`

### 3.4 Card JSON 2.0 的结构限制

飞书新版卡片 JSON 2.0 当前有几个重要结构边界：

- 一张卡片最多支持 `200` 个元素或组件
- 当前仅支持共享卡片，不支持独享卡片
- 低于飞书客户端 `7.20` 的版本无法完整显示 JSON 2.0 卡片内容

补充说明：

- 容器类组件文档还额外写了嵌套深度建议，当前容器类组件最多支持嵌套 `5` 层

这意味着：

1. 不能把“元素很多的长 turn”简单理解为只是字符串更长，组件数量同样可能成为上限。
2. 如果设计里打算把 plan、progress、审批表单、操作按钮、长正文都塞进一张卡，必须同时预算“字节体积”和“组件数量”。

来源：

- `https://open.feishu.cn/document/feishu-cards/card-json-v2-structure`
- `https://open.feishu.cn/document/feishu-cards/card-json-v2-components/containers/form-container`
- `https://open.feishu.cn/document/feishu-cards/card-json-v2-components/containers/column-set`

### 3.5 CardKit 卡片实体的有效期与发送约束

CardKit 卡片实体本身也有生命周期限制：

- 一个卡片实体仅支持发送一次
- 卡片实体有效期为 `14` 天

这意味着如果后续真迁到 CardKit entity 模式，生命周期管理、重发、重建卡片实体都不能偷懒地按“长期稳定对象”来理解。

来源：

- `https://open.feishu.cn/document/cardkit-v1/card/create`

### 3.6 CardKit 流式更新的频率限制

流式更新卡片文档明确写到：

- 对于单个卡片实体，卡片级和组件级 OpenAPI 操作的频率上限为 `10 次/秒`

这条限制是“单卡片实体”维度的限制，和接口自身的全局频控不是一回事。

这意味着：

1. 如果一个 turn 收敛到一张 live card，就算全局接口频控还很富余，单卡本身也有节流要求。
2. plan、正文流式、底部状态区、组件增删改如果都指向同一张卡，就会共享这同一份 10 次/秒预算。

来源：

- `https://open.feishu.cn/document/cardkit-v1/streaming-updates-openapi-overview`

### 3.7 OpenAPI 频控基线

相关接口常见的 OpenAPI 频控等级包括：

- `1000 次/分钟、50 次/秒`

同时，消息发送接口还补充了更贴近 IM 场景的限频：

- 向同一用户发消息：`5 QPS`
- 向同一群发消息：群内机器人共享 `5 QPS`

这意味着：

1. “全局接口没限流”不代表“同一用户/同一群/同一消息/同一张卡”没限流。
2. 产品设计不能只看接口目录里写的 50 次/秒，还要看更细粒度的资源限制。

来源：

- `https://open.feishu.cn/document/server-docs/api-call-guide/frequency-control`
- `https://open.feishu.cn/document/server-docs/im-v1/message/create`

### 3.8 交互回调时限与延时更新 token 约束

卡片交互回调当前至少有两条必须硬记的限制：

- 收到交互回调后，需要在 `3 秒` 内返回 HTTP 200
- 交互回调里附带的延时更新 token 有效期 `30 分钟`，且同一个 token 最多只支持更新 `2` 次卡片

这意味着：

1. 回调处理路径不能把慢业务逻辑直接堆在同步响应里。
2. 依赖交互 token 持续多次修改同一张卡的方案，本身就有次数上限，不能无限 patch。

来源：

- `https://open.feishu.cn/document/uAjLw4CM/ukzMukzMukzM/feishu-cards/handle-card-callbacks`

### 3.9 流式更新与交互之间的时序限制

飞书的流式更新文档还给出了一个非常关键的行为限制：

- 卡片处于 streaming mode 期间，如果用户对卡片发生交互，服务端收到回调后无法立即继续按原状态更新卡片；需要先关闭 `streaming_mode`

这意味着“正文持续流式输出”和“同一张卡中途弹出审批/授权/输入交互”不是天然兼容的，至少需要显式的状态切换设计。

来源：

- `https://open.feishu.cn/document/cardkit-v1/streaming-updates-openapi-overview`

## 4. 关于 30 KB 的当前仓库判断

这里有一个需要特别说明的点：

- CardKit `create` / `update` / `element content` 等接口的字段长度文档本身允许相当大的字符串
- 但这些接口的错误码表又明确出现 `Card content exceeds limit`，并建议把卡片大小控制在 `30KB` 以内

因此，当前仓库的保守基线不是按“字段理论长度”设计，而是按“实际 transport 请求体 30 KB 左右”做预算。

当前实现基线应明确区分两层：

1. **外层 ceiling**
   - 继续把 `30 KB` 视为消息卡片 transport 的硬天花板。
2. **内层测量对象**
   - 不再把 raw card JSON 字节数直接当作唯一预算。
   - 普通消息 `create / reply / patch` 应测量真实序列化后的请求体，并以三者中最严格的 envelope 为准。
   - 交互回调里的 inline replace 应测量 callback response 自身的序列化体积，而不是套用消息发送的 raw budget。

换句话说：

1. `3,000,000` 这类字段长度更像“请求字段格式允许多长”
2. `30 KB` 更接近“卡片 transport 能稳定发送和渲染的实际体积边界”
3. 同样的 raw card JSON，在消息发送 envelope 与 callback envelope 下占用的字节并不相同

因此，不要再在 projector、gateway trim 和 callback replace 之间共享一条未经说明来源的 raw `20 KB` 常量。需要判断是否超限时，统一按目标 transport 的真实 envelope 计算，再与 `30 KB` ceiling 对比。

在没有更强官方说明前，后续设计一律把 `30 KB` 视为实际运营上的硬天花板，而不是可轻易突破的建议值。

来源：

- `https://open.feishu.cn/document/cardkit-v1/card/create`
- `https://open.feishu.cn/document/cardkit-v1/card/update`
- `https://open.feishu.cn/document/cardkit-v1/card-element/content`

## 5. 这些限制对产品设计的直接影响

### 5.1 不要把单张卡片当成完整 transcript 容器

长 turn 的全部正文、plan、tool 过程、状态说明、审批交互一起放进一张卡，最先撞到的通常不是“不会渲染”，而是：

- 30 KB 卡片体积上限
- 200 元素上限
- 单卡更新频率上限

因此单张 live card 更适合承载：

- 当前摘要
- 最近一段输出窗口
- 当前 plan
- 当前状态 / 操作区

而不适合无限累积整轮完整原文。

### 5.2 长内容必须预留降级出口

如果设计天然可能变长，必须在方案里提前写清楚至少一种降级路径，例如：

- 只保留最近 N 段
- 将更早内容压缩成摘要
- 分页
- 分卡
- 溢出后改走文本消息
- 溢出后改走文件 / 预览页 / 外链

当前仓库默认不接受“先按整张卡全塞进去，超了再说”的方案。

### 5.3 流式设计必须带节流预算

无论走普通 `message.patch` 还是 CardKit streaming，都不能把模型原始 delta 逐条无脑透传到飞书。

至少应回答：

1. 节流窗口是多少。
2. 同一张消息或同一卡片的最大更新频率是多少。
3. plan、正文、状态提示是否共享同一更新预算。

### 5.4 交互阶段与流式阶段要显式切换

如果一张卡既承担持续输出，又承担审批/授权/表单输入，必须先设计清楚：

1. 何时进入 streaming state
2. 何时关闭 streaming
3. 何时显示交互组件
4. 交互完成后是否恢复流式，还是进入普通 patch 模式

当前仓库不应再默认假设“流式卡片和交互卡片可以天然混在同一状态里”。

## 6. 当前仓库的默认设计检查清单

以后只要出现飞书卡片相关方案，至少先回答下面 6 个问题：

1. **体积预算**：最坏情况下这张卡大约多大，是否接近 `30 KB`
2. **组件预算**：最坏情况下元素/组件数量是否可能接近 `200`
3. **table 预算**：单卡里是否可能出现过多 table，是否需要先把超额 table 改写成非 table 形态
4. **更新预算**：同一消息或同一卡片每秒最多会更新多少次
5. **交互预算**：是否依赖 3 秒回调、30 分钟 token、最多 2 次延时更新这类约束
6. **降级路径**：超限后是截断、摘要、分页、分卡，还是改走文本 / 文件 / 链接

如果这 6 个问题里有任何一个答不出来，就说明方案还没有达到可开工标准。

## 7. 当前仓库的实现纪律

### 7.1 先查限制，再决定交互形态

不要先按理想 UI 画完，再回头看平台能不能支持。

正确顺序应是：

1. 先确认飞书平台限制
2. 再决定是单卡、分卡、文本加卡片，还是卡片加预览页
3. 最后才决定具体视觉和交互细节

### 7.2 限制变更时优先更新本文档

飞书平台的频控、组件能力和卡片限制未来有可能变化。

因此：

- 如果后续查到更准确或更新的官方限制，应优先更新本文档
- 代码或设计里如果依赖某个具体限制值，也应尽量回指本文档，而不是把背景只留在聊天记录里

## 8. 官方参考

- 消息发送：`https://open.feishu.cn/document/server-docs/im-v1/message/create`
- 更新已发送消息卡片：`https://open.feishu.cn/document/server-docs/im-v1/message-card/patch`
- 频控策略：`https://open.feishu.cn/document/server-docs/api-call-guide/frequency-control`
- 卡片 JSON 2.0 结构：`https://open.feishu.cn/document/feishu-cards/card-json-v2-structure`
- 处理卡片回调：`https://open.feishu.cn/document/uAjLw4CM/ukzMukzMukzM/feishu-cards/handle-card-callbacks`
- 流式更新卡片：`https://open.feishu.cn/document/cardkit-v1/streaming-updates-openapi-overview`
- 创建卡片实体：`https://open.feishu.cn/document/cardkit-v1/card/create`
- 全量更新卡片实体：`https://open.feishu.cn/document/cardkit-v1/card/update`
- 流式更新文本：`https://open.feishu.cn/document/cardkit-v1/card-element/content`
