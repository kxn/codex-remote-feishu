# Web 管理界面重设计

> Type: `implemented`
> Updated: `2026-08-11`
> Summary: 同步当前 Admin 后端配置合同：Claude、Codex 和 OpenCode 都在“对话后端”区域按 tab 管理。

## 1. 文档定位

本文记录已经落地的 admin 页面合同。

相关参照：

- [web-admin-user-mock.html](../obsoleted/web-admin-user-mock.html)（历史 mock）
- [web-onboarding-admin-user-view.md](../obsoleted/web-onboarding-admin-user-view.md)（历史流程文档）
- [web-setup-wizard-redesign.md](../obsoleted/web-setup-wizard-redesign.md)（历史流程文档）

## 2. 用户可见合同

- 最终用户：已经完成安装，正在日常管理机器人和本机集成的用户
- 当前任务：查看机器人状态、增加机器人、管理对话后端配置、处理存储占用
- 允许展示：机器人状态、测试入口、机器集成状态、Claude 配置、Codex Profile、OpenCode 配置、存储状态和清理入口
- 不允许展示：额外总控台结构、和当前任务无关的大段说明
- 反馈槽位：页顶通知、各卡片内部的状态条和按钮区

## 3. 页面结构

admin 继续保持 `v1.7.0` 后形成的管理页主结构。当前固定顺序为：

1. `机器人管理`
2. `系统集成`
3. `对话后端`
   - `Claude`
   - `Codex`
   - `OpenCode`
4. `存储管理`

## 4. 机器人管理

`机器人管理` 继续使用 `v1.7.0` 的 `左侧列表 + 右侧详情` 结构。

其中：

- 左侧显示现有机器人和 `新增机器人`
- 右侧显示当前机器人的状态和测试入口
- `新增机器人` 的飞书接入流程，和 setup 里的飞书连接流程保持同样边界

## 4.1 页面标题与后台链接

- admin 顶部标题和浏览器标题都使用后端真实版本号，例如 `Codex Remote Feishu v1.7.0 管理`
- 机器人相关的飞书后台入口由后端返回真实链接
- admin 前端不再自己拼默认应用首页或旧兜底链接

## 4.2 新增机器人的连接

- `新增机器人` 和 setup 继续共用同一套连接交互：
  - 扫码新建飞书应用
  - 手动接入已有飞书应用
  - `App ID`、`App Secret` 必填，`机器人名称` 可选
- 连接完成后进入当前自动配置/确认路径；admin 不再保留旧的 `权限检查 / 事件订阅 / 回调配置` 分步合同
- 需要人工处理的飞书后台事项，应以当前后端返回的状态和链接为准

## 5. 新增机器人里的配置边界

新增机器人不再单独定义一套权限检查规则。

当前边界是：

- 连接入口由 admin 页面提供
- 飞书应用的能力收敛由后端自动配置路径负责
- 菜单、权限或其他飞书后台事项如果需要人工处理，应通过当前状态、链接和提示暴露，不在 admin 文档中保留旧三步流程

## 6. 对话后端

`对话后端` 直接放在 admin 主界面，不额外扩展新的页面骨架。

当前后端配置使用同一个区域内的 tab：

- `Claude`
- `Codex`
- `OpenCode`

### 6.1 Claude 配置

`Claude` tab 管理可复用的 Claude 配置。

### 6.2 Codex Profile

`Codex` tab 管理可复用的 Codex Profile。

它只负责：

- 管理可复用的 Codex Profile
- 对 API Profile 编辑名称、端点地址、API Key、模型和推理配置
- 对只读连接 Profile 保留上下文偏好修改入口
- 保留一个只读连接身份的 `本机默认`
- admin 只使用 `/api/admin/codex/profiles`；旧 `/api/admin/codex/providers` 已移除

### 6.3 OpenCode 配置

`OpenCode` tab 管理可复用的 OpenCode API 配置。

它只负责：

- 管理可复用的 OpenCode Profile
- 对 API Profile 编辑名称、端点地址、API Key、模型、small/subagent 模型、指令和推理配置
- 保留一个只读连接身份的 `本机默认`
- admin 使用 `/api/admin/opencode/profiles`
- OpenCode 的隐藏/预留字段不在 Web 表单中展示或提交；这些字段的抽象边界以对应 OpenCode profile issue 和实现文档为准

## 8. 当前结论

- admin 不是新的 setup 页面
- setup/admin 的主界面结构都以 `v1.7.0` 为基线
- 当前默认主界面里的后端管理入口是 `对话后端` 区域，包含 `Claude`、`Codex`、`OpenCode` 三个 tab
- 飞书接入流程的当前 SSOT 是 setup/admin 共用的连接入口和后端自动配置状态，不是旧的权限/事件/回调分步文档
- 如果以后要改默认结构，必须先同步文档和 mock，再改产品页面
