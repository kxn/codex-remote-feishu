---
name: issue-workflow-guardrail
description: "Use when handling a GitHub issue in this repository: raw issue shaping, implementability reassessment, fast/full classification, parent/child orchestration, execution snapshots on real handoffs, product decision gates, result roll-up, and close-out. Run the prepare/finish entry points and keep the issue body current."
---

# Issue Workflow Guardrail

## 定位

本 skill 是当前仓库 GitHub issue 工作流的唯一生命周期 owner，负责整形、分类、计划、执行编排、独立验收（仅在需要的场景）、发布和关闭。

- 不要叠加通用 Superpowers lifecycle skill（brainstorming、writing-plans、executing-plans、subagent-driven-development、requesting-code-review、finishing-a-development-branch、using-git-worktrees）。
- 方法 skill 照常可用：`systematic-debugging`、`test-driven-development`、`verification-before-completion`；并行 agent 只在任务真正独立可并行时使用。
- 持久化只保留一份：需求/决策/执行状态写 issue body 或链接设计文档；长设计写 docs 生命周期文档；独立验收由 `issue-verifier` 承担；机械检查和发布由 pre-commit + `safe-push` 承担。
- 不为 workflow 管理的 issue 创建 `docs/superpowers/specs` 或 `docs/superpowers/plans`。

## 模式选择

默认 `fast`。命中以下任一高风险信号时选择 `full`：

- 外部提单
- 母/子结构，或预计需要拆分
- 状态机、迁移、安全、权限、协议或跨 surface 行为
- 多阶段或多 turn 执行
- 分类不确定

`prepare` 会按 issue 正文和标签做机械升级：命中高风险信号时拒绝以 fast 继续。执行者还要按触碰文件面核对（状态机/迁移/安全/权限/协议等 guardrail 区域），命中即升 full。用户显式指定模式优先，但执行中发现 fast 已不安全时，必须说明证据并升级到 full。

### fast 准入清单

以下条件必须全部成立：

1. 单一执行面，预期一个 commit 能收尾
2. 不碰状态机 / 迁移 / 安全 / 权限 / 协议 / 跨 surface 行为
3. 有明确测试或验证路径
4. 不需要拆单、交人、跨 session 恢复

任一条件不成立，使用 `full`。

## 入口

两个固定入口；`lint` 和 `close-plan` 保留为可选复检，不属于必跑流程。

```bash
bash .codex/skills/issue-workflow-guardrail/scripts/issuectl.sh prepare --issue <number> --mode <fast|full>
bash .codex/skills/issue-workflow-guardrail/scripts/issuectl.sh finish --issue <number> --mode <fast|full> [--comment-file path] [--close]
```

职责：

- `prepare`：git pull --ff-only、拉取实时 issue、分类扫描、workflow 合同检查（含 lint 报告）、claim processing、写 prepare snapshot JSON。未 ready 时阻塞继续。
- body/标签改过后，重新跑 `prepare` 即可完成复检，不需要单独跑 `lint`。
- `finish --close`：内部先跑 close gate 检查，gate 不过就拒绝关闭。`close-plan` 只是给人工预览的 dry-run，不是关闭前置步骤。
- `finish` 不重跑本地格式、文档、diff 或测试；pre-commit、定向验证和 `safe-push` 负责这些。

## fast 合同

- 最小合同：`背景`、`目标`、`完成标准`，外加恰好一个 `status:*` 标签。
- 流程：`prepare` → 读 issue + 相关代码 → 实现 → 定向验证 → pre-commit → commit → safe-push → `finish --close`。
- 不写执行决策、执行快照、参考区，不跑独立 verifier。
- 执行中发现不再是单阶段/低风险时，立即升级 full 并补齐缺失的执行上下文，再继续。

## full 合同

最小结构：

- `背景` / `目标` / `完成标准`
- 执行上下文：
  - `当前执行单元`
  - `下一步`
  - `最后一致状态`
  - `未完成尾项`

以下内容都是**条件性**的，不是 full 的默认产物：

- `实现参考` / `检查参考` / `收尾参考`：仅在要交人、跨 session 或 issue 确实复杂到需要索引时写。
- 阶段计划 / 拆分表：仅当 issue 需要拆分或作为母 issue 调度时写。
- 执行快照：仅在真实停点（停 turn、交 worker、跨 session）时更新，不要求每个 stage 结束都更新。
- 独立 verifier：仅按下方「verifier」一节的范围执行。

流程：`prepare` → 写/刷新执行上下文 → 连续实现（阶段结束只做四问，不重读流程文档）→ 定向验证 → commit → push → `finish --close`。

## 读序

- 每个任务开始读一次本 skill，加上 issue body、标签、最新评论和相关代码。
- `docs/general/issue-orchestration-workflow.md` 是参考文档：需要拆分表、决策包、verifier 输出模板或 close gate 细节时才查阅，不是必读项。
- 连续执行时不重读流程文档；每个 stage 前重读 issue/评论/代码只在真实恢复或上下文压缩后需要。
- 如果 `.codex/private/issue-orchestration-private.md` 存在，仅在分类或拆分判断有歧义时读取；它不改变公开基线。

## 输出约定

- `prepare` / `lint` / `close-plan` / `finish` 默认只输出文本摘要；完整 JSON 用 `--json-file <path>` 写文件（prepare 也可用 `--snapshot-file`），stdout 不吐全文。
- 读 snapshot 或 inspect 文件时，先 `rg '^## ' <file>` 列结构，再 `sed -n '/^## 目标/,/^## 完成标准/p' <file>` 取需要的 section；不要 cat 整个文件。

## 输出防爆（阈值制）

- 小输出（预计 < 3k token）直接读，不写文件；只有大概率大输出（issue 全文、CI 日志、minified 文件、整仓 diff、sqlite 全表）才走文件/片段读。
- 未知大小先探一次：`wc -c` / `rg --count-matches` / `head -n 20`，再决定直读还是文件化。
- 限流用单 flag：`rg --max-count` / `-l`、`git diff --stat`、`go test -run`、sqlite `LIMIT`、`head`。
- 输出被截断或失败：禁止原样重试同一命令，先缩小范围或换 helper；不要为提高上限而提高 `max_output_tokens`。

## 执行规则

- 绿/黄/红不一致分级保持不变：绿色局部处理，黄色做一次有界探查，红色停止实现、回写 issue、交还 orchestrator。
- 产品决策门保持不变：停下自动化，写最小 `待决策` 包，只问最小的阻塞问题；拍板后先把结论回填 issue 再继续。
- worker 边界：子 issue 或未拆分直做单元只负责当前闭包内执行，不做闭包外的重新规划。
- 验证底线与模式无关：根因优先、定向测试、pre-commit、safe-push 全都要跑。

## 外部提单

- 单阶段、低风险的外部提单：直接在原 issue 上处理，保留提单人 body，完成后在原 issue 回一条简短评论。
- 多阶段或需要内部编排的外部提单：先建内部执行 issue，双向链接；所有 workflow 结构只写内部 issue，默认只关内部 issue，原 issue 由人工决定是否关闭。

## verifier

使用 `issue-verifier` 的场景：

- 关闭母 issue
- 外部提单的 close-out
- 安全 / 迁移 / 状态机类工作
- 用户明确要求 `验收` / `独立验证` / `完成前复核`
- 执行中风险上升，独立检查确实能降低风险

其他 full issue 不做独立 verifier pass，由同一执行者做一次 close-readiness 检查（目标/验收/diff/durable sync）。

## close-out

- `finish --close` 内置 close gate：需要 verifier 记录、父 issue 回卷或 legacy contract rehab 时，gate 不过会拒绝关闭。
- 完成评论保持简短：改了什么、怎么验证的、跑了 verifier 就给结论、durable 知识同步到哪里或为什么不需要、commit/PR 引用。
- 终态：工作区干净、没有未推送的本地提交、`finish` 已跑、验收满足时 issue 已关闭。
- 如果停在非终态，必须说明还差什么、为什么停，以及精确的恢复动作。

## GitHub CLI 强制规范

- 读写 issue 优先使用 `issuectl`，不要自己拼裸 `gh issue view <number> --comments`（projectCards GraphQL 必炸）。
- `gh issue view <number> --json ...` 或 REST `gh api ...` 可用，但 `--json` 字段必须先跑 `bash scripts/dev/gh-json-fields.sh --check <field,...> <gh-subcommand>` 验证，禁止凭记忆猜。
- issue 全文、评论、CI 日志一律重定向到文件再按需读（先 `rg '^## '` 列结构，再 `sed -n` 取段），不要打进 stdout。
- 编辑 body 一律 `--body-file <file>`；禁止把含反引号、`$()`、长文本的 body 直接拼进命令行。
- 多步命令拆成多次工具调用；禁止用 `&&` 接在 heredoc 之后。
- 命令失败后禁止原样重试同一条命令：先读错误，换 helper 或正确格式。
