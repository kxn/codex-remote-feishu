import type { FeishuAppAutoConfigRequirementStatus } from "../../lib/types";

export type AutoConfigRequirementDisplay = {
  label: string;
  detail: string;
};

export function describeAutoConfigTag(
  status: string,
): { label: string; warn: boolean } | null {
  switch (status) {
    case "clean":
      return { label: "已完成", warn: false };
    case "degraded":
      return { label: "有降级", warn: true };
    case "apply_required":
      return { label: "待补齐", warn: true };
    case "publish_required":
      return { label: "待发布", warn: true };
    case "awaiting_review":
      return { label: "待审核", warn: true };
    case "blocked":
      return { label: "受阻", warn: true };
    case "runtime_pending":
      return { label: "同步中", warn: true };
    case "loading":
      return { label: "检查中", warn: false };
    default:
      return null;
  }
}

export function describeAutoConfigHeadline(status: string): string {
  switch (status) {
    case "clean":
      return "已自动完成";
    case "degraded":
      return "已完成，但存在功能降级";
    case "apply_required":
      return "当前还需要自动补齐配置";
    case "publish_required":
      return "自动补齐已完成，还需要提交发布";
    case "awaiting_review":
      return "已提交发布，正在等待管理员处理";
    case "blocked":
      return "当前还不能继续自动配置";
    default:
      return "自动配置状态暂不可用";
  }
}

export function describeAutoConfigSummary(status: string): string {
  switch (status) {
    case "clean":
      return "当前飞书应用已经满足自动配置要求。";
    case "degraded":
      return "基础配置已完成，但仍有部分可选能力没有开通。";
    case "apply_required":
      return "当前检查到了仍需自动补齐的配置差异。";
    case "publish_required":
      return "自动补齐后的配置已经写入，仍需提交飞书发布。";
    case "awaiting_review":
      return "飞书应用变更已经进入审核流程，当前只需等待结果。";
    case "blocked":
      return "当前阻塞项仍未解除，自动配置暂时不能继续。";
    default:
      return "当前还没有读取到自动配置状态。";
  }
}

export function describeAutoConfigBlockingReason(reason: string): string {
  switch (reason) {
    case "application_under_review":
      return "飞书开放平台上的应用版本仍在审核中。";
    case "apply_required_before_publish":
      return "还需要先完成自动补齐，之后才能提交发布。";
    default:
      return reason.trim() || "当前状态暂未给出更多说明。";
  }
}

export function describeAutoConfigRequirementLabel(
  requirement: FeishuAppAutoConfigRequirementStatus,
): string {
  if (requirement.purpose?.trim()) {
    return requirement.purpose.trim();
  }
  if (requirement.feature?.trim()) {
    const feature = describeAutoConfigFeature(requirement.feature);
    if (feature) {
      return feature;
    }
  }
  if (requirement.kind === "scope") {
    return `权限 ${requirement.key}`;
  }
  return requirement.key;
}

export function describeAutoConfigRequirementDetail(
  requirement: FeishuAppAutoConfigRequirementStatus,
): string {
  if (requirement.degradeMessage?.trim()) {
    return requirement.degradeMessage.trim();
  }
  if (requirement.kind === "scope") {
    return "需要在飞书开放平台补齐对应权限后才能继续。";
  }
  if (requirement.kind === "event") {
    return "需要先在飞书后台开通对应事件。";
  }
  if (requirement.kind === "callback") {
    return "需要先在飞书后台开通对应卡片交互回调。";
  }
  return "";
}

export function describeAutoConfigRequirementDisplay(
  requirement: FeishuAppAutoConfigRequirementStatus,
): AutoConfigRequirementDisplay {
  return {
    label: describeAutoConfigRequirementLabel(requirement),
    detail: describeAutoConfigRequirementDetail(requirement),
  };
}

export function autoConfigNoticeTone(status: string): "good" | "warn" | "danger" {
  switch (status) {
    case "clean":
      return "good";
    case "degraded":
    case "publish_required":
    case "awaiting_review":
      return "warn";
    default:
      return "danger";
  }
}

export function onboardingAutoConfigNoticeTone(
  status: string,
): "good" | "warn" | "danger" {
  switch (status) {
    case "complete":
      return "good";
    case "deferred":
      return "warn";
    case "blocked":
      return "danger";
    default:
      return "warn";
  }
}

function describeAutoConfigFeature(feature: string): string {
  switch (feature) {
    case "core_message_flow":
      return "机器人基础消息能力";
    case "interactive_cards":
      return "卡片交互能力";
    case "markdown_preview":
      return "Markdown 预览";
    case "cron_bitable":
      return "/cron 多维表格";
    case "time_sensitive_indicator":
      return "等待输入提醒";
    case "group_mentions":
      return "群聊 @ 消息";
    case "p2p_chat":
      return "单聊消息";
    case "reaction_feedback":
      return "消息 reaction 反馈";
    case "message_recall_sync":
      return "撤回消息同步";
    case "bot_menu":
      return "机器人菜单";
    default:
      return "";
  }
}
