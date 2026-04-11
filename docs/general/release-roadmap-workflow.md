# Release Roadmap Workflow

> Type: `general`
> Updated: `2026-04-11`
> Summary: 定义版本 milestone、release tracker、显式 production 版本号、release branch 目标分支和自动发版闸门之间的关系。

## 1. 目的

这个仓库继续允许平时滚动开发，但正式版本不再依赖“临到发版时再看应该发什么”。

从现在开始，正式发版的计划来源是 GitHub milestone 和 release tracker issue：

- milestone 表示“这个版本准备交付什么”
- release tracker 表示“这个版本什么时候可以发”
- production release 必须使用显式版本号，而不是临时自动推导

这样可以避免两个老问题：

- milestone/roadmap 已经收敛，但最终发出的 tag 却不是原计划版本
- 中途有人手动发了一个别的 production 版本，导致后续自动版本计算基线被打乱

## 2. 核心对象

### 2.1 Milestone

- 一个 milestone 对应一个准备发出的版本
- milestone 标题必须直接等于目标版本号，例如 `v0.14.0`
- 要进入该版本的 issue，都挂到同一个 milestone

### 2.2 Release Tracker Issue

- 每个版本都创建一个 release tracker issue
- 使用 `.github/ISSUE_TEMPLATE/release-tracker.yml`
- tracker issue 的 milestone 必须与其“版本号”字段完全一致
- 需要从 release branch 发版时，在 tracker issue 的“发布分支”字段填写目标分支，例如 `release/1.5`
- 若“发布分支”留空，则自动发版回退到仓库默认分支
- tracker issue 关闭时，会按其中记录的版本号触发自动发版

### 2.3 Release Labels

- `release:tracker`
  - 标记“这个 issue 是版本 tracker”
- `release:stretch`
  - 标记“这个 issue 虽然被放进 milestone，但允许延期，不阻塞本次发版”
- `area:release`
  - 标记 release workflow、版本号、milestone 和自动发版相关工作

默认规则：

- milestone 内没有 `release:stretch` 的 open issue，都视为阻塞当前版本发版
- `release:stretch` 只用于明确允许延期的项，不要滥用

## 3. 版本号来源

### 3.1 Production

- `production` track 的版本号必须显式指定
- 手动触发 release workflow 时，必须填写 `version`
- 关闭 release tracker 自动发版时，也直接使用 tracker issue 中的显式版本号

这意味着：

- 手滑发了另一个 production 版本，不会改变已计划版本本身
- 但如果手滑提前发了完全相同的版本号，tracker 自动发版会因为 tag/release 已存在而失败，需要手工处理冲突

### 3.2 Beta / Alpha

- `beta` 和 `alpha` 继续允许沿用现有自动版本计算
- 需要时也可以走 release tracker，并显式指定某个预发布版本号

## 4. 日常使用方式

### 4.1 新建一个版本

1. 创建 milestone，标题直接写目标版本号，例如 `v0.14.0`
2. 用 Release Tracker 模板创建 tracker issue
3. 给 tracker issue 设置同名 milestone
4. 把要进这个版本的 issue 移到这个 milestone
5. 对允许延期但不阻塞的 issue，显式加 `release:stretch`

### 4.2 判断能不能发版

先执行：

```bash
bash scripts/check/release-readiness.sh --issue <tracker_issue_number>
```

readiness 通过的条件是：

- tracker issue 带有 `release:tracker`
- tracker issue 的 milestone 存在，且标题与版本号完全一致
- tracker issue 的“发布前检查”全部勾选完成
- milestone 下没有仍然 open 的非 `release:stretch` issue

### 4.3 触发正式发版

当 readiness 通过后，直接关闭 tracker issue。

关闭动作会：

1. 再做一次 readiness 校验
2. 从 tracker issue 中读取版本号、发布轨道和发布分支
3. 调用 release workflow，并 checkout 到目标发布分支
4. 按 tracker 指定版本创建 release

## 5. 建议边界

- tracker issue 负责承载版本元信息、检查项和发版闸门，不负责搬运每个功能 issue 的细节
- 真正的工作拆分仍然在普通 issue 中完成
- milestone 表达“计划范围”，release tracker 表达“发版动作”

## 6. 失败处理

如果关闭 tracker issue 后自动发版失败，优先看三类问题：

- tracker issue 的版本号、轨道、milestone 不一致
- tracker issue 指定的发布分支不存在，或者填错了分支名
- milestone 下还有未完成的非 `release:stretch` issue
- 目标版本号已经被人手工发过，导致 tag/release 冲突

冲突修正后，可以重新打开并再次关闭 tracker issue，或者手动触发 release workflow 并填写相同的显式版本号。
