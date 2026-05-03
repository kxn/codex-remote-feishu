# Web 管理界面重设计

> Type: `implemented`
> Updated: `2026-05-04`
> Summary: 当前 admin 页面以 `v1.7.0` 为基线，默认主界面保留机器人管理、系统集成、Claude 配置、Codex Provider、存储管理五块；页面标题使用后端真实版本号，新增机器人流程与 setup 保持相同边界，并支持权限强制跳过。

## 1. 文档定位

本文记录已经落地的 admin 页面合同。

相关参照：

- [web-admin-user-mock.html](../draft/web-admin-user-mock.html)
- [web-onboarding-admin-user-view.md](../draft/web-onboarding-admin-user-view.md)
- [web-setup-wizard-redesign.md](../draft/web-setup-wizard-redesign.md)

## 2. 用户可见合同

- 最终用户：已经完成安装，正在日常管理机器人和本机集成的用户
- 当前任务：查看机器人状态、增加机器人、管理 Claude 配置、管理 Codex Provider、处理存储占用
- 允许展示：机器人状态、测试入口、机器集成状态、Claude 配置、Codex Provider、存储状态和清理入口
- 不允许展示：额外总控台结构、和当前任务无关的大段说明
- 反馈槽位：页顶通知、各卡片内部的状态条和按钮区

## 3. 页面结构

admin 继续保持 `v1.7.0` 的默认主结构，当前固定顺序为：

1. `机器人管理`
2. `系统集成`
3. `Claude 配置`
4. `Codex Provider`
5. `存储管理`

## 4. 机器人管理

`机器人管理` 继续使用 `v1.7.0` 的 `左侧列表 + 右侧详情` 结构。

其中：

- 左侧显示现有机器人和 `新增机器人`
- 右侧显示当前机器人的状态和测试入口
- `新增机器人` 的连接与权限处理流程，和 setup 里的新建流程保持同样边界

## 4.1 页面标题与后台链接

- admin 顶部标题和浏览器标题都使用后端真实版本号，例如 `Codex Remote Feishu v1.7.0 管理`
- 机器人相关的权限、事件、回调、菜单入口都由后端返回真实飞书后台链接
- admin 前端不再自己拼默认应用首页或旧兜底链接

## 4.2 新增机器人的连接与测试

- `新增机器人` 和 setup 继续共用同一套连接交互：
  - `扫码创建` 和 `手动输入`
  - 扫码默认自动开始，并按 2 秒轮询
  - `App ID`、`App Secret` 必填，`机器人名称` 可选
- 权限检查仍允许 `强制跳过这一步`
- 安装期测试发送目标使用当前机器人绑定的 web test recipient，不再回退 recent-surface fallback

## 5. 新增机器人里的权限规则

新增机器人时，权限检查采用和 setup 一样的正式规则：

- 补齐权限后继续
- 允许 `强制跳过这一步`
- 跳过后继续后面的设置
- 后续仍可回到这里重新检查

## 6. Claude 配置

`Claude 配置` 继续直接放在 admin 主界面，不额外扩展新的页面骨架。

## 7. Codex Provider

`Codex Provider` 也直接放在 admin 主界面，和 `Claude 配置` 同级。

它只负责：

- 管理可复用的 Codex Provider
- 编辑名称、端点地址、API Key
- 保留一个只读的 `系统默认`

## 8. 当前结论

- admin 不是新的 setup 页面
- setup/admin 的主界面结构都以 `v1.7.0` 为基线
- 当前接受的新默认主界面新增内容是 `Claude 配置` 与 `Codex Provider`
- 如果以后要改默认结构，必须先同步文档和 mock，再改产品页面
