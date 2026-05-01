# Web Onboarding And Admin Workflow PRD

> Type: `draft`
> Updated: `2026-05-01`
> Summary: 当前 web workflow 已收敛到一个明确基线：setup 继续是 `v1.7.0` 向导，admin 继续是 `v1.7.0` 管理页，admin 默认加入 `Claude 配置` 与 `Codex Provider`，权限检查允许强制跳过。

## 1. 页面职责

### 1.1 setup

setup 只负责一次性安装配置。

它完成：

- 环境检查
- 飞书连接
- 权限检查
- setup 内保留的事件、回调、菜单、自动启动、VS Code 步骤

### 1.2 admin

admin 只负责后续日常管理。

它完成：

- 机器人状态查看
- 新增机器人
- 系统集成状态查看
- Claude 配置管理
- Codex Provider 管理
- 存储清理

## 2. 当前页面结构

### 2.1 setup

setup 保持 `v1.7.0` 向导结构：

- 左侧步骤栏
- 右侧单步正文

### 2.2 admin

admin 保持 `v1.7.0` 管理页结构：

1. `机器人管理`
2. `系统集成`
3. `Claude 配置`
4. `Codex Provider`
5. `存储管理`

## 3. 权限检查正式规则

权限检查现在统一采用同一条产品规则：

- 缺权限时允许用户先补齐再继续
- 允许 `强制跳过这一步`
- 跳过后继续后面的页面流程
- 后续仍可重新检查

这条规则同时适用于：

- setup 里的权限步骤
- admin 里新增机器人的相同权限处理步骤

## 4. runtime 入口边界

- `Codex Provider` 和 `Claude 配置` 共用 `send_settings` 同一个菜单位置
- 当前是 `codex headless` 时，只显示 `切换 Codex Provider`，slash 为 `/codexprovider`
- 当前是 `claude headless` 时，只显示 `切换 Claude 配置`，slash 为 `/claudeprofile`
- 当前是 `vscode` 时，两者都不显示
- 如果手动输入了当前 backend 不支持的那条命令，必须显式报错，不做 silent fallback
## 5. 变更要求

- 以后改 setup/admin 时，先以 `v1.7.0` 页面结构判断边界
- 流程变化优先放进现有区块，不新增默认主界面结构
- 如果必须改默认结构，必须先更新文档和 mock
