# Issue Orchestration Workflow

> Type: `general`
> Updated: `2026-08-06`
> Summary: issue workflow 参考基线已精简：入口和最小合同以 issue-workflow-guardrail skill 为准，本文只保留拆分、快照、决策包、verifier 和 close gate 的详细模板。

## 1. 定位

本文是 issue workflow 的参考基线，不是每次执行都要通读的必读材料。

- 入口、模式选择、最小合同、读序以 `.codex/skills/issue-workflow-guardrail/SKILL.md` 为准。
- 连续执行一个单子时不需要回头重读本文；需要拆分表、决策包、verifier 输出或 close gate 细节时才查阅。
- 若 `.codex/private/issue-orchestration-private.md` 存在，仅在分类或拆分判断有歧义时读取；它不改变公开基线。

## 2. 模式与边界

- 默认 `fast`；fast 准入清单见 skill。
- full 高风险信号：外部提单、母/子结构或预计要拆、状态机/迁移/安全/权限/协议、多阶段/多 turn、不确定。
- `prepare` 按 issue 正文/标签做机械升级；执行者按触碰文件面核对 guardrail 区域，执行中发现越界时立即升 full 并补执行上下文。

## 3. 最小合同

- fast：`背景` / `目标` / `完成标准` + 一个 `status:*` 标签。
- full：以上 + 执行上下文（`当前执行单元` / `下一步` / `最后一致状态` / `未完成尾项`）。
- 参考区、阶段计划、执行快照、verifier 都是条件性产物，不是 full 默认项。

## 4. 拆分

以下信号出现时优先拆成母 issue + 子 issue：

- 同时覆盖多个弱相关目标
- 需要多套弱相关背景
- 不同部分验证面明显不同
- 一部分失败不应阻塞另一部分
- 天然可并行

母 issue 至少包含：

- `背景` / `目标` / `完成标准`
- `拆分结构` / `推荐顺序` / `可并行组` / `当前风险`
- 总调度表：`单元`、`类型`、`当前状态`、`依赖`、`可并行组`、`当前闭包等级`、`下一步建议`、`备注`

进入 close-out 后，总调度表补三列：`结果回卷`、`verifier 状态`、`当前结论`。

子 issue 至少包含：

- `父 issue` / `背景` / `目标` / `非目标` / `完成标准` / `依赖`
- `信息索引`（需要时）

不要为了“结构好看”过拆；过拆的调度和回填成本高于收益。

## 5. 执行上下文与快照

执行上下文字段：

- `当前执行单元`
- `下一步`（只写一个动作）
- `最后一致状态`（哪些事实已确认、哪些已失效）
- `未完成尾项`

快照只在真实停点更新：

- 停 turn
- 交给另一个 worker
- 跨 session 恢复前

阶段结束本身不触发快照更新；阶段只触发四问：是否完成 / 硬阻塞 / 产品决策门 / 需要拆分。

恢复流程：

1. 读执行上下文或快照
2. 对照当前代码确认 `下一步` 仍成立
3. 失效就先回填，再继续

## 6. 产品决策门

进入条件：

- 技术约束迫使产品语义让步
- 用户可见行为存在多个合理方案
- 交互取舍会影响验收
- 继续实现等于替产品拍板

最小决策包：

- `触发原因`
- `当前约束`
- `备选方案`
- `各方案影响`
- `推荐方案`
- `需要拍板的问题`
- `受影响单元`

拍板后先回填 issue，再重新评估阶段、依赖和快照。

## 7. verifier

独立 verifier 只用于：母 issue 关闭、外部提单 close-out、安全/迁移/状态机、用户明确要求、或执行中风险上升。

结果等级：

- `独立 verifier 结果：pass`
- `独立 verifier 结果：pass with gaps`
- `独立 verifier 结果：fail`

`pass with gaps` 不允许直接 close，gap 必须 durable 记录；`fail` 阻断 close。

## 8. close gate

### 子 issue

关闭前必须满足：

- 已标出 `父 issue`
- 结果已 durable 回卷到母 issue

### 母 issue

关闭前必须满足：

- 本轮纳入关闭的子 issue 都已回卷
- 总调度表已补齐 `结果回卷` / `verifier 状态` / `当前结论`
- 母级 verifier 已跑且结果为 `pass`

### legacy issue

旧 issue 重新进入执行或 close path 时，先补齐当前合同：显式状态标签、执行上下文、父链接（如适用）、总调度表（如适用）。

### 外部提单

- 内部执行 issue 完成后，在原外部 issue 留一条简短回评（结果、内部 issue 链接、提交/版本、验证结论）
- 默认只关内部 issue；是否关原 issue 由人工决定

### finish --close

`finish --close` 内置 close gate 检查，gate 不过拒绝关闭。`close-plan` 是可选 dry-run，用于人工预览，不是前置步骤。

## 9. helper

```bash
bash .codex/skills/issue-workflow-guardrail/scripts/issuectl.sh prepare --issue <number> --mode <fast|full>
bash .codex/skills/issue-workflow-guardrail/scripts/issuectl.sh finish --issue <number> --mode <fast|full> [--comment-file path] [--close]
```

确定性检查优先仓库 helper：

```bash
bash scripts/dev/worktree-facts.sh
bash scripts/dev/resolve-repo-path.sh docs/general/issue-orchestration-workflow.md
bash scripts/dev/gh-json-fields.sh --check number,title,state issue view <number>
```
