# Agent 统一运行层：代码复用、迁移与操作归属审计

> Type: `draft`
> Updated: `2026-11-21`
> Summary: 依据现有代码建议同仓库重构统一运行层；单聊获得记忆，旧群体验替换，并区分人、Agent 与运行层的操作职责。

## 1. 决策与范围

建议在现有仓库演进，抽出统一运行内核与新的工作领域，不另开空白代码库，也不长期维护“旧单聊内核＋新团队内核”。已有协议、运行宿主、飞书接入、模型配置、Git 和部署能力值得复用；surface 独占执行、旧群单目录模型和人工管理原生会话的主流程需要替换。

本次用户明确的优先级：

1. 最高优先级是 Raft 式群工作体验：固定同事、负责人协调、同事直接协作、并行执行、评审、交付与持续记忆。
2. 单聊迁入同一底层并获得记忆；保留对话、工作的便利与连续性，不要求旧菜单逐项原样保留。
3. 原群聊体验可以整体丢弃，不维持旧 primary/workspace/coworkers 操作流程。
4. Worktree 等执行细节由 Agent 与运行层操作，用户无需理解或管理。
5. 模型、已有工作目录等精确选择保留控件；用户明确给出的选择直接采用，Agent 不自行猜选已有资源。

本文是代码审计与目标设计，不是生产变更。依据为当前源码、既有测试和本次定向验证，不以文件数量估算“复用百分比”。完整团队行为见 [产品草案](./feishu-agent-team-product-design.md)、[技术方案](./feishu-agent-team-technical-design.md) 和 [执行设计](./feishu-agent-team-execution-design.md)。

生产代码核查基线为 `a84bbadaeb0761952c1ef22d7db84d8b386a9dbe`；本轮只更新设计文档。

## 2. 为什么同仓库更合适

| 路线 | 真实成本与结果 |
| --- | --- |
| 同仓库、统一运行层 | 可以直接调用现有内部包，沿用安装、配置、连接与测试；需要明确重构执行归属，单聊与团队最终共同受益 |
| 新仓库复制现有代码 | surface 耦合也会被复制；之后双边同步 backend 修复、配置和发布，不能靠换目录消除架构问题 |
| 新仓库从零做 | 重做进程、协议、飞书、配置、升级与兼容；没有发现现有技术栈阻碍完整目标的证据 |
| 新仓库引用现有“SDK” | 当前没有可直接导入的独立 SDK：关键代码位于 Go `internal` 包，wrapper 的运行接口还接收包内 `*App`；需先完成与原地演进相似的抽取 |

`internal/app/wrapper/backend_runtime.go` 的接口虽已有清晰职责，但不是现成的跨仓公共 API。同仓库可先在正确边界实现完整产品；模块边界和测试独立不要求物理拆仓或拆进程。

## 3. 可复用资产与实际缺口

“复用”指代码职责与契约可以保留，不代表原来的 UI 流程、状态主键和失败处理可原样套用。

| 资产 | 已核实代码 | 复用方式与所需变化 |
| --- | --- | --- |
| 原生 Agent 接入 | [backendRuntime](../../internal/app/wrapper/backend_runtime.go:59)、[Codex 命令](../../internal/adapter/codex/translator_commands.go)、[Claude 命令](../../internal/adapter/claude/commands.go) | 保留启动／命令／事件翻译；补角色指令与维护生命周期，不重写模型调用循环 |
| 子进程与协议关联 | [child session](../../internal/app/wrapper/app_child_session.go:120)、[IO](../../internal/app/wrapper/app_io.go:114)、[应答 gate](../../internal/app/wrapper/command_phase_executor.go:20) | 保留 initialize、请求 ID、steer 拒绝、重启及 generation 检查；加入统一 session/attempt 归属 |
| 统一协议 | [Command/Event](../../internal/core/agentproto/types.go)、[DispatchPlan](../../internal/core/agentproto/prompt_dispatch.go:20) | command/thread/turn/CWD 可复用；Origin/Initiator 增补通用执行身份，不能把后台工作伪装为 helper 或人类 surface |
| 模型与 provider 配置 | [Codex resolver](../../internal/app/codexprofile/runtime_resolver.go:355)、[Bot 配置用例](../../internal/core/orchestrator/service_bot_capability_settings.go) | 保留凭据、模型目录、profile 和 Bot 单写源；抽成 UI/Agent 共用业务用例，增加 desired/applied revision |
| 多 Bot 与飞书投递 | [controller gateway](../../internal/adapter/feishu/controller_gateway.go)、[SDK 调用](../../internal/adapter/feishu/gateway_im_calls.go) | 保留连接、按 Bot 发消息、回复、附件、卡片更新；输入与输出目标由 scope/task/attempt 明确提供 |
| 身份与卡片校验 | [入口身份](../../internal/adapter/feishu/gateway/routing.go:709)、[回调校验](../../internal/adapter/feishu/gateway_runtime_support.go:95)、[request 校验](../../internal/core/orchestrator/service_request.go:75) | 保留操作者、gateway、chat、revision 的校验；冻结 owner 从 surface 上移到 session/run，surface 只负责交互 |
| 展示组件 | [projector](../../internal/adapter/feishu/projector.go)、[renderer](../../internal/core/renderer/planner.go) | 复用文本、卡片、文件／差异展示；新群业务投影重写，不保留人工管理底层运行的菜单逻辑 |
| 本地 Git worktree | [CreateWorktree](../../internal/app/gitworkspace/worktree.go:36)、[preview](../../internal/core/gitmeta) | 已有创建和错误分类；当前固定从 HEAD 建分支，需明确任务基线、operation、租约和可恢复生命周期 |
| 后台 Git 隔离 | [cronrepo.Materialize](../../internal/app/cronrepo/manager.go:40) | 已有镜像、来源锁、ref→SHA、detached worktree；可抽取这些构件，不能继承强制删除运行目录／按 TTL 清理未交付成果的策略 |
| 时钟与后台运行 | [Cron scheduler](../../internal/app/daemon/app_cron_scheduler.go)、[Cron launch](../../internal/app/daemon/app_cron_launch.go) | 复用触发与宿主能力；当前满额可 skipped，不能用作不能丢的协作收件箱；到期事件改写持久输入 |
| 进程与安装升级 | [runtime](../../internal/runtime)、[local upgrade](../../internal/app/install/local_upgrade.go:37) | 保留平台、安装、版本和恢复基础；调度升级/回收前必须考虑统一运行层的活动执行及持久待办 |
| 配置存储与组合根 | [statestore](../../internal/app/daemon/statestore/statestore.go)、[App wiring](../../internal/app/daemon/app.go:195) | 保留配置 atomic save 和 daemon wiring；任务/记忆/待办需要事务库，不能把单文件保存当跨对象事务 |

已追踪的实际路径：wrapper 接收命令 → Codex `thread/start` → 按 request ID 取新 thread 并继续 `turn/start` → stdout 转统一事件 → daemon `onEvents` → orchestrator。`turn/steer` 的 expectedTurnId 拒绝也有真实回执处理。这些是可复用执行链，不只是参数结构。

Backend 能力仍有具体实现缺口：Codex 已有 compact 命令链；Claude 当前命令 switch 不具备同样的 compact handler；OpenCode 默认能力不提供 TurnSteer。完整运行层必须补齐适配，或在不支持即时操作的边界持久排队／受控重建，不能把声明字段存在视为功能已完成。

现有 Bot record 还保存 `ProductMode`（headless/vscode）。新内核须把“使用哪个 backend/model”的角色策略与“这项工作由后台还是本地界面控制”分开：前者继续 Bot 级统一，后者属于 WorkerSession/controller。私聊接管本地编辑器不能把同 Bot 的所有项目群一起切换执行方式；旧 `/mode` 的混合含义要拆解，而不是照搬全局字段。

## 4. 必须重构的四个耦合点

### 4.1 Surface 同时充当界面与执行 owner

[SurfaceConsoleRecord](../../internal/core/state/types.go:313) 同时保存当前实例／thread、排队、PendingHeadless、审批、ReviewSession 和展示状态；[remoteTurnBinding](../../internal/core/orchestrator/service.go:99) 也依赖 SurfaceSessionID。

终点必须统一为 WorkScope（personal/project）→ WorkContext → WorkerSession → ExecutionAttempt。SurfaceBinding 只管理用户入口、查看对象和输入目标；单聊也使用该内核，不保留另一套 surface-owned 执行真相。普通对话可以没有 Task，但仍有 context/session/attempt 和记忆。

迁移期可以识别 legacy surface owner；完成迁移后撤掉其执行权威。旧 `/use` 的“打开另一段工作”能力可以保留为上下文导航，不能靠改 SelectedThreadID 隐式抢占原生执行。

### 4.2 队列、claim 与并发是旧界面模型

[入队](../../internal/core/orchestrator/service_queue.go:81) 会先检查 room 限制；旧 QueueItem 完成包含用户中断。新运行层先持久保存输入／任务，再准入执行；attempt 的 completed/interrupted/failed 与 Task 的交付状态分开。

[routing claims](../../internal/core/orchestrator/service_routing_claims.go) 以 room/surface 持有 workspace，以 surface 持有 instance/thread；selected thread 不匹配可能清理 claim。新 claim 统一以实际执行 session、原生资源和目录租约为依据，单聊与团队共用仲裁，不能各建一套锁后互相抢资源。

当前 [turn runtime](../../internal/core/orchestrator/service_runtime_clusters.go:323) 按 instanceID 保存单个 pending/active turn。完整并行可由多个执行 instance 承载；不能仅增加几条 WorkerSession 就在当前单值结构里同时运行多个 turn。若改为同 carrier 多 turn，必须整体改变关联合同，不能默默放宽 busy 检查。

### 4.3 启动与回收依赖是否有人附着

[headless 启动](../../internal/app/daemon/app_headless.go:325) 检查 surface.PendingHeadless，[pool](../../internal/app/daemon/app_headless_pool.go:14) 依赖 attached surface/active turn 判断忙碌，[detach](../../internal/core/orchestrator/service_detach_headless.go) 可在最后 surface 离开时释放实例。

新运行层用持久 launch operation、controller lease、session pin 和进程证据管理。离开菜单／切换查看对象不等于取消工作；明确停止或人工接管才改变执行资格。单聊长任务也应因此获得可靠后台连续性。

### 4.4 事件、恢复与投影混在同一处理链

[daemon.onEvents](../../internal/app/daemon/app_ingress.go:681) 将事件交给 ApplyAgentEvent 后生成 surface UI、同步恢复记录；现有 [surface resume](../../internal/app/daemon/surfaceresume/state.go:20) 保存路由，不保存完整工作待办。

新链应先按协议 ID 定位 session/attempt 并归并运行事实，再处理业务状态与投影。团队事件不能标为 internal helper，否则会被现有过滤丢弃。恢复依据是持久输入、检查点和外部证据，不依赖人再发消息。

现有 [ingress pump](../../internal/app/daemon/ingress.go:56) 有 per-instance FIFO 和轮转，值得保留；其内存队列不等于 durable inbox。新存储及 Git/网络副作用不能在 App 全局锁内长时间执行，避免团队工作拖慢单聊。

## 5. 人、Agent 与运行层的职责

最高目标仍是“向同事提出工作，团队自行推进”。自动化不是让 Agent 模拟点击旧卡片，而是通过明确业务能力完成工作。保留展示与选择组件，移除不再有产品意义的操作流程。

| 现有入口／能力 | 人应做什么 | Agent／运行层应做什么 | 代码取舍 |
| --- | --- | --- | --- |
| 选择模型／provider | 下拉或明确指令选择，知道影响哪个 Bot | 读取配置、在安全边界应用，解释待生效状态 | 保留 catalog、选择组件和配置；抽统一用例 |
| 选择已有目录／仓库 | 明确指定或用目录选择器选真实资源 | 校验与登记，不猜选其他目录；之后管理内部工作副本 | 保留路径浏览／校验；分离资源选择与执行目录 |
| 新建 worktree／分支／派生目录 | 表达要做的任务，不填写内部路径 | Agent 申请隔离工作环境，运行层创建、绑定、恢复、清理 | 复用 Git；替换 target-picker/确认卡/内存取消态为持久操作 |
| `/list`、`/use`、`/attach`、`/follow` | 打开某项工作、切换项目、接管执行 | 自动绑定正确 session 和后台进程 | 保留工作导航与接管能力，原生 instance/thread 列表转诊断 |
| `/new` | 表达新事项或重新开始，必要时选择是否沿用资料 | 判断 context/session 的复用或新建 | 保留原生创建能力；不能把新模型 thread 当新项目或删记忆 |
| `/compact`、会话重建 | 不必管理上下文窗口 | 运行层整理检查点、compact／恢复／重建并继续投递 | 保留已有命令能力，替换“先选线程再清空队列点卡”的主流程 |
| `/review` 与 commit 选择 | 提出检查重点，阅读结论、验收 | 依据成果版本自动安排角色审查，返工与复查 | 复用差异／审阅构件；替换每次人挑 commit 的例行流程 |
| `/autocontinue` | 控制是否暂停／取消工作 | 按明确剩余工作和错误原因恢复，有退避和边界 | 复用可用恢复策略，状态归 attempt，不要求人逐群打开开关 |
| `/autowhip` | 不需要反复催或配置停止口令 | 用任务、依赖、成果和阻塞决定继续／结束 | 旧提示循环不作为新内核，停止口令完成判定退役 |
| `/cron`、提醒 | 表达时间、内容、范围，查看／修改／暂停计划；精确时间可选控件 | Agent 整理计划，运行层持久触发到任务／inbox | 复用时钟和记录，替换满额丢周期作为协作语义 |
| `/sendfile`、成果发布 | 指定需要什么或打开成果 | Agent 主动提交正确版本，系统按目标投递 | 复用附件／预览能力，减少找文件再手动发送的主流程 |
| `/primary`、`/coworkers`、旧群 workspace | 选择负责人／团队及明确资源预算 | 项目成员、任务调度和工作区租约管理 | 旧群命令与交互可移除，不保持兼容流程 |
| 停止、暂停、取消、验收 | 随时直接控制工作与最终取舍 | 对正确任务执行控制、展示实际状态 | 保留简短可靠入口，不藏到 Agent 内部 |
| 管理、凭据、执行授权、升级 | 管理连接和政策，处理确实需人的授权 | 系统执行确定的配置／升级事务，遵守既有规则 | 保留管理能力；不让 Agent 代替人自我批准权限 |

人选“使用这个已有目录”与 Agent 创建其内部 worktree 是两个不同决策。界面默认显示任务、成员、进度、成果、阻塞和当前选择；不展示 branch/worktree/session/instance 参数来要求用户代为编排。用户主动调试时仍可查看必要技术信息。

## 6. 单聊的推荐体验与记忆边界

单聊是找同一位同事单独工作、讨论和指导。默认个人范围，直接对话、交付文件、继续过去工作；长期偏好和未完成事项随角色与个人工作空间恢复。无需为了聊天伪造一个飞书项目群，也不必每条消息建一张 Task。

需要项目资料时，用户明确提到项目或选择项目；系统展示当前作用范围，提供对应资料。模糊时请用户选项目，不按最近活跃群猜。讨论某项目不自动把私聊全文发布到群；形成项目决定／任务／成果时通过有来源的业务操作写入相应范围。

个人上下文读取角色职责、个人偏好、当前工作；项目上下文读取角色职责、项目共享知识与角色项目经验。私人信息不自动提升到全局 RoleMemory；跨范围共享通过明确动作和已有访问合同完成。

当前 DM identity 按 gateway＋actor 分开，是可用入口；但 preferred user ID 有不同类型，gateway 也可重绑 AppID。个人记忆应关联稳定 Principal/Role/Scope，记录应用身份、ID 类型和 binding generation；只把 `gateway` 字符串当永久记忆 key 会在重绑后出错。旧身份关联失效时保存资料与迁移记录，不默认让新 App 继承，也不直接删除历史。

同一个 Bot 的模型配置仍统一；多个用户的私人记忆、同一用户不同项目的工作记忆分别隔离。共享模型不是共享所有上下文。单聊和项目共享统一生命周期、执行、记忆服务，只有 scope、业务组织与展示不同。

## 7. 旧群退役与现有行为影响

可替换的群专用行为包括：[primary 命令](../../internal/core/orchestrator/service_feishu_primary_command.go)、[入群 primary bootstrap](../../internal/app/daemon/app_feishu_primary_bootstrap.go)、[单 workspace 与 reservation](../../internal/core/orchestrator/service_feishu_room_context.go)、[room 持久投影](../../internal/app/daemon/app_feishu_room_state.go)。群身份索引、多 Bot 连接、发送和回调能力继续保留。

不兼容旧群 UX 不等于清除用户文件、Bot 凭据或历史。迁移仍需核对活动执行、旧目录和资料来源；不自动把历史对话全量变成可信长期记忆，也不凭空补出旧任务的验收状态。

| 影响面 | 目标变化 | 必须保护的性质 |
| --- | --- | --- |
| 单聊导航 | 从原生会话／实例导航转为有记忆的工作上下文 | 用户能找到原工作、继续处理、明确当前资源，不混淆历史与记忆 |
| 单聊中断 | attempt 明确 interrupted，不再借 QueueItem completed 暗示任务完成 | 停止后不擅自恢复，已有成果保留 |
| 同 Bot 模型变更 | 一个 desired revision、多个 session 的 applied revision | 不在调用中重启自己，不静默改变已冻结执行 |
| 审批／旧卡片 | owner 改成 session/run，渠道是投影 | 旧消息、旧操作者、旧 revision 不能批准新工作 |
| Agent 并行 | 多 session/instance、明确工作区与预算 | 不抢单聊会话，不把某个角色的结果发到别人的私聊 |
| 配置与版本 | 沿用同一安装和凭据，增加新持久格式及协议归属 | 迁移有 journal；旧 wrapper 不识别新执行合同应明确拒绝，不误判成本地输入 |
| 群交互 | 项目、角色、任务、成果替代旧手动 room 控制 | 未就绪有具体原因；不因 UI 已删就遗失后台执行 |

## 8. 实现组织与迁移终点

建议逻辑边界为：工作领域（scope/context/task/memory）→ 统一执行内核（session/attempt/claims/dispatch/recovery）→ 现有 wrapper/backend；飞书单聊与项目群分别投影，CLI 调同一业务用例。daemon 负责事务与副作用组合，避免两个控制器同时拥有同一原生 turn。

迁移期逐个 context 进行 authority cutover：保存旧 surface/native thread/目录关联，核对运行证据，建立新 context/session，提交归属代次后旧执行字段停止写入。单聊和群都迁入同一执行 owner；旧 owner union 只是过渡，不是永久双内核。

没有必要为新群保留旧命令兼容适配器。共享代码按职责抽取后删除旧群专用流程；单聊的普通输入输出、精确选择和直接控制改接新用例。复用验证的是底层行为，不是要求所有旧测试永远保持旧产品假设。

## 9. 验证证据与目标回归

2026-11-21，本次以 `-count=1` 执行 8 个包的 29 个顶层测试及子测试，全通过：Codex、Claude、wrapper、orchestrator、daemon、gitworkspace、state、runtime。覆盖新线程后续 turn、steer 与拒绝、恢复与 child restart、claim 排他、审批排队、私聊／群恢复差异、实例回收、启动去重、ingress FIFO、真实 Git worktree 创建、Bot 配置与启动参数。日志在本次执行环境 `/tmp/fschannel-codebase-reuse-tests.jsonl`，不作为持久业务数据。

另有 3 包的 10 个定向用例通过，覆盖 DM／群入口身份、同群跨 gateway、私聊不建 room、Bot 全局配置与 gateway 隔离、冻结审批 owner、MCP caller 与当前 turn 目标。这些是现有底座证据，不代表新运行层已实现。

新内核必须增加的回归包括：

- 同 Bot × 两个人私聊 × 两个项目群同时工作，结果、调用身份和记忆不会串线。
- 人选模型／已有目录准确生效；Agent 创建内部工作区无需人操作，也不改变人所选项目资源。
- 私聊与项目使用同一恢复和记忆机制；新会话保留应有记忆，重新开始任务不意外抹去角色。
- 事件只由一个 session/attempt owner 处理；查看／退出界面不终止后台工作，明确停止不会被自动继续绕过。
- 多独立工作区并行、固定成果评审、跨项目同目标分支集成和冲突恢复。
- 消息／审批／compact／重启边界不丢输入、不错误确认；缺失原生能力的适配分支真实可运行。
- App 重绑、旧卡、旧 run、迁移中崩溃不会混用身份或出现双写。

当前旧群限制的测试可以证明旧行为是什么，但不应被当成新群必须继续满足的需求。实现阶段将其替换为新产品合同，同时保留协议、身份、资源互斥与数据完整性的保护。
