# Agent 统一运行层：代码复用与命令盘点依据

> Type: `draft`
> Updated: `2026-11-21`
> Summary: 保留原代码复用与完整 Slash 盘点；统一实现工作包、迁移合同和产品故事转由主文档规定。

文档定位：本文件是专项依据／附录，不是另一份实施入口。用户行为以[产品主文档](./feishu-agent-team-product-design.md)为准，领域所有权、状态与迁移以[技术主文档](./feishu-agent-team-technical-design.md)为准，后端具体时序见[原生运行合同](./feishu-agent-team-runtime-contracts.md)。本文件保留原调查证据和解释；实现拆分、异常处理及验收统一按主文档的 US／T／UAT 编号追踪。

## 1. 决策与范围

建议在现有仓库演进，抽出统一运行内核与新的工作领域，不另开空白代码库，也不长期维护“旧单聊内核＋新团队内核”。已有协议、运行宿主、飞书接入、模型配置、Git 和部署能力值得复用；surface 独占执行、旧群单目录模型和人工管理原生会话的主流程需要替换。

本次用户明确的优先级：

1. 最高优先级是 Raft 式群工作体验：固定同事、负责人协调、同事直接协作、并行执行、评审、交付与持续记忆。
2. 单聊迁入同一底层并获得记忆；保留对话、工作的便利与连续性，以及现有适用的单聊 Slash Command；显示布局可调整，功能接到统一内核。
3. 原群聊体验可以整体丢弃，不维持旧 primary/workspace/coworkers 操作流程。
4. Worktree 由 Agent 通过原生 Git 自主管理，平台不另造目录生命周期或合并引擎，用户无需理解或管理。
5. 模型、已有工作目录等精确选择保留控件；用户明确给出的选择直接采用，Agent 不自行猜选已有资源。
6. 旧 Cron 产品与实现弃用；会话 Goal 不充当项目目标；团队／项目共享资料拥有稳定入口及版本。

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
| 本地 Git worktree | [CreateWorktree](../../internal/app/gitworkspace/worktree.go:36)、[preview](../../internal/core/gitmeta) | 已有 Git 调用与诊断可按需保留；不要求扩建管理器。群内 Agent 使用原生 shell/Git；单聊显式目录命令与必要选择控件保留 |
| 进程与安装升级 | [runtime](../../internal/runtime)、[local upgrade](../../internal/app/install/local_upgrade.go:37) | 保留平台、安装、版本和恢复基础；调度升级/回收前必须考虑统一运行层的活动执行及持久待办 |
| 配置存储与组合根 | [statestore](../../internal/app/daemon/statestore/statestore.go)、[App wiring](../../internal/app/daemon/app.go:195) | 保留配置 atomic save 和 daemon wiring；任务/记忆/待办需要事务库，不能把单文件保存当跨对象事务 |

已追踪的实际路径：wrapper 接收命令 → Codex `thread/start` → 按 request ID 取新 thread 并继续 `turn/start` → stdout 转统一事件 → daemon `onEvents` → orchestrator。`turn/steer` 的 expectedTurnId 拒绝也有真实回执处理。这些是可复用执行链，不只是参数结构。

Backend 能力仍有具体实现缺口：Codex 已有 compact 命令链；Claude 当前命令 switch 不具备同样的 compact handler；OpenCode 默认能力不提供 TurnSteer。完整运行层必须补齐适配，或在不支持即时操作的边界持久排队／受控重建，不能把声明字段存在视为功能已完成。

现有 Bot record 还保存 `ProductMode`（headless/vscode）。新内核须把“使用哪个 backend/model”的角色策略与“这项工作由后台还是本地界面控制”分开：前者继续 Bot 级统一，后者属于 WorkerSession/controller。私聊接管本地编辑器不能把同 Bot 的所有项目群一起切换执行方式；旧 `/mode` 的混合含义要拆解，而不是照搬全局字段。


### 3.1 资料接入的新增审计

当前 [上传预览](../../internal/adapter/feishu/preview/markdown_preview.go) 默认 24 小时清理；[缓存 key](../../internal/adapter/feishu/preview/markdown_preview_helpers.go) 含 scope、路径与 content hash，内容改变通常新建远端文件；[后台清理](../../internal/adapter/feishu/preview/markdown_preview_managed.go) 会实际删除远端对象。这解释了“每次都产生新文件”的现状。用户补充其原始用途是逐次监督：模型总结提到文件时预上传供人查看。它可以作为可选辅助保留，但不能当作长期资料或正式交付的唯一依据；新团队默认不自动外发所有过程文件。

- 可复用 [Drive adapter](../../internal/adapter/feishu/markdown_preview_lark_api.go) 的上传／元数据／共享传输，必须新设永久资料目录、记录及清理边界，不沿用预览的默认 full_access 授权。
- [BitableAPI](../../internal/adapter/feishu/bitable_api.go) 已有表、字段、记录增改和分页；仅保留通用 API adapter，不复用 Cron 表 schema、绑定业务、时钟或任务模型。
- [附件落地](../../internal/app/daemon/app_inbound_file.go) 与 StagedFileRecord 只有消息／surface／本地路径，需接入长期 Resource 与版本；IM file_key 不是云文档身份。
- [评论工具](../../internal/app/daemon/tool_service_drive_comments.go) 只有评论，当前拒绝 Wiki 链接；Docx 正文与 blocks、Wiki 节点解析、Sheets 内容读写需新增接入，不能用已有 SDK 类型冒充功能完成。
- [issueworkflow](../../internal/issueworkflow/github.go) 是本仓研发流程工具，不是产品通用 GitHub connector。新产品需将 repo/issue/PR 身份和回执接到工作与资料领域。

新增 ResourceService 管理资源引用、共享集合、版本、发布更新与索引，不重造飞书编辑器。接口证据、身份限制、冲突与版本合同见 [共享资料设计](./feishu-agent-team-materials-design.md)。

## 4. 必须重构的四个耦合点

### 4.1 Surface 同时充当界面与执行 owner

[SurfaceConsoleRecord](../../internal/core/state/types.go:313) 同时保存当前实例／thread、排队、PendingHeadless、审批、ReviewSession 和展示状态；[remoteTurnBinding](../../internal/core/orchestrator/service.go:99) 也依赖 SurfaceSessionID。

终点必须统一为 WorkScope（personal/project）→ WorkContext → WorkerSession → ExecutionAttempt。SurfaceBinding 只管理用户入口、查看对象和输入目标；单聊也使用该内核，不保留另一套 surface-owned 执行真相。普通对话可以没有 Task，但仍有 context/session/attempt 和记忆。

迁移期可以识别 legacy surface owner；完成迁移后撤掉其执行权威。旧 `/use` 的“打开另一段工作”能力可以保留为上下文导航，不能靠改 SelectedThreadID 隐式抢占原生执行。

### 4.2 队列、claim 与并发是旧界面模型

[入队](../../internal/core/orchestrator/service_queue.go:81) 会先检查 room 限制；旧 QueueItem 完成包含用户中断。新运行层先持久保存输入／任务，再准入执行；attempt 的 completed/interrupted/failed 与 Task 的交付状态分开。

[routing claims](../../internal/core/orchestrator/service_routing_claims.go) 以 room/surface 持有 workspace，以 surface 持有 instance/thread；selected thread 不匹配可能清理 claim。新 claim 以实际执行 session、原生资源和已登记执行位置占用为依据，单聊与团队共用仲裁，不能各建一套锁后互相抢资源。

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
| 新建 worktree／分支／派生目录 | 表达任务，不填写内部路径 | Agent 原生 Git 自主管理并登记恢复位置，平台记录绑定与成果 | 人工 picker 退出群主流程；单聊可显式使用，不另建目录生命周期／自动合并引擎 |
| `/list`、`/use`、`/useall`、`/detach`、`/follow` | 单聊可直接选择、查看、接管；群里表达工作目标 | 群自动定位 context；单聊命令绑定真正 session owner | 保留适用单聊入口；当前未注册 `/attach`，不能按旧印象列为现有 Slash |
| `/new` | 表达新事项或重新开始，必要时选择是否沿用资料 | 判断 context/session 的复用或新建 | 保留原生创建能力；不能把新模型 thread 当新项目或删记忆 |
| `/compact`、会话重建 | 不必管理上下文窗口 | 运行层整理检查点、compact／恢复／重建并继续投递 | 保留已有命令能力，替换“先选线程再清空队列点卡”的主流程 |
| `/review` 与 commit 选择 | 按项目阶段提出检查重点或反馈 | 在实际需要时安排审查、返工和复查，不要求所有工作都经专职评审 | 复用差异／审阅构件；固定版本评审采用时绑定真实版本 |
| `/autocontinue` | 控制是否暂停／取消工作 | 按明确剩余工作和错误原因恢复，有退避和边界 | 复用可用恢复策略，状态归 attempt，不要求人逐群打开开关 |
| `/autowhip` | 单聊按需保留显式催办工具；群不要求使用 | 群由持久任务与依赖推进；单聊可在支持后端调用局部功能 | 不作为团队调度内核；局部循环不能自行判定项目任务完成 |
| `/cron` | 新主流程没有此入口 | 不导入旧计划为自动运行，不沿用 scheduler、run、cronrepo 业务模型 | 旧产品与实现退役；确需定时业务时另行完整设计，不是本次交付前提 |
| `/goal` | 可查看、调整当前工作的持续执行约束与预算 | 支持的 backend 使用会话 Goal，暂停与任务修订联动 | 当前仅 Codex headless；移到执行详情，不升格为项目目标管理 |
| `/sendfile`、成果发布 | 打开资料、指定更新或下载版本 | Agent 更新同一资料或登记成果，群内发送稳定链接 | IM 传输保留；临时预览与长期资料存储及清理彻底分开 |
| `/primary`、`/coworkers`、旧群 workspace | 选择负责人／团队及明确资源预算 | 项目成员、任务调度和执行位置绑定 | 旧群命令与交互可移除，不保持兼容流程 |
| 停止、暂停、取消、验收 | 随时直接控制工作与最终取舍 | 对正确任务执行控制、展示实际状态 | 保留简短可靠入口，不藏到 Agent 内部 |
| 管理、凭据、执行授权、升级 | 管理连接和政策，处理确实需人的授权 | 系统执行确定的配置／升级事务，遵守既有规则 | 保留管理能力；不让 Agent 代替人自我批准权限 |

人选“使用这个已有目录”与 Agent 创建其内部 worktree 是两个不同决策。界面默认显示任务、成员、进度、成果、阻塞和当前选择；不展示 branch/worktree/session/instance 参数来要求用户代为编排。用户主动调试时仍可查看必要技术信息。

上述操作归属不替项目规定工作流程。是否需要 issue、何时测试／评审／人验收、交付到修改／PR／合入／部署哪一步，均从项目及当前事项的约定确定并可随阶段变化。程序保留可靠执行、来源及状态真实性，不能把“当前项目采用了某流程”固化成所有项目必须遵守的产品规则。

固定入口可新增薄 Feishu adapter：现有 SDK 已支持 ChatTab Create/ListTabs/UpdateTabs/DeleteTabs/SortTabs，生产尚未接入；TopNotice 与 Pin 同样可经 broker 调用。项目绑定自有 tab/message IDs 并指向稳定资料，用户群内的其他入口保持其原有归属。API 事实见 [入口能力表](./feishu-agent-team-materials-design.md#21-群原生固定入口)。

### 5.1 Slash Command 盘点与去向

本轮核查 [主 specs](../../internal/core/control/feishu_commands.go)、[附加 Goal spec](../../internal/core/control/feishu_commands_goal.go)、[parser](../../internal/core/control/feishu_commands_parse.go)、[display profiles](../../internal/core/control/feishu_command_display_profiles.go) 与关键 handler。现有 44 条 spec 对应 43 个不同 canonical slash；两条 `/admin` spec 分别处理根入口与隐藏参数路径。菜单显示、解析成功和可执行是三个不同事实。

| 能力组 | 现有 canonical 命令与用途 | 新群模式 | 单聊 |
| --- | --- | --- | --- |
| 执行控制（3） | `/stop` 中断并清未发输入；`/steerall` 合并排队输入；`/compact` 压缩会话 | 自然语言指导＋直接到运行层的停止／暂停控件 | 保留适用命令，重接统一 session |
| 会话与历史（7） | `/new` 新会话；`/history` 历史；`/list` 工作区／实例；`/use` 当前工作区会话；`/useall` 跨工作区会话；`/detach` 解除接管；`/follow` 跟随 VS Code | 通过任务／项目定位工作上下文，不让人管理原生 thread | 保留导航、历史和手动控制，不保留旧 surface 执行所有权 |
| 工作区（7） | `/workspace`、`/workspace list`、`/workspace new`、`/workspace new dir`、`/workspace new git`、`/workspace new worktree`、`/workspace detach` | 已有资源精确选择；内部工作位置由 Agent 自主组织 | 保留显式资源与目录操作，不因群自动化取消单聊入口 |
| 运行配置（10） | `/mode`、`/model`、`/reasoning`、`/access`、`/plan`、`/verbose`、`/codexprofile`、`/claudeprofile`、`/opencodeprofile`、`/mcpoauth` | 对话引导选择控件，跨项目 Bot 配置在适当设置面处理 | 保留后端适用配置及认证入口，准确显示影响范围 |
| 持续工作与审阅（5） | `/goal` 会话目标；`/autowhip` 空闲催办；`/autocontinue` 可重试失败恢复；`/review` 审阅；`/bendtomywill` 修补当前会话最新助手回复 | 持久协作、方法库和工作状态承接，不靠循环催办或改写回复判业务完成 | 保留适用直接操作；局部工具不擅自改团队事实 |
| 文件交付（1） | `/sendfile` 从工作区选文件发送 | Agent 按实际需要显式交付，使用稳定入口 | 保留，接共用发送／资料能力 |
| 旧群专用（2） | `/primary` 未 @ 消息的默认 Bot；`/coworkers` 群并发上限 | 旧命令退役，由负责人路由和调度预算承接 | 原 handler 就只允许群，不新增到单聊 |
| 导航与维护（7） | `/status`、`/help`、`/menu`、`/admin`、`/upgrade`、`/debug`、`/vscode-migrate` | 项目原生入口及自然语言状态查询；管理移至已有管理入口 | 保留适用命令并投影新内核状态 |
| 已决定弃用（1） | `/cron` 旧定时运行与多维表格流程 | 按此前决定退役 | 单独的 Cron 弃用决定仍有效，不因保留单聊入口恢复旧功能 |

实际 Slash 别名为 `/effort`→`/reasoning`、`/approval`→`/access`、`/threads`与`/sessions`→`/use`、`/sessionsall`与`/sessions/all`→`/useall`、`/mcp-oauth`→`/mcpoauth`。`patch`、`newthread` 等菜单 key 不等于 Slash alias；当前未注册 `/attach`、`/patch`、`/cancel`。`/goal` 的 new/edit/pause/resume/clear 与 `/review` 的 uncommitted/commit 等是参数，不重复计数。

不能把“保留命令”解释成所有后端已支持所有操作。当前 Goal 仅 Codex headless；Claude／OpenCode 的模型设置通过各自 profile，不能直接套用 `/model`；它们的 compact、持续催办等也有拒绝分支。`/list /use /useall /detach` 在部分 headless 目录隐藏但仍可 dispatch；`/mcpoauth` 已注册、隐藏且有后端限制。迁移保留真实可用能力与明确限制，不用一个通用菜单假装能力齐全。

新群不发布 Slash 目录，旧命令文本不再路由到旧 surface/room handler；识别为旧操作意图时给当前自然语言或按钮入口，引用／代码里的文本不当命令。单聊继续按 specs／后端规则解析，并通过相同业务用例操作新执行 owner。群紧急停止按钮直接进入运行层，即使负责人忙碌也可用；群退出 Slash 不等于把停止控制完全交给模型。

### 5.2 方法库与单聊命令不是同一层

`/review` 是人显式启动一次操作；“同行评审”方法说明何时值得评审、谁承担、怎样反馈。两者可以调用相同检查能力，但方法库不能退化为命令别名集合。群由角色根据当前约定采用方法；单聊用户仍能直接调用操作。人的参与和汇报约定只决定何时找谁，不等于开关所有质量实践。

## 6. 单聊的推荐体验与记忆边界

单聊是找同一位同事单独工作、讨论和指导。默认个人范围，直接对话、交付文件、继续过去工作；长期偏好和未完成事项随角色与个人工作空间恢复。无需为了聊天伪造一个飞书项目群，也不必每条消息建一张 Task。

需要项目资料时，用户明确提到项目或选择项目；系统展示当前作用范围，提供对应资料。模糊时请用户选项目，不按最近活跃群猜。讨论某项目不自动把私聊全文发布到群；形成项目决定／任务／成果时通过有来源的业务操作写入相应范围。

个人上下文读取角色职责、个人偏好、当前工作；项目上下文读取角色职责、项目共享知识与角色项目经验。私人信息不自动提升到全局 RoleMemory；跨范围共享通过明确动作和已有访问合同完成。

当前 DM identity 按 gateway＋actor 分开，是可用入口；但 preferred user ID 有不同类型，gateway 也可重绑 AppID。个人记忆应关联稳定 Principal/Role/Scope，记录应用身份、ID 类型和 binding generation；只把 `gateway` 字符串当永久记忆 key 会在重绑后出错。旧身份关联失效时保存资料与迁移记录，不默认让新 App 继承，也不直接删除历史。

同一个 Bot 的模型配置仍统一；多个用户的私人记忆、同一用户不同项目的工作记忆分别隔离。共享模型不是共享所有上下文。单聊和项目共享统一生命周期、执行、记忆服务，只有 scope、业务组织与展示不同。

## 7. 旧群退役与现有行为影响

可替换的群专用行为包括：[primary 命令](../../internal/core/orchestrator/service_feishu_primary_command.go)、[入群 primary bootstrap](../../internal/app/daemon/app_feishu_primary_bootstrap.go)、[单 workspace 与 reservation](../../internal/core/orchestrator/service_feishu_room_context.go)、[room 持久投影](../../internal/app/daemon/app_feishu_room_state.go)。群身份索引、多 Bot 连接、发送和回调能力继续保留。

旧 Cron 在迁移切换时停止触发并核对在途执行，保存旧计划／历史供只读查阅或导出，不自动转成新任务，也不继续运行旧清理器删除资料。删除 Cron 业务代码时通用 Bitable adapter 按职责留下，不继续保留 Cron schema。

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

没有必要为新群保留旧命令兼容适配器。共享代码按职责抽取后删除旧群专用流程；单聊的普通输入输出、适用 Slash、精确选择和直接控制改接新用例。复用验证的是底层行为，不是要求所有旧测试永远保持旧产品假设。

## 9. 验证证据与目标回归

2026-11-21，本次以 `-count=1` 执行 8 个包的 29 个顶层测试及子测试，全通过：Codex、Claude、wrapper、orchestrator、daemon、gitworkspace、state、runtime。覆盖新线程后续 turn、steer 与拒绝、恢复与 child restart、claim 排他、审批排队、私聊／群恢复差异、实例回收、启动去重、ingress FIFO、真实 Git worktree 创建、Bot 配置与启动参数。日志在本次执行环境 `/tmp/fschannel-codebase-reuse-tests.jsonl`，不作为持久业务数据。

另有 3 包的 10 个定向用例通过，覆盖 DM／群入口身份、同群跨 gateway、私聊不建 room、Bot 全局配置与 gateway 隔离、冻结审批 owner、MCP caller 与当前 turn 目标。这些是现有底座证据，不代表新运行层已实现。

新内核必须增加的回归包括：

- 同 Bot × 两个人私聊 × 两个项目群同时工作，结果、调用身份和记忆不会串线。
- 人选模型／已有目录准确生效；Agent 创建内部工作区无需人操作，也不改变人所选项目资源。
- 私聊与项目使用同一恢复和记忆机制；新会话保留应有记忆，重新开始任务不意外抹去角色。
- 事件只由一个 session/attempt owner 处理；查看／退出界面不终止后台工作，明确停止不会被自动继续绕过。
- Agent 自行创建／恢复独立工作位置、固定成果评审、跨项目集成与冲突处理；平台不私自重置或清理目录。
- 同一资料原位更新、团队引用不复制、旧预览清理不碰永久资料；issue/PR 与文档的权威范围不冲突。
- 会话 Goal 停止／恢复不绕过工作控制，原生完成不等于项目验收；旧 Cron 迁移后不再触发。
- 消息／审批／compact／重启边界不丢输入、不错误确认；缺失原生能力的适配分支真实可运行。
- App 重绑、旧卡、旧 run、迁移中崩溃不会混用身份或出现双写。

当前旧群限制的测试可以证明旧行为是什么，但不应被当成新群必须继续满足的需求。实现阶段将其替换为新产品合同，同时保留协议、身份、资源互斥与数据完整性的保护。
