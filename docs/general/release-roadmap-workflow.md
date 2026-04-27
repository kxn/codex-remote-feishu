# Release Roadmap Workflow

> Type: `general`
> Updated: `2026-04-27`
> Summary: 定义 `master` 单主干、短命 `release/*` 封版分支、固定 `dev-latest` 滚动 prerelease，以及 milestone / release tracker / 显式 production 版本号驱动的发版闸门。

## 1. 当前基线

当前仓库的发布基线先明确为：

- 日常开发长期只保留 `master` 这一个主干。
- `release/x.y` 只在正式版封版窗口短期存在，用于 beta/RC 验证和 release blocker 修复；正式版发完后尽快删除。
- 对外稳定语义继续是 `production` / `beta`。
- 高频“最新 push 成功构建”不再持续新增 `alpha.N`，而是收敛到固定一条 `dev-latest` prerelease。

这意味着：

- 不再引入长期 `dev` 分支。
- 不再把多个旧 `release/*` 当成长期常驻维护分支。
- `dev-latest` 和正式版本历史展示面分离，避免 GitHub Releases 被连续 alpha 冲散。

## 2. 目的

这个仓库继续允许平时滚动开发，但正式版本不再依赖“临到发版时再看应该发什么”。

从现在开始，正式发版的计划来源是 GitHub milestone 和 release tracker issue：

- milestone 表示“这个版本准备交付什么”
- release tracker 表示“这个版本什么时候可以发”
- production release 必须使用显式版本号，而不是临时自动推导

这样可以避免两个老问题：

- milestone/roadmap 已经收敛，但最终发出的 tag 却不是原计划版本
- 中途有人手动发了一个别的 production 版本，导致后续自动版本计算基线被打乱

## 3. 核心对象

### 3.1 Milestone

- 一个 milestone 对应一个准备发出的版本
- milestone 标题必须直接等于目标版本号，例如 `v0.14.0`
- 要进入该版本的 issue，都挂到同一个 milestone

### 3.2 Release Tracker Issue

- 每个版本都创建一个 release tracker issue
- 使用 `.github/ISSUE_TEMPLATE/release-tracker.yml`
- tracker issue 的 milestone 必须与其“版本号”字段完全一致
- 需要从 release branch 发版时，在 tracker issue 的“发布分支”字段填写目标分支，例如 `release/1.5`
- 若“发布分支”留空，则自动发版回退到仓库默认分支
- tracker issue 关闭时，会按其中记录的版本号触发自动发版

### 3.3 Release Labels

- `release:tracker`
  - 标记“这个 issue 是版本 tracker”
- `release:stretch`
  - 标记“这个 issue 虽然被放进 milestone，但允许延期，不阻塞本次发版”
- `area:release`
  - 标记 release workflow、版本号、milestone 和自动发版相关工作

默认规则：

- milestone 内没有 `release:stretch` 的 open issue，都视为阻塞当前版本发版
- `release:stretch` 只用于明确允许延期的项，不要滥用

## 4. 版本号来源

### 4.1 Production

- `production` track 的版本号必须显式指定
- 手动触发 release workflow 时，必须填写 `version`
- 关闭 release tracker 自动发版时，也直接使用 tracker issue 中的显式版本号

这意味着：

- 手滑发了另一个 production 版本，不会改变已计划版本本身
- 但如果手滑提前发了完全相同的版本号，tracker 自动发版会因为 tag/release 已存在而失败，需要手工处理冲突

### 4.2 Beta / Alpha / Dev-Latest

- `beta` 继续允许沿用现有自动版本计算
- `alpha` 只保留兼容 / 手动语义，不再作为日常开发快照公开分发主面
- `dev-latest` 不参与 semver 版本计算，它是一条固定 tag / fixed prerelease，只滚动覆盖 asset 与 manifest
- 需要时预发布仍可通过 release tracker 显式指定某个 `beta` / `alpha` 版本号

## 5. 日常使用方式

### 5.1 新建一个版本

1. 创建 milestone，标题直接写目标版本号，例如 `v0.14.0`
2. 用 Release Tracker 模板创建 tracker issue
3. 给 tracker issue 设置同名 milestone
4. 把要进这个版本的 issue 移到这个 milestone
5. 对允许延期但不阻塞的 issue，显式加 `release:stretch`

### 5.2 判断能不能发版

先执行：

```bash
bash scripts/check/release-readiness.sh --issue <tracker_issue_number>
```

readiness 通过的条件是：

- tracker issue 带有 `release:tracker`
- tracker issue 的 milestone 存在，且标题与版本号完全一致
- tracker issue 的“发布前检查”全部勾选完成
- milestone 下没有仍然 open 的非 `release:stretch` issue

### 5.3 触发正式发版

当 readiness 通过后，直接关闭 tracker issue。

关闭动作会：

1. 再做一次 readiness 校验
2. 从 tracker issue 中读取版本号、发布轨道和发布分支
3. 调用 release workflow，并 checkout 到目标发布分支
4. 按 tracker 指定版本创建 release

如果是仓库本地的手动发版动作，需要临时切到 `release branch` 做检查、打 tag、补 notes 或执行其他发布辅助操作：

- 在切分支前先记录当前所在分支或起始 ref
- 发版动作结束后，无论成功还是失败，都切回开始前的分支或 ref
- 不要把仓库停留在临时发布分支，除非用户明确要求保留在那里

### 5.4 平时给内部试最新开发构建

内部 / 自测要跟最新 push 成功构建时，不再新建一串 `alpha.N` GitHub Release，而是更新固定的：

- tag: `dev-latest`
- release: `dev-latest`（prerelease）

已经安装好的实例直接使用：

```text
/upgrade dev
```

客户端会读取固定的 `dev-latest.json` manifest，再解析当前平台的公开 release asset 并完成升级；目标机器不需要 `gh login`。

当前实现里，这条 `dev-latest` 更新链路直接挂在受支持 push 分支（`master` / `main` / `release/**`）的 CI 成功收尾上：

- 只有当前一次 CI 的全部 job 都通过，才会进入 dev feed 更新
- dev feed 更新会复用同一次 CI 已打好的发布产物并覆盖 `dev-latest` 资产
- 不重复跑正式 release 才需要的 smoke / 发布校验链路

### 5.5 发布说明放在哪里

- 对外的正式版本说明，主维护位置放在仓库根目录的 `CHANGELOG.md`
- release workflow 会优先把 `CHANGELOG.md` 中当前版本的小节提取进 GitHub release notes
- release tracker issue 负责版本号、轨道、发布分支、检查项和发版闸门，不负责承载完整 changelog 正文
- tracker issue 里可以保留一段很短的发布摘要，或直接放 `CHANGELOG.md` / GitHub Release 的链接

## 6. 建议边界

- tracker issue 负责承载版本元信息、检查项和发版闸门，不负责搬运每个功能 issue 的细节
- `CHANGELOG.md` 负责用户视角的版本变化摘要，不需要在 tracker issue 和 `docs/` 目录里各写一份完整副本
- 真正的工作拆分仍然在普通 issue 中完成
- milestone 表达“计划范围”，release tracker 表达“发版动作”

## 7. 失败处理

如果关闭 tracker issue 后自动发版失败，优先看三类问题：

- tracker issue 的版本号、轨道、milestone 不一致
- tracker issue 指定的发布分支不存在，或者填错了分支名
- milestone 下还有未完成的非 `release:stretch` issue
- 目标版本号已经被人手工发过，导致 tag/release 冲突

冲突修正后，可以重新打开并再次关闭 tracker issue，或者手动触发 release workflow 并填写相同的显式版本号。
