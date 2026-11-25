# 单聊旧版行为对照与完整流程验收

> Type: `inprogress`
> Updated: `2026-11-25`
> Summary: 按团队功能加入前的版本重建单聊使用场景、入口调用图和逐项验收，明确尚未验证的流程。

本次以 `edffb35f` 为旧版基线，初始审查对象为 `42159c64`，后续在同一分支统一修复。初始缺口和整改后的实测结果分别保存；当前结论见末尾“本轮整体验收”。保留用户已明确取消 Cron、VS Code 接管和历史改写的决定；其余单聊差异均需逐项证明，不能自行称作产品取舍。单聊与群团队继续独立。

## 验收如何判定

用户在飞书看到什么，就以该时点作为下一步输入的依据。卡片说“已准备好”后立即发 `hi`，必须真正执行并得到答复；测试不能先等待内部 online、手工附着实例或人为清除恢复状态，再假定用户流程已通过。首轮结束还要完成第二轮，核对会话上下文、消息顺序、展示和错误出口。

每项记录前置状态、入口和完整操作序列、旧版预期、实际平台输出、原生行为、异常分支和验证证据。验证状态分为“已执行通过”“已执行失败”“仅有局部测试”“未执行”；静态代码审查不能写成已通过。受控飞书端可验证生产 SDK 与发布协议，不能代替客户端视觉验收。真实原生后端、受控原生后端和真实平台分别记录，不相互替代。

初始审计结论：当时尚未通过。下文初始盘点里的测试名称只代表覆盖线索，不是整改后的通过声明；整改后的新鲜执行证据见末尾。

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
| `/use` `/useall` | 有已验证历史→列表或显式 ID 选会话→确认→下一条确实发原 session；不可见/其他 owner/未静止旧执行给明确原因 | 核心历史隔离与 alias 另有 focused；缺 SDK 输入→同一 native session→最终平台答复，及 `/useall` 多项选择完整回执。 |
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
8. **历史、目录、恢复**：history 走原 picker + native `thread.history.read` 结果，`/use` 在 `private_thread_policy.go` 过滤已归属 team/其他私聊的 session，launch 前 registry 再校验；`private_clean_restart_sdk_journey_test.go` 覆盖受控 native 的重启/原 session、队列、未发 prompt。真实 OAuth 进程跨 daemon 重启后续聊、真实旧个人历史迁移、混合全权限进程与 unknown 结果对账尚未在此次运行验证。
9. **图片/文件**：SDK 输入异步 staging 后绑定原消息；`private_sdk_staging_order_test.go` 覆盖 /new 与 /detach 期间迟到图片及 manifest 清理；`private_assembled_sendfile_journey_test.go` 覆盖文件选择与受控 SDK/文件上传，非实际外部飞书。
10. **明确排除项**：cron、VS Code 接管和 bendtomywill 按用户要求不恢复；不应把旧代码仍存在当成 private 漏接。


## 本轮整体验收

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
