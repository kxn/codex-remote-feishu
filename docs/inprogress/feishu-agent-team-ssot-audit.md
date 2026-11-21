# 持久团队改版后的状态所有权与旧流程审计

> Type: `inprogress`
> Updated: `2026-11-22`
> Summary: 记录团队改版后的唯一事实来源、实际发现的旧流程与配置脱节、修复边界及验证证据。

## 审计范围与结论

这次检查覆盖生产启动、持久工作调度、原生启动配置、Bot 身份替换、状态接口、飞书发布菜单、安装与清理入口以及读者文档。问题不只是旧代码还在：有些旧状态仍有写入路径，有些新设置没有进入实际执行，还有些入口向用户宣传已经退役的行为。

修复以维持一套执行事实为准。群聊仍由负责人组织团队，私聊保留有用的工作控制。没有重建旧 primary/coworker 群模式，也没有恢复 Cron、VS Code 会话接管或历史改写命令。复用的原生适配器、配置目录、凭据存储和迁移材料继续保留。

产品定义见 [面向人的产品说明](../draft/feishu-agent-team-product-overview.md)。当前执行规则见 [运行时状态机第 0 节](../general/remote-surface-state-machine.md) 和 [卡片状态机第 0 节](../general/feishu-card-ui-state-machine.md)。本记录提供审计证据，不定义第二套产品行为。

## 各类事实由谁负责

| 事实 | 唯一写入归属 | 其他位置的作用 |
| --- | --- | --- |
| 项目、工作上下文、输入、任务、执行与占用 | workstore 的 SQLite 事务 | 飞书群、卡片和状态接口只呈现结果 |
| Bot 当前全局后端、Profile、模型与推理选择 | 既有持久 Bot settings 与配置目录 | SQLite Role 保存引用和变更版本，不复制可编辑凭据 |
| Profile 的上下文偏好 | profile context preference store | 参与有效配置计算与角色版本，不能只影响设置页 |
| 当前工作 access/plan 偏好 | SQLite WorkContext preferences | 原生启动快照记录当次值，旧 Bot/surface 不再补默认值 |
| 已开始 Worker 的实际配置 | 冻结的 LaunchRecord 与原生回执 | 后续设置不重写在途历史；在安全边界为新执行重建 Worker |
| Bot 身份代次 | 身份状态与绑定变更事务 | 老 App 的地址与旧执行不能自动借新 App 继续发送 |
| 原生会话与后台执行状态 | 对应 backend 的真实协议回执 | 不能用旧 surface 的 selected thread 推断当前团队执行 |
| 可发布飞书菜单及 key 解释 | `internal/feishuapp/team_menu.go` | 发布 manifest 和 daemon 分发共用此表；部署模板有一致性测试 |
| 运行状态接口 | 同一 SQLite 快照，加真实网关状态 | 旧 orchestrator 不作为团队状态的备用数据源 |
| 旧 room/surface、Claude workspace、Goal interlock 文件 | 历史原件与迁移快照 | 可解释和审计，不能启动后顺手规范化或继续写入 |
| 安装与旧集成清理 | 安装记录及真实本实例所有权证据 | 不再安装旧接管 shim；旧路径指针不能授权删除其他实例文件 |

角色设置引用是可重新生成的投影。配置更改必须在执行准入前同步投影并校验，不能把后台定时同步当作正确性的前提。上下文偏好在原生启动占位和实际任务准入事务中都校验版本。

## 查到并处理的问题

### 设置保存与实际执行不一致

Claude 上下文偏好原先不参与团队角色指纹，已有 Worker 会继续复用旧设置；内置 Profile 启动还会丢掉有效的扩展上下文模型。模型与推理选择也存在持久记录有值、原生启动没有应用的情况。现在有效 Profile、角色指纹和原生环境使用同一份解析结果，内置配置仍继承原来的认证与服务地址。

Codex 同样把独立保存的上下文偏好计入有效设置版本，不能只散列配置文件。原生配置未指定 provider 时使用明确的继承策略，保留真实上下文参数，不臆造 provider；非原生配置仍要求明确 provider。修改设置不会突然杀掉在途执行；下一次可安全重建时使用新配置。

OpenCode 原先在应用工作上下文权限之前编译启动配置，同时可能沿用旧 Bot 冻结的 Profile 引用。现在新执行解析当前 Profile，先读取当前工作权限再生成原生启动参数。清除工作偏好后恢复后端默认，不恢复旧 surface/Bot 值。

当前 Bot 仍选择的 Claude Profile 不允许被管理页删除；历史 Worker 的冻结记录不等于永远占用可编辑 Profile。保存选择前也重新检查目标仍可用，避免页面候选已失效却返回成功。

设置变更与准入之间还有竞态：已生成的 plan 不能在设置变化后继续以旧版本领取输入；保存设置的回执不能拼接另一位操作者刚写入的 revision。配置准入边界与 SQLite 版本校验共同处理这个问题，发生冲突时输入仍留在队列，重新计划。

### 已退役文件仍被写入

原先生产启动的 `SetHeadlessRuntime` 会通过通用加载器保存经过规范化的旧文件，还会同步旧 surface/room/Claude workspace 状态。Profile catalog 首次迁移另有直接 `ReplaceAll`；Bot 身份更换也会清理旧文件，并要求旧文件可写。结果是历史记录仍在被当作运行状态维护，甚至损坏的历史文件会干扰现行配置。

现在生产入口在加载前选择 history-only。旧文件装载、首次 Profile 迁移、周期同步和身份更换都尊重这一边界。共享 Bot settings、Profile context 和当前身份存储继续按原职责写入。规范化只用于解释历史，不能改写原始证据。旧 bot.added primary bootstrap 也有显式退役检查。

原始文件捕获仍有真实限制：不可读、非普通文件或超过捕获大小限制会阻断启动，不能声称这些文件已经成功备份。可读但内容损坏的文件可以原样捕获；后续现行配置不会因无法解释这些历史内容而失败。

### 状态接口与工具入口继续依赖旧执行器

两条状态接口原先读取旧 orchestrator，可能展示空的执行状态或旧 surface 残留。现在 `authority=persistent_teams` 时，`work` 从单一事务读取项目、上下文、Worker、待处理输入与执行占用。项目和 Worker 明细各最多 50 条，同时返回总数和限制。旧执行数组明确为空，工作库不可读时显示不可用，不假装系统没有工作。

生产启动不再发布旧 instance/surface MCP 服务。过渡环境中的旧处理器也会返回停用，即使调用方持有原来的有效 token。团队 Agent 使用运行范围内的 CLI，旧服务不能成为另一条发送与执行通道。

### 菜单、订阅和安装仍宣传旧流程

原默认菜单由旧 command catalog 生成，而新接收器另行解释 key，造成入口与处理不一致。现在发布和分发共享菜单表；旧 model/reasoning/access key 进入当前设置，旧 stop/new 提供明确的私聊操作说明，不能只回首页而让用户误以为动作完成。

发布事件列表补齐实际处理的机器人移出和群解散事件，移除没有对应处理器的 reaction deleted。多维表格权限继续保留，因为团队资料仍然使用它，说明不再称其为 Cron 权限。

WebSetup 移除 VS Code 接管阶段，Admin 不再提供安装/重装。命令行 install、交互安装与发行包首次安装共用无接管的安装流程；旧显式接管参数在写入前报错，发行脚本使用的 `-bootstrap-only` 保持兼容。已有集成记录保留供明确清理，安装升级不擅自拆除它。清理以实际 sidecar 所有权为准，不能因旧安装记录仍指向该位置就拆掉另一个实例；只删除本实例的编辑器覆盖，不保留需要用户再点一次的残余设置。

### 文档继续把历史行为写成当前产品

README、QUICKSTART、用户指南和飞书部署说明改为当前团队与私人工作入口。Cron 设计移入 obsoleted，并同步引用。产品草案、技术设计、实施账本和运行时规则明确各自用途，不再同时声称尚未实现和已经交付。用户手册不再承诺团队发布链路没有调用的旧自动上传和链接改写；明确保留文件发送与团队资料操作。

长状态机文档保留旧实现记录，但明确第 0 节定义当前持久团队规则；后续旧 surface 内容只作历史与迁移审计。删除旧章节会丢失迁移依据，因此没有以删除行数作为清理目标。

## 回归覆盖

| 边界 | 证据位置 |
| --- | --- |
| 有效、损坏、需要规范化的旧文件在身份替换后保持原字节 | `app_work_authority_test.go` |
| 旧 Claude workspace/Goal 文件只读，旧 primary 和 MCP 不执行 | `app_work_authority_test.go` |
| 新 Profile 迁移不受退役 surface 可写性影响 | `profile_catalog_migration_test.go` |
| 设置变化使空闲 Worker 重建，在途执行保留原快照 | `app_work_settings_test.go` |
| 旧偏好 plan 不领取输入，重计划后应用新值 | `workengine/guards_test.go` |
| 崩溃后未发送启动意图重核设置，替换旧代次并释放占位 | `workhost/host_test.go`、`workhost/recovery_test.go` |
| Codex 原生继承策略发送上下文参数，实际值只认回执 | `internal/adapter/codex/translator_resume_policy_test.go` |
| 状态读真实工作库、忽略旧残留，读失败明确显示 | `admin_runtime_status_test.go` |
| 发布菜单与回调一致、旧 key 提供正确反馈 | `internal/feishuapp/manifest_test.go`、`app_work_menu_test.go` |
| 旧集成安装拒绝、清理保留、新安装不装 shim | `admin_vscode_test.go`、`internal/app/install` 测试 |
| Setup/Admin 无接管入口、嵌入静态资源同步 | `web/src/routes/SetupRoute.test.tsx`、`AdminRoute.test.tsx` |

2026-11-22 最终验证：

- `go test ./...` 全仓通过。
- 设置变更、任务准入、Worker 预留与崩溃恢复的定向 race 检查通过；旧状态只读、身份替换、Profile 迁移、状态投影另跑定向 race 并通过。
- Web 全套 11 个测试文件、69 项测试通过；`npm run build` 通过，嵌入静态资源与构建输出一致。
- `GOOS=windows GOARCH=amd64 go build ./...` 通过。
- `scripts/check/pre-commit.sh` 与差异空白检查通过。路径检查仍会报告仓库既有的非阻断跨平台测试路径提醒，不把该提醒描述为零告警。

运行时与卡片状态机审计已核对：设置冲突不吞输入；未发送启动意图有重计划出口；sending/unknown 不因设置变化重放；旧菜单提供可用的下一操作；退役安装明确失败；历史损坏不回退成旧执行器。本轮没有遗留需要另行拍板的新增产品取舍。

自动化检查不代替真实飞书租户、真实原生 Agent 和真实主机安装升级验收；本轮不部署服务，不执行真实安装/卸载，不把组件测试描述成真实生产重启。
