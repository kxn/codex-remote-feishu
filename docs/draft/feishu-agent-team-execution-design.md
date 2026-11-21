# 飞书 Agent 团队完整执行与工作区设计

> Type: `draft`
> Updated: `2026-11-21`
> Summary: 单聊与项目群统一到有明确作用域的运行内核，补齐个人记忆、精确资源选择及内部工作区自动管理边界。

## 1. 目标与证据边界

本设计落实用户要求的完整 Raft 式协作：稳定角色自主分工、并行工作、直接交流、互审、持续跟进、跨项目记忆与故障恢复。实现可以按依赖顺序推进，交付目标不能退化成“群中多个独立聊天机器人”或“共享目录轮流运行”。

Raft 式群工作是最高产品优先级。单聊也迁入同一运行层，获得角色记忆和持续工作能力；旧群交互可整体替换。单聊／群的差异放在作用域、业务组织与展示，不永久维护两套执行内核。已有资产及替换面见 [代码审计](./feishu-agent-codebase-reuse-assessment.md)。

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

未找到足够源码证据证明 Raft 自动为每个任务创建 Git worktree 并自动合并。下面的独立执行目录、版本审查与集成流程，是我们为支持可靠并行开发作出的设计，不冒充 Raft 的已证实实现。

## 2. 对象与唯一事实所有权

| 对象 | 所有者与作用 | 不等同于 |
| --- | --- | --- |
| Role | 稳定同事身份、通用职责，引用 Bot 统一模型配置 | 模型名称、进程或单个项目 |
| RoleHome | 角色持久资料、通用记忆索引和受控运行资料 | 项目源码目录；OS 安全沙箱 |
| WorkScope / PersonalScope | personal/project 作用域；个人范围关联稳定用户身份与角色 | 最近发言人、裸 gateway 配置槽位 |
| Project | 目标、成员、需求、共享知识、资源关联及调度策略 | chat ID 或 cwd |
| ProjectChannelBinding | 项目与主群／其他讨论入口的关联和绑定代次 | 项目本身 |
| ProjectResource | repository、文档空间或其他项目资源的稳定引用及访问策略 | 某个机器上的路径 |
| Task / Workstream | 工作责任、依赖、验收、需求版本和成果 | 一次 turn |
| ExecutionWorkspace | 工作流使用的 checkout／worktree／文件快照，含基线、位置、租约和保留状态 | 整个项目的唯一目录 |
| WorkContext / WorkerSession | 工作流恢复上下文与实际 backend session；绑定 role、scope、execution workspace；project scope 再关联项目 | 飞书 surface 或话题 |
| ExecutionAttempt | 一次启动／执行尝试、输入范围、冻结版本与结果证据 | Task 的最终状态 |
| Artifact / Review / Integration | 不可变成果、针对其版本的评审、进入目标基线的集成记录 | 最近一条消息或可变路径 |

协作库是团队模式 Project、资源、工作区、调度策略与任务的权威写源。Bot 模型配置仍以已有 Bot capability store 为唯一写源；不能为了协作再复制一套可独立修改的模型配置。

该库同时保存个人 WorkScope、记忆与执行事实，是统一工作存储。Bot backend/model/profile 的默认策略共享，headless／本地接管等执行控制属于具体 session；私聊选择本地控制不能改变同 Bot 的所有项目执行方式。

执行 owner 最终统一为 WorkerSession，单聊与项目均使用它。legacy surface owner 仅作为迁移过渡。现有 `AttachedInstanceID`、`SelectedThreadID` 和 `ActiveQueueItemID` 不能继续承担执行权威；queue、claim、approval、pending launch 与原生 turn 统一关联真正的执行 owner，surface 仅保存交互关联与读模型。

群默认承接一个明确项目；项目可以关联多个沟通入口和多个资源。群换名字、换主群、项目换本地目录，都不改变 ProjectID。绑定保存 revision，消息与卡片保存 ProjectID／TaskID／ConversationID，不靠当前群设置回推过去成果的归属。

多个项目可以关联同一个 RepositoryID，并使用各自工作区与上下文。不能把“仓库相同”判断成“同一个项目”，也不能用“不同群抢同一目录”作为拒绝两个项目协作的理由。

## 3. 角色、会话与并发

一个角色保持一套身份与通用职责，各项目有独立工作上下文。一个项目内的协调、实现、评审等工作流可以有明确绑定的多个 WorkerSession；是否新建会话由上下文和写入资源决定，不默认将所有任务塞入一个无限增长的会话。

个人对话也有 PersonalScope、WorkContext、WorkerSession 和 Attempt，但不要求每条消息都有 Task。个人与项目使用相同启动、投递、记忆、维护和恢复合同；每次读取与更新都保留真实 scope。私聊选择某项目时附加明确项目上下文，不改变个人记忆 owner，不将私人 transcript 自动交给团队。

每个原生会话一次只有一个活动 turn，保证原生协议和输入顺序；不同角色／工作流／项目的独立会话可以并行。多个会话仍以同一个角色身份出现，项目中显示其汇总忙碌状态与具体任务，不把每个会话变成一个新的飞书 Bot。

WorkContext 在会话重建后保留，WorkerSession 则记录具体 runtime session 与代次；不能把旧 thread ID 换掉就丢失工作流的恢复索引。上下文隔离是信息归属规则，不等于同一 OS 用户下的严格文件安全隔离；强隔离需由宿主的进程／文件访问边界实施。

Task 同时只有一个负责推进的 owner；多个角色协作拆成关联工作项。WorkerSession 取得执行资格不自动取得任务所有权。转交 owner 时更新 assignment generation，旧执行的成果可以归档，但不能继续修改新 owner 的任务状态。

调度分别管理 host/provider 执行预算、启动并发、项目预算、角色预算和资源写入租约。达到预算时待办持久等待，不拒绝任务登记。公平队列防止一个项目持续占满全部角色，依赖结果与明确用户 steering 可以优先处理，但不能违反同一会话单 turn 合同。

等待依赖的工作释放模型执行名额；它的工作区、检查点、责任关系仍保留。依赖完成后生成 inbox 事件，恢复原工作流或用持久检查点建立替代会话。不存在通过 sleep 保持模型一直在线才能继续的要求。

## 4. 并行开发与成果集成

### 4.1 独立工作区

Git 项目为独立写工作流准备独立分支与 checkout/worktree，保存 RepositoryID、base commit、branch、实际路径、host 和 workspace generation。相同工作流的重试与恢复复用其目录；不是每次 turn 都新建 worktree。

创建工作区采用可恢复 operation：登记意图、创建目录、验证基线与归属、发布 ready；中途失败按记录核对，不因路径已存在就拿来复用，也不覆盖用户未提交改动。使用现有仓库、远程仓库或多个仓库的项目均通过 ProjectResource 明确关联；多仓库集成分别记录结果，不声称跨仓库提交原子完成。

原有用户目录可作为导入来源或明确选择的执行资源，不默认成为所有角色的写入地点。角色个人记忆与 runtime 状态留在 RoleHome／会话专属存储；代码执行 cwd 指向具体 ExecutionWorkspace，继续读取该项目的 AGENTS.md／CLAUDE.md 等规则。

人精确选择已有目录／仓库，Agent 不替人猜这个选择。选定之后，Agent 根据任务申请内部工作区，运行层管理基线、分支、路径和生命周期；用户只需关心任务与成果，不需要填写 worktree 参数。现有 Git 创建、Cron 隔离构件可以复用，但人工 picker 流程及强制清理语义不成为新合同。

非 Git 文件使用工作副本和带版本的成果交接。确实不能复制的共享服务、设备、端口或数据库操作需要声明资源租约；只对冲突资源排队，不能因此串行化整个团队。

### 4.2 交付与审查

实现角色提交不可变成果引用，例如 commit、文件版本或文档 revision。测试与评审角色在指定成果上工作，使用独立评审目录／快照，不能一边验收一边跟着实现目录变化。

评审保存 task revision、artifact revision、检查依据和结论。需求或成果改变时，已有评审标明对应旧版本；重新评审范围由变更决定，不能把旧的通过状态直接继承给新成果。需要修复时创建明确反馈和工作项，修改角色产生新成果版本，再通知相关评审者。

### 4.3 集成与冲突

Project 为目标分支指定 integration owner；它可以是负责人，也可以是明确的集成角色。不同实现工作流并行，只有更新同一仓库目标分支的短集成操作按目标基线协调。

Integration 保存期望的 target head、候选成果、评审和测试版本。先在独立集成目录组合并验证，再以期望版本更新目标引用；目标已经前移时重新计算集成结果，不直接覆盖。

集成协调键为 RepositoryID＋target ref，覆盖多个项目。自动候选先落在系统拥有的集成分支／checkout；目标分支正在被用户目录检出时，须通过该工作区的安全整合操作更新，保留未提交修改，禁止仅通过移动 ref 使 HEAD、索引与工作树失配。不能安全整合时保留已验证候选并显示具体阻塞；其他独立开发继续。不能以 Project 各自加锁替代仓库目标引用的竞争检查。

冲突进入需解决状态，附冲突基线和候选成果，交给指定 owner 修复。解决后对新集成版本补齐适用的检查。任务完成规则区分“实现已交付”“评审通过”“已集成”“用户已验收”，按照任务约定推进；不能把 Git 无冲突等同于产品验收完成。

合并、推送与发布遵守已有用户授权和仓库工作流。设计不会把每次内部交接变成额外的人类确认，也不会把用户验收解释成新的发布授权。

### 4.4 工作区收尾

工作区状态包括 provisioning、ready、in_use、retained、cleanup_pending 和 removed。只有任务、运行中子进程、待评审／集成、恢复引用都不再依赖它时，才可执行清理。未提交修改、失败证据和未交付成果先保存；清理可重试，不能通过删除目录掩盖未解决的状态。

## 5. 交流、唤醒与需求变化

消息先持久化，再投递执行；飞书公开消息与内部工作事件由同一操作派生，平台回流只核对送达，避免第二次执行。FYI、问答、任务交接、评审请求、需求变更分别携带意图和稳定关联。

空闲会话收到可行动事件时调度启动；忙碌会话按 backend 能力进行 steer／输入通知，遇到压缩、恢复或不支持中途输入的边界时持久暂存。投递成功、模型读到、业务处理完成是三个不同状态，不能在写入 stdin 后就确认任务完成。

Claude adapter 负责 stream-json 输入与原生排队的对应证据；Codex adapter 负责 turn/steer 以及目标 turn 已结束时的重新入队。通知和输入都关联 session generation、turn 和 inbox IDs，不能把迟到输入打进下一项无关任务。

改变需求时增加 task revision，通知当前 owner 和受影响依赖。按影响决定 steering、请求中断或下一轮处理；旧版本成果保留且明确标旧。紧急暂停先关闭新执行准入，再请求停止活动执行，确认原生进程及资源状态后释放租约。

任务依赖图拒绝自依赖和环。阻塞有具体原因、负责解除者和关联对象；循环互相提问或多轮无进展时形成可见阻塞，交给负责人判断，而不是继续自动互相 @。

提醒、消息、任务变化都进入同一持久待办机制。负责人离线、执行超预算或系统重启时不会丢失；恢复不依赖人再发一句话。

## 6. 原生上下文与长期记忆

原生 runtime 保留自己的会话和压缩机制；我们保存角色、项目、任务和成果的持久事实，并负责建立／恢复会话所需的材料。角色身份不依赖某个 runtime session 永远存在。

ContextAssembler 统一处理 fresh、resume、compact 和 rebuild，而非在各个飞书消息入口零散拼接。BackendAdapter 明确提供 Prepare、StartOrResume、Deliver、Observe、Maintain、Recover、Stop 生命周期；不具备某个即时操作时采用持久排队或受控重建这一确定分支。

每次输入记录实际送入的 task revision、inbox IDs、角色说明版本和记忆版本。新建／重建会话读取有预算的启动材料：职责、项目入口、正在处理的任务、检查点、未消费信息与相关记忆；详细历史按需读取，不把整个群历史塞进提示词。

在任务交接、交付、长执行检查点和会话回收前保存恢复资料；后台回收不能只指望模型最后一次成功写笔记。保存失败不确认资料已持久化，不删除唯一可恢复会话。崩溃时从此前已提交的检查点和事件恢复，未证实的副作用进入核查。

Claude／Codex 的 standing instruction 分别通过已有附加系统说明／developer instruction 通道组合，保留原生基础指令。角色规则变更和记忆纠正记录版本，在相关会话安全边界更新；必须即时纠正的任务输入通过 steering 送达，必要时重建上下文。

记录 desired/applied instruction revision，字段发送成功和 hash 一致不等于新规则已经生效。Raft 的 Codex 接入也记录恢复会话更新说明可能延迟生效；我们不能可靠刷新时，保存检查点并建立 fresh session。Claude fresh session 出现 ID 也不直接标为可恢复，需要原生可靠落盘／turn 结束等证据。启动 ready 同时覆盖身份、会话与所需工具可用性，失败保留待办并给出具体运行错误。

compact 属于维护状态：按原生能力执行，有明确触发条件、单次执行和重试边界。维护期间保存新消息，完成后继续投递，不触发业务任务完成；存在待投递工作、未结束子进程或不安全边界时推迟维护。压缩失败保留原会话与检查点，不重复盲目发送命令。

原生自动记忆必须明确作用域：能按角色与项目隔离就配置隔离；否则关闭重复自动写入，让团队记忆服务承担持久知识。隔离包括 runtime state、自动记忆与配置落点，不能仅靠项目提示词。模型服务认证沿用受管理凭据，不通过覆盖全局 HOME 搬走用户配置。

不同工作流更新同一项目记忆时通过 revision 检查；通用知识提升保存来源和适用范围。纠错与撤销使索引、后续启动材料和订阅该知识的活动工作流一起收到更新，不能只删除数据库一行就声称活动模型已经忘记。

个人记忆绑定稳定用户、角色及个人 scope，记录来源应用身份与 user ID 类型，不只使用可重绑的 gateway 槽位。App 重绑时旧资料保留并隔离，只有明确身份映射才继续关联。个人偏好不自动写入 Role 全局记忆，私聊讨论项目也不改变该边界。

## 7. 执行宿主与人类接管

后台托管 worker 是持续工作所需的执行宿主，与用户打开哪个界面无关。飞书、桌面和编辑器是查看、指导、接管工作的入口，surface 不再拥有唯一的执行身份。

已有手动会话可以观察、附着或在明确交接后绑定团队工作流。一个 session 的控制权只能由自动 dispatcher 或人工控制者持有；人工接管期间暂停自动派发，后台信息仍进入收件箱。归还时提交检查点、工作区与当前任务版本，再恢复自动调度。

用户操作的本地会话不能被后台悄悄借用；后台会话也不能只因 cwd 和模型相同就被替换。绑定须核对 role、scope/context、workstream、workspace generation、instruction version、backend 和 controller lease；personal scope 核对稳定 principal，project scope 核对项目绑定。

不具备受控身份或生命周期接口的外部 Bot 可以作为群参与者；要成为可调度 worker，必须实现身份、inbox/ack、执行状态和结果回执合同。这是执行能力边界，不由群里显示一个 Bot 名字自动获得。

## 8. 持久恢复与迁移

协作库原子记录任务变更、inbox、outbox、执行尝试、租约和幂等结果。外部平台发送、工作区创建、Git 操作和进程启动均在事务外执行，并以持久 operation 关联回执。

重启对 queued、claimed、starting、running、waiting、integrating 等状态逐项恢复。lease 到期只表示需要核查，不能无证据重启第二个写进程；平台发送状态不明时也不能重复派发业务。稳定 ID、generation 和不可变成果引用贯穿整个链。

旧群切换团队模式使用有 journal 的显式迁移：

1. 记录旧 room/surface/config 的快照与预期 generation，冻结该群新的旧式工作区写入；已有执行排空或经明确接管转交。
2. 导入 Project、ChannelBinding、ProjectResource 和调度策略，保存旧目录与未完成执行的归属。原用户 checkout 保留，不强制 reset／clean。
3. 准备角色身份、独立工作区及 session 绑定，核对可恢复性和平台成员状态。
4. 提交 mode/authority generation，工作库接管该群项目配置；旧群 `/workspace`、primary、并发菜单退出主流程，残留卡只提示已迁移／打开当前项目，不继续旧写入。
5. 切换前失败可回退旧模式；切换后已有新任务时只能按 journal 前滚修复，不能把旧 JSON 重新覆盖回去。

旧模式群的 room JSON 仍是旧模式权威；团队模式的旧记录只作迁移快照或只读兼容投影。禁止两个存储同时独立写入群目录／并发真相。历史消息和旧卡按记录的代次访问当前事实，不触发第二次迁移或覆盖新配置。

上述旧模式仅是迁移期间状态。单聊同样导入 PersonalScope、WorkContext 和 WorkerSession，将旧 native thread／cwd 作为有来源关联，不将历史全文自动当作记忆。每个 scope 切换后，旧 surface queue／resume 不再是执行写源；最终单聊和项目只有统一内核。旧群 UX 无需兼容，迁移记录用于数据连续性而非保留旧交互。

显式用户设置的资源预算在迁移时保留并说明；旧实现默认值不能被误称为新产品只能串行的结构限制。并行能力必须按可用预算工作，容量为 1 时验证的是不丢任务和不死锁，而非完整交付的替代条件。

## 9. 完整目标的交付证据

以下是实现必须交付的行为与证据，不是用来缩减目标的可选研究：

- 同一项目中两个角色同时实施独立子任务，第三个角色可以讨论／评审，负责人仍能接受新要求。
- 同一角色参与两个项目、同一仓库关联两个项目时，会话、记忆、成果和消息归属清楚。
- 同 Bot 的多个用户私聊与项目群同时工作，使用同一运行机制但个人／项目记忆隔离；人精确选择模型与已有资源，内部 worktree 无需人管理。
- 同一角色多个明确工作流不会并发写同一个原生会话或工作目录；汇总状态与每项任务可解释。
- 新消息在忙碌、压缩、重启和 turn 结束边界不丢失，不送错工作流，不重复确认业务。
- 对指定成果进行评审；实现继续前进时旧评审不被套用；基线前移与合并冲突有完整解决流程。
- 两个项目同时集成同一目标分支时共享竞争检查；目标在用户脏工作区检出时，不覆盖修改或造成 HEAD／索引失配。
- 提交结果、更新目标分支、发送飞书消息之后立即崩溃，恢复能识别已发生与未确定的操作。
- 等待、提醒、预算满额、角色离线后自动恢复，无需人类再次唤醒；依赖循环形成明确阻塞。
- 人工接管与归还期间没有双重控制；任务暂停后不会提前释放仍被子进程使用的写入资源。
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
