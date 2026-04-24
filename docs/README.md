# Documentation Index

> Type: `general`
> Updated: `2026-04-24`
> Summary: 更新文档索引并纳入上游可重试 turn 自动恢复设计草稿。

## 1. 适用范围

本规范强制适用于 `docs/**/*.md`。

这些文档承载的是：

- 设计草案
- 实施中方案
- 已落地功能说明
- 长期有效的架构/协议/流程文档
- 已废弃但仍需保留的历史设计

像 `README.md`、`QUICKSTART.md`、`deploy/**/README.md` 这类与目录强绑定的共位说明文件，继续保留在原目录，不纳入生命周期归档目录；但后续如有重写，也建议补齐同样的元信息头。

## 2. 目录结构

`docs/` 根目录只保留索引或共享模板，具体文档按生命周期进入子目录：

- `docs/draft/`
  - 还在讨论，边界和结论都可能继续变化
- `docs/inprogress/`
  - 方案已基本定稿，但尚未全部实现，或实现到一半
- `docs/implemented/`
  - 功能已经落地，文档主要记录当前已实现行为和设计边界
- `docs/general/`
  - 长期有效的架构、协议、流程、测试、部署类文档
- `docs/obsoleted/`
  - 已不再作为当前实现依据，仅保留历史背景或设计理由

## 3. 必填头信息

每个 `docs/**/*.md` 文件都必须在标题下方紧跟一个可见元信息块：

```md
# 文档标题

> Type: `draft`
> Updated: `2026-04-06`
> Summary: 一句话说明本次更新了什么。
```

要求：

- `Type` 必须与所在目录一致
- `Updated` 使用 `YYYY-MM-DD`
- `Summary` 只写最近一次有效改动，不写长 changelog

对 `obsoleted` 文档，建议额外补一行：

```md
> Superseded By: `docs/general/xxx.md`
```

## 4. 分类标准

### 4.1 `draft`

放这里的典型信号：

- 还在和需求方讨论交互或边界
- 关键抽象还可能重命名
- 尚未承诺进入实现

### 4.2 `inprogress`

放这里的典型信号：

- 方案已经决定做
- 已经开始编码，或已实现一部分
- 仍然存在明显未完成部分

### 4.3 `implemented`

放这里的典型信号：

- 功能已经进入当前代码
- 文档描述的是现有行为、取舍和已知边界
- 它不是全局架构文档，但值得长期保留

### 4.4 `general`

放这里的典型信号：

- 当前和未来一段时间都会持续有效
- 是团队默认应遵循的架构/协议/流程基线
- 不绑定单一 feature 的短期设计阶段

### 4.5 `obsoleted`

放这里的典型信号：

- 当前实现已经不再按本文执行
- 但历史决策背景仍有保留价值
- 如果继续阅读，必须明确知道新的 source of truth 在哪里

## 5. 命名与移动规则

- 文件名继续使用英文 kebab-case，避免中文文件名和空格
- 文档状态变更时，直接移动到对应目录，不保留旧副本
- 目录迁移后，必须同步修正 README 和其他文档中的相对链接
- 新增文档时，优先先判断它属于哪种生命周期，再决定放到哪个目录

## 6. 当前索引

### 6.1 `general`

- [architecture.md](./general/architecture.md)
- [codex-mcp-app-server-protocol.md](./general/codex-mcp-app-server-protocol.md)
- [dev-conversation-trace.md](./general/dev-conversation-trace.md)
- [feishu-api-timeout-discipline.md](./general/feishu-api-timeout-discipline.md)
- [feishu-business-card-interaction-principles.md](./general/feishu-business-card-interaction-principles.md)
- [feishu-card-api-constraints.md](./general/feishu-card-api-constraints.md)
- [feishu-card-interaction-model.md](./general/feishu-card-interaction-model.md)
- [feishu-menu-card-usage-guidelines.md](./general/feishu-menu-card-usage-guidelines.md)
- [feishu-card-content-context-guidelines.md](./general/feishu-card-content-context-guidelines.md)
- [feishu-product-design.md](./general/feishu-product-design.md)
- [feishu-im-message-research.md](./general/feishu-im-message-research.md)
- [feishu-card-ui-state-machine.md](./general/feishu-card-ui-state-machine.md)
- [go-test-strategy.md](./general/go-test-strategy.md)
- [install-deploy-design.md](./general/install-deploy-design.md)
- [issue-orchestration-workflow.md](./general/issue-orchestration-workflow.md)
- [local-self-upgrade-flow.md](./general/local-self-upgrade-flow.md)
- [release-roadmap-workflow.md](./general/release-roadmap-workflow.md)
- [remote-surface-state-machine.md](./general/remote-surface-state-machine.md)
- [relay-error-reporting-protocol.md](./general/relay-error-reporting-protocol.md)
- [relay-protocol-spec.md](./general/relay-protocol-spec.md)
- [user-guide.md](./general/user-guide.md)
- [web-design-guidelines.md](./general/web-design-guidelines.md)

### 6.2 `implemented`

- [cron-bitable-scheduler-design.md](./implemented/cron-bitable-scheduler-design.md)
- [feishu-md-preview-design.md](./implemented/feishu-md-preview-design.md)
- [feishu-request-approval-design.md](./implemented/feishu-request-approval-design.md)
- [managed-headless-pool-design.md](./implemented/managed-headless-pool-design.md)
- [new-thread-command-design.md](./implemented/new-thread-command-design.md)
- [relay-backpressure-hardening-design.md](./implemented/relay-backpressure-hardening-design.md)
- [shared-exploration-progress-card-design.md](./implemented/shared-exploration-progress-card-design.md)
- [authenticated-external-access-foundation-design.md](./implemented/authenticated-external-access-foundation-design.md)
- [web-admin-ui-redesign.md](./implemented/web-admin-ui-redesign.md)

### 6.3 `inprogress`

- [claude-backend-integration-plan.md](./inprogress/claude-backend-integration-plan.md)
- [codex-app-server-state-machine-audit.md](./inprogress/codex-app-server-state-machine-audit.md)
- [cross-layer-event-contract-redesign.md](./inprogress/cross-layer-event-contract-redesign.md)
- [final-message-feidex-audit.md](./inprogress/final-message-feidex-audit.md)
- [relay-daemon-autostart-design.md](./inprogress/relay-daemon-autostart-design.md)
- [thread-description-unification-plan.md](./inprogress/thread-description-unification-plan.md)
- [unified-binary-design.md](./inprogress/unified-binary-design.md)

### 6.4 `draft`

- [acp-claude-integration-design.md](./draft/acp-claude-integration-design.md)
- [backend-aware-command-catalog-design.md](./draft/backend-aware-command-catalog-design.md)
- [daemon-package-refactor-plan.md](./draft/daemon-package-refactor-plan.md)
- [feishu-call-broker-design.md](./draft/feishu-call-broker-design.md)
- [feishu-menu-frontstage-architecture-redesign.md](./draft/feishu-menu-frontstage-architecture-redesign.md)
- [file-length-split-audit-2026-04.md](./draft/file-length-split-audit-2026-04.md)
- [feishu-owner-card-bypass-prompt-audit-2026-04.md](./draft/feishu-owner-card-bypass-prompt-audit-2026-04.md)
- [feishu-slash-menu-owner-card-audit-2026-04.md](./draft/feishu-slash-menu-owner-card-audit-2026-04.md)
- [feishu-inline-card-update-design.md](./draft/feishu-inline-card-update-design.md)
- [feishu-file-preview-handler-design.md](./draft/feishu-file-preview-handler-design.md)
- [feishu-system-markdown-boundary-design.md](./draft/feishu-system-markdown-boundary-design.md)
- [multi-feishu-app-design.md](./draft/multi-feishu-app-design.md)
- [non-linux-user-autostart-research.md](./draft/non-linux-user-autostart-research.md)
- [plan-mode-feishu-support-design.md](./draft/plan-mode-feishu-support-design.md)
- [repository-review-2026-04.md](./draft/repository-review-2026-04.md)
- [upstream-retryable-turn-autocontinue-design.md](./draft/upstream-retryable-turn-autocontinue-design.md)
- [web-preview-snapshot-design.md](./draft/web-preview-snapshot-design.md)
- [web-install-admin-prerequisites-design.md](./draft/web-install-admin-prerequisites-design.md)
- [web-preview-renderer-architecture-redesign.md](./draft/web-preview-renderer-architecture-redesign.md)
- [web-onboarding-admin-workflow-prd.md](./draft/web-onboarding-admin-workflow-prd.md)
- [web-onboarding-admin-user-mock.html](./draft/web-onboarding-admin-user-mock.html)
- [web-onboarding-admin-user-view.md](./draft/web-onboarding-admin-user-view.md)
- [web-setup-wizard-redesign.md](./draft/web-setup-wizard-redesign.md)
- [workspace-mode-redesign.md](./draft/workspace-mode-redesign.md)

### 6.5 `obsoleted`

- [app-server-redesign.md](./obsoleted/app-server-redesign.md)
- [claude-feidex-reassessment.md](./obsoleted/claude-feidex-reassessment.md)
- [claude-normal-mode-poc-design.md](./obsoleted/claude-normal-mode-poc-design.md)
- [claude-provider-protocol-mapping.md](./obsoleted/claude-provider-protocol-mapping.md)
- [feishu-headless-instance-design.md](./obsoleted/feishu-headless-instance-design.md)
- [web-install-admin-ui-design.md](./obsoleted/web-install-admin-ui-design.md)
