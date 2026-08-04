import type {
  FeishuAppAutoConfigPlan,
  FeishuAppAutoConfigRequirementStatus,
} from "../../lib/types";

export type AutoConfigRequirementDisplay = {
  label: string;
  detail: string;
};

export type AutoConfigRequirementRow = {
  key: string;
  label: string;
  meta: string;
  impacts: string[];
};

export function describeAutoConfigSummary(status: string): string {
  switch (status) {
    case "clean":
      return "当前飞书应用已经满足自动配置要求。";
    case "degraded":
      return "基础配置已完成，但仍有部分可选能力没有开通。";
    case "apply_required":
      return "当前还有飞书配置差异需要处理。";
    case "awaiting_review":
      return "飞书应用变更已经进入审核流程，当前只需等待结果。";
    case "verification_failed":
      return "暂时无法确认飞书配置状态，请重新检查或稍后再试。";
    case "blocked":
      return "当前阻塞项仍未解除，自动配置暂时不能继续。";
    default:
      return "当前还没有读取到自动配置状态。";
  }
}

export function buildMissingScopesImportJSON(
  plan: FeishuAppAutoConfigPlan | undefined | null,
): string {
  const missing = plan?.diff?.missingScopes || [];
  const tenant: string[] = [];
  const user: string[] = [];
  const seen = new Set<string>();
  for (const item of missing) {
    const scope = item.scope?.trim();
    if (!scope || seen.has(scope)) {
      continue;
    }
    seen.add(scope);
    if (item.scopeType === "user") {
      user.push(scope);
    } else {
      tenant.push(scope);
    }
  }
  return JSON.stringify({ scopes: { tenant, user } }, null, 2);
}

export function describeAutoConfigRefreshFeedback(status: string): string {
  switch (status) {
    case "clean":
    case "degraded":
      return "已重新检查，当前配置可以继续。";
    case "awaiting_review":
      return "已重新检查，飞书仍在审核发布。";
    case "apply_required":
      return "已重新检查，仍有飞书配置差异需要处理。";
    case "verification_failed":
      return "已重新检查，但暂时仍无法确认最终状态。";
    case "unsupported":
    case "blocked":
      return "已重新检查，仍有需要先处理的问题。";
    default:
      return "已重新检查当前状态。";
  }
}

export function describeAutoConfigBlockingReason(reason: string): string {
  switch (reason) {
    case "feishu_read_failed":
      return "暂时无法读取飞书应用配置，请稍后重新检查。";
    case "credential_invalid":
      return "当前飞书应用凭证已经失效，请重新连接飞书机器人。";
    case "permission_denied":
      return "当前账号没有修改飞书应用配置的权限，请使用有权限的管理员账号处理。";
    default:
      return "飞书返回的状态暂时无法处理，请稍后重新检查或到飞书后台处理。";
  }
}

export function describeAutoConfigRequirementLabel(
  requirement: FeishuAppAutoConfigRequirementStatus,
): string {
  if (requirement.kind === "scope") {
    return `权限 ${requirement.key}`;
  }
  if (requirement.kind === "event") {
    return `事件 ${requirement.key}`;
  }
  if (requirement.kind === "callback") {
    return `回调 ${requirement.key}`;
  }
  return requirement.key;
}

export function describeAutoConfigRequirementDetail(
  requirement: FeishuAppAutoConfigRequirementStatus,
): string {
  return describeAutoConfigRequirementImpacts(requirement).join("、");
}

export function describeAutoConfigRequirementDisplay(
  requirement: FeishuAppAutoConfigRequirementStatus,
): AutoConfigRequirementDisplay {
  return {
    label: describeAutoConfigRequirementLabel(requirement),
    detail: describeAutoConfigRequirementDetail(requirement),
  };
}

export function groupAutoConfigRequirements(
  requirements: FeishuAppAutoConfigRequirementStatus[],
): AutoConfigRequirementRow[] {
  const rows = new Map<string, AutoConfigRequirementRow>();
  for (const requirement of requirements) {
    const key = autoConfigRequirementGroupKey(requirement);
    const row =
      rows.get(key) ||
      {
        key,
        label: describeAutoConfigRequirementLabel(requirement),
        meta: describeAutoConfigRequirementMeta(requirement),
        impacts: [],
      };
    for (const impact of describeAutoConfigRequirementImpacts(requirement)) {
      if (!row.impacts.includes(impact)) {
        row.impacts.push(impact);
      }
    }
    rows.set(key, row);
  }
  return Array.from(rows.values());
}

export function autoConfigNoticeTone(status: string): "good" | "warn" | "danger" {
  switch (status) {
    case "clean":
      return "good";
    case "degraded":
    case "awaiting_review":
    case "verification_failed":
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

function describeAutoConfigRequirementImpacts(
  requirement: FeishuAppAutoConfigRequirementStatus,
): string[] {
  const impacts: string[] = [];
  const purpose = requirement.purpose?.trim();
  if (purpose) {
    impacts.push(purpose);
  }
  const feature = requirement.feature?.trim();
  if (feature) {
    const label = describeAutoConfigFeature(feature);
    if (label && !impacts.includes(label)) {
      impacts.push(label);
    }
  }
  const degradeMessage = requirement.degradeMessage?.trim();
  if (degradeMessage && !impacts.includes(degradeMessage)) {
    impacts.push(degradeMessage);
  }
  return impacts;
}

function describeAutoConfigRequirementMeta(
  requirement: FeishuAppAutoConfigRequirementStatus,
): string {
  if (requirement.kind === "scope") {
    return requirement.scopeType ? `权限 · ${requirement.scopeType}` : "权限";
  }
  if (requirement.kind === "event") {
    return "事件";
  }
  if (requirement.kind === "callback") {
    return "回调";
  }
  return "配置";
}

function autoConfigRequirementGroupKey(
  requirement: FeishuAppAutoConfigRequirementStatus,
): string {
  return [
    requirement.kind,
    requirement.scopeType || "",
    requirement.key,
  ].join(":");
}
