# Web Onboarding And Admin Workflow PRD

> Type: `draft`
> Updated: `2026-04-29`
> Summary: 当前接受的 web workflow 以 `v1.7.0` 页面结构为基线：setup 仍是分步向导，admin 仍是简单管理页，默认主界面只额外增加 `Claude 配置`，不再引入共享 onboarding 总览。

## 1. 文档定位

本文只记录当前接受的页面级工作流约束。

它回答四件事：

1. setup 解决什么问题。
2. admin 解决什么问题。
3. 页面默认主结构是什么。
4. 哪些后来出现的额外页面叙事不应继续保留。

相关文档：

- [web-setup-wizard-redesign.md](./web-setup-wizard-redesign.md)
- [web-onboarding-admin-user-view.md](./web-onboarding-admin-user-view.md)
- [web-admin-ui-redesign.md](../implemented/web-admin-ui-redesign.md)

## 2. 产品结论

当前接受的方向只有三条：

1. `setup` 继续使用 `v1.7.0` 的向导结构。
2. `admin` 继续使用 `v1.7.0` 的管理页结构。
3. 在 `admin` 上只额外增加 `Claude 配置` 区域。

除此之外，不再默认追加新的共享 onboarding 主界面。

## 3. Setup 负责什么

setup 只负责：

- 完成一次安装向导
- 接入并验证一个可用的飞书应用
- 走完 setup 内固定保留的步骤

setup 当前保留的步骤为：

1. `环境检查`
2. `飞书连接`
3. `权限检查`
4. `事件订阅`
5. `回调配置`
6. `菜单确认`
7. `自动启动`
8. `VS Code 集成`
9. `完成`

当前不采纳的方向：

- 把 setup 改成共享 onboarding workflow 工作台
- 用统一“能力检查页”替代事件、回调、菜单这些独立步骤
- 在 setup 顶部增加额外总览和推荐处理区

## 4. Admin 负责什么

admin 只负责日常管理。

当前默认主界面固定为：

1. `机器人管理`
2. `系统集成`
3. `Claude 配置`
4. `存储管理`

其中：

- `机器人管理` 继续沿用 `v1.7.0` 的 `左侧列表 + 右侧详情`
- `新增机器人` 允许复用和 setup 很像的连接流程
- `Claude 配置` 是唯一接受的新主界面分区

当前不采纳的方向：

- 运行概览
- 工作实例
- 技术详情单独分区
- 共享 onboarding 总览
- “当前推荐处理”“剩余建议项”之类的额外大段文字

## 5. 页面默认可见边界

### 5.1 setup

允许默认展示：

- 当前步骤
- 当前步骤结果
- 当前步骤入口
- 当前步骤需要的最短后台入口和复制信息

不允许默认展示：

- 全局 workflow 总览
- admin 级内容
- 与当前步骤无关的说明块

### 5.2 admin

允许默认展示：

- 机器人状态
- 机器人测试入口
- 系统集成状态
- Claude 配置列表和编辑区
- 存储占用和清理入口

不允许默认展示：

- workflow 总览
- 运行概览
- 工作实例
- 技术详情大分区
- 大段解释性文案

## 6. 后续变更规则

- 以后继续调整 setup/admin 时，先以 `v1.7.0` 页面结构为基线判断是否有必要改动。
- 如果只是流程逻辑变化，优先消化在现有区域内部，不新增主界面分区。
- 如果必须改变默认可见结构，先同步文档和 mock，再改产品页面。
