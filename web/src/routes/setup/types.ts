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
  { id: "start", label: "环境检查", summary: "先确认这台机器现在能不能继续完成安装。" },
  { id: "connect", label: "接入飞书应用", summary: "选择这次要处理的飞书应用，并完成连接验证。" },
  { id: "capability", label: "能力检查", summary: "先把基础对话与交互准备好；增强项可以稍后再补。" },
  { id: "autostart", label: "自动启动（可选）", summary: "按当前机器情况决定是否开机后自动运行。", optional: true },
  { id: "vscode", label: "VS Code（可选）", summary: "不用 VS Code 可以直接跳过；要用时再处理这一项。", optional: true },
  { id: "finish", label: "完成", summary: "提示首次对话路径，并进入本地管理页。" },
];
