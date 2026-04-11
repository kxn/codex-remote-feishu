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
  | "capability"
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
  { id: "start", label: "环境检查", summary: "先确认这台机器当前能不能正常使用，再进入后面的飞书应用接入。" },
  { id: "connect", label: "接入飞书应用", summary: "选择这次要处理的飞书应用，并完成连接验证。" },
  { id: "capability", label: "能力检查", summary: "先把基础对话与交互准备好；增强项可以稍后再补。" },
  { id: "autostart", label: "自动启动（可选）", summary: "按当前平台展示自动启动能力；Linux 支持接入 systemd --user。", optional: true },
  { id: "vscode", label: "VS Code（可选）", summary: "不使用 VS Code 可以直接跳过；如果要用，再按当前机器场景选择接入方式。", optional: true },
  { id: "finish", label: "完成", summary: "提示首次对话路径，并进入本地管理页。" },
];
