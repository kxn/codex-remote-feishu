# AGENTS

## 会话握手

- 收到直接指令时，先简短复述：用户要求什么、我马上做什么。
- 然后直接执行，除非用户明确要求暂停。
- 用户纠正方向时立即切换。

## 触发规则

- Skill 触发使用并集匹配：用户措辞/意图、逻辑载体、触碰文件、已知症状，任一命中即触发。
- 多个 skill 同时命中就一起用；排除说明只在能确认逻辑载体未变时适用（例如纯文案/样式/日志/测试）。
- 不得为了少读文件而缩小触发范围。

## 仓库 skill 触发速查

- issue：`issue-workflow-guardrail`（单子生命周期）、`issue-verifier`（独立验收，仅 full 高风险场景）、`issue-doc-sync`（已关 issue 同步 docs）。
- relay / 飞书 / 远程面：`relay-stack-playbook`、`remote-state-machine-guardrail`、`feishu-ui-state-machine-guardrail`。
- 安装/升级/推送：`local-upgrade`、`safe-push`。
- 页面：`build-page-mock`。

## 单子入口（GitHub Issue）

- 对话中出现 issue 编号或 URL 时，先进入单子流程（`issuectl prepare`），不直接评估或实现；能当场一次改完的 tiny 修复除外。
- 分类默认 `fast`；命中以下任一条件即 `full`：外部提单、母/子结构或预计要拆、状态机/迁移/安全/权限/协议、多阶段或多 turn、风险不确定。
- `prepare` 按 issue 正文/标签做机械升级；执行者还要按触碰文件面核对（状态机/迁移/安全/权限/协议等 guardrail 区域），命中即升 `full`。
- 单子全流程由 `.codex/skills/issue-workflow-guardrail/` 唯一负责，不叠加通用 lifecycle skill（brainstorming、writing-plans、executing-plans、subagent-driven-development、requesting-code-review、finishing-a-development-branch、using-git-worktrees）。
- 方法 skill 照常可用：`systematic-debugging`、`test-driven-development`、`verification-before-completion`；并行 agent 仅在任务真正独立可并行时使用。
- 持久化只保留一份：需求/决策/执行状态写 issue body，长设计写 docs 生命周期文档，独立验收只由 `issue-verifier` 承担（full 高风险场景），机械检查和发布由 pre-commit + `safe-push` 承担。
- 用户要求分阶段推进时按连续执行处理：阶段只是顺序不是停止点；每阶段结束只做四问（是否完成 / 硬阻塞 / 产品决策门 / 需要拆分），否则继续下一阶段。

## 工作区干净

- 任何新任务开始实质工作前先 `git status --short`。
- 干净则继续；不干净先区分 `same-task` / `different-task`：
  - different-task：停下问用户先提交/暂存/还是继续脏工作区。
  - same-task：明确说明假设后继续。
- 默认不把无关改动混进同一个提交。

## 根因优先

- bug/回归/失败一律先找根因，禁止症状掩盖或最小补丁兜底，除非用户明确批准临时方案。
- 修复前先收集运行证据、跨组件追踪、给出根因假设；根因未知就继续调查，不把不确定变成补丁。
- 若根因是错误抽象/所有权/状态机/协议边界，优先做更深修正，而不是叠兼容层。

## 验证与发布底线

- 声明“完成/通过/已修复”前必须有新鲜验证证据；同一提交已证明过的检查不重复跑。
- 提交前跑 `scripts/check/pre-commit.sh`（含文件长度 gate），不得绕过。
- 推送一律走 `./safe-push.sh`；rebase 后出现 review gate 时先人工核对再 `--confirm-rebase-review`。
- 提交后默认同轮推送；除非用户要求本地-only、临时实验分支、或推送被真实阻塞。停止本地状态时明确报告 LOCAL-ONLY、分支、HEAD 和下一步。
- 收尾前重查 `git status --short` 和本地 HEAD 是否领先 upstream。
- 重复 tail-state 检查优先 `scripts/dev/worktree-facts.sh`；路径探测用 `scripts/dev/resolve-repo-path.sh`；陌生 `gh --json` 字段先 `scripts/dev/gh-json-fields.sh`；同一确定性失败不原样重跑。

## 领域基线（按需读取）

- `docs/**/*.md` 改动：遵守 docs/README.md 的元信息（Type/Updated/Summary）、生命周期目录和链接同步规范。
- Web/管理页改动：先读 `docs/general/web-design-guidelines.md`（含 mock 时另读 `page-mock-guidelines.md`）。
- 飞书卡片/菜单/文本改动：按触碰面读 `docs/general/feishu-card-api-constraints.md`、`feishu-menu-card-usage-guidelines.md`、`feishu-card-content-context-guidelines.md`、`feishu-card-ui-state-machine.md`。
- 远程面/状态机逻辑改动：读 `docs/general/remote-surface-state-machine.md`，提交前跑对应 guardrail skill 并同步 canonical doc，审计 dead/half-dead 状态。
- Windows 仓库操作（WSL 禁用、PowerShell/gh 规范）：读 `docs/general/windows-repo-operations.md`。

## 工程约束

- 生产 Go 代码不得直接 `exec.Command` / `exec.CommandContext`，统一用 `internal/execlaunch`。
- 本地 localhost 调试先清 proxy 环境变量；`relay-wrapper` 本身不带 proxy，子进程恢复捕获的 proxy。
- 配置迁移/安装写入必须保留既有凭据；服务生命周期操作不得对同一 daemon 重叠 stop/start/restart/bootstrap。
- 调试流量分类用协议关联 id，不用线程/时序启发式。
