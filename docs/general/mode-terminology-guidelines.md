# 模式术语基线

> Type: `general`
> Updated: `2026-04-30`
> Summary: 固定 headless / vscode、用户可见 mode、以及 `normal` 兼容 alias 的仓库级术语边界。

## 1. 文档定位

这份文档是当前仓库关于 mode / backend / headless 的 canonical 术语基线。

后续文档、菜单、状态机、help copy、代码注释如果涉及这些概念，默认都应该以本文为准。

## 2. 一级划分：运行形态

当前产品先分两种运行形态：

- `headless`
  - 指不跟随 VS Code 当前焦点的所有远程工作形态
- `vscode`
  - 指跟随 VS Code 当前焦点的工作形态

在内部状态层，当前仍使用 `ProductMode` 承载这一级划分：

- `ProductModeNormal`
  - 当前持久化 token，表示 `headless`
- `ProductModeVSCode`
  - 表示 `vscode`

这里的 `normal` 是历史存量 token，不应继续被当成新的一级产品概念扩散。

## 3. 二级划分：用户可见 mode

当前用户可见、可以直接说出的 mode 名只有三个：

- `codex`
- `claude`
- `vscode`

它们的当前语义是：

- `codex`
  - `headless + codex backend`
- `claude`
  - `headless + claude backend`
- `vscode`
  - `vscode + codex backend`

当前实现里还不存在单独的 `vscode + claude backend` 产品形态。

## 4. `normal` 的定义

`normal` 不是一个独立的一等 mode 名。

当前只允许把它理解成下面两件事：

- slash 兼容 alias
  - `/mode normal` 兼容等价于 `/mode codex`
- 文档 umbrella term
  - 当文档要统称所有 headless 场景时，可以说 “headless（历史上也常被叫作 normal）”

当前不建议再这样写：

- “当前有 normal / claude / vscode 三种 mode”
- “normal 和 codex 是并列概念”
- “normal 是 codex/claude 之外的第三种 headless backend”

## 5. 代码层表达要求

涉及运行形态判断时，优先表达：

- `headless vs vscode`

涉及 provider/backend 判断时，再表达：

- `codex vs claude`

不要把“是不是 headless”继续编码成“是不是 normal 特例”。

当前推荐的理解顺序是：

1. 先看运行形态是不是 `headless`
2. 再看 backend 是 `codex` 还是 `claude`
3. 最后再做具体命令、路由或恢复语义分支

## 6. 文档与文案要求

面向用户的文档和提示文案默认使用：

- `codex mode`
- `claude mode`
- `vscode mode`

如果需要解释旧概念，可以补一句：

- “`/mode normal` 仍兼容，但它只是 `codex mode` 的旧 alias”

需要描述总类时，优先用：

- `headless`

而不是继续把 `normal` 写成与 `codex` / `claude` / `vscode` 并列的可见模式名。
