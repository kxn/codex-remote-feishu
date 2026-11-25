# 单聊旧版行为对照与完整流程验收

> Type: `inprogress`
> Updated: `2026-11-25`
> Summary: 从旧版用户可见行为提取完整流程与相邻场景衔接，禁止预置私人运行状态或等待内部就绪冒充用户验收。

本次以 `edffb35f` 为旧版基线，初始审查对象为 `42159c64`，后续在同一分支统一修复。初始缺口和整改后的实测结果分别保存；当前结论见末尾“本轮整体验收”。保留用户已明确取消 Cron、VS Code 接管和历史改写的决定；其余单聊差异均需逐项证明，不能自行称作产品取舍。单聊与群团队继续独立。

## 验收如何判定

用户在飞书看到什么，就以该时点作为下一步输入的依据。卡片说“已准备好”后立即发 `hi`，必须真正执行并得到答复；测试不能先等待内部 online、手工附着实例或人为清除恢复状态，再假定用户流程已通过。首轮结束还要完成第二轮，核对会话上下文、消息顺序、展示和错误出口。

每项记录前置状态、入口和完整操作序列、旧版预期、实际平台输出、原生行为、异常分支和验证证据。验证状态分为“已执行通过”“已执行失败”“仅有局部测试”“未执行”；静态代码审查不能写成已通过。受控飞书端可验证生产 SDK 与发布协议，不能代替客户端视觉验收。真实原生后端、受控原生后端和真实平台分别记录，不相互替代。

初始审计结论：当时尚未通过。下文初始盘点里的测试名称只代表覆盖线索，不是整改后的通过声明；整改后的新鲜执行证据见末尾。

## 本轮重验：用户流程及其衔接

**本轮重验开始时未通过，当前执行结果见末尾。** `678f491a` 线上再次出现“成功待命卡后 hi 返回未接管”。前轮的全仓测试、真实模型及局部旅程日志是实际发生的验证，但部分 fixture 直接 launch 载体、读取内部 online/route/receipt 后才操作，不能证明用户从入口连续使用。因此前轮“完整流程已接通”的概括撤回，保留各自有限证据，不把测试数量当产品验收。

现场按原始事件可重建为：先前工作区已准备 → 服务退出核验未完成，旧载体留在 draining → 同库重启 → 普通消息提示接管 → `/list` 选原目录并新建 → 本次准备实际失败，但旧 ready 快照盖成成功卡 → 下一条 hi 仍被消费为 not_attached。工作目录和声明的目标仍在，问题是目标、实际执行环境和本次选择结果没有形成一致的生命周期。

本轮把以下要求作为整体完成条件：

1. 安装和外部世界可准备目录、账户及原生历史；私人接管、实例、会话、队列必须从 SDK 人类操作产生。不能在旅程开头直接 UpsertInstance、launchPrivateNative、写 claim/checkpoint/retired 来代替用户流程。
2. 下一动作以人可见的卡片、文字、反应或 HTTP 同卡响应为时钟。看到成功立即输入/点击；不先查询内部 online、checkpoint、PublishedMessage、CardRef 或 receipt。它们只用于事后断言和失败诊断。
3. 必须核对衔接两边：上一步是否真的兑现、下一步是否实际完成。没有 noWorkspace 或有任意卡不等于通过；须有正确工作目录、会话、原生输入及可见最终回复，再继续下一步。
4. 对用户允许的相邻操作组合逐条登记；包括多种菜单/slash入口、同目录/换目录、忙态/确认/附件、取消与断线/正常关闭/退出核验超时。失败必须具有真正能继续的操作路径。
5. 物理载体已停止与旧业务结果已确定分别证明。真实进程组及写入封锁可完成载体退役；未知结果保持原记录，不补造终态、不自动重跑，也不能因此无期限挡住无关的新工作。

下表每条需登记可见步骤、原生终点、实际结果和日志；缺失或只分层测试继续记未验，不能以函数接线相同代替体验相同。

### 旧版可见序列和衔接验收合同

以下来源是旧提交的真实代码行为，用作外部承诺的起点，不要求复刻其实现缺陷：`internal/core/orchestrator/service.go:ApplySurfaceAction` 先处理启动/脱离 gate、文本对 picker 的关闭、再路由文本/图片/文件/reaction/stop/detach；`service_target_picker.go:dispatchTargetPickerConfirmed` 在选择完成时输出同卡成功并处理曾保存的待发输入；`service_surface_actions.go:prepareNewThreadWithOverlayCleanup/stopSurface/cancelPendingHeadlessLaunch/detach` 记录就绪、阻塞、停止请求和草稿处理；`service_snapshot_runtime.go:ApplyInstanceConnected` 连接后完成 pending 生命周期。`app_ingress.go:HandleGatewayAction/onHello/onDisconnect` 是旧平台到这些动作的路径。旧代码中“已有成功判定的实现”并不构成其时序正确的证明，仍以人能看到的承诺和紧随动作验收。

| 序列及起点 | 用户可见动作→承诺→紧邻动作→终点/失败出口 | 证据定位与执行断言 |
|---|---|---|
| U01 未接管、普通文本 | 发 hi→被要求选择工作目标→选已有目录→新建→成功卡→立即 hi；未接管时的原 hi 明确未发送、不自动重播，final 后第二条 | `edffb35f` 的 p2p `handleText` 先返回 `not_attached`；picker 的 pending/replay 不等于未接管私聊自动保存输入。核原 message ID 与无重复 native dispatch。 |
| U02 未接管、图片优先 | 发图片或图+文字→目标引导→取消或选目录→继续消息 | 旧 `stageImage/handleText`；核图片簇、source IDs、顺序和最终 native 输入；不准事先构造队列。 |
| U03 目录 A、无历史 | `/list`→选 A→新建→成功后文本→同 session 二轮 | 旧 `dispatchTargetPickerConfirmed` 的 `new_thread_ready` 成功文案明确称下一条创建；以平台可见卡为唯一等待。 |
| U04 A 已有运行环境→A 新建 | 正在 A→用户选择 A、新会话→成功即发→新 thread；若旧 instance 不可用则可恢复或显示真实失败，不能成功卡后 noWorkspace | 旧 `enterTargetPickerNewThread` 同目录短路；现场指出旧 route/CWD 非可用证据。 |
| U05 A→B→A 已有 | A final→选 B 新建→B final→选择 A 原 session→A final；跨页旧回调、迟到 A/B 输出不串 | 旧 `useThread` 和 workspace attach；检查具体 A/B 的 cwd、身份与 thread IDs，不预置 claim。 |
| U06 已有 session→同 session | `/list` 或 `/use` 打开卡片并明确选择已有→切换成功→下一输入到该会话；占用/不可见输出实际失败且原会话不丢 | 旧 `useThreadWithOverlayCleanup` 的 busy、not_found、claim 分支。 |
| U07 text 绕过 picker | 菜单/目标卡编辑中直接发文本→picker 关闭或失效→原路由正确处理；processing 时按旧门禁提示，不吞文字 | 旧 `ApplySurfaceAction` 的 dismiss 和 `blockActionForActiveTargetPicker` 的顺序。 |
| U08 启动中取消→new | 选择目录显示处理中→`/detach` 或可见取消→确认取消与关联启动结束→重新选目录/新建→文本 final；迟到旧 hello 不抢路由 | 旧 `cancelPendingHeadlessLaunch` 输出 exact instance kill、清待发并最终提示；新版不把 kill 请求等同退出。 |
| U09 排队/忙→new | 活跃 turn + 草稿/排队→`/new`/目标卡；按旧忙态允许/阻止给真实反馈→结束或取消→再新建并发下一轮 | 旧 `blockNewThreadPreparation`、`discardDrafts`；未获许可的旧已发输入不能伪造取消。 |
| U10 pending request→cancel→new | 原生确认显示→取消/拒绝/处理→确认原请求状态→`/new`→下一消息；重复按钮无重复动作 | 旧 `activePendingRequest` 阻止错误路由变更；真实 request/frame 与可见卡两者都核。 |
| U11 run→stop→new | 活跃 turn→`/stop`→明确“已发送停止请求”或未可中断；不提前称结束→终态后 new→下一轮 final | 旧 `stopSurface` 的 `stop_requested/stop_not_interruptible` 和草稿清理；新版用 native terminal/未知回执守住边界。 |
| U12 多图排队→new | 图 1、图 2、文字在下载/排队中→明确新建/取消→迟到下载→再发文字 | 旧 stage/route-change discard；新架构核 origin 顺序与 manifest，旧附件不得自动流入新 thread。 |
| U13 附件→detach/reselect | 文件下载中→detach→picker 重新接管→新消息；managed staging 精确清理，非系统文件保留 | 旧 route-change discard 与新 durable cleanup；需验真实 SDK 收据而非伪造 staging 完成。 |
| U14 附件→stop | 文件/多图暂存或排队→stop→可见清理数量/结果→迟到完成→下一消息 | 旧 `stopSurface` 先计数并 discard drafts、clear requests；不对已 accepted 输入虚称未执行。 |
| U15 配置→执行 | 无载体选 profile/model/access/plan→原卡成功→立刻选目录/新建→下一 native request 精确取用户所选设置→final | 旧设置 action + 启动 contract；新 profile/账号环境与 cwd 均验，不能先手工写设置再宣称 SDK 完整。 |
| U16 忙时配置→停止/下一轮 | 活跃中改设置→明确即时生效或等待/失败→本 turn 正确完成或 stop→下一轮与重启按已保存设置执行 | 查旧 backend contract 和重启语义；不能靠静态菜单/当前 snapshot 代替 native 下一帧。 |
| U17 Codex→Claude→OpenCode | 选后端/profile→成功反馈→目录/新建→首轮 final→再切后端；无效凭据给具体失败，切回可修复 | 每个 backend 各有真实载体身份、会话命名空间；受控 frame 与可用真实账户边界分别登记。 |
| U18 `/model` 冷目录→选择→执行 | 打开等待卡→原卡自动更新→选模型→成功→下一 native turn 确认模型，失败仍可重试 | 同一原卡最新 token、callback 时序和平台 receipt；不得把异步模型列表完成当设置已保存。 |
| U19 正常重启→继续 | 用户 A 选 session 并完成→干净退出/重启→用户看到恢复结果→原 session 立即下一轮；后台完成与 UI 都可追溯 | 旧 onHello/dispatch 链；新 journal 以真实 exact hello/receipt 恢复，不能由测试直接设置 attached。 |
| U20 断线 unknown→new | 原 turn/消息未知→用户看见未知/恢复反馈→明确 `/new`→新目录或相同目录→新消息 final；旧 late terminal 不污染 | 旧能力须保留可逃生交互，新 durable unknown 不视作成功、不重发；逐原始来源验证。 |
| U21 任务回复→文件/预览→下一轮 | native 工具/思考/最终答复、diff/preview→平台可见→选文件发送→正确回执→下一轮 | 旧 `onEvents` 与 renderer/preview；确认 response 和文件内容均来自该 turn，失败不能阻塞正文。 |
| U22 查询和动作同卡→切页 | `/history`/`/goal`/`/review`/`/compact`/`/mcp` 在发出后返回另一页→结果只更新仍有效原页；目标动作终态后下一输入可发 | 旧 active owner flow 和 pending request；分开“请求写入”“后端接受”“完成”，不可只验按钮或 ACK。 |
| U23 按钮/平台菜单入口 | `/menu`、平台菜单、slash 别名及同卡按钮从各起始状态达同一产品操作→完成后重放旧 token 被拒→当前卡可继续 | 旧 gateway parse/routing 和 owner flow；同卡不能被旧异步 PATCH 覆盖，重复 event 不重复发送。 |
| U24 故障/权限→修复 | profile/config/目录不可用或模型探测失败→原卡给真实原因→返回设置并修正→目录选择→final | 身份/存储错误不伪装配置错误；已确认设置不能因旧缓存或旧 carrier 隐式退回。 |

| U25 自动继续与 AutoWhip | 新会话首轮上游中断→真实自动续轮→关闭→开启 AutoWhip→原时限补发→停止短语→关闭→普通消息 final | 不手工推进 tick；自动输入各一次，所有轮次同一真实原生 session。 |
| U26 排队与表情操作 | busy→排队首条文字及非队首图文→重复点赞／鼓掌→撤回另一项→`/steerall`→`/stop`→下一轮 final | 冻结原 source 和图片配对；重复 reaction 不重复发送，撤回文本不进入 native。 |

执行时每个 U 编号必须登记其 SDK 入口、可见反馈、用户下一动作、确切 native session/turn/input、结果或失败、重启/重复和平台 receipt；某个步骤由内部状态注入或等待则记“局部”，不填整条通过。真实飞书服务与受控平台、真实后端与模拟 native 必须明确区分。


## 初始审计的现场事实

初始调查时服务由用户手工停止；调查阶段未启动线上服务、未修改线上数据库。用户随后授权修复完成后启动服务，当前部署结果另行登记。系统记录 1 小时 22 分钟累计 CPU 时间，这是该进程生命周期的总量，不能直接用来证明截图时的即时占用率。

私人 journal 的序号 18 为工作区选择确认，序号 19 为 `hi`。该消息的已应用结果只有 `headless_starting` 提示，没有原生 prompt 或排队项；wrapper 随后完成初始化并接入。该 SDK 消息时间为新加坡时间 2026-11-24 15:45:29，不能未经核对称为 11 月 25 日新接收消息。仍需把成功卡发布、hello、启动回执与首条输入按精确记录对应。

之前的真实 Codex 新建测试明确不发送业务 prompt，只验证新会话准备态；隔离 HOME 仅复制登录资料且手工设置环境。这项证据不覆盖真实配置下的首条执行、完整结果发布或连续对话。原“完整流程已通过”的结论不成立。

## 初始审计已经执行的检查

- 已在临时目录分别构建旧版和当前版本的命令目录及解析器，用旧版 canonical slash、菜单 key、选项和别名组成 241 个输入实际对照。解析差异只有已明确删除的 `patch`／`bendtomywill` 三个入口；新加项目入口单独验收。因此不能把问题笼统归因于 Slash parser 全部重写，重点在后续接线和生命周期。
- 现有飞书 adapter、control、orchestrator、privatechat 与 daemon 测试重新运行通过，日志 `/tmp/private-nov25-baseline-current-suite.log`。该结果只说明原有测试仍通过，不能证明下列使用场景已验收。
- 首条实际消息的现场记录显示：15:45:25.691 工作区卡处于处理中，15:45:29.908 `hi` 到达，15:45:30.784 成功卡确认送达，15:45:30.874 较旧的“启动中”提示才确认送达。后两者倒序是新版独立发布链的真实现象。工作区卡自身有 display chain，独立提示未关联该链；interactive effects 可并行发布。需按同一业务阶段定义发布顺序及过期状态处理，而不是一律全局串行或吞提示。
- 真实 Codex 0.171.0 的两轮测试已通过，日志 `/tmp/private-real-codex-two-turn-final.log`。从 SDK picker 新建，实际原生模型目录选择账号支持的模型，经精确 input→dispatch→native binding 关联，核对同一 thread 的两次不同 turn、assistant 文本、成功终态及对应 revision 的已确认平台发布。原生后端真实，飞书端受控，原 HOME 配置只读复制隔离；这不等于所有场景通过。首次试跑因隔离测试默认模型不被账号支持而失败，保留该证据，不作为线上同一故障根因。

## 入口调用图与初始缺口

| 场景 | 旧版调用链（均指 edffb35f） | 新版生产调用链（均指 42159c64） | 可确认结果 / 缺口 |
|---|---|---|---|
| 私聊文本及 slash | `internal/adapter/feishu/gateway/inbound_lane.go` `PlanInboundMessageEvent` → `QueuedMessageWork.parseAction` / `gateway/routing.go:parseTextAction` → `daemon/app_ingress.go:HandleGatewayAction` → `handleActionLocked` → `applyIngressActionLocked` → `Service.ApplySurfaceAction`；`app_ui.go` 投影、发送 | `application_ingress.go:hook.Receive` → `ingressmapper.Message` → `Router.Admit/Deliver` → `privateApplicationAdmitter.BuildPrivateIngress`（`feishu/private_ingress.go:DecodePrivateMessage` 复用旧 parser）→ `Journal.Admit` → `privateWorkRunner` → `Processor.ApplyIngress` → `privateHostReducer.Reduce` → `App.Action` → 同一 `Service.ApplySurfaceAction` → checkpoint/effects → publisher | 入站变为先持久且 owner/epoch 隔离。旧代码保留不代表生产入口仍调用旧 `HandleAction`。已确认测试缺口：目前 assembled 执行测试先人工建立并选择载体再发文本，不能证明全新无实例第一条 hi 经选择目录→启动→原生最终回答→飞书 final；`private_new_workspace_sdk_journey_test.go:TestPrivateSDKPickerNewWorkspaceRealCodex` 明确不发送业务 prompt。 |
| card callback / 菜单 | 旧 `HandleGatewayAction` → `handleActionLocked` → `synchronousCurrentCardActionResultLocked`，同卡直接返回 inline 替换；异步事件交 `handleUIEventsLocked` | `hook.ReceiveInteractionResponse` → neutral owner/card token 校验 → `BuildPrivateIngress` → `Processor.ApplyIngress` → `privateHostReducer.PlanCard` → `private_callback_response` 同事务保存响应 → SDK HTTP 原卡 response；后续异步更新经 `privatePublisher` 和确认 receipt | `private_assembled_menu_matrix_test.go:TestPrivateAssembledSDKMenuMatrix` 测菜单分类往返/旧事件重放；`application_ingress_unknown_card_test.go` 测 unknown 后消息归属。须补真实多步 callback→长操作→同卡异步最终状态、延迟旧回调/跨 owner 拒绝与网络错误全链，而非只有解析/每步卡片。 |
| 图片/富文本/引用 | SDK planner 按 text/post/image/file/merge_forward 分支；引用及媒体下载由 `QueuedMessageWork.parseAction` 进行，再按 surface lane 入旧 `HandleAction` | 同一 planner：纯文本先 decode；图片、文件、引用返回 `ErrInboundStaging`，neutral 保留原消息，`application_ingress_staging.go:RunStaging/stagePrivate` 下载后 `StoreStagedPrivateAction`，完成 `BuildPrivateIngress` → `Journal.Admit`；`processor` 按 neutral router/sequence 与成功 route-discard 原子取消 | `private_sdk_staging_order_test.go:TestPrivateSDKOldImageCannotCrossNewOrDetach` 受控阻塞下载、/new 与 /detach→picker 重选、manifest cleanup/reopen；`private_host_media_test.go` 测附件本地暂存与失败。尚无真实飞书下载/引用 message API 的闭环证据；必须补多图簇+post/quote/merge_forward 真正进入 native 输入并显示最终答复的受控 SDK 全旅程。 |
| 点赞 / 鼓掌 / 撤回 | 旧 `gateway/inbound_lane.go:HandleInboundMessageReactionCreatedEvent` → `ParseMessageReactionCreatedEvent` → lane → core `Service.ApplySurfaceAction` / `handleReactionCreated` | `hook.ReceivePlatformEvent` → `ingressmapper.Observation` → `BuildPrivateIngress` → observation lane；`privatechat.App.ValidateObservation/Observe` → 同 core；effect 冻结 queue source/command、native receipt、reaction identity 后发布 | `private_assembled_execution_journey_test.go` 测真实 wrapper + 受控 native 的图片、reaction、steer、shell、stop；`service_applause_test.go` 测非队首选定 source 和图片组。应补 SDK 撤回+晚下载、不同用户伪反应、相同消息不同时刻点赞/鼓掌以及已选非队首图片簇→实际 native 最终回复 + 恢复的端到端组合。 |
| 纯文件及带文件文本 | 旧 SDK `parseAction` 下载文件，旧 daemon 通过 workspace 上下文路径/附件预处理后 core | `privateHostReducer.Reduce` 使用 `BeginPrivateStage` 冻结原 queue 目标和占位 → `runPrivateStageInput` 真实工作区文件暂存 → durable receipt → `CompletePrivateStage` / `FailPrivateStage`，再 dispatch | `private_host_media_test.go` 覆盖文件暂存、symlink、取消、源缺失、迟到 receipt；`private_reconnect_staging_journey_test.go` 覆盖旧 stage 重连。应补 SDK 文件+文本多文件 FIFO 真 native 读文件→平台 final、下载/暂存失败后下一消息仍可用，以及中断/重启原源 cleanup。 |
| 入站持久与顺序 | 旧 surface lane / 内存排序，core 队列为业务事实 | neutral 事件先于 private journal；`Journal.Admit` 记录 owner、generation、router origin；`processor` 序列化 reduce→checkpoint+applied+effects 原子提交；正常与控制 lane 分开；`checkpoint.go` 根据 core discard outcome 原子取消原顺序旧输入 | 最近 SDK 图片与取消两序 `private_sdk_staging_order_test.go`、`privatechat/ingress_origin_test.go`；严格说不同外部队列/未知回执/错误 epoch 的完整平台旅程仍需补。失败不应以 mock 直接确认投递。 |

## 使用场景矩阵与初始覆盖盘点

证据缩写：旧入口统一 `edffb35f:internal/core/control/feishu_commands.go`、同目录 `feishu_commands_{goal,review,sendfile,opencode}.go` 与 `edffb35f:internal/app/daemon/app_ingress.go:203`；旧 core 分发 `edffb35f:internal/core/orchestrator/service.go:ApplySurfaceAction`。现入口统一上述新调用图。测试缩写 M=`private_assembled_menu_matrix_test.go`（只证明出现响应、部分导航）；X=`private_assembled_execution_journey_test.go`；P=`private_profile_catalog_sdk_journey_test.go`；C=`private_clean_restart_sdk_journey_test.go`。除另述外，不能把 M 的“有任意卡”当该命令全流程成功。

| 命令 / 入口 | 前置 → 全操作序列 → 可见结果 / 失败出口 | 初始覆盖和缺口（整改证据见末尾） |
|---|---|---|
| 普通首条 hi | 空私人 owner、无 carrier → SDK hi → 必要时选目录及新建 → 真 wrapper hello → native 首轮→final→同聊天最终回复；目录不可用/启动失败明确提示且不丢原输入 | `private_new_workspace_sdk_journey_test.go` 只到 new_thread_ready，X 先 `/list` 人工选择再发送；**完整首条 hi 到最终 Feishu 答复未证明**。 |
| 连续第二轮 | 首轮真实 final 已投影且 receipt confirmed → 第二条 SDK 文本 → 同 session 第二轮 native command → 第二条正确最终回复，不重复首轮 | X 的 seed→hello→next 主要验证 turn/start、steer/interrupt/队列；需明确逐轮验证最终消息正文、session 及平台 receipt，不只 native turn 数。 |
| `/menu` `/help` | 未绑定/运行中 → 打开、分类、返回、重复回调 → 同卡导航/各自可用项；过期或 foreign card 拒绝且不改当前卡 | M 验证三个分类、back、重复回调；需各 command 的参数提交、异步结果与陈旧卡完整组合。 |
| `/status` | idle、运行、等待确认、无载体/恢复中分别打开 → 原状态/队列/工作区信息；查询失败明确提示 | M 仅有输出；X 测运行时菜单。缺状态与实际 native/receipt 精确一致及恢复后同卡验收。 |
| `/workspace` `/workspace list` `/list` | 未绑定或原会话 → 打开列表→选择目录→选择已有会话/新会话→确认→选中路由→发送消息→最终答复；失效路径/忙/冲突不丢输入 | X 有 SDK 选择 carrier；`private_new_workspace_sdk_journey_test.go` 真 Codex 新建到待命；缺两者串成首次 prompt→final、目录/账号切换及进程启动失败恢复。 |
| `/use` `/useall` | 有已验证历史→列表及卡片选会话→确认→下一条确实发原 session；不可见/其他 owner/未静止旧执行给明确原因 | 核心历史隔离与 alias 另有 focused；缺 SDK 输入→同一 native session→最终平台答复，及 `/useall` 多项选择完整回执。 |
| `/new` | 有原 session/待命→明确新建→冻结/丢弃旧草稿→新 thread 首条→final；未知旧执行不可重发 | `private_sdk_staging_order_test.go` 控制下载并核取消；真实 Codex 新建只到待命；缺新旧 session ID 区别、第一条 native 最终答复。 |
| `/detach` `/workspace detach` | 已选会话/运行中/未知执行→slash 或 workspace 卡确认退出→旧队列按原规则处理→重新选目录→新输入及 final；旧执行未知保留证据 | `private_detach_assembled_test.go` 两种入口；旧图下载晚到的 SDK picker 复选已测；需真实 active carrier 终态、重接及平台最终答复贯通。 |
| `/stop` | 活跃→停止→native interrupt 精确 receipt→状态/原卡反馈→再输入同 session 正常回答；无 active、未知发送要正确反馈不误报成功 | X 有真实 SDK 到 wrapper interrupt 与下一 turn/start，仍需 stop 后 actual final 内容及 unknown/拒绝/重试组合。 |
| `/compact` | 活跃/idle 合法时提交→native 压缩完成→进度卡/结果、后续输入；不支持或未发送有明确反馈 | `private_compact*`/native focused；M 不证明完成。缺 SDK→native compact result→同卡反馈及下次真实答复。 |
| `/steerall` | 运行中含多条待发/图片簇→明确 steer→准确冻结来源与顺序→native steer receipt→进度/最终答复；不支持/unknown 保留队列 | X 有 reaction steer 和图片，但需 typed `/steerall`+多 source+final 和 unknown receipt 恢复。 |
| 点赞 / 鼓掌 | 运行中或排队→指定非队首文本反应→只消费冻结项及关联图片簇→native steer/shell→receipt→更新相应 emoji，原顺序不变 | X 有 SDK reaction 与 native 帧，`service_applause_test.go` 有 core 选择逻辑；缺两者非队首组合、真实最终答复及 SDK 去重/错误 actor。 |
| `/history` | 选中 session → 查询→列表/翻页→选择→原 native thread；未绑定、其他 owner、历史隔离给明确反馈 | M 仅有响应，native thread-read focused；缺完整 SDK 列表选择→thread/read→final。 |
| `/model` `/model refresh` | 三 backend 各自的当前 profile→打开列表→选择或手填→真实 settings owner 持久→同卡结果→下一 prompt 用该 model；刷新失败保留 last-good、显式重试 | P 覆盖无载体目录/异步同卡/缓存与重启/stale；X 覆盖 attached Codex 同卡和真实配置选择；OpenCode 有 `private_assembled_opencode_settings_test.go`。缺三后端都验证选择后下一 native request 的模型及平台 final。 |
| `/reasoning` `/access` `/plan` `/verbose` | 打开→选有效参数→owner receipt/相应 native→原卡反馈→下一轮携带设置；非法参数/不支持显示错误、后续可继续 | `private_assembled_settings_backend_test.go`、`private_invalid_settings_test.go` 及 M；缺 Codex/Claude/OpenCode 全组合 next-prompt frames 与重启持久、请求批准后的真实体验。 |
| `/codexprofile` `/claudeprofile` `/opencodeprofile` | 列表→换 profile→有待发时依旧版忙态/延期→设置实际持久→新 carrier/session 正确配置→final；失效凭据可修复 | 配置 owner 与切换 focused、M 缺逐项表单和真实启动；应测旧实例仍 attached 时不泄漏其账号目录。 |
| `/mode` | 打开→Codex/Claude/OpenCode→真实配置 receipt→忙时等待→新 native 开始并最终回复；废弃 VSCode 不开放 | M + settings focused；缺三后端 SDK→重启/原卡→实际首轮。VSCode、follow 为明确取消。 |
| `/autowhip` `/autocontinue` | 打开/参数 on/off→持久→触发旧策略定时动作/继续轮→正常 final；禁止 busy 或 unknown 重发 | M 仅文本可见；需真实 tick、控制并发、恢复、禁止重复派发全链。两者没有用户取消，不能删除。 |
| `/goal` | 查看/设置/清除目标→native 精确 command/request→结果同卡→重启仍能结算；unknown/拒绝有界反馈 | 旧 `feishu_commands_goal.go` 与 Goal 核心 retained；focused outcome/checkpoint；M 未包含 `/goal`，缺完整 SDK→native→原卡及运行中并发。 |
| `/review` | 参数/菜单选择 uncommitted、commit 等→native review→结果/后续会话；不支持、选择失效明确反馈 | M 仅 opens；专用 core/native focused，缺用户实际 review→final→新输入全链。 |
| `/sendfile` | 打开 picker→导航目录→选择→同卡确认→preflight→后台 SDK upload 顶层消息→receipt→不重发；文件更改/上传 unknown 安全反馈 | `private_assembled_sendfile_journey_test.go` + `private_send_file_test.go`；需补真实 SDK upload 失败/重启与选择源在 confirm 后变化的组合；禁止发送用户真实文件。 |
| `/mcpoauth` / `/mcp` | 打开已有 MCP server→选择 OAuth→真实 native 请求/链接/完成→原卡状态；无服务器/登录失败提示 | 旧 `feishu_commands.go` 保留，M 不含；目前无法由已有矩阵断言端到端可达；需单独审 native 命令/回执及受控 OAuth。不能只因 daemon 旧 handler fence 判断失效。 |
| `/admin` | 管理菜单打开→查看→逐项 network/autostart 等选择→共享设置 owner 成功 receipt→同卡刷新→重启验证；无权/失败给原卡说明 | `private_system_command_test.go:TestPrivateSystemMenuUsesOriginalBackendAndUpdatesCurrentCard`；须逐项动作与真实后端/可设置性对应，M 只开页。 |
| `/debug` | 菜单→选择日志/诊断→实际后台动作→原卡结果；路径/权限/失败说明 | 上述 system test 部分路径；逐子项覆盖需查，不可把打开页当功能通过。 |
| `/upgrade` | 打开→检查→同卡确认→共享升级事务与维护状态→实际完成/失败/unknown→同卡结果；busy/多 owner 不重复安装 | `private_codex_upgrade_test.go` 证明私聊 journal→共享 request、卡片与 unknown 重放，专门不伪造真实安装；需 coordinator 真正 stop/install/rebuild/release 与同卡结合。 |
| `/project` | 新版增加，非 edffb35f 私聊回归项：查看本人授权项目→选择分享→明确确认→最小权限结果 | `private_project_journey_test.go`；与旧版差异为新增，单独核权限，不替代旧入口。 |
| `/primary` `/coworkers` | 群设置；私人 `privatechat.App.ValidateAction` 明确拒绝，不作为私人缺口 | 对照旧 `feishu_command_primary.go`，不改群语义。 |
| `/cron` `/follow` `/vscode-migrate` `/bendtomywill` | 用户明确取消，不复活；遗留 catalog 文本不等同私人可用 | `privatechat/app.go:ValidateAction` 阻断 Cron、follow、VS Code；应检查 typed/卡片都不会意外挂 pending，而不是恢复功能。 |


## 调用图中必须验证的接线差异

旧 daemon 的 `handleActionLocked` 除了调用 core，还负责动作后的问题卡重新显示、目录偏好同步、工作区交互上下文和统一 UI 派发。新版私人入口绕开整个旧 handler，复用了 `ApplySurfaceAction`，却必须逐项证明外围职责已在新 owner 中接通。保留 core 源码不能作为这些职责已保留的证据。

- 问题卡重新显示：旧入口对非 `/status` 动作调用 `ReplayActivePendingRequestVisibility`，新私人路径尚未找到同一调用。core 在非 status 动作中仍调用会改变重试计数的 replay，却丢弃其事件；旧外层补发，新外层未补发。须用 SDK 复现 pending/degraded 确认卡时发普通消息、开菜单及重启后的恢复，不能只补一个新的提示门禁。
- 文件工具交互上下文：旧入口同步工作区的 surface-context 文件；新版采用冻结 owner 的原生工具身份。`private_tools.go` 已以 carrier credential 与 `Processor.ToolSurface` 取代文件来确认 MCP 发件归属。这部分属于有明确替代的实现差异，仍需验证发文件、主动回复和多 Bot 完整旅程，不能简单恢复共享文件造成串身份。
- 状态展示：旧入口把事件流送进统一 UI handler，新版按效果与 display chain 异步发布。单条链内有序不等于同一启动操作的全部提示有序，现场已证明启动提示和成功卡倒置。
- 热路径：新版每次 runner pass 会对载体刷新历史策略，并扫描团队历史／session claims。扫描失败时，普通输入和 native 事件投影会 continue，形成“历史列表读模型错误影响当前私聊执行”的错误依赖。历史列表快照应有独立更新与版本，当前执行的安全校验仍由精确原生租约／会话登记负责；不能用缓存绕过授权。须测实际耗时和查询量，尚无事故时 CPU profile，不把复杂度分析当作高 CPU 根因证明。

## 必须按用户顺序执行的连续旅程

1. 从未绑定工作目录开始，发送 `/menu`，在原卡进入工作区列表，选择一个目录并新建。看到成功结果后立即发首条文本，得到真实最终答复，再发第二条验证同一上下文；不手工等待内部状态。
2. 保留原会话，开另一个工作区并发送消息；返回第一个已有会话继续，验证目录、模型选择、权限和上下文归属，不把旧回复投到新会话。
3. 执行中打开菜单、查看状态、切设置页再返回；逐个保存模型、配置、权限等选择，下一轮实际生效。卡片内导航不增加菜单副本，结果未知时不能假报已保存。
4. 连续发文本、单图、多图、纯文件、带文件文本、富文本和引用，检查原有暂存、reaction、排队及非队首选择规则。验证模型实际收到的顺序和内容，最终答复可见；下载失败和取消不会堵住下一条。
5. 需要原生确认时接受、拒绝、取消，补发要求、steer 和停止；验证原执行真正结束，下一条能继续，重复按钮不重复操作。
6. 按旧设置交付本地文件、临时预览及 diff；链接可用且目标正确，失败给具体原因。不会把群团队的发布流程强加给单聊。
7. 空闲、执行中、排队中、等待确认时分别断线或正常关闭，再恢复原会话。只恢复能确认的输入与原生状态；新建、退出及修复菜单可用，未知旧结果不阻断无关新工作。
8. 原样核对每个仍保留的 Slash、参数、别名、平台菜单和卡片按钮。管理项须实际完成设置／取消／失败路径，仅打开页面不算通过；升级在隔离安装目标验证，不影响已停的线上服务。

## 异步时序整改的统一设计

异步 I/O 保留，但必须恢复旧版同一操作的因果关系。所有权、业务状态和展示的权威分别明确，不能在每个错误点再叠一套 gate。

1. **状态入口**：同一私人 owner 的文本、按钮、启动结果、原生事实和关闭结果统一由 Processor reduction 修改状态。外部任务不持状态锁做网络 I/O，也不能绕过 owner 直接写 core。任务启动前冻结输入来源、原操作身份、目标会话与必要配置；结果返回时只完成那个任务，不按“当前会话”猜目标。
2. **操作完成**：进程已启动、协议已连接、会话已准备、消息已发送和执行已完成是不同事实。新建成功只由真实可用事实触发。启动回执晚于 hello 时只能补全原操作证据，不能把状态倒退；被取消操作的旧回执也不影响后来操作。复用原 picker／request／queue／command 身份，不另造第二个并行会话。
3. **可见状态**：启动过程只维护原有工作区操作卡，临时启动提示必须引用同一操作与版本。由该操作唯一的发布处理者决定更新。尚未开始发送的旧状态在发送前核对当前事实并合并；已经开始的发送遵守收据与未知结果规则，不假称没发。成功、失败或取消之后不能再补发此前的“正在进行”。其他操作和群团队不被一个全局网络锁串起来。
4. **发布顺序**：同一可替换卡的更新必须串行结算；同一执行的正文、终态与最终交付遵守各自的因果关系。独立业务消息可以并行。发布失败不回滚已经成立的执行事实，未知发送不自动重跑业务。卡片顺序不应靠线程调度、睡眠或重试次数保证。
5. **旧版外围职责**：问题卡补发、设置实际持久化、原生工具身份和预览发布等逐项对照旧 handler。已由新权威接管的职责使用明确端口；尚未接通的职责补到正确层。只复用 core 而遗漏原 handler 的业务步骤，仍是使用流程回归。
6. **输入合同**：按旧版逐状态定义收到文本／图片／文件后的行为：是否待选目录、进入草稿、排队、执行、被拒绝及怎样继续。入站持久化只代表收到，不能代表执行。若旧版启动期间拒绝输入，审计如实记录；改变为自动保留重放属于额外产品行为，不能偷偷加入来遮盖显示时序问题。
7. **可诊断性**：每个入口关联原始事件、操作、native command、turn、状态 revision 和发布收据。错误保留所属阶段，禁止所有失败只退化成“操作未完成”。计时器只触发超时事件，不能代表后台动作完成；后台扫描需要有工作依据和限额，并量化空闲成本。

### 故意乱序的验收

同一完整旅程分别延迟：入口处理、进程创建、hello、启动回执、native ACK、模型响应、文件下载、平台发送及回执落库。两种完成次序均测试；在每个异步边界前后取消或重启。断言用户看到的状态不倒退、首轮和第二轮不丢不重复、原生目标不改变、菜单原卡更新、未知结果仍有明确出口。仅单个 publisher 构造事件证明反序的测试是缺陷复现，不代表 SDK 到 native 的完整验收。

## 证据登记与后续修改规则

所有问题须同时记录旧版调用点、新版调用点、可见差异和复现证据。修复按所有权和状态边界处理，不能用吞错误、伪成功、盲目重试或让用户先绕道点击来代替。新增回归必须从真实生产入口进入并验证终点；只有对象层单测的项目继续标为局部覆盖。

每次修改后先跑对应失败用例，再执行受影响的连续旅程；整体验收要求整张场景清单有结果。未验证项和环境阻塞不能被测试总数覆盖。完成后同步[应用分离记录](./private-team-application-separation.md)及对应 canonical 状态机；本文件在验收结束前保持 inprogress。

## 执行、恢复和交付的调用图及初始覆盖

1. **身份与 SDK 入站**：`entry.go` 的 `prepareApplicationCore/configureApplicationIngress` → `applicationIngress` neutral router → `privateApplicationAdmitter.Admit` → private journal 持久 ingress → `privateWorkRunner.pass` → `Processor.ApplyIngress` → 原 `Service.ApplySurfaceAction`。callback 同步响应和异步 effect 分开；团队仍由 `teamApplicationAdmitter` 入独立状态机。测试 `application_runtime_test.go`、`private_assembled_execution_journey_test.go`、`private_sdk_staging_order_test.go`；平台网关为可控 fixture。
2. **配置与启动**：`headless.start` effect → `runPrivateHeadlessStart` → `launchPrivateNative`；冻结 cwd、profile、backend，`entry.go:117–132` 用 `buildDaemonHeadlessBaseEnv(os.Environ(), capturedProxyEnv)`，启动解析保留生产 profile 配置、native history namespace、executionowner 与独立 credential。wrapper 经真实 `wrapper.LoadConfig`，Codex/Claude 共用子进程启动和 MCP 注入；private bearer 验证在 daemon。常规测试只测显式 fixture 的 env/profile，无法证明所有现场 provider、配置 include、代理、custom MCP、文件访问策略组合。
3. **连接、首次工作**：wrapper authenticated hello → `privateNativeHost.acceptHello` → `ApplyAuthenticatedNativeHello` → `ApplyInstanceConnected`；用户可见 picker 成功卡后才发送文本。冻结 `prompt.send` effect → `PrepareEffectDispatch`/`MarkSending` → `Outbound.SendLeased` 指定连接 → wrapper journal/native frame → 后端 response / binding → private native event journal → Processor 原 core reducer。
4. **回复与下一轮**：事件按 carrier/sequence/hash 先持久后 ACK；live delta 有独立 seq，仅展示。`item.completed` / `turn.completed` 更新同一 surface queue，生成 publication；`privatePublisher` 按 operation 持久计划和平台 receipt 提交。真实 Codex 双轮：日志 `/tmp/private-real-codex-two-turn-final.log`；先选真实 model.list 中 `gpt-6.2-astra`；两个独立 SDK 消息、不同 turn、相同 native session，均有精确 command→native accepted binding→对应 `agent_message` 正文→completed 终态→同 revision 已确认 gateway publication。验证测试为 `private_new_workspace_sdk_journey_test.go` 加 opt-in `private_real_codex_two_turn_test.go`；由测试进程启动 wrapper、temp HOME/CODEX_HOME，复制源 auth/config，受控 Feishu gateway，未实际对飞书发消息。测试复用了生产 base-env 组装，但 admin/bot 配置为 test App，不能说完整复刻现场。
5. **发送中 stop/steer/二次输入**：原 core queue/command 与 `private_work_runner.go` 的 per-owner input lane + native journal 证据；`private_assembled_execution_journey_test.go` 覆盖受控 native Codex/OpenCode 的用户输入、图片、reaction steer、stop、下一条、重启；真实模型本次只覆盖非中断顺序双轮，未覆盖真实 Codex 的 steer、stop、工具执行和确认请求。
6. **确认与输入结果**：`processor_native.go:20–49,246–261` 用真实 frame written 与 request resolved 分开更新；`journal_native_outcomes*` 相关结果匹配、checkpoint 原子提交；native request 真机承载和卡片失败后非 status 重放尚欠 SDK e2e。
7. **展示、diff、preview**：private core 与旧 core 共用输出/reducer；`privatePublisher`/`private_preview*` 承接 block、最终结果、文件预览/重试。`processor_native_test.go` Codex/Claude final 模拟协议回放，真实 Codex 两轮验证纯文本最终消息；真实工具、diff、自动预览及外部文件内容完整性未验证。
8. **历史、目录、恢复**：history 走原 picker + native `thread.history.read` 结果，`/use` 入口在 `private_thread_policy.go` 过滤已归属 team/其他私聊的 session，launch 前 registry 再校验；`private_clean_restart_sdk_journey_test.go` 覆盖受控 native 的重启/原 session、队列、未发 prompt。真实 OAuth 进程跨 daemon 重启后续聊、真实旧个人历史迁移、混合全权限进程与 unknown 结果对账尚未在此次运行验证。
9. **图片/文件**：SDK 输入异步 staging 后绑定原消息；`private_sdk_staging_order_test.go` 覆盖 /new 与 /detach 期间迟到图片及 manifest 清理；`private_assembled_sendfile_journey_test.go` 覆盖文件选择与受控 SDK/文件上传，非实际外部飞书。
10. **明确排除项**：cron、VS Code 接管和 bendtomywill 按用户要求不恢复；不应把旧代码仍存在当成 private 漏接。


## 前轮实测记录及其证据边界

### 已落地的根因修正

1. 私人 reduce、native event 和 receipt 仍由原 core 和同一 Processor 事务管理；外部 I/O 不持状态锁。新增的是明确的原操作发布顺序，不是另一套私人管理 Agent。启动提示不能越过成功卡；hello/PID 回执两种先后顺序、取消后的迟到连接均按精确实例归属处理。
2. 同卡同步 callback 与异步 PATCH 共用写入顺序。通常仍同步替换原卡；已有在途写入时先提交点击，随后在原卡更新，避免旧 PATCH 把已返回的菜单覆盖。新 `/menu`、独立设置页不再复用旧未决卡的 flow。
3. 重启后的审批根据同一 wrapper 的真实 pending inventory、原事件序号/hash 和当前 response intent 核验，更新原卡后能继续执行；未决投递不二发。暂不完整清单限频重查；旧运行器缺能力明确给 `/detach` 出口。
4. 团队历史完整扫描改为显式历史/会话选择时读取，普通私人消息、终态和 idle 不扫描全部团队。历史不可用不会卡住当前对话，也不会错误开放未经验证的历史会话。
5. 未手选 Codex 模型时继承原生配置；用户既有模型和固定 API 设置优先。计划模式按真实 0.171 协议补必需字段，使用准确关联的原生响应模型；缺响应字段明确失败，不猜测或伪造成功。
6. schema 9 迁移同事务回填可证发布顺序，不改原 effect ID、内容、状态或收据。没有冻结操作/flow/display 证据的旧独立提示不能事后补造因果；它不成为无关新工作的全局屏障。

### 连续旅程实测登记

| 用户操作 | 执行方式与判定终点 | 当前证据 |
|---|---|---|
| 新建目录后的首次消息、连续对话 | 实际 SDK picker → ready 卡 → 立即输入 → 原生成功终态 → 对应平台答复确认 → 下一轮同 session | 真实 Codex 0.171；默认配置三轮含真实写读文件 `/tmp/private-native-default-final-real3.log`；原有显式模型三轮 `/tmp/private-existing-model-final-tools.log`；原生默认模型且先 `/plan on` 双轮 `/tmp/private-native-plan-final-real3.log` |
| 启动两种顺序与取消后迟到 hello | 真 wrapper、受控 native 和 SDK；先 hello 后 PID、先 PID 后 hello，各自 ready 后双轮；取消后新工作区不受旧 hello 影响 | `/tmp/private-plan-startup-fixture-race.log`；`private_new_workspace_order_sdk_test.go` 与 `private_new_workspace_late_hello_sdk_test.go` |
| 菜单与设置并发 | 同卡导航和等待期间返回；已在途旧更新之后最终仍是用户选中的页面；旧 unknown 卡不挡新 `/menu` | `/tmp/private-model-inflight-wait-check.log`、`/tmp/private-menu-flow-final.log`、`/tmp/private-menu-unknown-sdk-final.log` |
| 新卡已显示、收据尚未落库即点击 | SDK 刻意阻塞真实 gateway Apply 回执，立即点新 token；slash/同卡菜单、超时同事件重投、缺 chat_id；完成同卡终态后新开 `/model` 再选；旧 token/跨用户不借机生效 | `/tmp/private-model-pending-all-race6.log`；不是先等内部 CardRef 才点击。最终 eligibility 在既有 RecordPublished owner fence 内核验，原事实先持久捕获 |
| ready 后未发首条消息就关闭重启 | 实际形成 new_thread_ready、保存 cwd、旧 attached、空 selected/active/queue → 干净关闭并证明 retired → 同库重开 → 首条 hi final → 第二轮 final | `/tmp/private-new-ready-restart-race.log`；使用临时目录复制状态类型，不访问线上项目目录，不手工清 hold |
| 请求期间聊天、菜单、重启审批、下一轮 | SDK 普通操作；同 wrapper 保留，重开 daemon 同库，确认原请求仍 pending → 原卡新视图 → 点击 → native final → 下一轮；unknown 变体不二发 | `/tmp/private-request-sdk-final-race6.log`；`/tmp/private-request-retry-race.log` 覆盖 incomplete 与缺能力出口 |
| 已有会话恢复、排队及未开始输入 | 停止/重建 daemon → 精确原 session → 已证明未发输入恢复；不得重跑未知输入 | `/tmp/private-plan-daemon-fixture-race.log`；原 clean restart SDK 三条连续旅程 |
| 历史不可用时继续开发 | 关闭团队历史读取能力，SDK 私人消息仍到 native final 和下一轮；显式历史选择给明确错误 | `/tmp/private-history-native-final3.log`、`/tmp/private-plan-daemon-fixture-race.log` |
| 复杂媒体与错误后的下一条 | SDK → neutral/staging → wrapper/native：多文件、独立多图、post 图文及引用进入真实命令输入，连续 final；延迟图片下载失败后后续文字仍完成 | `/tmp/private-structured-uat-race2.log`；外部下载及 post/引用解析回调受控，真实 LiveGateway 解码另由 adapter/wrapper 分层用例覆盖，不能称真实飞书下载全链 |
| 线上旧库迁移 | 仅只读备份，临时副本调用生产 OpenJournal 和 Restore | `/tmp/private-schema9-live-audit.log`：8→9，47 张原有表 rowcount 不变，全部 checkpoint 可恢复，完整性及外键通过 |

测试中的平台网关受控，因此上表的“答复确认”指生产 SDK/发布协议在受控飞书端取得正确 MessageID 和持久 receipt，不冒充飞书客户端实际呈现。真实 Codex 测试隔离复制原生登录/config，并使用生产环境构造；没有向真实用户发送合成业务消息。当前未实测的真实飞书客户端视觉、所有 provider 的真实登录、真实模型中断与长期多项目运行，不用全仓测试总数代替。

初次全仓检查暴露了旧模拟 native 响应缺少真实协议 model 字段，已按真实响应补齐，保留原断言；该次运行终止，不能作为成功证据。最终冻结代码的 v5 全仓检查通过（`/tmp/private-final-uat-v5.jsonl`）：7096 个测试/子测试通过，13 个显式跳过，0 失败。真实后端 opt-in 在默认全仓中跳过，其实际执行使用上表独立日志。组件整包 race 通过 `/tmp/private-final-components-race3.log`，最终 callback 与首张卡 writer 的边界 race 通过 `/tmp/private-card-boundaries-final-race.log`；Windows 全仓编译通过 `/tmp/private-final-windows-build2.log`。最终 daemon、privatechat、ingressowner、orchestrator 整包 race 通过 `/tmp/private-final-daemon-race4.log`；提交前检查通过 `/tmp/private-final-precommit3.log`。本文件记录代码行为与验证证据，实际部署版本以安装事务、进程和二进制哈希为准。


最终回归期间曾出现模型卡“平台可见但新 token 未登记即点击”的真实漏操作，已以刻意阻塞 gateway 回执的 SDK 红测证明，不能用测试等待内部状态掩盖。实现通过先捕获、精确结算、本次 HTTP 回调内继续的方式修正；过期权限和投递未知仍按原证明规则处理。测试同时允许真实同步 HTTP 替换和异步同卡 PATCH，并按旧 UI 的模型设置终态验证后续重开入口，不要求旧终态卡新增表单。


### 部署后空闲性能复核

`d1df2523` 版本在六个飞书连接均在线、无原生执行的 30 秒采样中消耗单核 11.8%；10 秒真实 CPU profile 再测为 11.3%。采样 `/tmp/private-live-cpu.pprof` 显示主要热点为团队定时维护反复 `ListOperations` 和 JSON 解码，不是私人请求恢复。只读聚合确认历史操作正文约 3 MB，主要是旧 runtime.inspect 结果；不能用此前私人历史扫描优化冒充此处已解决。

后续修正把 goal policy、停止核对、项目准备的候选筛选下推到 SQL；载体启动和压缩使用已有生成列，避免 JSON 全历史扫描。使用现有 operations_pending 索引，不删除旧资料，不改调度频率。保留 retryable、原 scope/ID 顺序，以及 worker/host/代次/重建条件。测试用无关历史的大 payload、损坏但不属于候选的历史、候选种类/状态与查询计划验证过滤先于解码；合成耗时仅作开发证据，不代替再次部署后的真实 CPU 采样。


性能修正后的全仓回归通过 `/tmp/private-perf-final-all.log`；workstore/workruntime 全包 race 通过 `/tmp/team-operations-filter-final-race.log`，启动与压缩相关 workstore/workhost/workruntime race 通过 `/tmp/native-maintenance-index-race2.log`。本轮仅改变候选读取，不改变执行状态合同。


2026-11-25 部署复核：运行代码 `678f491a`，安装事务 `committed`，运行进程 SHA-256 与仓库本次构建一致（`8d9d160ae90ddc9d8815a074d16cb35a726e7df566afdb5ede31c55a27fbf38b`），六个飞书连接均为 connected，配置内容与修复前备份一致。相同 30 秒空闲窗口中，daemon 使用 0.53 CPU 秒，即单核 1.77%，相较前一部署的 11.8% 明显下降；该采样不代表执行模型任务时的 CPU 上限。证据 `/tmp/private-perf-live-ready.json`、`/tmp/private-perf-live-idle-cpu.json`。真实飞书业务消息未人工合成发送；前述 SDK 旅程的平台端仍是受控网关，真实 Codex 调用已单独实跑。


## 本轮整体验收

当前状态：下述连续旅程、最终全仓普通测试、相关组件整包 race、真实 Codex 探针和 Windows 构建均已通过；修复代码已部署，实际运行身份及现场恢复状态已核对。真实飞书客户端视觉效果不由受控平台测试替代。

### 根因和统一修正

1. 中断停机后旧 carrier 留在 draining，原目标仍在却被旧入口报为未接管。现在用精确物理退出证明完成已有退休意图，保留业务未知证据；下一输入或明确选择沿原目录和会话恢复，不预先创建闲置替代进程。
2. 选择器把旧 ready 快照当成本次成功。目录／已有会话／Git／Worktree 和异步完成统一按本次结果及可用实例判定；旧 snapshot 不再覆盖真实失败。
3. detach 已发 kill 的旧实例仍能被重选，迟到 stop 再清掉新绑定。停止中的候选投影从同一持久 outbox 恢复，显式 attach 与公共候选统一排除旧实例。
4. 平台菜单没有原始 chat ID，私人入口遗漏填入已经核实的目的地；现在只从 authenticated p2p binding 补足，保留原 SDK 事实和顶层新卡语义。
5. MCP 拒绝只显示错误，事件丢失顶层命令关联使控制队列卡在 sending。公共错误构造器保留原 CommandID，原生 RequestID 和帧证明结算该失败，失败后重试及普通对话均继续。
6. 一批事件中的 live delta 拒绝使后续持久 final 丢失。现在每条仍独立认证，live 拒绝不阻断之后事件；持久事件提交失败仍停止该批并保留重放次序。
7. 新会话首轮已落实到真实线程后，自动继续仍比较启动前的路由，立即清掉刚安排的续轮。现在只在同实例、同执行线程及 follow-execution 策略下提升为原线程续接，其他导航仍取消。
8. 宿主库 v1 reader 会丢弃新物理退出证据字段。事务迁移至 v2，既有载体 JSON、凭据 hash 与凭据文件保持原样；旧 reader 拒绝新版本，私人 journal 仍为 schema 9。

9. 停止 worker 取消了已经进入 publisher 收据投影的上下文，导致 core 已变而提交取消。现在调度停止与在途事务上下文在唯一 runner 边界分开，逐项检查停止信号；关闭超时保留存储，已开始收据结算完成后才排空。

10. 同一取消窗口还可在 SDK、关闭通知和 ownership lease 出现。统一 `beginProjection` 在锁和 authority 后判断取消，本地投影一旦开始即完成提交；未开始仍拒绝。registry 普通／scoped／维护 lease 获取可取消，callback 与 scoped session 绑定的栅栏持续至提交。对应竞争测试证明取消不会提前释放转移锁，SQLite 获取等待仍遵守调用期限。

### 连续旅程证据

本轮新增连续旅程的业务最终答复均匹配 `FinalSourceBody` 和原 `ReplyToMessageID`；统一 fixture 答复文字本身不作为下一轮已经完成的证据。数据库、receipt 和 runtime 状态只在用户序列结束后核对或用于失败诊断。

| 合同 | 新鲜执行的实际链及代码证据 | 结果与范围 |
|---|---|---|
| U01、U04 | `TestPrivateSDKUnboundHiExistingWorkspaceNewSessionJourney`：首条 hi 引导→已有目录/历史→新建→成功即发送→两个对应 final | 定向 race 通过；旧 source 不重放，新 thread 独立。 |
| U02 | `TestPrivateSDKUnattachedMediaRequiresResend`：未接管图片／富文本→引导→选择→新文本 final | 定向 race 通过；富文本 staging 使用受控 parser callback，不计为真实飞书客户端解析验证。 |
| U03、U18 | `TestPrivateSDKPickerNewWorkspaceRealCodex`：冷 SDK 入口、真实模型列表选择、保存反馈后立即两轮 | 真实 Codex 0.171 + 隔离 OAuth 账户配置；平台为受控 gateway。原生默认模型、Plan 对话和 full access 写／读文件分别执行。 |
| U05、U06 | `TestPrivateSDKUserGraphWorkspaceAToBAndBack`：A final→B final→A 原 session final；菜单旅程另验 `/use` 及旧 aliases→普通对话 | 定向 race 通过；事后核确切 cwd、session 与各源输入各一次。`/use` 是打开卡片，旧版不定义 `/use 任意ID` 文本参数。 |
| U07、U08 | `TestPrivateSDKTextClosesUnattachedPicker`；`TestPrivateSDKCancelledAuthenticatedHelloCannotReplaceNextWorkspace` | 定向 race 通过；旧按钮不可使用，已认证旧 hello 延迟到第二次 ready 后才放行，新输入有对应 final。 |
| U09、U11 | `TestPrivateSDKBusyNewThenStopThenNew`：真实 busy→new 拒绝→stop→THINKING 移除→new→final | 定向 race 通过；停止 ACK 不充当执行终态。 |
| U10 | `TestPrivateSDKDeclineApprovalThenNew`：原生审批→可见 decline 按钮→原生确认→new→final | 定向 race 通过；实际 native response 为 decline。 |
| U12–U14 | `TestPrivateSDKDelayedImageDiscardedAcrossRouteCommands`：下载延迟→new/detach/stop→迟到下载结束→连续两轮 final | 三支定向 race 通过；原图片不进入新目标。旧 cleanup 局部测试另保留 managed 删除、foreign 保留断言，不再要求重用已停止实例。 |
| U15、U16 | `TestPrivateSDKUserGraphSettingsThenBusyAndNextTurn`：未绑定设置→选择→首轮→忙态菜单/设置→原轮结束→下一轮 | 定向 race 通过；真实原生请求核 model、reasoning、full access 与 plan。不是所有 provider 配置的笛卡尔积。 |
| U17、U24 | `TestPrivateSDKUserGraphBackendsAndInvalidProfileRecovery`：Codex→Claude→OpenCode→坏 Profile 明确失败→修复→final→Codex final | 定向 race 通过；真实 wrapper、受控 native；默认 Profile 与 OpenCode 无效输入修正，不冒充三个外部商业后端的账户实测。 |
| U19 | `TestPrivateSDKCleanShutdownThenDirectMessageRecovery`：冷启动完成→正常关闭→同库新 App→两轮 | 定向 race 通过；事后核原 session 未改变。这里指 daemon 正常重启，不是模拟整台宿主机重启。 |
| U19 异常退出核验 | `TestPrivateSDKInterruptedShutdownThenDirectMessageRecovery`、`TestPrivateSDKInterruptedShutdownThenSameWorkspaceNewSession` | 定向 race 通过；实际原生检查挂起、停机期限到达、旧进程退出，再验证直接输入／先 list 两条不同入口。 |
| U20 | `TestPrivateSDKInterruptedUnknownTurnCanDetachAndStartNew`：原任务已接收无终态→退出中断→detach→新建→连续两个 final | 定向 race 通过；旧输入恰一次、旧队列在 detached 记录、旧 kill 已应用，不补造任务完成。 |
| U21、U22 | `TestPrivateSDKUserWorkflowHistoryGoalAndContinuation`：冷启动→native final/diff/fileChange→preview URL→history 详情→goal 读写清除→compact→review final/退出→sendfile→系统入口/受控 autostart→MCP 失败重试→普通 final | 定向 race 通过；SDK upload 实际调用并去重，preview provider 与平台为受控服务。打开 upgrade 页不等于执行安装。 |
| U23 | `TestPrivateSDKUserGraphMenuEntrancesAndContinuation`：首次 hi→平台菜单→五分组/返回/旧 token→选目录→各 slash 页面及六个会话 aliases→每次下一条对应 final | 定向 race 通过；导航不新增卡，不以本地 publication 已登记作为点击前提。客户端视觉仍需真实飞书验收。 |
| U25 | `TestPrivateSDKUserGraphAutomationAndNextMessage`：新会话失败→自动继续→AutoWhip→停止短语→关闭→下一条 | 定向 race 通过；原时钟运行，自动来源各一次，只有一次 thread/start 且所有后续 turn 在原 session；不同线程、实例和保留原选择分支有负例。 |
| U26 | `TestPrivateSDKColdQueueReactionStopJourney`：非队首图文重复点赞、另一文字鼓掌、撤回、steerall、stop、下一轮 | 定向 race 通过；精确两次 steer、一次 shell、图片原配对，撤回项不发送。 |

### 证据边界与最终检查

本轮表中的连续旅程替换平台网络和外部 native 服务，不替换私人 session／claim／队列的业务状态；接管、选择、启动、设置、停止均由 SDK 动作完成。原有 `private_assembled_*`、`TestPrivateSDKCleanRestartNewThreadReadyTwoTurns` 等包含人工装配的测试继续只算组件补充，不因名称含 SDK 被提升为完整用户链。真实飞书客户端的显示、外部文档服务及实际安装升级没有被本轮测试冒充验证。Git/Worktree 的工作区创建、取消、失败与当前操作结果由原有核心测试补充，未在这里声称运行过真实远程 Git 服务。

取消边界另有确定性证据：HostReceipt、SDK ApplyIngress、owner lifecycle 的真实 reducer 在变更 core 后阻塞，取消调用上下文再释放；checkpoint 与 applied／receipt 同 revision 提交，下一操作可继续，重放不再调用 reducer。预取消分支 core／DB 均不变；平台 publisher 写入后被阻塞，stopWorkers 会等待收据应用，未领取的 effect 不执行。原有 callback 认证／receipt 等待超时测试证明不产生迟到后台动作。

最终统一检查已有新鲜结果：

- `go test ./... -count=1` 通过，daemon 包 197.687 秒，日志 `/tmp/private-final-all-v3.log`。
- `GOOS=windows GOARCH=amd64 go build -buildvcs=false ./...` 通过，日志 `/tmp/private-final-windows-v3.log`。此工作区 Go VCS stamping 探测失败，显式关闭 stamping，不跳过代码构建。
- 最终真实 Codex 三组独立探针通过：模型列表／选择两轮 18.312 秒；原生默认模型加 full access 写文件、读取核对及第三轮 24.957 秒；Plan 两轮对话 22.465 秒。分别见 `/tmp/private-final-real-model-v3.log`、`/tmp/private-final-real-tools-v3.log`、`/tmp/private-final-real-plan-v3.log`。
- 写文件探针与 Plan 对话分开执行。一次把两种环境变量叠加的实验未得到预期的第三轮 `BOREAL`，不能把要求 Plan 模式执行改动的测试配置算成有效写入证明；测试现显式拒绝这种组合，并在失败时打印已看到的对应 final，避免将不同答复误报为根本无回复。
- `scripts/check/pre-commit.sh` 已执行通过；测试路径提示属于该脚本既有非阻断检查，未跳过 guardrail。

- 最终相关组件整包 race：`go test -race ./internal/app/daemon/... ./internal/app/privatechat ./internal/core/orchestrator ./internal/app/wrapper/... ./internal/adapter/codex ./internal/adapter/feishu -count=1` 通过，41 个有测试包，daemon 包 443.593 秒，日志 `/tmp/private-final-race-v3.log`。全仓普通测试共 122 个有测试包，无失败。

### 已部署的现场证据

2026-11-25 按 `safe-push` 和 `local-upgrade` 仓库流程发布代码提交 `d516d37eeb0e18e8c5d79f414715ec8e2fdb413b`，目标为已核实的单一 `codex-remote.service`／`stable` 安装。升级事务为 `committed`，安装版本 `local-e6a8639bbd24`。实际新 PID `2174308`；`/proc/2174308/exe` 与安装二进制的 SHA-256 同为 `e6a8639bbd2413449112d7cdb0576dd29f72c076f20fbc6e520d3e1f8d650046`，不是只核对构建文件。

- `/healthz` 返回 `ok`；运行时 authority 为 `separate_applications`，私人读取无错误，六个 gateway 均为 `connected`，团队运行时可用。
- 当前用户的原工作目标保留为 `new_thread_ready`、`DispatchMode=normal`，无 pending launch、无 recovery hold。导致本次失败的旧 PID `1130575` 对应载体已由新代码完成物理证明并进入 `retired`；没有手工改数据库或构造结束事件。
- 一条更早的旧 PID `310905` 载体缺少完整原生 inventory，继续隔离保留，不冒充已证明退休；它不占用当前工作目标，也不构成该目标的 recovery hold。
- 宿主库实际为 v2，私人 journal 保持 v9；配置文件 SHA 与升级前一致。升级前保留了 11 个数据库各自的 SQLite 一致快照及配置／安装状态，备份定位文件 `/tmp/private-continuity-predeploy-location.json`；不将这些独立数据库快照声称为跨库原子快照。
- 部署前原进程空闲 CPU 为单核 `1.902%`（144.05 秒）；新进程为 `1.928%`（52.92 秒）。测量来自实际服务 `/proc`，新进程日志未出现 poison、panic、fatal 或启动失败。
- 升级构建产生的两份 shim 嵌入产物已只恢复其生成改动；代码树、源提交和运行中的二进制分别核对，不把构建目录副作用混入业务提交。

现场证据文件为 `/tmp/private-continuity-runtime-status.json`、`/tmp/private-continuity-upgrade.log`、`/tmp/private-continuity-cpu-baseline.json`、`/tmp/private-continuity-cpu-deployed.json`。以上未发送伪造的真人业务消息到实际飞书会话；用户连续操作的验证来自前述真实 SDK 接口与受控平台／原生端，真实 Codex 探针另外执行。

## 2026-11-25 重启后首条消息的恢复说明

现场核对：14:16 重启后的首条 `hi` 仍在原生 session `01a21157-f99b-7ab2-9321-34fa5257ff4d` 执行，输入 seq 28 的派发恰一次。问题是后台恢复期间没有解释，却在就绪时显示旧的“开始执行这条排队消息”。旧版也有该排队文字；本次不将它从真正忙队列中删除。

当前合同：同一次私人自动载体恢复，在收到真人输入后回复一次“后台正在恢复，已收到这条消息，恢复完成后会自动执行。”；恢复后的队首直接执行。后续等待前一输入的消息保留原 reaction 和开始提示。原队列项记录等待原因，原 pending launch 记录说明去重，不建立第二套队列，也不要求用户手动恢复。闲置恢复不主动刷消息，团队输出规则不变。

说明与原消息的答复按来源关联排序，原生启动和派发不等待说明回执。实际平台操作尚未开始时，若输入已经结束、取消或被导航丢弃，则结算为本地 suppressed。最终核验与操作 begin 共用 Processor 锁，网络 I/O 在锁外；effect 已标 sending／unknown 但操作全部 prepared 的崩溃窗口也需重新核验。有实际 uncertain 操作的说明保留证据，不重发、不伪造未发送；其 unknown 状态不挡最终答复。

连续验证从未绑定 SDK 入口开始，先选择目录、新建并完成首轮，再真实关闭 App、保留原数据库重开。第二个 authenticated hello 被延迟，用户收到恢复说明后立即再发第二条消息，随后才放行 hello。验证两条对应原消息的 final、第一条没有队列开始提示、第二条保留正常排队提示，下一轮继续，最后才核对原生 session 和派发次数。另一条旅程在可见恢复说明后立即 `/detach`，再选择同目录新建并获得下一条对应 final，旧输入不能改投新会话。

发布交错测试覆盖 completed／discarded 与 effect prepared、sending、unknown 但操作未开始、unknown 且操作已开始的八种组合；另验 prepared／sending 的说明次序、unknown 不挡答复、checkpoint 恢复不重复说明及撤回后等待原因。独立复核曾发现 effect sending 到实际操作 begin 间的取消与崩溃窗口，最终实现将判断移到真实操作 begin 边界；普通正文、菜单及 reaction 不增加这个恢复说明专用锁。

`TestPrivateSDKRecoveryNoticeUnknownDoesNotBlockInput` 另从相同冷入口完成重启旅程，仅将恢复说明的真实平台 Apply 替换为一次结果未知失败。放行 authenticated hello 后，原消息及下一轮均得到 source-bound final，所有 turn 继续原 session；说明只尝试一次，未被记作成功可见，事后核对持久 op 保留 unknown 和实际错误。定向 race 通过，日志 `/tmp/private-recovery-unknown-sdk-race2.log`。

生产实现冻结后的全仓普通测试通过（122 个有测试包，daemon 207.333 秒），日志 `/tmp/private-recovery-notice-all.log`。恢复与发布交错、clean/interrupted restart、普通队列 reaction 和菜单后继续对话的定向 race 通过（daemon 94.963 秒），日志 `/tmp/private-recovery-notice-race.log`。Windows 全仓生产构建通过，日志 `/tmp/private-recovery-notice-windows.log`；最后新增的 unknown 旅程另有上述新鲜 race 证据，不声称它已包含在较早开始的全仓测试二进制中。真实飞书客户端仍不是该受控 gateway 测试的覆盖范围。

测试文件最终冻结后，延迟 hello 的恢复接续、恢复中取消后新建、恢复说明 unknown 三条 SDK 旅程再次统一执行 race 通过（21.442 秒），日志 `/tmp/private-recovery-notice-final-sdk-race.log`。提交前 `scripts/check/pre-commit.sh` 通过，包含文件长度 gate；原有跨平台测试路径扫描仍为脚本的非阻断提示，没有跳过检查。

修复提交 `ec9090fd9a6428ecef446c926712a2213c450c0d` 已经 `safe-push` 推送，并按 `local-upgrade` 发布到已核实的单一 `stable`／`codex-remote.service`。升级事务 `committed`，版本 `local-abf723324f26`，实际 PID `2281905`；`/proc/2281905/exe`、安装文件和升级 artifact 的 SHA-256 同为 `abf723324f2634f3b945865c603c695437a4c3b939acb49fa371a88458e07ed1`。健康检查 `ok`，六个 gateway connected、团队运行时可用、私人读取无错误；现场原 session、pinned 导航保留，dispatch normal，无 pending launch 和队列输入。配置 SHA 与升级前相同，未手动改业务数据库；升级前留存 13 个数据库各自的一致 SQLite 快照，定位 `/tmp/private-recovery-notice-predeploy-location.json`，不作为跨库原子快照。构建产生的两份 shim 嵌入文件已仅恢复其生成改动。

本次部署证据：`/tmp/private-recovery-notice-upgrade.log`、`/tmp/private-recovery-notice-deployed.json`、`/tmp/private-recovery-notice-deployed.log`。该进程启动日志未见 poison、panic、fatal 或失败。没有向实际飞书会话发送伪造的真人业务消息；新提示的来源关联和失败流程由上述 SDK 连续旅程证明。

## 2026-11-25 独立审阅协议与连续操作

现场 `review.start` 原生事件 seq 154 明确失败：`unknown variant detached, expected inline`，派发目标仍是原 parent。实际运行的是受管 Codex 0.171.0；PATH 上的另一版本不能作为现场协议依据。原 workflow peer 不检查 delivery、固定返回一个审阅线程，因此此前通过的受控流程没有证明原生审阅可用；本次修正原 peer 的协议形状，并新增严格独立 peer 与实际 Codex 探针，原 workflow 断言不放宽。

本次修复的合同与证据边界：

1. 产品独立审阅以专属 `thread/start(ephemeral=true)` 加 child 上的 `review/start(inline)` 实现。冻结 parent 的目录和有效配置，主线程选择保留；不能把非法枚举简单换成 parent 上 inline。来源仅由真实响应建立，native 默认 `source=vscode` 不掩盖派生的 review 归属，也不伪造 fork 历史。
2. 第一帧的 `review.child_created` 只表示已创建，不表示审阅 accepted。提前到达的正文等待准确证明，accepted 后先物化已核实的 review 来源。原命令两帧分别关联真实 request／frame hash，失败、unknown 与重复投递不重建线程或重发。
3. 真实 0.171 stdout 证实：`review/start` 返回 root A，另一个 `turn/started` 是 B 且 `rootTurnId=A`；正文和完成事件仍对应 A。保存实际 A／B，不按时间或同线程猜归属。ReviewSession、实例的逻辑执行占用和退出检查统一依据已核实 root；缺少 B terminal 不形成虚假忙碌，也不补造 B terminal。
4. accepted child 的 session 归属先在现有 registry 幂等提交，再在投影 fence 中复核。foreign owner 冲突在 core 变更前拒绝；receiver 登记不能代替后台重放校验。退休载体只登记有精确停机证明的 inactive 归属，不发新执行 lease。
5. 临时 child 没有可 resume 的 rollout；活跃、同 parent／surface 的追问直接 turn/start。实际 closed／deleted／archived／notLoaded 后保留身份、明确失败，不误走普通 resume。未知关闭不猜测。
6. 审阅应用和计划执行都使用 `control_action` 来源，以本人本次已验证 callback 的 event ID 授权，平台卡片只作回复锚。计划只有 execute／execute_new 可授权，cancel 和非法 option 不可授权。checkpoint、实际 dispatch 和 queued-only 恢复分别核对来源与 applied 状态，重复 callback 不生成第二次修改。
7. 私人 delivery v2 仅提高独立 review 的最低能力，原 v1 的答复请求、压缩和 thread.read 继续可用。聊天 journal 升到 schema 10，保留既有记录，旧 v9 reader 在打开时拒绝；宿主 carrier 数据库仍为独立 v2。

reader fence 已有实际旧代码证明：从修复前 `cb7a0a45` 导出完整源码，以其原 v9 `OpenJournal` 打开非空 schema 10 的 control-action checkpoint，明确返回 unsupported schema，原 checkpoint digest 不变。当前 reader 的 v9→v10 输入／checkpoint 原文与 digest 保留、control-action 重开及 future-version 拒绝也通过 race。日志 `/tmp/private-review-v9-reader-proof.log`、`/tmp/private-review-schema10-race.log`；临时旧源码定位 `/tmp/private-review-v9-reader-location.txt`，未对线上数据库运行旧 reader。

严格 SDK 旅程覆盖原始目录／新建、最终卡待提交审阅、初审后直接退出、追问后退出、提交审阅后应用、相同 callback 重投、旧卡再次点击；放行真实 response 前断言 early item 未 applied，放行后可见 final 即刻操作。分别对 thread/start、review/start 控制延迟并明确拒绝，用户能查看状态，失败后 parent 正常答复且可重试。peer 使用真实 A／B 形状，不通过额外追问碰巧清掉 B 占用来掩盖直接退出缺口。

真实模型探针使用隔离 git 仓库、临时认证副本、实际 Codex 0.171.0 和受控平台，所有最终回复按原消息关联，事后核 parent／child 及 registry；不会向实际飞书群或真实用户仓库发测试工作。失败记录保留 owner-only 原始 stdout、SQLite 一致快照和所编译源码清单，区分旧编译版本与修复后的新鲜结果。真实模型完成不等于正常停机完成，inspection／shutdown 必须单独通过，最终结果另行登记。

### 原生完整旅程的最终结果

- 同一固定测试二进制分别通过真实 direct（39.76 秒）与 follow-up（53.19 秒）两条链：初审后直接退出／初审后追问再退出，随后均在原 parent 得到对应输入的 final，真实 inspection／shutdown 完成、carrier retired。日志 `/tmp/private-real-review-native-direct.log`、`/tmp/private-real-review-native-followup.log`。
- 所用测试二进制 SHA-256 为 `e0ddaf8dc7ac5229b6e01250639ef25492849dcaab30dd7e2ab2915dde04f72b`，源码清单 `/tmp/private-review-direct-native-build-manifest.txt`。生产 Go 源码逐文件 hash 与清理后的代码完全一致（`/tmp/private-review-tested-production-manifest.txt` 对比 `/tmp/private-review-current-production-manifest.txt`）；之后只删除临时测试取证代码，不借旧模型运行声称新生产实现通过。
- 严格 peer 的 ordered／early／真实 root A-B 形状、两个 native RPC 的阻塞后明确失败与重试、原 workflow 的 focused race 均通过，日志 `/tmp/private-review-cleanup-focused-race.log`；清理后的 daemon 测试代码编译通过 `/tmp/private-review-cleanup-final-build.log`。
- 曾用于抓取 stdout 的 Python tee 改变了 native root 的进程树：真正 Codex 成为后台 descendant，StopChildren 会结束它，因而带 tee 的 shutdown 红测不构成生产关闭缺陷。已删除该取证脚本及入口，没有为它增加生产豁免。无 tee 的最终真实两条链证明正常关闭；此前采集的原始帧只用于协议事实分析。
- 真正的 ephemeral API 差异已修复并单独验证：临时 child 的 read(true)／Goal 请求不能套用普通持久线程的成功结果；关闭检查核 read(false) 真 idle 与精确 unsupported Goal 及本地两帧证明，不把任意 error 当空闲。

最终清理后的统一检查：`go test -json ./... -count=1` 通过，122 个有测试包、daemon 213.6 秒（`/tmp/private-review-final-all-v3.jsonl`）；Codex adapter、privatechat、executionowner、wrapper／hostinventory／runtimejournal 六包整包 race 通过（`/tmp/private-review-final-components-race-v3.log`）；Windows 全仓生产构建通过（`/tmp/private-review-final-windows-v3.log`）。提交前完整 pre-commit 含文件长度 gate 通过，测试路径提示为既有非阻断项。真实飞书客户端视觉效果仍不是受控平台与真实模型探针的覆盖范围。

core/control、orchestrator、nativeinspection 三包整包 race 也通过（`/tmp/private-review-final-core-race.log`）。审阅修复提交 `a1ab1439` 已按 safe-push 推送，并用 local-upgrade 部署到单一 stable／codex-remote.service：事务 committed，版本 `local-06b0938e7997`，实际 PID `2560238`。进程 `/proc/2560238/exe`、安装二进制与升级 artifact 的 SHA-256 均为 `06b0938e7997752cd5891ab5516558c4256122680237af11ea2d5ccbbb27a902`。

现场 healthz=ok、六 gateway connected、团队运行时可用，私人读取无错误，原 parent 的 pinned／normal 导航保留且无 active／queued／pending launch。实际聊天 journal 为 schema 10，宿主库仍为2，配置 SHA 与升级前相同；没有手改业务数据库或发送伪造真人消息。升级前保留14个数据库各自一致快照及配置，定位 `/tmp/private-review-predeploy-location.json`；它们不是跨库原子快照，旧v9二进制回退须配合对应数据快照，不能强行降低版本。现场证据 `/tmp/private-review-upgrade.log`、`/tmp/private-review-deployed-runtime.json`、`/tmp/private-review-deployed-daemon.log`，新进程日志无 poison、panic、fatal 或失败。构建产生的两份 shim 文件仅恢复生成改动。


## 2026-11-25 Codex 协议升级与子功能状态机复核

本节承接上一节已部署的独立审阅修复。审计比较实际受管 Codex 0.171.0 的 app-server schema、对应协议实现、改版前私人状态机及当前私人入口；不能以 PATH 中的 0.169.1 或历史测试中的简化消息代替协议事实。既有个人会话、原卡菜单、reaction、图文队列及群团队仍各沿自己的原有体验，协议修复不另建私人调度产品。

### 已确认并纠正的根因

1. 权限请求按数组读取／返回，丢失实际文件和网络权限范围。现冻结完整对象及 presence，明确同意只回传经校验的原范围，拒绝回空对象，未知范围只可拒绝。旧数组模拟不是历史 Codex 合同，已纠正测试。审阅子 turn 的 full-access 自动授权只认可持久 root proof。
2. 历史读取收到 JSON-RPC 错误时仍生成空成功。现精确失败并允许原卡重试，错误回执不再让控制等待永久挂起。
3. `isBlocking=false` 丢失，补充问题错误地阻止新输入；新会话首轮又因线程直到终态才落实，问题卡已经出现仍拒绝排队。现分开判断问题门禁和已证明的会话绑定，复用既有 bootstrap 提交；旗标只有实际 pinned route 成功才设置，CWD／claim 冲突不能伪装提交。非阻塞问题期间输入沿原队列，绝不提前开第二轮。
4. OAuth 通知按服务器／线程猜当前尝试，迟到旧完成可能结算新尝试，省略可选线程又无法完成。现用原生 loginId 精确关联，处理响应前通知、明确重试、失败和无 ID；专用状态段保证真实渲染。浏览器授权等待不阻塞普通对话。
5. 会话作用域只覆盖部分聊天命令，所选历史被其他工作接管后，压缩、Goal 和指定线程 OAuth 仍可能绕过发送核验。现统一按实际作用域检查，明确未发送仅结算自身；helper 历史拒绝也不再让 prepared 效果无限重试。
6. 只读查询预检与网络发送／结果应用之间有归属竞态。现使用按 dispatch 的有界读意图与应用时的 registry 栅栏；失去归属只结算本地观察，不发布其他工作内容，也不改写原始成功事实。Goal 内部 probe、用户查询、恢复读取和 OAuth 各自有精确负完成，不能丢掉原输入或把查询失败当作线程空闲证明。
7. 只读观察的超时处理原先有调度早退、退休后无法取得活跃 lease、误当归属变化和跨库丢失期限的风险。截止事实现与 dispatch 在私人 journal 同事务保存，registry 使用同值，观察收据沿原 checkpoint 提交；prepared 可证未发送与 unknown 结果未知分别处理。Goal 超时通过明确的新只读查询重试，原输入保留，旧迟到结果不能释放新等待；新认证连接仍保留旧版自动恢复观察，不把它误作业务重跑。
8. Goal 的自动恢复确认晚于本轮可见答复时，新输入进入 `resume_pending` 队列；成功 ACK 只清 lease，没有重新调度。现从准确确认的收口重新推进队列，仍需完整 pause／idle probe；通过受控早到／晚到 ACK 验证，不以手动清理状态作为前置条件。
9. 原生设备验证需要受信客户端生成证明，普通表单无法替代。当前明确不支持，只提供拒绝／取消，没有宣称新增设备验证能力。

### 按用户顺序的新增验证

下列新旅程使用真实 SDK hook、私人应用、relay 和 wrapper，原生端是严格校验 0.171 字段的受控协议 peer；它们能覆盖帧顺序、失败和回执竞态，不能冒充真实模型调用。

| 连续流程 | 永久回归 |
| --- | --- |
| 无预设 ready，新建首轮／已有会话提出非阻塞问题→继续发消息→回答原问题→原轮结束→排队消息答复→下一轮 | `private_nonblocking_input_sdk_test.go` |
| 阻塞问题→拒绝继续发送→停止真实原 turn→下一轮 | `private_blocking_input_sdk_test.go` |
| 完整权限对象→单次／会话同意或拒绝→原生关闭请求→本轮答复→下一轮；审阅子 turn 自动授权 | `private_permission_strict_sdk_test.go`、`private_review_strict_sdk_test.go` |
| 历史读取失败→同卡重试→下一条普通消息 | `private_user_workflow_sdk_test.go` |
| OAuth URL→继续聊天→精确完成；重试 A→B→旧 A 结果不完成 B；失败、缺 ID 和无可选线程 | `private_mcp_oauth_sdk_test.go`、`translator_mcp_oauth_attempt_test.go` |
| Goal 读空→设置→读取→清除→读空→下一轮 | `private_goal_strict_sdk_test.go` |
| Goal probe 超时→本人 retry→原输入 final→恢复 ACK 晚到时立即新输入→重新核查→新 final | `private_goal_probe_timeout_sdk_test.go` |
| 开计划模式→计划完成→本人明确点击执行→原生执行结果→普通下一轮 | `private_plan_strict_sdk_test.go` |
| 两个新会话→切回首个→压缩 resume 明确失败→重试压缩→完成→下一轮 | `private_compact_retry_sdk_test.go` |
| 只读超时→明确重试→旧回执迟到→原卡保持新结果→下一轮；app 重组、registry 意图缺失后沿原期限结算且不重发 | `private_read_expiry_sdk_test.go`、`private_read_crash_journey_test.go` |
| 选择历史会话→归属变化→压缩／Goal 明确未发送→选择其他工作区→下一轮；审阅 live→持久终态→下一轮 | `private_session_scope_journey_test.go`、`private_review_strict_sdk_test.go` |

新增组合 race 证据：`/tmp/private-state-journey-final-race.log`；权限、bootstrap 与 Goal ownership-lost 的独立 core/Feishu race：`/tmp/private-bootstrap-owner-goal-final-race.log`。shell／steer 与 0.171 的必填参数已核对，连续图文、队列、reaction、停止和后续答复沿既有 `private_queue_reaction_sdk_test.go` 纳入整体验证。

Goal 的相邻终态也纳入本轮：`private_goal_verification_sdk_test.go` 验证自动暂停返回 active／明确错误后没有自动写重试，用户读取、明确暂停、再次确认后原输入与下一轮完成；resume pending 时收到 complete，重新核查后继续并且不重新激活 Goal。`/tmp/private-goal-verification-sdk-race.log` 与 `/tmp/private-goal-prior-after-verification-race.log` 均通过。新的 verification 读超时与 ownership_lost 分开，完整 core race 证据为 `/tmp/private-goal-all-final-core-race2.log`。

### 数据兼容性独立验证

execution-owner registry 1→2：用部署前 HEAD `1ac41368` 的实际旧 writer 写入非空 carrier、会话、写预留、转移与停止证明，再由当前 reader 迁移；11 张既有表的行逐值保持不变。实际旧 reader 打开新库明确拒绝。证据 `/tmp/private-owner-schema2-migrate.log`、`/tmp/private-owner-v1-reader-proof.log`。

私人 journal 10→11：为新的“只读观察已到期”本地收据设置 reader fence；其结算不代表原生失败或未发送。以非空 schema 10 副本升级，48 张既有表的逐表行数及内容 hash 保持不变；部署前 HEAD 的真实 v10 reader 明确返回 `unsupported private journal schema 11 (maximum 10)`。证据 `/tmp/private-protocol-schema11-migrate-final.log`、`/tmp/private-protocol-v10-reader-proof-final.log`。以上均对隔离副本操作，未让旧 reader 打开线上数据库。


### 本轮冻结代码的整体验证

- `go test -json -buildvcs=false ./... -count=1`：122 个测试包通过，32 个无测试包，7324 个 test/subtest pass 事件，daemon 包耗时 290.931 秒；最终证据 `/tmp/private-protocol-final-all-v6.jsonl`。初次整仓发现旧附件恢复测试未提供新增读取投影 authority，随后仅将该 fixture 接入真实 registry；success／changed_route／damaged_receipt 的整条 race 通过，未给生产 fence 增加例外。之后权限提取的旧正向测试也改用真实对象，Codex adapter 整包普通及 race 再通过：`/tmp/codex-permission-fixture-full.log`、`/tmp/codex-permission-fixture-race.log`。
- 完整组件 race：Codex／Feishu adapter、control／nativeinspection 为 `/tmp/private-protocol-final-core-race.log`；最后 Goal 修改后的完整 orchestrator 为 `/tmp/private-goal-all-final-core-race2.log`；privatechat 与 executionowner 为 `/tmp/private-protocol-final-host-race.log` 中各自的成功结果；wrapper 为 `/tmp/private-protocol-final-wrapper-race.log`（118.920 秒）。daemon 自身的整包结果单独登记，不把其初次旧 fixture 失败或整包期限当成通过。
- daemon 整包 race 使用保留原断言的 20 分钟总预算：1500 个 test/subtest pass 事件，整包 637.851 秒通过，无失败或数据竞争报告，证据 `/tmp/private-protocol-final-daemon-race-v7.jsonl`。先前默认 10 分钟的整包期限及旧 fixture 失败均不计为通过。
- Windows 生产交叉构建：`GOOS=windows GOARCH=amd64 go build -buildvcs=false ./...` 通过，证据 `/tmp/private-protocol-final-windows-v3.log`；关闭 VCS stamping 仅处理当前受管 checkout 的构建环境，不改变产品行为。
- 实际受管 `codex-cli 0.171.0`，经真实 SDK 入口、生产 wrapper 和独立复制的账号配置：从冷工作区选择与原卡成功直接推进三轮对话（含实际写文件并读回），再修改仓库、点击独立审阅、追问、退出审阅、原主会话继续答复，以及真实 inspection／停止证明收尾，53.05 秒通过。网关为受控平台，模型／app-server 是实际受管二进制；未声称真实飞书客户端绘制验收。证据 `/tmp/private-protocol-real-native-v2.log`。
- 上述实际测试程序 SHA256 为 `1659b00760d554df7cdad25eb913cd98d05bf816a5a41d5b456a027f2c503fbe`。构建冻结的 1883 个生产 Go 文件逐一 SHA 对照当前代码，无新增／缺失／变化；清单 `/tmp/private-protocol-final-production-manifest-v2.json`。后续只有测试 fixture／证据文档变化，不借旧程序结果覆盖生产修改。


### 故障注入本身的并发约束

整包 race 复核修正了两类 fixture 问题，均未放宽生产判断：prepared 注入不再跨 SDK 入站持有全局调度锁，改为短锁交换饱和的新调度槽，已有任务使用原槽正常退出；过期／重启注入不再用第二个 SQLite writer 改写活跃 journal。后者曾出现有效收据 confirmed 但未 applied、原进程 poisoned 的快照；重建 Processor 可处理该收据，首次底层错误未被捕获，不能将可能的 SQLite snapshot 竞争表述为已证唯一原因。

当前所有期限通过流量前安装的 fixture-only trigger，在生产 PrepareEffectDispatch 的同一事务写入，且仅命中指定 kind 的首次 dispatch；后续重试沿原生产期限。crash 场景只在旧 app、relay 与交付回调关闭后模拟丢失 registry intent。没有新增生产 hook、缩短产品时限、重发 unknown 或跳过用户可见步骤。最新七项用户旅程 race 连续三次通过 `/tmp/private-intransaction-final-race3.log`；OAuth global／thread 两范围另有独立十次 race 旁证 `/tmp/private-oauth-isolated-independent-race.log`（首次 dispatch 限定前的等价单次登录 fixture）。最终整包结果另按当前源码登记。


### 精确图片来源与最终部署

完整 race 另修正了旧重启测试的假前置条件：已绑定的上一张图片仍保留在 StagedImages，不能凭集合非空认定 restart-image 已暂存，也不能取同 owner 最后一条 input 跳过更早来源。当前按 SourceMessageID 和 staged 状态等待精确图片，并按 frozen-before-shutdown 的原 eventID 选择输入、断言之前没有待处理输入；生产 FIFO 未放宽。原测试 race 十次出现两次失败，修后十次通过 `/tmp/frozen-prompt-exact-race.log`，随后上述完整普通与 race 整包均通过。

最终以干净且已推送的 checkout `c576853e94f9b2c3b0f35b2cf8ff5c5d45b46950`，通过仓库 upgrade-local helper 升级唯一的 stable／codex-remote.service。业务修复提交为 `2eb24795`，后续提交仅收严测试与记录证据。升级事务为 committed；实际新 PID **3040189**，安装版本 **local-1dd9fdc0fc51**。`/proc/3040189/exe`、install-state 指向的实际安装文件及固定升级产物三者 SHA256 一致：`1dd9fdc0fc51b239f6ae5b8c3cc6f2edcd1426c1283e5eca369cb55bac153535`。没有将 helper 的 prepared 提示当成已部署。

线上 healthz=ok，authority=separate_applications，六个 gateway 均 connected，团队运行时 available。47 个既有 surface 的 selected thread、route/input mode、工作区键、后端与配置引用与升级前一致，active／pending remote turn 均为空；没有伪造真人消息或手改业务数据。配置 SHA256 保持 `2e7fcef80d7a183dea7327a789aec58da3d1cedaa634625176a9e61d0fd47e7e`。实际数据库版本为 private-chat=11、private-carriers=2、execution-owners=2；旧 reader fence 验证见前文，回退必须配合对应备份，不能强制降低版本。

升级前保留 14 个数据库各自一致的备份及配置／install-state，定位 `/tmp/private-protocol-predeploy-location.json`，目录 `/tmp/private-protocol-predeploy-ffrktb6f`；不是跨数据库原子快照。现场证据为 `/tmp/private-protocol-upgrade.log`、`/tmp/private-protocol-deployed-runtime.json`、`/tmp/private-protocol-deployment-proof.json`、`/tmp/private-protocol-deployed-daemon.log`。新 PID 日志确认 ready，未见 poison／panic／fatal／failed；构建生成的两份 shim 文件仅恢复生成改动，不带入源码提交。

### 2026-11-25 现场发现：历史发布积压饿死新回复

用户在 21:18:40 发送 `hi` 后无反馈。现场 PID 3040189 仍运行，healthz=ok，六个 gateway connected；原生后台在 21:18:51 已生成“嗨！”并完成该轮，私人 checkpoint 也已保存，但恢复说明、reaction 与最终答复都仍为 prepared，没有平台发送收据。截图中最近一条关闭提示的实际发送时间是 19:25:54，属于升级前的历史消息，不代表服务此刻仍在关闭。

实际候选列表保留了旧 unknown 菜单和旧 unknown 过程卡，后续过程卡仍须等待精确前序收据。它们没有与本次输入共享因果屏障；在隔离 journal 副本中逐项调用原 publisher，新产生的七项发布都可以成功，实际 neutral owner／Bot generation／迁移绑定也已独立核验。问题来自有限并发的领取顺序：每轮从最早候选开始，旧效果反复抢占八个发布名额，新效果到达时名额已满，于是一直没有尝试机会。未知状态保留本身是正确行为；把扫描顺序当成公平调度才是根因。

本次补充验收必须包含超过执行槽及跨分页的历史积压，验证新的独立回复、reaction 和菜单仍能获得执行机会，同时旧 unknown 不重发，同卡／同 turn 的因果顺序和原输入 FIFO 不变。现场原始日志、状态、只读栈和隔离诊断分别保存于 `/tmp/private-no-reply-since-start.log`、`/tmp/private-no-reply-runtime.json`、`/tmp/private-no-reply-goroutines.txt`、`/tmp/private-no-reply-snapshot-diagnostic.log`。未手改线上数据库或重新发送真人输入。


本轮修复并非仅调整领取顺序。私人普通、停止／取消和发布三个执行池共用公平调度；按 owner／epoch 先确定首条可执行输入再轮转，跨页及跨 owner 的 revision／ordinal 相同情况用 effect ID 排序，不扩大并发。`PendingHostEffects*` 保留核查账本，普通 worker 改用 `RunnableHostEffectsAfter`；未知平台操作、依赖未满足的未渲染发布、缺少冻结目标的旧命令与显式 detached 载体上未派发命令，均不因保留证据而反复领取。只读观察超时及确认收据的应用仍可推进。共享 SQL 谓词同时用于实际授权／因果检查，避免查询和执行各维护一套规则。

独立复核再补齐三处相关边界：原始异常不再静默丢弃，按阶段／效果记录原原因并去重限流；已冻结发布在实际平台调用前重验 owner／epoch／原始视图并捕获精确 AppID 的 gateway，配置替换不能改变目的 Bot，明确尚未开始的失败仍保持 prepared；暂存清理用稳定主键遍历回执与取消来源，旧授权不可确认的记录不再挡住后续合法文件清理。没有增加第二份队列、修改 Codex 协议或数据库格式，也没有删除旧未知结果冒充已处理。

查询执行计划发现同一用户历史效果曾先于精确依赖键被扫描。共享依赖查询改为先取 current effect／chain key，再查 prior 及其实际 effect；只读现场五次测量候选均为相同三项，中位时间 24.4ms→2.3ms，证据 `/tmp/private-publication-candidate-query-plan.log`。该时间不是平台回复延迟承诺，也不把 race 插桩下的慢查询说成服务死锁。

已获得的定向证据：

- 旧发布调度在 140 条长期失败记录、18 轮完整 pass 后仍未发送新回复；旧源码 overlay 的完整 SDK 流程也在 14 条未知旧发送后，出现原生已接收 `hi` 但最终答复未呈现，证据 `/tmp/private-publication-backlog-sdk-old-scheduler.log`。早先按错误标题注入的 fixture 未创建积压，不计为产品失败证明。
- 修后 SDK 路径验证首条 `hi` 的实际正文及同一 THINKING reaction 的开始／移除，随后 `/menu`、不等待内部收据就点击原卡、下一条 `hi` 均成功；旧 14 次平台未知调用未重复，两个真人输入各只发往原生一次。最终完整积压、冻结发布授权和诊断 race 连续三次通过 `/tmp/private-publication-final-race.log`。
- 三个执行池的饥饿、首条合法输入顺序、跨页、同卡／同 turn 因果、确定退出的旧命令休眠及关停收据 race 通过 `/tmp/private-publication-keyfirst-race.log`；140 条积压调度在 race 下 3.57 秒完成，daemon 定向组合 10.602 秒，privatechat 2.098 秒。
- 已冻结视图的 AppID 替换、owner epoch 改变、无按钮卡片发行记录及原应用恢复的测试，通过 `/tmp/private-frozen-publication-verified.log` 和上述最终 race。凭据未在测试日志中输出。
- 129 条旧授权不可确认的暂存清理记录曾让新回执与取消来源的文件均无法清理；修后两者真实文件删除并各自结算 done，旧文件与待核证据保留。以旧源码 overlay 复现，定向 race 包括既有图片取消和附件恢复链路。

### 后续机制建议：发现被遗忘的异步工作（尚未作为通用机制实现）

公平调度和可执行候选筛选是本轮修复；它们不能替代所有异步操作的完整结束合同。后续应在既有 ingress／effect／dispatch／receipt 身份上补齐统一的操作生命周期，不建立另一份工作队列：

1. 每个异步开始点必须同时登记负责处理者、所属业务及代次、原始来源、完成／失败／取消条件、最长等待条件和可否重试。外部发送结果未知与尚未发送分开，不能由 TTL 猜测副作用是否发生。
2. 会话退出、操作被替代或来源取消时，沿明确依赖边撤销尚未开始的后续；已开始的步骤保存结果核对义务。取消后的迟到回调只补历史，不复活已撤销工作。
3. 常规路径自己原子提交推进或终态；另用低频对账检查“有待处理记录但没有处理者”“依赖已终结却仍等待”“超过期限无进展”。能凭证据修复的自动结算；证据不足则退出自动执行集合，进入带原因和核查入口的待核区，不形成整个用户的新门禁。
4. 监控分别展示尚未处理输入、会话排队、后端执行与飞书交付的进度和最老等待原因；healthz 只证明进程存活，不能替代这些结果。
5. 已终结且没有活动引用的记录按保留策略归档；待核记录与历史文件有独立索引和存储预算。归档或删除须保留恢复及去重所需的证据关系；当前没有统一物理归档策略，本轮未用删除历史来代替结束操作。


### 本轮最终验证与现场交付

最终源码提交 `cb232c3f`，在所有生产改动冻结后执行 `go test -json -buildvcs=false ./... -count=1`：122 个测试包通过，32 个无测试包，7336 个 test/subtest pass 事件，daemon 290.057 秒，无失败；证据 `/tmp/private-lifecycle-final-all.jsonl`。Windows 交叉构建通过 `/tmp/private-lifecycle-final-windows.log`。真实受管 Codex 0.171.0 的冷工作区选择、三轮对话含实际文件读写、独立审阅／追问／退出及返回主会话流程再通过，51.88 秒，证据 `/tmp/private-lifecycle-final-native.log`。平台网关仍为受控测试入口，不冒称真实客户端视觉验收。暂存清理定向 race 的工具输出中 privatechat 1.483 秒、daemon 3.467 秒通过；新增两个用例也包含在上述最终整仓日志中。

通过唯一 stable 服务的仓库升级 helper 部署，事务 committed，实际新 PID **3193359**，版本 **local-e5b67ccd49ba**。进程实际执行文件、安装文件及固定升级产物 SHA256 都为 `e5b67ccd49ba9c7efa4fb67f75047ed116f0e416e3ee4b6d43c1e4a34891d9c2`。healthz=ok，六个 gateway connected，active／pending remote turn 均为空；配置 SHA256 仍为 `2e7fcef80d7a183dea7327a789aec58da3d1cedaa634625176a9e61d0fd47e7e`。升级前 15 个数据库各自一致的备份及配置保存在 `/tmp/private-publication-predeploy-_5tx7byu`，不是跨库原子快照。证据 `/tmp/private-lifecycle-backup.log`、`/tmp/private-lifecycle-upgrade.log`、`/tmp/private-lifecycle-deployment-proof.json`。

用户 21:18:40 的原始 `hi` 已生成的“嗨！”最终答复，在新加坡时间 **21:48:27.485** 由飞书返回真实消息 ID `om_x100b7f5a3c7d6ca0b363b52a96bf652`；对应 publication op 与 effect 均 confirmed，effect 回执已 applied。由原 MessageID 关联的原生 dispatch 只有一次。没有重发真人输入、重跑 Codex 或手改业务数据库。

调用当前版本真正的 journal 查询进行只读现场检查：未应用历史清单 16 项，可执行候选 1 项，其中可执行发布 **0** 项；原来的两项未知发布与受其阻挡的历史记录仍保留，已不反复占用发送池。剩余候选是历史停止核对，不表示旧运行已经有完整退出证明，不能借本次发布修复宣布它成功或删去证据。证据 `/tmp/private-lifecycle-live-candidates.log`。新进程日志 `/tmp/private-lifecycle-deployed-journal.log` 保留启动初期精确 gateway 尚未安装时的未开始重试，以及该历史停止项的 `another stop is in progress`；未把这些记录说成无错误启动。发布已随后获得真实平台收据，诊断不反复刷同一错误。构建生成的两份 shim 文件只恢复生成差异，未带入源码提交。
