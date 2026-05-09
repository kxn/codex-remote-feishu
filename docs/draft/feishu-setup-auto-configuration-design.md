# 飞书 Setup 自动配置改造设计（vNext）

> Type: `draft`
> Updated: `2026-05-09`
> Summary: 基于 Feishu `application/v7` 自动配置能力重设计 setup 的飞书配置阶段，删除脆弱的 events/callback 安装期测试，并补齐发布与审核不确定性下的降级策略。

## 1. 文档定位

本文讨论的是下一版 setup / web admin 中“飞书应用配置”这一段的改造方案。

它要解决四件事：

1. 把当前依赖人工跳转后台的权限、事件、回调配置，尽可能改成程序化自动收敛。
2. 删除当前脆弱的 `验证 events` / `验证 callback` 安装期测试链路。
3. 在 Feishu 发布与管理员审核存在不确定性的前提下，定义可接受的交互和降级路径。
4. 给后续实现提供一个单一版本设计基线，避免 setup、admin、manifest、后台调用各自发散。

当前已生效的旧版页面基线仍记录在：

- [web-setup-flow-v2.md](./web-setup-flow-v2.md)
- [web-setup-wizard-redesign.md](./web-setup-wizard-redesign.md)

本文是 vNext 目标设计，不代表当前代码已经实现。

## 2. 当前问题

### 2.1 手工流程过碎

当前 setup 固定步骤仍是：

1. `环境检查`
2. `飞书连接`
3. `权限检查`
4. `事件订阅`
5. `回调配置`
6. `菜单确认`
7. `自动启动`
8. `VS Code 集成`
9. `完成`

其中 `权限检查`、`事件订阅`、`回调配置` 都要求用户自己跳去飞书后台处理。系统只能给 checklist 和链接，不能真正把目标状态收口。

### 2.2 安装期测试链路很脆

当前 `事件订阅` 和 `回调配置` 会尝试发送测试消息或测试卡片。这条链路依赖 `web test recipient`，而这个 recipient 只在部分路径上能可靠拿到。

直接问题有三个：

1. 不是扫码创建出来的 app，经常根本不知道该发给谁。
2. 即使发出去了，也只是“给某个用户发了测试消息”，并不是稳定的配置真值校验。
3. setup 和 admin 都因此背上了一套额外的运行时状态、测试按钮和过期清理逻辑。

这条链路应整体删除，而不是继续修补。

### 2.3 现有 manifest 不足以表达自动配置目标

当前 [manifest.go](../../internal/feishuapp/manifest.go) 主要表达：

1. 需要哪些 scopes
2. 需要哪些 events
3. 需要哪些 callbacks
4. 菜单清单和人工 checklist

它还不能表达：

1. 订阅方式目标值
2. 回调方式目标值
3. 机器人能力目标值
4. 哪些项是核心阻塞项，哪些项可降级
5. 哪些项在 UI 上应如何提示功能降级

如果不先把 manifest 扩成“目标状态模型”，自动配置就只能写死在流程代码里，后面一定再次发散。

## 3. 已确认的外部能力边界

### 3.1 Feishu CN 已有可用的 `application/v7` 写接口

公开文档已覆盖：

1. `PATCH /open-apis/application/v7/applications/:app_id/config`
   - 可修改 scopes、事件订阅、回调配置、事件/回调加密策略
2. `PATCH /open-apis/application/v7/applications/:app_id/ability`
   - 可修改机器人能力配置
3. `POST /open-apis/application/v7/applications/:app_id/publish`
   - 可自动创建或提交待发布版本

这意味着 setup 已经具备“程序化写配置”的官方能力基础。

### 3.2 当前仍缺少公开的机器人菜单自动配置接口

公开接口里能看到 IM 菜单相关 API，但没有等价的“应用级机器人菜单配置”公开接口。

结论：

1. `菜单确认` 这一步暂时保留手工处理。
2. 本轮自动化范围不包含机器人菜单增删改。

### 3.3 可读能力强弱不对称

当前能较可靠读取的有：

1. app 基本信息和版本信息
2. scopes 授权状态
3. 当前版本的事件列表
4. 某些版本级卡片相关配置
5. 版本在线/待审核状态

当前没有找到同等可靠的公开读接口来完整回读：

1. 回调方式
2. 回调项列表
3. 回调 request URL
4. 事件订阅方式
5. 事件订阅 request URL

这意味着新方案不能假设“一切都能读回精确真值”，必须接受一部分字段属于“强幂等写入、弱读取校验”。

### 3.4 Feishu 与 Lark 文档不同步

当前公开文档呈现出明显差异：

1. Feishu CN 公开可见 `application/v7`
2. Lark 公网文档对应路径仍未对齐

结论：

1. 本轮自动配置按 `Feishu CN first` 设计。
2. 不默认把同能力直接开放给 Lark。
3. 实现上必须做“平台能力探测或平台 gating”，而不是把 Feishu 文档能力当成全局能力。

## 4. vNext 产品结论

### 4.1 setup 步骤改成下面这组

1. `环境检查`
2. `飞书连接`
3. `飞书自动配置`
4. `菜单确认`
5. `自动启动`
6. `VS Code 集成`
7. `完成`

其中 `飞书自动配置` 替代原来的：

1. `权限检查`
2. `事件订阅`
3. `回调配置`

### 4.2 删除安装期测试链路

以下产品行为直接废除：

1. setup 中的 `验证 events`
2. setup 中的 `验证 callback`
3. admin 中的 `测试事件订阅`
4. admin 中的 `测试回调`
5. 因这些测试而存在的 `web test recipient` 依赖

替代规则：

1. 配置正确性以“配置收敛 + 官方读接口验证”为主。
2. 不再通过“给某个用户发测试消息/卡片”来判断安装是否成功。

### 4.3 自动配置的目标范围

`飞书自动配置` 阶段必须自动检测并尽可能自动补齐：

1. 机器人能力
2. 权限 scopes
3. 事件订阅方式
4. 事件列表
5. 回调方式
6. 回调列表
7. 事件/回调相关的加密策略配置

菜单暂不在自动化范围内，继续保留独立的 `菜单确认` 阶段。

## 5. 目标状态模型

### 5.1 需要把 manifest 升级成“可执行目标状态”

现有 `DefaultManifest()` 需要从“人工 checklist 数据”扩成“自动配置 source of truth”。

建议补充的表达维度：

1. `scope`
   - `scope`
   - `scopeType`
   - `feature`
   - `required`
   - `degradeMessage`
2. `event`
   - `event`
   - `feature`
   - `required`
   - `degradeMessage`
3. `callback`
   - `callback`
   - `feature`
   - `required`
   - `degradeMessage`
4. `config`
   - `eventSubscriptionType`
   - `callbackType`
   - `eventAndCallbackEncryptStrategy`
5. `ability`
   - 机器人能力目标项
   - 可选的卡片相关回调 URL 等能力字段

### 5.2 必须引入“阻塞/可降级”语义

因为发布和审核结果无法完全预判，manifest 里必须把配置项分成两类：

1. 核心阻塞项
   - 缺失时不能宣称机器人已可正常使用
2. 可降级项
   - 缺失时允许 setup 继续，但要明确告诉用户会失去什么能力

第一版建议按“功能是否影响机器人核心收发与卡片交互”来分层，而不是按权限 level 粗暴判断。

原因很简单：

1. 权限 level 不是租户内是否要审核的可靠预测器。
2. 用户真正关心的是“我现在到底能不能用，以及少了什么能力”。

## 6. 后端编排设计

### 6.1 单次自动配置的标准流程

`飞书自动配置` 的后端编排建议固定为：

1. 读取 app 基本信息
2. 读取当前 scopes 授权状态
3. 读取当前在线版本 / 待发布版本信息
4. 计算“目标状态 vs 当前状态”的差异
5. 如有差异，先执行一次 `config PATCH`
6. 如机器人能力有差异，再执行一次 `ability PATCH`
7. 重新读取 scopes 与版本信息
8. 判断是否需要 `publish`
9. 如用户确认发布，则执行一次 `publish`
10. 轮询或重读版本状态，给出最终结论

约束：

1. 不按 scope / event / callback 一项一项分散调用。
2. 单次写操作尽量收敛为 `1 次 config PATCH + 1 次 ability PATCH + 可选 1 次 publish`。

### 6.2 复用现有 call broker

这类调用应复用 [callbroker.go](../../internal/adapter/feishu/callbroker.go) 现有节流与退避框架，不新造一套独立限速器。

原因：

1. 自动配置会产生一组短时间内连续的 meta HTTP 调用。
2. 现有 broker 已经有 app 级节流、429 退避和权限阻塞缓存。
3. 后续 admin 与 setup 可以共用同一条调用纪律。

### 6.3 建议的新接口形态

建议把 setup 和 admin 的自动配置入口做成同构接口：

1. `GET .../auto-config/plan`
   - 返回当前状态、目标状态、diff、是否支持自动化、是否需要发布、潜在降级项
2. `POST .../auto-config/apply`
   - 执行自动写配置，但不隐式发布
3. `POST .../auto-config/publish`
   - 执行发布，并返回发布后的版本状态

不建议继续沿用“权限检查接口 + test-events 接口 + test-callback 接口”的拼装式接口模型。

## 7. 校验与确认规则

### 7.1 强校验项

以下项可以作为“自动配置后的强校验依据”：

1. scopes 授权状态
2. 当前版本事件列表
3. app / version 的发布状态
4. 机器人能力中的可回读字段

### 7.2 弱校验项

以下项按当前公开能力更适合作为“幂等写入后的弱校验项”：

1. 回调方式
2. 回调列表
3. 回调 request URL
4. 事件订阅方式
5. 事件订阅 request URL

对这些项的处理原则是：

1. 只要目标值明确，就每次按目标值重放写入。
2. 不因缺少公开读回能力而保留旧的“人工测试消息”链路。

### 7.3 发布前确认规则

当前公开 API 不能可靠回答“这次变更在这个租户里是否一定会触发管理员审核”。

因此发布前必须给出确认，而不能无提示自动发版到底。

确认框需要明确告诉用户：

1. 将要写入哪些配置
2. 哪些配置属于核心阻塞项
3. 哪些配置缺失时只会造成功能降级
4. 系统无法在发布前精确判断租户侧审核要求

用户至少需要两个动作选项：

1. `继续发布`
2. `先跳过，按降级继续`

如果本次 diff 只包含非阻塞项，允许默认推荐第二项。

### 7.4 发布后结果判定

发布后只根据真实版本状态收口，不再做测试消息验证。

判定规则：

1. 已进入在线态
   - `飞书自动配置` 完成
2. 进入待审核/审核中
   - 阶段进入“等待管理员处理”
   - 若只影响非阻塞项，允许 setup 继续并标记能力降级
   - 若影响阻塞项，保留阻塞并明确原因
3. 发布失败
   - 给出明确错误与重试入口

## 8. 前端交互设计

### 8.1 新的 `飞书自动配置` 阶段

该阶段不再展示三张分裂的页面，而是展示单阶段进度：

1. `读取当前配置`
2. `计算差异`
3. `写入配置`
4. `等待发布结果` 或 `等待你的确认`
5. `收口结果`

用户可见信息只保留：

1. 当前在做什么
2. 还缺什么
3. 缺失的后果是什么
4. 下一步按钮是什么

不向普通用户暴露 raw API payload。

### 8.2 结果展示

阶段完成后给出三类结果之一：

1. `已自动完成`
2. `已完成，但存在功能降级`
3. `仍需管理员处理`

如果存在降级，文案必须直接对应 manifest 里的 `degradeMessage`，而不是泛泛写“部分能力不可用”。

### 8.3 平台显隐

如果当前 app 平台不支持这套自动化能力：

1. setup 回退到手工引导模式
2. admin 同样回退到手工引导模式
3. 页面上明确说明“当前平台暂不支持自动配置”

不要在 Lark 路径上静默展示一个实际上不可用的自动配置按钮。

## 9. 需要删除或替换的旧链路

### 9.1 前端

需要删掉或改写：

1. setup 里的 `权限检查` / `事件订阅` / `回调配置` 三步拆分 UI
2. setup 里的 `test-events` / `test-callback` 请求
3. admin 里的测试按钮与对应状态提示

### 9.2 后端

需要删掉或改写：

1. 安装期测试接口
2. 测试运行时状态
3. `web test recipient` 绑定逻辑
4. 基于测试消息/测试卡片判断配置完成度的逻辑

权限检查接口可以保留其“读取 scopes 真值”的部分，但应被新的自动配置编排包起来，而不是继续作为独立主流程。

## 10. 分阶段落地建议

### 10.1 第一阶段

先做后端能力闭环：

1. manifest 升级为目标状态模型
2. 接入 `application/v7` 的读写编排
3. 做 `plan/apply/publish` 三段式接口
4. 接入 call broker 节流纪律

### 10.2 第二阶段

替换 setup：

1. 把三个旧步骤合并成 `飞书自动配置`
2. 加入发布前确认与降级继续
3. 删除 setup 安装期测试链路

### 10.3 第三阶段

替换 admin：

1. 改成与 setup 同构的自动配置入口
2. 删除 admin 手动测试按钮
3. 清理旧 runtime 状态和无用接口

## 11. 主要风险

1. Feishu 与 Lark 能力差异会导致“同产品名、不同平台行为不一致”。
2. 公开读接口不完整，必须接受一部分字段只能做弱校验。
3. 发布前无法精确预测租户管理员审核要求，UI 必须诚实表达不确定性。
4. 如果不先把 manifest 扩成统一目标状态，setup 和 admin 很快会再次分叉。

## 12. 外部接口参考

Feishu CN 官方文档：

1. `application/v7 config`
   - `https://open.feishu.cn/document/uAjLw4CM/ukTMukTMukTM/application-v7/application-v7/application-config/patch`
2. `application/v7 ability`
   - `https://open.feishu.cn/document/uAjLw4CM/ukTMukTMukTM/application-v7/application-v7/application-ability/patch`
3. `application/v7 publish`
   - `https://open.feishu.cn/document/uAjLw4CM/ukTMukTMukTM/application-v7/application-v7/application-publish/create`
4. `application/v6 get app`
   - `https://open.feishu.cn/document/uAjLw4CM/ukTMukTMukTM/application-v6/application/get`
5. `application/v6 get app version`
   - `https://open.feishu.cn/document/uAjLw4CM/ukTMukTMukTM/application-v6/application-app_version/get`
6. `application/v6 list scopes`
   - `https://open.feishu.cn/document/uAjLw4CM/ukTMukTMukTM/application-v6/scope/list`

