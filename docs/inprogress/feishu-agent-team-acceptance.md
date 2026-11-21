# 持久团队模式：验收范围和证据

> Type: `inprogress`
> Updated: `2026-11-21`
> Summary: 对照 60 项产品场景记录实际入口、自动验证和仍需真实环境观察的边界，避免把组件通过当作完整产品验收。

本文件对应[产品设计的 UAT 清单](../draft/feishu-agent-team-product-design.md#8-uat-场景与完成证据)和[实施账本](./feishu-agent-team-implementation.md)。自动测试使用真实 SQLite、真实协议适配器、受控 HTTP／进程对端及实际 Git 仓库；它们没有向生产飞书发送资料，也没有要求真实模型完成业务任务。表中“覆盖”表示对应程序约束已有可运行验证，不表示人已在飞书逐条完成验收。

## 场景到实现的映射

测试路径以下均相对 `internal/app/daemon/`；原生适配器和 wrapper 的测试另列完整仓库路径。

| 产品场景 | 实际入口与约束 | 主要验证位置 |
| --- | --- | --- |
| 01—04：角色、项目职责例外、团队与方法版本 | 真实个人来源修改全局职责；项目例外独立保存；团队可不依赖 Wiki；方法采用固定版本 | `workservice/management_lifecycle_test.go`、`workservice/knowledge_test.go`、`workstore/knowledge_boundary_review_test.go`、`workpractices/install_test.go` |
| 05—08：建立项目和中途失败 | SDK 原消息、项目建立 saga、实际成员核查、首任务一次采纳；创建未知不重做，旧选择回执不覆盖新绑定 | `workprovision/service_test.go`、`workprovision/recovery_test.go`、`workprovision/member_provision_test.go`、`workprovision/selection_test.go`、`workprovision/directory_test.go` |
| 09—10：同角色跨项目、同仓库并行 | 角色配置共用，scope/context/session 隔离；计算、进程、位置、任务分别占用；选定资源内登记独立执行位置 | `workstore/scope_review_test.go`、`workstore/ownership_review_test.go`、`workservice/execution_location_test.go`、`workengine/reserve_test.go` |
| 11—12：资料入口和平台限制 | 分别记录 Docx、Tab、Pin／顶部入口结果；不改人工入口，不把权限失败说成成功 | `workprovision/recovery_test.go`、`workprovision/service_test.go`、`internal/adapter/feishu/project_*_test.go` |
| 13—14：问答、持续工作和澄清 | 消息直接进入负责人上下文，按需建工作；模糊对象由负责人澄清，不设置跨人的“下一条消息”全局捕获 | `workingress/ingress_test.go`、`workingress/identity_test.go`、`workingress/control_selection_test.go`；模型是否恰当澄清仍需真实使用观察 |
| 15—17：交接、并发与重复 | 待办持久接收、真实停止后释放；异步有界准备和管理保留容量；依赖环拒绝；相同消息无新人工输入／任务版本时只产生一个协调提示 | `workengine/scheduler_test.go`、`workengine/preparation_capacity_test.go`、`workstore/transactions_test.go`、`workservice/message_repetition_test.go`、`internal/app/wrapper/work_pipeline_group_test.go` |
| 18—21：变更要求、进展、旧消息和专家 | 要求／责任版本独立，旧证据不可跨版本冒充；撤回保存实际事实；话题与提及路由按真实项目成员和消息映射 | `workstore/versions_test.go`、`workstore/dependency_generation_review_test.go`、`workingress/topic_test.go`、`workingress/recipient_membership_test.go`、`workstore/platform_observations_test.go` |
| 22—29：人的参与、冲突、评审和方法 | 参与约定与事项例外持久化；决定对象和保存版本冻结；转交不等于人工批准；方法采用不自动改变人参与规则 | `workservice/agreement_test.go`、`workservice/decision_requests_test.go`、`workservice/task_review_source_test.go`、`workstore/knowledge_boundary_review_test.go`、`workpractices/install_test.go` |
| 30—32：发送和成果保存 | 正式交付、通知、临时文件发送分别留收据；保存版本独立于 worktree 和预览缓存；未知通知不重复创建成果 | `workservice/resources_test.go`、`workpublish/publication_review_test.go`、`workpublish/ui_delivery_test.go`、`app_work_artifact_text_test.go` |
| 33—35：意见、版本和补读 | 意见与原成果版本绑定；新要求不隐式继承旧验收；评论读取有范围和完整度，不承诺飞书全量实时通知 | `workservice/resources_comments_test.go`、`workservice/resources_feishu_test.go`、`workstore/versions_test.go`、`workstore/knowledge_boundary_review_test.go` |
| 36—37：集成和人现有 checkout | Agent 执行 Git；平台只记录真实位置、资源和副作用，不自动 reset/clean；独立评审的 Git 采集禁外部 diff/textconv | `workservice/execution_location_test.go`、`workhost/review_diff_test.go`、`workstore/ownership_review_test.go`；具体冲突解决质量属于模型及项目验证 |
| 38—41：需求资料、原位编辑、事实边界 | SDK 附件实际展开；同一登记资源更新而不是每次复制；实际 Docx／Sheets 命令写入记录局部目标、前后观察与未知，独立只读核对不把未知写入改成成功；无法同版读取的表格标明观察数据集 | `workingress/attachments_test.go`、`workservice/instructions_attachments_test.go`、`workservice/resources_feishu_test.go`、`workservice/resources_docx_assets_test.go`、`workservice/resources_write_service_test.go`、`workservice/resources_feishu_edit_test.go`、`app_work_resource_recovery_test.go` |
| 42、45：记忆修订和失效 | 来源、范围、版本校验；实际资源新版本使精确旧观察来源失效；运行中的适用上下文收到一次纠正通知；新模型重新注入当前范围，不能拿本地全局历史替代项目记忆 | `workstore/memory_notices_test.go`、`workstore/memory_resource_sources_test.go`、`workstore/knowledge_test.go`、`workstore/versions_test.go`、`workservice/knowledge_test.go`、`workservice/instructions_test.go` |
| 43、46—48：群命令和私聊归属 | 群旧命令不启动旧引擎；个人按实际 principal＋role；导航冻结输入归属，显式分享才转项目；新版不提供原生人工接管 | `app_work_steer_pipeline_test.go`、`workingress/private_navigation_test.go`、`workingress/private_reply_test.go`、`app_work_private_reply_test.go`、`workingress/private_adopt_test.go`、`workingress/identity_test.go`、`workstore/personal_destination_test.go`、`app_work_pickers_domain_test.go` |
| 44、49、59：资料保护、移除成员、归档和交接 | 引用资料不随临时缓存清理；真实成员健康核查不自动拉回；归档保留历史，负责人／任务责任代次让旧写入失效 | `workprovision/membership_health_test.go`、`workservice/project_lifecycle_test.go`、`workservice/task_lifecycle_test.go`、`workstore/lifecycle_review_test.go` |
| 50、53—54：暂停、停止和迟到结果 | 原始业务队列与管理沟通分离；未知停止仍占用；取消不等于回滚外部副作用；关机等待准备和执行真实收敛 | `workingress/control_blocked_test.go`、`workengine/control_test.go`、`workruntime/recovery_test.go`、`app_work_shutdown_test.go`、`app_work_recovery_ui_test.go` |
| 51—52：模型设置、上下文和 Goal | 精确模型列表及设置快照；各 native 能力真实区分；Goal 预算和 continuation 绑定实际 Attempt；结果报告不自动完成任务 | `workruntime/goal_owner_test.go`、`workruntime/native_goal_source_test.go`、`workruntime/review_bridge_test.go`、`app_work_review_views_test.go`、`workservice/report_test.go`、`app_work_native_goal_domain_test.go`、`app_work_codex_upgrade_ui_test.go`、`workupgrade/coordinator_test.go` |
| 55—56、60：崩溃、未知外部写入和迁移 | SQLite 接单／执行／收据、wrapper journal、单权威切换；未知不重放；真实宿主归属和进程证据不足时隔离对应资源 | `workengine/recovery_test.go`、`workruntime/protocol_test.go`、`workprovision/recovery_test.go`、`workpublish/publisher_test.go`、`app_work_migration_capture_test.go`、`workmigration/` 与 `internal/app/wrapper/runtimejournal/` 测试 |
| 57：重复入站、回显和话题丢失 | SDK 多 Bot 合并、真实平台消息映射；原根撤回／首次发送未知时真实人点击新锚点，保留旧 unknown 和旧话题版本 | `workingress/ingress_test.go`、`workingress/topic_test.go`、`workpublish/reanchor_test.go`、`workpublish/topic_observation_test.go`、`workpublish/recovery_observation_test.go` |
| 58：旧控制和旧批准 | 旧人工状态仅迁移隔离；卡片按真实对象／代次重新核对；无领取／归还原生控制权入口 | `workruntime/historical_human_test.go`、`workruntime/request_delivery_review_test.go`、`workpublish/ui_delivery_test.go` |

## 本轮十二项复核回归

九项缺陷与三项设计缺口的逐项修复、实际生产接线和测试文件见[实施账本本轮记录](./feishu-agent-team-implementation.md#独立复核后的完整修复2026-11-21)。本轮补充验收重点：

- 暂停／归档必须经 daemon 周期真正收尾；暂停及归档后首页仍能刷新托管状态。
- 后端切换创建新执行身份，容量回滚可重试；池回收复用兼容原生会话，确证缺失且检查点可用时自动接回原待办。
- Hello 前失败核查实际进程退出；空闲重连核查原生、Goal、后台工作，不以网络重连直接放行。
- 冷项目中的私聊采纳和任务记忆更正必须真正可领取，旧输入修复有确定来源且幂等。
- 同条款换 ID 不能取得别人的约定修改权；无项目访问的人不能接收决定责任；新参与点覆盖全部未完成任务或明确排除。
- 修改待决定任务保存新要求，旧卡失效，新问题保持参与门禁直到当前决定完成。
- Tab／Pin 的移除可发现、明确恢复，未知创建不会换键重做；卡片在外部成员核查中失效也不能继续暂停项目。
- 团队首页只有一个原件，Drive／Wiki／已有首页及失败恢复均有入口；共享按当前原件和实际群明确记录，历史项目创建不授权未来资料。归档不删原件，访问撤销后需要明确重连。

## 完整执行链证据

`internal/app/wrapper/work_pipeline_integration_test.go` 在 Codex 与 Claude 后端运行真实 Engine→Host→Driver→WebSocket→受控原生子进程→CLI 业务服务→持久 journal→三域停止核查→原对话发布，覆盖首轮准入而不是绕开 Host 的组件拼接。`work_pipeline_group_test.go` 用同一链路完成负责人委派、专家处理和内部回复、负责人继续及群内结果发布；容量 1 验证交接无死锁，容量 2 以实际同时打开的 native turn 和 HTTP 屏障验证独立进程并行。

## 必须区分的证据层次

- 程序约束：存储事务、身份隔离、状态转换、队列与有限并发，可由自动测试验证。
- 实际协议：使用现有 SDK 和原生协议适配器构造／解析请求；受控对端可验证字段、错误和崩溃窗口，但不能替生产平台决定权限或配额。
- 实际进程与 Git：本机受控进程和临时 Git 仓库可验证身份、退出、目录及只读差异采集。OpenCode 本机缺少可运行二进制时，ACP 管道测试不能冒称真正 OpenCode 全栈通过。
- 模型行为：负责人是否合适地澄清、分工、采用方法、节省人注意力，以及代码质量，需真实工作负载观察。平台能固定事实和授权边界，不能把提示词等同确定性验收。

## 当前剩余核对

资料原位写入、评论、MCP、自动继续策略、上下文重建、精确追加输入及 Codex 程序维护已有实际入口和自动验证。未知资料写入的人决策出口、纯读取不阻塞归档、引用旧上下文的精确命令及旧任务负责人退出后的协调入口均已补齐自动回归。全仓测试、状态机审计与提交检查证据统一写入实施账本；分支提交与推送以 Git 状态为准。
