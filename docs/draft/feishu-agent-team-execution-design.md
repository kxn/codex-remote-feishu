# 飞书 Agent 团队：执行原则与 Raft 对照依据

> Type: `draft`
> Updated: `2026-11-24`
> Summary: 按用户决定取消原生会话人工接管，保留飞书指导、审阅、暂停、恢复与旧状态迁移边界。

> 架构决议更新（2026-11-23）：单聊与团队改为独立应用，共享原生接入和平台通信。本文中“单聊统一进入团队执行内核”的要求已由[应用分离设计](../inprogress/private-team-application-separation.md)替代；现有行为与迁移状态以该文的实施记录为准，不能把目标设计当成已部署事实。


文档定位：本文件是专项依据／附录，不是另一份实施入口。用户行为以[产品主文档](./feishu-agent-team-product-design.md)为准，领域所有权、状态与迁移以[技术主文档](./feishu-agent-team-technical-design.md)为准，后端具体时序见[原生运行合同](./feishu-agent-team-runtime-contracts.md)。本文件保留原调查证据和解释；实现拆分、异常处理及验收统一按主文档的 US／T／UAT 编号追踪。

## 1. 目标与证据边界

本设计落实用户要求的完整 Raft 式协作：稳定角色自主分工、并行工作、直接交流、互审、持续跟进、跨项目记忆与故障恢复。实现可以按依赖顺序推进，交付目标不能退化成“群中多个独立聊天机器人”或“共享目录轮流运行”。

Raft 式群工作是最高产品优先级。私人应用沿用原私人 core 的会话、队列和恢复语义，群团队应用独立管理成员、任务与执行；两者共享原生适配、平台通信及配置来源，不共用业务写入权。旧群交互可整体替换。已有资产及替换面见 [代码审计](./feishu-agent-codebase-reuse-assessment.md)。

自建多身份 CLI 是已确定的能力入口，不再把 MCP／动态工具选型作为主线。本文与 [产品草案](./feishu-agent-team-product-design.md)、[技术方案](./feishu-agent-team-technical-design.md) 共同描述目标，均不代表生产代码已经实现。

Raft 公开源码用于研究行为和接口，不将其实现代码移入本仓库。以下区分源码事实和 fsChannel 设计；不把公开源码某个版本中的机制描述成所有 runtime 的无条件保证。

运行生命周期核查快照为 `5214e34e726ed715f455e78c96fa521791ef2de3`；文末保留源码定位入口，后续实现需记录所用 Claude Code／Codex 版本及适配证据，不将 Raft 的测试代替本项目测试。

| 已核实的 Raft 机制 | 对本项目的含义 |
| --- | --- |
| 每个 Agent 有 `dataDir/agentId` 个人目录，初始化 `MEMORY.md` 与 `notes/`，已有内容保留 | 角色个人资料与群绑定目录必须分离 |
| runtime 启动 `workingDirectory` 使用 Agent 个人目录 | Raft 的 Workspace 不等于全团队共享的项目 checkout |
| 不同 Agent 并行协作；启动协调器按 agent 去重并限制同时启动数量 | 启动限流不应变成整个项目的全局串行限制 |
| 工作中先保存 inbox，按 runtime 能力发送中途通知／输入，不能接收时留到安全边界 | 忙碌不能拒收；收到消息也不等于开启同一会话的第二个并行 turn |
| Claude 使用常驻 stream-json 与恢复会话；Codex 使用 app-server；角色说明通过各自的附加指令入口进入 | 继续使用原生 Agent，团队生命周期由我们管理，不必重新实现模型请求循环 |
| 个人记忆、启动记忆块、压缩及会话回收有独立处理 | 原生会话压缩与团队持久记忆必须共同设计，重置会话不能丢掉任务与角色 |

未找到足够源码证据证明 Raft 自动为每个任务创建 Git worktree 并自动合并。下面由 Agent 组织独立执行目录、版本审查与集成的流程，是我们为支持可靠并行开发作出的设计，不冒充 Raft 的已证实实现。

## 2. 对象与唯一事实所有权

| 对象 | 所有者与作用 | 不等同于 |
| --- | --- | --- |
| Role | 稳定同事身份、通用职责，引用 Bot 统一模型配置 | 模型名称、进程或单个项目 |
| RoleHome | 角色持久资料、通用记忆索引和受控运行资料 | 项目源码目录；OS 安全沙箱 |
| WorkScope / Project | 团队项目的业务作用域；旧 PersonalScope 只作为迁移来源 | 私人应用的运行 owner、最近发言人或裸 gateway 配置槽位 |
| Project | 目标、成员、需求、共享知识、资源关联及调度策略 | chat ID 或 cwd |
| ProjectChannelBinding | 项目与主群／其他讨论入口的关联和绑定代次 | 项目本身 |
| ProjectResource | repository、文档空间或其他项目资源的稳定引用及访问策略 | 某个机器上的路径 |
| Task / Workstream | 工作责任、依赖、采用的需求／验收版本和成果；可关联权威 issue | 一次 turn |
| ExecutionLocation | Agent 登记的执行位置、host、cwd、关联资源与绑定代次；必要时附 Git 检查点 | 平台 worktree 生命周期管理器 |
| WorkContext / WorkerSession | 工作流恢复上下文与实际 backend session；绑定 role、scope、execution location；project scope 再关联项目 | 飞书 surface 或话题 |
| ExecutionAttempt | 一次启动／执行尝试、输入范围、冻结版本与结果证据 | Task 的最终状态 |
| Artifact / Review / Integration | 不可变成果、针对其版本的评审、进入目标基线的集成记录 | 最近一条消息或可变路径 |

协作库是团队模式 Project、资源引用、执行位置绑定、调度策略与执行事实的权威写源。issue-backed Task 的业务要求／状态以 issue 为源，内库存采用版本和调度事实；未接 issue 的 Task 由内库维护业务事实。Bot 模型配置仍以已有 Bot capability store 为唯一写源；不能为了协作再复制一套可独立修改的模型配置。

团队工作库存储项目、成员、任务和团队记忆；私人 journal 与 checkpoint 保存个人队列、执行及私人记忆。旧团队个人 scope 只用于有证据的迁移。Bot backend/model/profile 默认策略共享，停止和恢复控制属于具体私人 session 或团队执行；不提供本地原生会话接管。

团队执行使用 WorkContext、WorkerSession 与 Attempt；私人执行使用独立 owner、checkpoint、队列和已认证 carrier/session 关联。共享 execution registry 对实际载体实行唯一归属，不能用目录或 session 名猜测，也不以团队 WorkerSession 取代私人状态。

群默认承接一个明确项目；项目可以关联多个沟通入口和多个资源。群换名字、换主群、项目换本地目录，都不改变 ProjectID。绑定保存 revision，消息与卡片保存 ProjectID／TaskID／ConversationID，不靠当前群设置回推过去成果的归属。

多个项目可以关联同一个 RepositoryID，并使用各自工作区与上下文。不能把“仓库相同”判断成“同一个项目”，也不能用“不同群抢同一目录”作为拒绝两个项目协作的理由。

项目的测试／评审强度、人介入方式、交付终点和工具采用方式可随阶段演进；产品只固定真实运行与数据合同。相关约定从已有规范和自然语言决定中读取并保留来源，不要求每项目填写一套流程配置。变更影响活动工作时关联适用 revision 并通知角色，不通过改旧检查点伪造已经满足新要求。

## 3. 角色、会话与并发

一个角色保持一套身份与通用职责，各项目有独立工作上下文。一个项目内的协调、实现、评审等工作流可以有明确绑定的多个 WorkerSession；是否新建会话由上下文和写入资源决定，不默认将所有任务塞入一个无限增长的会话。

个人对话由独立私人 core 的会话、队列和原生执行处理，不创建团队 WorkContext、WorkerSession 或 Attempt。用户可显式查询当前有权访问的项目资料，或确认分享指定内容；这些操作不改变私人身份、会话与 cwd，不自动共享私人 transcript。

每个原生会话一次只有一个活动 turn，保证原生协议和输入顺序；不同角色／工作流／项目的独立会话可以并行。多个会话仍以同一个角色身份出现，项目中显示其汇总忙碌状态与具体任务，不把每个会话变成一个新的飞书 Bot。

WorkContext 在会话重建后保留，WorkerSession 则记录具体 runtime session 与代次；不能把旧 thread ID 换掉就丢失工作流的恢复索引。上下文隔离是信息归属规则，不等于同一 OS 用户下的严格文件安全隔离；强隔离需由宿主的进程／文件访问边界实施。

Task 同时只有一个负责推进的 owner；多个角色协作拆成关联工作项。WorkerSession 取得执行资格不自动取得任务所有权。转交 owner 时更新 assignment generation，旧执行的成果可以归档，但不能继续修改新 owner 的任务状态。

调度分别管理 host/provider 执行预算、启动并发、项目预算、角色预算和执行位置占用记录。达到预算时待办持久等待，不拒绝任务登记。公平队列防止一个项目持续占满全部角色，依赖结果与明确用户 steering 可以优先处理，但不能违反同一会话单 turn 合同。

等待依赖的工作先控制原生持续执行并确认当前运行结束，再释放模型执行名额；它的工作区、检查点、责任关系仍保留。依赖完成后生成 inbox 事件，恢复原工作流或用持久检查点建立替代会话。不存在通过 sleep 保持模型一直在线才能继续的要求。

## 4. 并行开发与成果集成

### 4.1 Agent 自主组织 Git 工作

人精确选择已有目录／仓库；负责人根据工作划分责任，执行 Agent 用原生 shell／Git 自行选择、创建、复用和清理 checkout/worktree。平台不指定每任务一个 worktree，不另建分支分配、自动 merge 或目录回收引擎，也不要求通过专用 workspace CLI 才能使用 Git。

Agent 根据任务约束安排并行：独立写工作通常使用不同 checkout/worktree，评审读取固定 commit；不相互 reset、clean 或覆盖未提交修改。负责人协调共享分支、测试服务等冲突资源，只有冲突操作等待，其他工作继续。Git 是分支、HEAD 与 worktree 的事实来源。

运行层保留 `ExecutionLocation`：host、启动／恢复 cwd、用户选定资源引用、context generation、当前使用该位置的 worker，以及 Agent 提交的恢复检查点。不在数据库复制一套 Git 状态机。已知同一可写目录被其他 worker 占用时，不再派发第二个写 worker；Agent 在绑定新位置后继续。这个检查只约束本系统的调度，不能声称禁止任意 shell 跨目录写入或外部进程修改文件。

Agent 在创建目录后登记位置与检查点；若原生 session 不能安全更换 cwd，由 adapter 建立新 session 并载入检查点。创建后尚未登记便崩溃时，恢复角色先检查 Git／磁盘证据，找回或重新安排位置，不因数据库里没有记录就删除它。不确定操作进入核查，平台不盲目重放任意 shell。

恢复发现目录不存在、宿主不可用或 Git 基线改变时，唤醒 Agent 在仍可用的角色工作目录检查并修复，记录明确阻塞；平台不自行重建目录、重置代码。角色资料目录与项目源码目录分开，源码执行继续遵守项目 AGENTS.md／CLAUDE.md。

多仓库工作分别记录资源和成果，不声称跨仓库提交原子完成；非 Git 资料按其载体的稳定资源 ID 和版本协作，见 [共享资料设计](./feishu-agent-team-materials-design.md)。旧 Cron 与 cronrepo 不作为新执行模型的基础。

### 4.2 按项目需要交付与审查

产品支持直接答复、当前页面走查、未提交修改演示，以及固定 commit／文件／文档快照评审；采用哪种取决于项目当前阶段、约定和任务。不是每项工作都必须安排质量角色或生成不可变成果。

当工作约定需要固定版本评审时，实现角色提交明确版本，评审者准备对应环境；不能把持续变化的目录／在线页面当作固定版本证据。当前状态走查则如实记录观察对象和时间，不冒充严格快照。

存在评审时保存适用 task revision、实际查看的版本或当前状态标记、检查依据和结论。需求或成果改变时，已有评审标明对应旧版本；重新评审范围由变更决定，不把旧通过状态继承给新成果。需要固定版本验收时，“当前链接”与“这次验收版本”同时可定位。

### 4.3 Agent 负责集成

当项目当前工作需要集成时，由约定的责任角色（可由负责人协调）通过仓库既有 Git／PR 工作流完成组合、冲突修复、适用检查与合入；系统记录交接、候选 commit、目标分支和结果证据，不实现另一个 merge 调度引擎。`integration_request` 表示派发集成工作，不表示 daemon 自己执行合并。

多个项目关联同一 repository＋target ref 时，共享集成责任／占用记录；如果目标是 GitHub PR，则利用仓库分支保护及其已有合并流程协调。记录不能代替 Git 的竞争检查：Agent 在实际操作前核对目标 HEAD，发现前移后重新整合、验证，禁止强行覆盖他人成果。

目标分支在用户目录检出时，Agent 负责保留未提交修改和正确的 HEAD／索引／工作树关系，不仅移动 ref。无法安全推进时留下候选成果并说明阻塞；其余独立任务继续。这些事实分别记录，不要求每个任务都经历所有阶段，也不把无冲突合并等同用户验收。

合并、推送、发布遵守既有授权与仓库规则；不增加逐次内部交接确认。平台不能仅凭 Agent 一句“完成”更新可机械核对的合并事实，需读取 commit／PR 回执。

### 4.4 收尾与恢复边界

Agent 决定何时收尾自己创建的工作目录；先确认无运行进程、未提交成果及待评审／恢复引用，再用 Git 正常清理。平台负责提示仍被引用的执行位置、保留检查点和成果，不按 TTL 删除项目工作目录，也不自动清理用户原目录。丢失本地唯一成果不能被一条“已完成”记录掩盖。

单聊用户可继续通过适用 Slash 显式选择工作、控制原生会话和发起审查；这些操作由私人应用和其已认证的执行载体处理。群用自然语言组织工作，运行层的直接停止控件仍存在；自动化不取消人的直接控制，也不保留旧群 Slash 执行权威。

## 5. 交流、唤醒与需求变化

消息先持久化，再投递执行；飞书公开消息与内部工作事件由同一操作派生，平台回流只核对送达，避免第二次执行。FYI、问答、任务交接、评审请求、需求变更分别携带意图和稳定关联。

空闲会话收到可行动事件时调度启动；忙碌会话按 backend 能力进行 steer／输入通知，遇到压缩、恢复或不支持中途输入的边界时持久暂存。投递成功、模型读到、业务处理完成是三个不同状态，不能在写入 stdin 后就确认任务完成。

Claude adapter 负责 stream-json 输入与原生排队的对应证据；Codex adapter 负责 turn/steer 以及目标 turn 已结束时的重新入队。通知和输入都关联 session generation、turn 和 inbox IDs，不能把迟到输入打进下一项无关任务。

改变需求时增加 task revision，通知当前 owner 和受影响依赖。按影响决定 steering、请求中断或下一轮处理；旧版本成果保留且明确标旧。紧急暂停先关闭新执行准入，再请求停止活动执行，确认原生进程及资源状态后释放租约。

人的参与／汇报约定改变也产生持久事件：明确委托将尚待人的对应决定转交责任角色并记录旧待办已转交；提高参与在未完成工作中增加实际需要的请求。通知汇总和决策责任分别处理，少发消息不能让任务无限停在人待办中。已完成决定保留历史，旧回调不覆盖新关系；当前执行在适当边界采用新的约定版本。

任务依赖图拒绝自依赖和环。阻塞有具体原因、负责解除者和关联对象；循环互相提问或多轮无进展时形成可见阻塞，交给负责人判断，而不是继续自动互相 @。

消息与任务变化进入同一持久待办机制。负责人离线、执行超预算或系统重启时不会丢失；恢复不依赖人再发一句话。

## 6. 原生上下文与长期记忆

原生 runtime 保留自己的会话和压缩机制；我们保存角色、项目、任务和成果的持久事实，并负责建立／恢复会话所需的材料。角色身份不依赖某个 runtime session 永远存在。

ContextAssembler 统一处理 fresh、resume、compact 和 rebuild，而非在各个飞书消息入口零散拼接。BackendAdapter 明确提供 Prepare、StartOrResume、Deliver、Observe、Maintain、Recover、Stop 生命周期；不具备某个即时操作时采用持久排队或受控重建这一确定分支。

每次输入记录实际送入的 task revision、inbox IDs、角色说明版本和记忆版本。新建／重建会话读取有预算的启动材料：职责、项目入口、正在处理的任务、检查点、未消费信息与相关记忆；详细历史按需读取，不把整个群历史塞进提示词。

在任务交接、交付、长执行检查点和会话回收前保存恢复资料；后台回收不能只指望模型最后一次成功写笔记。保存失败不确认资料已持久化，不删除唯一可恢复会话。崩溃时从此前已提交的检查点和事件恢复，未证实的副作用进入核查。

Claude／Codex 的 standing instruction 分别通过已有附加系统说明／developer instruction 通道组合，保留原生基础指令。角色规则变更和记忆纠正记录版本，在相关会话安全边界更新；必须即时纠正的任务输入通过 steering 送达，必要时重建上下文。

记录 desired/applied instruction revision，字段发送成功和 hash 一致不等于新规则已经生效。Raft 的 Codex 接入也记录恢复会话更新说明可能延迟生效；我们不能可靠刷新时，保存检查点并建立 fresh session。Claude fresh session 出现 ID 也不直接标为可恢复，需要原生可靠落盘／turn 结束等证据。启动 ready 同时覆盖身份、会话与所需工具可用性，失败保留待办并给出具体运行错误。

compact 属于维护状态：按原生能力执行，有明确触发条件、单次执行和重试边界。维护期间保存新消息，完成后继续投递，不触发业务任务完成；维护屏障会缓冲待投递工作，不能要求整个 durable inbox 永远清空才维护；存在未结束子进程、悬挂原生请求或不安全边界时推迟维护。压缩失败保留原会话与检查点，不重复盲目发送命令。

原生自动记忆必须明确作用域：能按角色与项目隔离就配置隔离；否则关闭重复自动写入，让团队记忆服务承担持久知识。隔离包括 runtime state、自动记忆与配置落点，不能仅靠项目提示词。模型服务认证沿用受管理凭据，不通过覆盖全局 HOME 搬走用户配置。

不同工作流更新同一项目记忆时通过 revision 检查；通用知识提升保存来源和适用范围。纠错与撤销使索引、后续启动材料和订阅该知识的活动工作流一起收到更新，不能只删除数据库一行就声称活动模型已经忘记。

个人记忆由独立私人应用按稳定 owner 和 epoch 管理，记录来源应用身份与 user ID 类型，不只使用可重绑的 gateway 槽位。App 重绑时旧资料保留并隔离，只有明确身份映射才继续关联。个人偏好不自动写入 Role 全局记忆，私聊讨论项目也不改变该边界。


### 6.1 会话 Goal 与业务目标

Project 的长期方向、Task 的需求／验收和原生 Session Goal 是三个层级。Session Goal 用于持续推进当前会话的工作、减少提前停下与偏离目标，属于执行策略，不承担项目拆解、资料权威或团队验收。

已有 [Goal 用例](../../internal/core/orchestrator/service_goal_command.go) 仅允许 Codex headless，调用 [thread/goal/set|get|clear](../../internal/adapter/codex/translator_goal.go)；Claude 当前没有同等 Goal 适配，不能宣称两端已具备同一原生能力。兼容策略是原生支持时使用其能力，不支持时依赖工作检查点、inbox 与受控 continuation 达到持续工作的业务要求，不仿造不存在的协议。

工作层保存 goal 与 WorkContext、session generation、task revision 的关联，以及确有用户设置时的预算。目标内容从已确认工作导出；变更必须有可追溯的新指令或任务修订，不能因模型自行改写 native goal 而反向改业务要求。原生接口允许设置目标，不构成不可绕过的目标锁。

暂停／停止同时关闭工作层准入并暂停原生持续执行；新任务占用会话前处理旧 goal，避免双重 continuation。会话重建先读取当前工作与停止状态再决定是否恢复 goal。native goal complete 只是运行证据，不能直接关闭 Task 或验收项目。用户可在工作详情查看“会话持续执行”状态和预算，不在项目导航创建另一个“目标”中心。

### 6.2 资料与记忆的关系

共享资料保存人和团队可共同维护的需求、规范、业务数据与交付；MemoryEntry 保存摘要、经验和恢复线索，必须引用资料版本，不能成为隐藏的另一份需求原文。资料更改使关联摘要过期，活动任务收到变化事件；已确认验收基线只能通过明确修订更新。完整的资料归属、API 接线、版本与冲突处理见 [共享资料设计](./feishu-agent-team-materials-design.md)。

## 7. 执行宿主与人的指导

后台托管 worker 是持续工作的执行宿主，与用户打开哪个界面无关。人通过飞书指导、审阅、暂停和继续；surface 不拥有唯一执行身份。

2026-11-21 用户决定取消原生会话人工接管。已有手动会话不自动绑定团队工作流，新版不实现领取／归还控制权或进入桌面／终端操作同一原生会话。旧人工控制状态保留迁移事实，核实执行、续轮与后台工作后才能恢复新的自动工作。

用户自行操作的本地会话不能被后台借用；后台会话也不能只因 cwd 和模型相同就被替换。团队绑定核对 role、scope/context、workstream、执行位置代次、指令版本和 backend；私人载体核对独立 owner、epoch、会话、账户命名空间与 backend。

不具备受控身份或生命周期接口的外部 Bot 可以作为群参与者；要成为可调度 worker，必须实现身份、inbox/ack、执行状态和结果回执合同。这是执行能力边界，不由群里显示一个 Bot 名字自动获得。

## 8. 持久恢复与迁移

协作库原子记录任务变更、inbox、outbox、执行尝试、租约和幂等结果。平台 API 与进程启动在事务外执行，并以持久 operation 关联回执；Agent 的 Git 操作用执行检查点、原生记录和实际 Git 证据恢复，不伪装成平台可原子回滚的操作。

重启对 queued、claimed、starting、running、waiting、integrating 等状态逐项恢复。lease 到期只表示需要核查，不能无证据重启第二个写进程；平台发送状态不明时也不能重复派发业务。稳定 ID、generation 和不可变成果引用贯穿整个链。

旧群切换团队模式使用有 journal 的显式迁移：

1. 记录旧 room/surface/config 的快照与预期 generation，冻结该群新的旧式工作区写入；已有执行排空，无法确认结束的保留隔离。
2. 导入 Project、ChannelBinding、ProjectResource 和调度策略，保存旧目录与未完成执行的归属。原用户 checkout 保留，不强制 reset／clean。
3. 准备角色身份、执行位置及 session 绑定，核对可恢复性和平台成员状态。
4. 提交 mode/authority generation，工作库接管该群项目配置；旧群 `/workspace`、primary、并发菜单退出主流程，残留卡只提示已迁移／打开当前项目，不继续旧写入。
5. 切换前失败可回退旧模式；切换后已有新任务时只能按 journal 前滚修复，不能把旧 JSON 重新覆盖回去。

旧模式群的 room JSON 仍是旧模式权威；团队模式的旧记录只作迁移快照或只读兼容投影。禁止两个存储同时独立写入群目录／并发真相。历史消息和旧卡按记录的代次访问当前事实，不触发第二次迁移或覆盖新配置。

上述团队私人模式仅是迁移来源。将可证明的私人输入、记忆、native thread／cwd 和发送证据导入独立私人 journal、checkpoint 与恢复记录，不将历史全文自动当作记忆。source fence 和中立入口转移后，旧团队个人 scope 不再接收私人新工作；未决记录按组件保留。旧群 UX 无需兼容，迁移记录用于数据连续性而非保留旧交互。

显式用户设置的资源预算在迁移时保留并说明；旧实现默认值不能被误称为新产品只能串行的结构限制。并行能力必须按可用预算工作，容量为 1 时验证的是不丢任务和不死锁，而非完整交付的替代条件。

## 9. 完整目标的交付证据

以下是实现必须交付的行为与证据，不是用来缩减目标的可选研究：

- 同一项目中两个角色同时实施独立子任务，第三个角色可以讨论／评审，负责人仍能接受新要求。
- 同一角色参与两个项目、同一仓库关联两个项目时，会话、记忆、成果和消息归属清楚。
- 同 Bot 的多个用户私聊与项目群同时工作，分别使用私人和团队运行语义且个人／项目记忆隔离；人精确选择模型与已有资源，内部 worktree 无需人管理。
- 同一角色多个明确工作流不会并发写同一个原生会话或工作目录；汇总状态与每项任务可解释。
- 新消息在忙碌、压缩、重启和 turn 结束边界不丢失，不送错工作流，不重复确认业务。
- 对指定成果进行评审；实现继续前进时旧评审不被套用；基线前移与合并冲突有完整解决流程。
- 两个项目同时集成同一目标分支时共享竞争检查；目标在用户脏工作区检出时，不覆盖修改或造成 HEAD／索引失配。
- 提交结果、更新目标分支、发送飞书消息之后立即崩溃，恢复能识别已发生与未确定的操作。
- 依赖等待、预算满额、角色离线后自动恢复，无需人类再次唤醒；依赖循环形成明确阻塞。
- 旧人工控制状态不会被新调度自动认领；任务暂停后不会提前释放仍被子进程使用的写入资源。
- 会话重建、换模型、记忆纠正后职责与任务连续；原生自动记忆不会把项目知识串到另一个项目。
- 迁移中用户改配置或进程崩溃不会产生双写；既有未提交文件与历史任务可以追溯。

任何一项未通过都应记录为实现缺口，继续修复对应 adapter、状态机或存储合同；不能改成“暂时只支持全队串行”来宣告完成。

## 10. 源码与官方资料

- [Raft workspaces](https://github.com/botiverse/raft-source/blob/main/packages/daemon/src/workspaces.ts)
- [Raft launch workspace](https://github.com/botiverse/raft-source/blob/main/packages/daemon/src/agentLaunchWorkspace.ts)
- [Raft process manager](https://github.com/botiverse/raft-source/blob/main/packages/daemon/src/agentProcessManager.ts)
- [Raft start coordinator](https://github.com/botiverse/raft-source/blob/main/packages/daemon/src/agentStartCoordinator.ts)
- [Raft Claude launch](https://github.com/botiverse/raft-source/blob/main/packages/daemon/src/drivers/claudeLaunch.ts)
- [Raft Codex instruction shape](https://github.com/botiverse/raft-source/blob/main/packages/daemon/src/drivers/codexInstructionShape.ts)
- [Raft OAR Codex launch](https://github.com/botiverse/raft-source/blob/main/packages/daemon/src/agentHost/oarCodexLaunch.ts)
- [Raft startup memory](https://github.com/botiverse/raft-source/blob/main/packages/daemon/src/startupMemoryBlock.ts)
- [Raft session recycle](https://github.com/botiverse/raft-source/blob/main/packages/daemon/src/wakeSessionRecycle.ts)
- [Raft task behavior](https://docs.raft.build/features/collaboration/tasks/)
- [Raft durable inbox](https://docs.raft.build/developers/sdk/external-agents/wake-and-status/)
