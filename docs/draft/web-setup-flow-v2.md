# Web Setup / Web Admin 改造方案与技术调研（V2 合并版）

> Type: `draft`
> Updated: `2026-04-29`
> Summary: 这份 V2 记录当前只保留一个页面级结论：setup/admin 的用户可见结构以 `v1.7.0` 为基线，admin 只额外增加 `Claude 配置`，不再继续扩展默认主界面。

## 1. 文档定位

本文不再承担新的页面方案设计职责。

它现在只保留一个结论性说明：

- 当前接受的页面级形态已经收敛到 `v1.7.0` 页面结构

如果要看当前页面合同，请直接参考：

- [web-setup-wizard-redesign.md](./web-setup-wizard-redesign.md)
- [web-onboarding-admin-user-view.md](./web-onboarding-admin-user-view.md)
- [web-onboarding-admin-workflow-prd.md](./web-onboarding-admin-workflow-prd.md)
- [web-admin-ui-redesign.md](../implemented/web-admin-ui-redesign.md)

## 2. 当前接受的页面结构

### 2.1 setup

setup 继续保持 `v1.7.0` 的向导结构：

- 左侧步骤栏
- 右侧当前步骤正文
- `环境检查`
- `飞书连接`
- `权限检查`
- `事件订阅`
- `回调配置`
- `菜单确认`
- `自动启动`
- `VS Code 集成`
- `完成`

### 2.2 admin

admin 继续保持 `v1.7.0` 的管理页结构，并只新增一块：

1. `机器人管理`
2. `系统集成`
3. `Claude 配置`
4. `存储管理`

## 3. 当前不再继续推进的页面方向

以下方向不再作为默认主界面目标：

- 运行概览
- 工作实例
- 技术详情单独分区
- 共享 onboarding 主界面
- 全局 workflow 总览
- “当前推荐处理”“剩余建议项”这类额外解释块

## 4. 使用方式

如果后续有人继续改页面，应按下面顺序判断：

1. 这次改动是否仍在 `v1.7.0` 的页面结构内？
2. 如果不是，是否真的需要改变默认主界面结构？
3. 如果需要，是否已经先改文档和 mock？

若以上任一答案是否，则不应继续直接扩页面。
