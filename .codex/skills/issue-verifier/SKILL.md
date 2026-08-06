---
name: issue-verifier
description: "Independent read-only verification for high-risk GitHub issue work in this repository: parent close, external reporter close-out, security/migration/state-machine work, or explicit 验收/独立验证/完成前复核 requests. Scope is diff + acceptance first, full context only when needed."
---

# Issue Verifier

## 触发场景

只在以下场景做独立验收 pass：

- 关闭母 issue
- 外部提单 close-out
- 安全 / 迁移 / 状态机类工作
- 用户明确要求 `验收` / `独立验证` / `对齐验证` / `完成前复核`
- 执行中风险上升，独立检查确实能降低风险

其他 full issue 由同一执行者做 close-readiness 检查，不单独跑本 skill。

## 工作方式

默认只读。除非用户明确要求在同一轮修复，否则不静默改代码。

### 读序（轻量优先）

1. 目标与完成标准（issue body 里的最小字段即可）
2. diff / 改动文件
3. 验证证据（测试、CI、命令输出）
4. 只有判断需要时，再读完整评论、设计文档或 parent/child 结构

不要为了形式把整包上下文重读一遍。

## 检查维度

- 目标对齐：实现是否解决 issue 目标
- 非目标纪律：是否漂移到排除范围
- 验收覆盖：每条完成标准是否满足
- 回归面：有没有未验证的高风险路径
- durable 同步：是否需要更新 issue body、docs、状态机文档、AGENTS 或 skill
- close-out 就绪：是能关，还是只是本地完成

母 issue 额外检查：

- 每个已完子 issue 是否已 durable 回卷到母 issue
- 母 issue 总视图是否包含回卷状态、verifier 状态和当前关单判断

子 issue 额外检查：

- 父链接是否 durable 记录
- 子结果是否已回卷到父 issue

## 输出格式

findings-first，首行固定结果记录：

- `独立 verifier 结果：pass`
- `独立 verifier 结果：pass with gaps`
- `独立 verifier 结果：fail`

然后是按严重度排序的 findings、开放问题或假设、pass/fail 建议、简短 close-out 说明。

- `pass`：可以继续正常 close path
- `pass with gaps`：不允许直接 close，gap 必须先 durable 记录
- `fail`：阻断 close

无 findings 时也要明确说没有，并列出残留验证缺口。

## Guardrails

- 不因为“看起来显然”就顺手改代码。
- 不把验收缺口降级成风格建议。
- 不自己关 issue，除非用户明确要求 verifier 同时关闭。
- 缺少足够 durable 上下文时，说需要整形或闭包索引修复，交回 `issue-workflow-guardrail`。
