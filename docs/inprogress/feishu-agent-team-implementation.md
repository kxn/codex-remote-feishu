# Agent 团队完整实施计划与执行记录

> Type: `inprogress`
> Updated: `2026-11-21`
> Summary: 在专用分支实施完整团队产品，按依赖拆分任务，由轻量子 Agent 实施和交叉验证，主代理负责契约、整合与纠偏。

## 1. 范围与责任

最新整合证据：`TestSDKEngineHostDriverWrapperNativePipePipeline` 已在 Codex／Claude 两条后端通过实际 SDK 入站、Engine、Host、Driver、WebSocket、真实受控子进程、CLI 服务、journal、三域停止核查和原对话发送的整条链路。`TestSDKGroupLeaderExpertReplyNativePipeline` 在容量 1 和 2 下验证负责人→专家→负责人，无平台 Bot 回显依赖；容量 2 以实际协议与 HTTP 屏障证明两个独立进程同时处于原生执行中。测试不等于真实云模型或生产飞书验收。

私聊引用回复补齐接收时归属：实际原消息映射确定工作，不追随新导航；不明引用保存并提示，不能猜到当前工作。独立评审原文及失败通知另行持久展示，源工作暂停／忙碌不阻塞人阅读；回到原工作仅改变导航。完整后续检查与提交状态仍以下文和最终整合记录为准。

分支：`codex/feishu-agent-teams`；起点：`36b1e1e676effb72c32b1f2f5ca8c428ea00bb59`。本分支最终交付[产品阅读版](../draft/feishu-agent-team-product-overview.md)、[产品规格](../draft/feishu-agent-team-product-design.md)和[技术设计](../draft/feishu-agent-team-technical-design.md)定义的完整能力，不把中间基础包视作产品完成。

主代理制定接口和任务、审查实现、维护依赖、集成结果和收口。用户授权优先使用较轻量子 Agent 实施与验证；当前使用 `gpt-6.1-sol`。同一项高风险改动由另一工作上下文复核，作者单测不是独立验收。子代理不得自行提交、推送、重启本机服务或修改分配之外的文件。

常规实现取舍、错误实现、失败测试和接口调整由主代理处理。只有明显改变用户体验、既定产品边界或用户授权范围时，说明具体影响并请用户决定；可独立继续的工作不因此停下。

## 2. 分工与集成规则

共享工作区按明确文件／包分工。先确定公开类型和函数，再并行实现消费者；依赖不稳定时返回主代理协调，禁止为了通过测试另造一套类型或状态库。不得把其他代理的 same-task 改动回滚或混作自己完成的证据。

任务交付包含：改动文件、公开合同、focused tests、错误与恢复覆盖、未接线项。完成状态依次为待实施、实施中、待集成、待交叉验证、通过；需要真实外部环境的行为明确记录已验证与未验证内容。未接入生产调用的包只算组件完成。

合并进本分支的检查由主代理统一执行。涉及 native 协议／并发／持久化的工作按 full 处理；涉及远程执行／卡片状态机时同步 canonical 文档并审计退出路径。每次提交遵守 pre-commit 与 safe-push，保持一个可追踪的工作分支，不直接合并 master。

## 3. 完整任务拆分

| 任务 | 实现及边界 | 依赖 | 验收／交叉验证要求 | 当前状态 |
| --- | --- | --- | --- | --- |
| D01 领域合同 | core/work typed 身份、scope、context、session、attempt、任务、输入、操作、资料、约定；纯不变量 | 无 | 非法状态／scope／依赖环；供 store/service 直接使用 | 已实现；非法身份、版本、范围与依赖约束有自动验证 |
| D02 工作库骨架 | SQLite schema/version/FK/WAL、短事务、条件更新、数据目录与关闭 | D01 类型可并行协调 | 重开恢复、版本拒绝、事务回滚、并发唯一约束 | 已实现；真实 SQLite 重开、事务和并发验证通过 |
| D03 身份与范围仓储 | principal/role/bot代次/team/project/context/location、单群主绑定 | D01/D02 | 同角色两项目、两用户私聊、App重绑隔离 | 已实现；角色／个人／项目／机器人代次隔离已接入实际入口 |
| D04 输入与幂等仓储 | canonical Message、跨gateway观察、inbox、outbox、operation、request hash | D02/D03 | 重复接收／同key异正文拒绝、同事务无半写、崩溃重开 | 已接线；真实 SDK 入站、内部投递、实际发送回执进入持久队列 |
| D05 任务与协作服务 | 创建／委派／依赖／修订／结果／取消，来源与责任代次 | D03/D04 | 容量1交接、环拒绝、迟到结果不复活任务 | 已接线；负责人→专家→负责人完整进程链通过，继续核对边界 |
| D06 执行领取与调度 | attempt/input freeze、单session/carrier、计算额度与写位置分离、公平调度 | D04/D05 | 真并行、占用冲突、lease到期不盲复跑 | 已接线；容量 1 完成交接、容量 2 证明独立进程并行，准备和停止有界 |
| D07 原生目标严格关联 | Claude缺失目标拒绝，Codex start/resume错误关联 | 无 | 错目标不发当前session、合法fresh/resume/fork回归 | 已实现；Codex／Claude 严格目标及完整进程链通过 |
| D08 新运行协议 | owner/generation/launch/input/receipt合同与能力协商 | D01/D07 | 老wrapper拒绝承接不兼容工作、精确command关联 | 已接线；canonical dispatch envelope、宿主与 wrapper 实际握手通过 |
| D09 关键事件持久回执 | wrapper journal、daemon receipt、command去重与重连重放 | D02/D08 | 发送／ACK／崩溃窗口、重复terminal/request一次归并 | 已接线；真实 journal 与三域停止核查贯通，迟到与重放有回归 |
| D10 上下文和原生维护 | 指令组合、默认profile、desired/applied、cwd切换、compact/rebuild | D03/D08/D09 | Codex fast path、Claude launch snapshot、两后端实测 | 已接线；只读评审、MCP、compact／安全重建、精确 steer 与全池程序升级完成自动验证 |
| D11 运行控制与恢复 | pause/stop/continue、Goal bounded continuation、请求批准；按用户新决定取消原生人工接管 | D05/D06/D10 | 停止未知不释放 writer、旧批准失效、旧人工状态隔离 | 已接线；实际停止、Goal 预算、请求批准与恢复有验证；无原生人工接管 |
| D12 多身份CLI与API | registry/help/discover、session凭据＋显式run_ref、共同业务用例 | D03/D05/D08 | 跨scope拒绝、旧run拒绝、幂等异步操作持续收尾 | 已接线；多身份 CLI／HTTP 使用共同注册表和真实业务服务 |
| D13 记忆和方法 | 分层memory、来源失效、纠正删除、恢复摘要、版本方法包按需读 | D03/D12 | 两项目/个人隔离、默认profile可发现、方法≠结果 | 已接线；范围记忆、运行中纠正、精确来源版本失效及版本方法库完成 |
| D14 参与约定 | 自然语言用例、责任／汇报分开、现存决定转交、旧卡版本 | D05/D12 | 委托真转交、少通知不等批准、多方冲突 | 已接线；真实参与约定、版本决定及精确人类批准证据已验证 |
| D15 飞书原生入口 | doc/url Tab、Pin/top notice、已有成员邀请和查询的typed adapter | 无 | SDK请求、分页、错误、无隐式覆盖人工入口 | 已实现；真实 SDK 请求与受控响应验证，未向生产飞书写入 |
| D16 项目建立及入口维护 | bootstrap、分步provisioning、首任务一次、群绑定／入口恢复 | D03/D04/D12/D15 | 部分成功、未知创建结果、改目录迟到回调 | 已接线；建立、成员、未知恢复及真人中途修改目录通过，归档期间迟到回执有回归 |
| D17 资料连接器 | Docx/Wiki/Sheets、通用Bitable、评论、现有预览分离 | D02/D15可并行 | 局部更新、分页完整度、无强CAS假象、tenant限制替代 | 已接线；Docx／Wiki／Sheets／Bitable、typed 原位写入、评论及只读核对完成验证 |
| D18 资料注册与版本 | source identity、collection、操作对账、版本／观察数据集、引用保护 | D03/D04/D17 | 原位更新、并发冲突、事件遗漏补读、临时源清理 | 已接线；实际注册、不可变版本、观察范围和引用保护完成 |
| D19 显式成果交付 | artifact/review/integration/delivery、要求版本、通知outbox | D05/D12/D18 | 缺交付不假完成、保存与送达分开、旧结果不继承 | 已接线；版本化交付、真实发送、精确旧卡验收边界有回归 |
| D20 Agent Git与资源 | execution-location注册、原生Git指南、repo/ref协调和位置交接 | D06/D10/D19 | 同仓并行、用户脏目录保护、不另造worktree引擎 | 已接线；人选择现有资源，Agent 执行 Git 并登记位置，运行位置不偷换 |
| D21 群入站和投影 | 持久接收、话题、显式输出、bot回流去重、群退出旧slash | D04/D12/D16/D19 | 同消息多Bot一次、锚点失败不乱投、正文slash不执行 | 已接线；SDK 入站、去重、附件、话题、负责人自然语言与恢复入口完成 |
| D22 私聊迁入同内核 | 适用命令、个人scope、明确项目切换、导航不拥有执行 | D10/D11/D13/D21 | 原命令能力、/new范围、/stop保留项目回信、不泄露私聊 | 已接线并验证；引用旧工作时的命令精确作用于冻结上下文，不改变当前导航 |
| D23 设置和身份切换 | 抽设置用例、Bot配置单写、session控制宿主、App代次失效 | D03/D10/D12/D22 | 冻结执行不偷换配置、重绑保留旧资料但不越权继承 | 已接线；实际模型目录、冻结配置、身份代次与设置选择有回归 |
| D24 迁移与旧功能退出 | journal/capture/quiesce/import/cutover、旧writer fencing、Cron退役 | D02—D23 | 单权威、在途命令查证、回滚缓冲不丢、旧卡引导 | 已接线；启动捕获、迁移备份、单权威切换与旧入口隔离有崩溃回归 |
| D25 故障和生命周期闭环 | 归档重开、成员更换、无出口暂停、native/外部unknown对账 | D09/D11/D16/D24 | 不盲重跑、迟到结果不复活、独立项目不受牵连 | 已接线并验证；归档、成员调整、原生及话题恢复、资料未知写入的人决策出口完成 |
| D26 完整验收及收口 | 全部产品UAT、双native/飞书闭环、状态机审计、部署兼容 | D01—D25 | 组件/存储/协议/真实外部证据分别记录，剩余缺口不得报完成 | 自动整合验收通过；实际双后端／群协作进程链、全仓测试、双状态机审计及 Windows 构建完成；真实租户／模型验收未进行 |

## 4. 不允许通过缩减范围解决的困难

- 不保留旧单聊执行owner与新群owner两套永久内核；迁移桥必须退出。
- 不把持久接收等同模型已消费，不把native turn结束等同业务完成。
- 不以共享cwd或模型相同借用别人的原生会话，不以全局串行化躲避并发问题。
- 不用最终文本扫描替代显式交付，不让旧preview TTL删除正式资料。
- 不恢复旧Cron，也不把Agent Git操作改成人工必经步骤或平台worktree管理器。
- 不把UI卡片、CLI、MCP各自变成独立产品状态机。
- 不用缺少权限配置作为设计退化理由；真实环境不可用时保留完整接线并明确验证缺口。

## 5. 决策与证据日志

- 2026-11-21：用户批准专用分支、轻量子代理实施与验证、主代理自行处理常规调整；重大体验／设计变化再由用户决定。
- 2026-11-21：工作区干净，从 master 建立 `codex/feishu-agent-teams`。首批分工为领域合同、native目标错误修复、飞书项目入口adapter；主代理落实持久库和整合契约。

- 2026-11-21：用户明确取消新版原 VS Code 式原生会话接管；不做领取／归还及原生终端／桌面交接。飞书沟通、审阅、暂停继续保留，旧人工控制状态仅作迁移隔离。

## 6. 验证账本

尚未执行本分支完整产品验收。每项任务的实际测试命令、日志路径、复核结论和生产接线状态在此追加。设计阶段通过的测试不能作为本分支新能力的交付证据。

### 当前实现与独立复核

- 领域与工作库已经具备真实 SQLite 事务、跨句柄 CAS、收件箱、任务要求版本、指派代次、carrier／task／位置独立占用、项目成员和 principal＋role 个人隔离。普通与 race 测试已通过。
- 独立代理用红测试发现并推动修复：同 carrier 双领取、幂等操作悬空引用、删除旧要求后历史证据误挡完成、未满足依赖绕过完成、busy worker 偷换进程代次、个人范围接收其他角色不可执行任务。测试保存在 `ownership_review_test.go`、`lifecycle_review_test.go`、`scope_review_test.go`。
- 新发现跨范围知识发布不能仅靠给记录改一个 owner。正在补冻结正文版本、来源输入和目标范围的明确分享授权；不能把任意旧人类消息当作永久授权。
- wrapper 已实现真实 SQLite journal 和逐帧写入阶段，进程崩溃测试覆盖写前／写后未记账窗口；关键事件按 incarnation＋sequence＋hash 精确 ACK 并分页重放。transport accepted 不提升为 native running。
- 本地 CLI 与 HTTP 调用共同 service；会话凭据绑定身份，run_ref 明确传入。项目建立只回报已登记，未谎称成员、资料和入口已在飞书建立。
- Agent 处理报告单独持久化，暂停后可保存本轮事实，但报告不会自己释放执行占用或替用户验收。晚到输入不会被上一轮报告顺带清除。

本轮已留证日志包括 `/tmp/native-contract-packages-final.log`、`/tmp/work-execution-packages.log`、`/tmp/work-execution-race.log`、`/tmp/d09-packages.log`、`/tmp/d09-race.log`、`/tmp/work-review-all.log`。后者记录审查期间的历史红测，修复后的 fresh test 以当前测试运行结果为准。所有这些均为组件／本地协议证据；生产群闭环、迁移和完整 UAT 尚未完成。

### 持久执行与实际入口集成（2026-11-21）

- Codex、Claude、OpenCode 适配和 wrapper journal 已接入真正的 owner／执行代次协议。原生会话首次返回的 session ID 会绑定到对应 worker；原生完成不自动等同任务完成。Codex／Claude 的原生自动记忆按受管策略关闭，由产品提供有范围的记忆；OpenCode 的第三方插件记忆不宣称完全可控。
- 项目暂停后，业务输入保留，负责人可用隔离的管理上下文处理人的暂停／继续／参与约定。管理上下文不因项目暂停被同时禁用，也没有普通业务工具权限。
- 私聊仍拥有个人前台；查看项目不会把私人全文共享给项目。每条消息和按钮冻结当时的工作位置，迟到操作不会转向新选择。项目访问依据真实 SDK 身份／成员查询，过时消息不会恢复已经撤销的访问。
- 原生问题的表单、人的答复和发送操作持久化后才交给传输。发送结果未知不重复批准；同 ID 请求内容变化、Bot 重绑和执行代次变化都会阻止旧答复。反馈产生的后续人类输入与答复操作同事务保存。
- 项目入口、设置、私聊导航、原生问题均使用持久化 UI 视图及共用 outbox。设置继续使用原 BotCapabilitySettings 唯一来源；上下文偏好进入实际 launch snapshot。真实模型列表仍在接入，不能借用旧 surface 的缓存冒充。
- 主程序正在接入迁移捕获与单权威切换：启动时先原样备份旧文件，再冻结迁移清单，关闭旧调度／执行／Cron，再激活新 SDK 接收。历史选中的 native 会话只作为历史资料，不因其存在就虚构在途执行；确有未结束证据的旧执行隔离对应资源。旧系统从未持久保存的队列不能在重启后凭空重建，此限制保留在迁移证据中。

新鲜验证：`/tmp/domain-d22-race-verified.log` 的九个相关包 race 通过；`/tmp/native-handoff-full.log` 的十三个相关包通过；`/tmp/native-handoff-race.log` 的七个相关包 race 通过；`/tmp/migration-related-full.log` 的五个迁移相关包通过。Windows wrapper 和 macOS inventory 已交叉编译。原生问题、UI 回执及请求语义变化的定向测试在 `/tmp/team-control-ui-check.log`、`/tmp/team-native-request-review-fix.log`、`/tmp/team-form-followup-verified.log`、`/tmp/team-ui-return-routing.log` 留证。

这些仍不是完整产品验收：主程序切换后的端到端测试、完整私聊命令覆盖、附件展开、迁移历史取回、原生恢复入口、最终 UI／远程状态机审计以及全仓检查尚未全部完成。当前没有升级或重启本机正在运行的服务，也没有执行真实模型请求或向飞书写入生产资料。

### 入口、恢复与交付补齐（2026-11-21，继续实施）

- 启动入口已实际执行旧文件捕获、迁移冻结、单权威切换与新运行时启动；旧群调度、旧 Cron 和旧进程关机清理不会与新引擎并行写入。正常关机先停止调度，保留 relay 回执直到精确停止核查结束；未知执行保持占用。
- 原生模型目录通过当前工作会话实际查询并按设置／会话代次缓存；菜单事件使用真实私聊发送回执。私聊已有工作与历史加入分页，导航不改变已收到输入的归属。
- 附件保存实际字节和校验值；撤回先于原消息到达也保留事实。未执行输入可以取消，已执行部分保留证据并提醒核查，不宣称撤销外部副作用。关联记忆不再继续使用已撤回消息作为有效来源。
- Runtime gate 有独立状态投影与实际恢复入口；预算耗尽明确显示任务未完成。恢复证据后来到达时会更新可用按钮，gate 清除或转入重建后旧恢复按钮退役。
- 导航原位更新仅复用当前实际操作、已确认发布的消息。未知 PATCH 不自动重放，避免旧内容覆盖后来界面。新增消息、决定结果和正式交付仍可独立保留。
- `decision.request` 已通过 typed service、真实人回调与保存版本绑定进入业务评审流程；人仍可用自然语言指导。决定不自动等同任务完成。资料首页正在索引真实登记资料和已保存交付；保存版本回看与外部操作未知结果的恢复入口继续补齐。
- 会话 Goal 的明确预算保留；没有声明时，`work.defaultGoalTokenBudget` 默认 100000，补全经过实际原生控制回执。已耗尽目标不复制到恢复后的新 carrier。平台 `session.goal` 使用当前实际执行身份，不能越过恢复 gate。
- 用户已取消原生人工接管，相关产品／技术文档和验收场景已同步。当前继续清理暴露接口，历史人工控制记录仍需隔离核查。

新鲜验证：`go test ./...` 已于本轮通过，日志 `/tmp/team-full-integration-current.log`。这次全仓结果之后仍有新增集成改动，不能作为最终提交的唯一证据。专项通过包括 `/tmp/team-inline-patch-verified.log`、`/tmp/team-private-destination-regression.log`、`/tmp/team-recovery-presentation.log`、`/tmp/team-root-wired-check-next.log`、`/tmp/team-ui-core-full.log`。原位更新的首次红测暴露平台消息映射不可被新操作覆盖，现保持原始映射并单独保存新视图回执；旧协议相关断言没有删除。

仍需完成：自然语言管理及保留容量的最终交叉复核、全部私聊命令最终复核、未知外部操作恢复的人类入口、历史宿主归属与资源隔离收敛、资料媒体的保存完整性、跨用户故事验收、最终 canonical guardrail／pre-commit，以及提交和推送。未运行真实模型业务请求，未向生产飞书写入资料，未升级或重启本机服务。


### 最后接线与异常回路复核（2026-11-21）

- 新版原生人工接管已取消；仅保留旧人工状态的迁移隔离和核查。技术用例清单中的旧 RequestControl 名称也已移除。
- 独立只读 `/review` 冻结范围和目录，真实 Git 差异采集、native 只读配置、原样结果证据和回到源会话的桥已接；目录未选择时保存请求并在真实选择后继续。独立评审不执行普通业务 CLI，也不替人验收任务。
- 私聊命令与入站处理分为独立通道；真人卡片动作每群保持持久接收顺序、最多四群并行。准备执行有界且关机必须先停止准备，再核查实际进程。
- 非 Codex `/compact` 已接 Host.RequestCompact/RunCompactions，明确采用实际停止后重建原生历史的语义；保留记忆、检查点和旧会话。Codex 保留真实 compact 协议。
- 正式报告增加显式 `work_disposition`。`complete` 仅描述 Agent 报告，不会完成任务或释放仍在运行的执行；自动检查只接受明确 `unfinished` 的已授权工作。
- 相同消息反复交接增加持久事实保护：同发送／接收／会话／正文／任务版本、没有新人工输入时，第三次返回 `needs_coordination`，只唤醒负责人协调一次。不会把不同技术意见按文本相似度拦截。
- `publication.recovery` 实际入口已接。真实原卡回调＋同 Bot GetMessage 可补发布回执；纯文本不凭相同正文认领。首次话题发送未知或实际根消息撤回时，人可选择恢复卡为新根；旧发送状态、原消息映射和历史话题内容保留，旧冻结请求禁止再发。恢复卡自身未知不递归制造更多通知。
- [验收映射](./feishu-agent-team-acceptance.md)已覆盖全部产品 UAT 编号，同时说明自动测试、实际协议、真实模型体验三类证据不能互相替代。

新鲜 root 证据：`/tmp/team-root-ui-domain-current.log` 的 daemon/service/publish/ui 全包通过；`/tmp/team-root-current-race.log` 的定向 UI、重锚、重复交接、报告边界 race 检查通过；`/tmp/team-topic-reanchor-recall.log` 包含真实人点击、原 unknown 保留、迟到旧回执拒绝和 Bot 根消息撤回后的话题迁移；文档规范与文件长度检查通过。最新全仓检查 `/tmp/team-full-integration-next.log` 曾发现恢复测试伪造 identity evidence，已交由负责代理改为真实持久 SDK 来源并定向通过，最终全仓检查尚需在所有集成完成后重跑。


### 用户追加决定及完整调用链审查（2026-11-21）

用户明确要求完全删除 `/bendtomywill`。已删除 parser/catalog/menu/action、daemon flow/transaction、rollout 改写及回滚存储和对应专属测试；旧设计转入 obsoleted。`internal`、`cmd`、`scripts` 已无该功能符号。新鲜定向验证 `/tmp/team-remove-turn-patch-second.log` 通过，没有保留 hidden alias。

独立审查还发现旧 standalone Codex 程序升级只协调 legacy 实例，不能协调新执行池。已在实际 start 入口增加新权威栅栏，保留真实版本查询但明确本次未更新；daemon 自身升级仍走现有持久升级流程。随后已实现独立的全池维护事务和飞书确认／进度／结果入口：真实停止、受管安装进程、目标版本固定、未知结果只读恢复及旧迁移执行隔离均有自动回归；不再通过旧实例总编排执行更新。

完整 Engine→Host→Driver→Coordinator 测试发现两处此前被分包替身遮住的接线错误：Host.Plan 在 Attempt 尚未准入时调用了要求已保存 Attempt 的输入构造接口；native 注册器没有读取 engine 实际保存的 dispatch envelope。这两处已分别以显式 pre-admit 验证及严格 canonical command reader 修复，随后 Codex／Claude 私聊与负责人→专家→负责人群协作整条进程链通过。前述分包绿色结果不被用于替代完整调用链验证。

运行中记忆更正现有同事务、幂等的定向通知：只给实际可读范围中的普通工作上下文，不复制原文，不假称已经清除 native 历史。对应 race 证据见 `/tmp/domain-memory-correction-race-2.log`。

### 后续真实入口和界面审计（2026-11-21）

- 私聊引用原消息的归属与未知引用提示已通过 `/tmp/team-private-reply-race-second.log`；原生历史不被改写，导航不随回复自动改变。
- 卡片业务动作必须有非空精确消息回执；unknown 发送的点击先只读核对，再决定是否允许操作。收紧后 `go test ./internal/app/daemon/...` 通过 `/tmp/team-strict-view-receipt-tests.log`；实际 unknown→读证→原卡可用回归见 `/tmp/team-ui-receipt-authority-race.log`。
- 旧成果卡的自然语言批准必须绑定原 SDK reply、精确 submission／要求／责任版本及允许的真人，不能由模型把 V1 的“可以”记到 V2；角色评审保持独立。对应 `/tmp/team-human-review-final-race.log`。

Codex 维护入口新鲜 UI 验证见 `/tmp/team-upgrade-ui-tests-second.log` 与 `/tmp/team-root-final-ui-race-second.log`；安装与崩溃核查见 `/tmp/native-upgrade-final-frozen-installer-race.log`，Windows wrapper／workhost／workupgrade 与 Darwin inventory 交叉构建通过。完整执行链现已通过实际 `teamcli.Run` 参数解析、原生子进程环境凭据读取和 Client，而不只是直接构造 HTTP 请求；见 `/tmp/native-real-cli-private-group-pipeline.log`。

### 最终整合检查（2026-11-21）

`go test ./...` 在 `/tmp/team-final-frozen-all-tests.log` 完整通过；`GOOS=windows GOARCH=amd64 go build -o /tmp/fschannel-team-windows.exe ./cmd/codex-remote` 在 `/tmp/team-final-frozen-windows-build.log` 通过。领域侧 12 包完整 race 见 `/tmp/domain-quoted-final-owned-race.log`，旧任务负责人退出的最终 ingress 整包 race 见 `/tmp/domain-owner-exit-final-ingress-race.log`，native 普通／race 与跨平台记录见上文。

双状态机审计发现并修复了未确认卡片收据仍能触发动作、引用旧工作命令停错新工作、旧任务负责人退出后输入无出口，以及未知资料写入永久阻止归档的问题。资料恢复必须经过实际读取、精确真人确认和独立审计；原操作仍为 unknown，禁止重放，迟到回执另存。纯读取的历史发送状态不会伪造完成，也不再阻止归档。

代码验证没有向生产飞书写入、调用真实云模型完成业务或升级本机服务。真实租户权限、跨空间资料移动的最终平台结果、客户端呈现以及模型长期协作质量仍需实际使用验收；这些限制不能被受控端到端测试替代。

资料恢复最终六包完整普通／race 验证分别见 `/tmp/team-resource-recovery-final-six-normal.log` 与 `/tmp/team-resource-recovery-final-six-race.log`。它们覆盖四类写入的实际 UI→读取→真人确认→归档、伪造或过期证明拒绝、正常在途调用不出恢复卡、重开与迟到结果、纯读遗留以及飞书卡片大小上限。全部子 Agent 已停止写入，主代理对冻结代码再次运行全仓测试和完整 Windows 构建，均通过。

提交检查使用 `scripts/check/pre-commit.sh`；其有效树哈希保存在仓库 Git 元数据的 `codex-remote/pre-commit-tree`，仅对同一提交树有效。提交和推送按 `safe-push` 执行，最终提交标识与远端同步状态以 Git 分支为准。
