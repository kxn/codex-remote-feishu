export const newAppID = "__new__";

export type SetupDraft = {
  isNew: boolean;
  name: string;
  appId: string;
  appSecret: string;
};

export type SetupNotice = {
  tone: "good" | "warn";
  message: string;
};

export type FeishuConnectMode = "new" | "existing";

export type FeishuConnectStage = "mode_select" | "new_qr" | "new_qr_notice" | "existing_manual";

export type BlockingErrorState = {
  title: string;
  message: string;
  detail?: string;
} | null;

export type StepID =
  | "start"
  | "connect"
  | "permissions"
  | "events"
  | "longConnection"
  | "menus"
  | "publish"
  | "runtimeRequirements"
  | "autostart"
  | "vscode"
  | "finish";

export type WizardStep = {
  id: StepID;
  label: string;
  summary: string;
  optional?: boolean;
};

export type StepCompletion = Record<Exclude<StepID, "finish">, boolean>;

export const wizardSteps: WizardStep[] = [
  { id: "start", label: "开始", summary: "说明安装向导会做什么。" },
  { id: "connect", label: "创建并连接飞书应用", summary: "创建应用、添加机器人能力，并完成连接测试。" },
  { id: "permissions", label: "配置应用权限", summary: "复制 scopes JSON，并在“批量导入/导出权限”里保存申请。" },
  { id: "events", label: "配置事件订阅", summary: "按 manifest 订阅需要的飞书事件；卡片回调放到下一步配置。" },
  { id: "longConnection", label: "配置回调订阅方式", summary: "把“回调订阅方式”设为长连接，并完成卡片回调配置。" },
  { id: "menus", label: "配置机器人菜单", summary: "按 key 创建真正会生效的机器人菜单。" },
  { id: "publish", label: "发布应用", summary: "发版后执行一次服务端验收检查。" },
  { id: "runtimeRequirements", label: "运行环境检查", summary: "统一检查当前机器是否已经满足正常使用要求，尤其是 headless 基础可用性。" },
  { id: "autostart", label: "自动启动（可选）", summary: "按当前平台展示自动启动能力；Linux 支持接入 systemd --user。", optional: true },
  { id: "vscode", label: "VS Code（可选）", summary: "不使用 VS Code 可以直接跳过；如果要用，再按当前机器场景选择接入方式。", optional: true },
  { id: "finish", label: "完成", summary: "提示首次对话路径，并进入本地管理页。" },
];
