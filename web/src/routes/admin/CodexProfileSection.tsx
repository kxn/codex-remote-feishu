import {
  type Dispatch,
  type FormEvent,
  type SetStateAction,
  useState,
} from "react";
import {
  APIRequestError,
  formatError,
  requestJSON,
  requestVoid,
  sendJSON,
  sendJSONWithHeaders,
} from "../../lib/api";
import type {
  CodexContextPreferenceResponse,
  CodexProfileReference,
  CodexProfileReferencesResponse,
  CodexProfileResponse,
  CodexProfileSummary,
  CodexProfileWriteRequest,
} from "../../lib/types";
import {
  ConfigDeleteConfirmModal,
  ConfigFormDetailCard,
  ConfigSectionShell,
  type ConfigEditorSectionState,
  type EditorMode,
  useConfigEditorSection,
} from "./ConfigEditorShared";
import {
  appendOrReplaceProfileItem,
  maxProfileTextLengthMessage,
  removeProfileItem,
  requiredProfileFieldMessage,
} from "./ProfileEditorShared";

type CodexProfileDraft = {
  name: string;
  baseURL: string;
  apiKey: string;
  model: string;
  reviewModel: string;
  subagentModel: string;
  instruction: string;
  reasoningEffort: string;
  visionSupported: boolean;
  contextMode: string;
};

type CodexProfileSectionProps = {
  profiles: CodexProfileSummary[];
  loadError: string;
  setProfiles: Dispatch<SetStateAction<CodexProfileSummary[]>>;
  onReload: () => Promise<void>;
};

const newCodexProfileID = "new-codex-profile";
const codexReasoningOptions = ["low", "medium", "high", "xhigh"] as const;
const codexContextModeDefault = "codex_default";
const codexInstructionMaxChars = 16000;

export function CodexProfileSection(props: CodexProfileSectionProps) {
  const { profiles, loadError, setProfiles, onReload } = props;
  const [deleteReferences, setDeleteReferences] = useState<CodexProfileReference[]>([]);
  const editor = useConfigEditorSection<CodexProfileSummary, CodexProfileDraft>({
    items: profiles,
    newItemID: newCodexProfileID,
    createEmptyDraft,
    createDraftFromItem: createDraftFromProfile,
  });
  const {
    activeItem: activeProfile,
    activeItemID,
    actionBusy,
    applyNextItems,
    cancelCreate,
    deleteTargetID,
    detailNotice,
    draft,
    editorMode,
    handleItemSelect,
    selectPersistedItem,
    setActionBusy,
    setDeleteTargetID,
    setDetailNotice,
    setDraft,
    startCreateBlank,
  } = editor;

  async function handleSave(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    const validationError = validateDraft(draft, editorMode, activeProfile);
    if (validationError) {
      setDetailNotice({ tone: "warn", message: validationError });
      return;
    }

    setActionBusy("save-codex-profile");
    setDetailNotice(null);
    try {
      if (editorMode === "create") {
        const response = await sendJSON<CodexProfileResponse>(
          "/api/admin/codex/profiles",
          "POST",
          buildCreatePayload(draft),
        );
        let nextProfile = response.profile;
        if (draft.contextMode !== codexContextModeDefault) {
          nextProfile = await saveContextPreference(nextProfile, draft.contextMode);
        }
        setProfiles((current) => appendOrReplaceProfileItem(current, nextProfile));
        selectPersistedItem(nextProfile);
        setDetailNotice({ tone: "good", message: "Codex 配置已创建。" });
        return;
      }

      if (!activeProfile) {
        setDetailNotice({
          tone: "danger",
          message: "当前配置不存在，请重新选择后再试。",
        });
        return;
      }

      let nextProfile = activeProfile;
      const connectionEditable = activeProfile.kind === "api" && activeProfile.editable;
      if (connectionEditable) {
        nextProfile = (
          await sendJSONWithHeaders<CodexProfileResponse>(
            `/api/admin/codex/profiles/${encodeURIComponent(activeProfile.id)}`,
            "PUT",
            buildUpdatePayload(draft),
            { "If-Match": activeProfile.etag ?? "" },
          )
        ).profile;
      }
      if (activeProfile.contextEditable && draft.contextMode !== contextMode(activeProfile)) {
        nextProfile = await saveContextPreference(nextProfile, draft.contextMode);
      }
      setProfiles((current) =>
        appendOrReplaceProfileItem(current, nextProfile, activeProfile.id),
      );
      selectPersistedItem(nextProfile);
      setDetailNotice({
        tone: "good",
        message: connectionEditable ? "Codex 配置已保存。" : "上下文偏好已保存。",
      });
    } catch (error) {
      setDetailNotice({
        tone: "danger",
        message: `保存没有完成：${describeCodexProfileError(error)}`,
      });
    } finally {
      setActionBusy("");
    }
  }

  async function handleStartDelete(profileID: string | null) {
    if (!profileID) {
      return;
    }
    setActionBusy("load-codex-profile-references");
    setDetailNotice(null);
    try {
      const response = await requestJSON<CodexProfileReferencesResponse>(
        `/api/admin/codex/profiles/${encodeURIComponent(profileID)}/references`,
      );
      setDeleteReferences(response.references || []);
    } catch {
      setDeleteReferences([]);
    } finally {
      setActionBusy("");
      setDeleteTargetID(profileID);
    }
  }

  async function handleDelete() {
    if (!deleteTargetID) {
      return;
    }

    const profile = profiles.find((item) => item.id === deleteTargetID) ?? null;
    if (!profile || !profile.deletable) {
      setDeleteTargetID(null);
      setDetailNotice({
        tone: "warn",
        message: "这个配置不能删除。",
      });
      return;
    }

    setActionBusy("delete-codex-profile");
    setDetailNotice(null);
    try {
      await requestVoid(
        `/api/admin/codex/profiles/${encodeURIComponent(deleteTargetID)}`,
        {
          method: "DELETE",
          headers: { "If-Match": profile.etag ?? "" },
        },
      );
      const nextProfiles = removeProfileItem(profiles, deleteTargetID);
      setProfiles(nextProfiles);
      setDeleteTargetID(null);
      setDeleteReferences([]);
      applyNextItems(nextProfiles);
      setDetailNotice({ tone: "good", message: "Codex 配置已删除。" });
    } catch (error) {
      setDetailNotice({
        tone: "danger",
        message: `删除没有完成：${describeCodexProfileError(error)}`,
      });
    } finally {
      setActionBusy("");
    }
  }

  return (
    <>
      <ConfigSectionShell
        sectionTitle="Codex"
        sectionDescription="管理 Codex 连接与上下文偏好"
        emptyLoadErrorTitle="当前还不能读取 Codex 配置"
        loadError={loadError}
        onReload={onReload}
        items={profiles}
        activeItemID={activeItemID}
        newItemID={newCodexProfileID}
        onItemSelect={handleItemSelect}
        onStartCreate={startCreateBlank}
        getItemTitle={profileTitle}
        getItemSummary={profileCardSummary}
        getItemTag={profileKindLabel}
        newItemTitle="新增配置"
        newItemSummary="新建 API 配置"
        detailCard={renderCodexProfileDetailCard({
          actionBusy,
          activeProfile,
          deleteTargetID,
          detailNotice,
          draft,
          editorMode,
          onCancelCreate: cancelCreate,
          onDeleteTargetChange: (value) => void handleStartDelete(value),
          onDraftChange: setDraft,
          onSave: (event) => void handleSave(event),
          onStartCreate: startCreateBlank,
        })}
      />

      <ConfigDeleteConfirmModal
        targetID={deleteTargetID}
        items={profiles}
        dialogTitle="确认删除 Codex 配置"
        confirmDisabled={actionBusy === "delete-codex-profile"}
        getItemTitle={profileTitle}
        onCancel={() => {
          setDeleteTargetID(null);
          setDeleteReferences([]);
        }}
        onConfirm={() => void handleDelete()}
      >
        {deleteReferences.length > 0 ? (
          <div className="notice-banner warn">
            <strong>当前仍有使用中的会话。</strong>
            <ul>
              {deleteReferences.map((reference, index) => (
                <li key={`${reference.kind}-${index}`}>
                  {safeReferenceLabel(reference)}
                </li>
              ))}
            </ul>
          </div>
        ) : null}
      </ConfigDeleteConfirmModal>
    </>
  );

  async function saveContextPreference(
    profile: CodexProfileSummary,
    mode: string,
  ): Promise<CodexProfileSummary> {
    const preference = profile.contextPreference;
    const response = await sendJSONWithHeaders<CodexContextPreferenceResponse>(
      `/api/admin/codex/profiles/${encodeURIComponent(profile.id)}/context-preference`,
      "PUT",
      { mode },
      { "If-Match": preference.etag },
    );
    return {
      ...profile,
      contextPreference: response.contextPreference,
    };
  }
}

type CodexDetailCardProps = Pick<
  ConfigEditorSectionState<CodexProfileSummary, CodexProfileDraft>,
  "actionBusy" | "deleteTargetID" | "detailNotice" | "draft" | "editorMode"
> & {
  activeProfile: CodexProfileSummary | null;
  onCancelCreate: () => void;
  onDeleteTargetChange: (value: string | null) => void;
  onDraftChange: Dispatch<SetStateAction<CodexProfileDraft>>;
  onSave: (event: FormEvent<HTMLFormElement>) => void;
  onStartCreate: () => void;
};

function renderCodexProfileDetailCard(props: CodexDetailCardProps) {
  const {
    actionBusy,
    activeProfile,
    deleteTargetID,
    detailNotice,
    draft,
    editorMode,
    onCancelCreate,
    onDeleteTargetChange,
    onDraftChange,
    onSave,
  } = props;

  const isConnectionEditable = editorMode === "create" || Boolean(activeProfile?.editable);
  const canSave = isConnectionEditable || Boolean(activeProfile?.contextEditable);
  const title =
    editorMode === "create"
      ? draft.name.trim()
        ? `新增配置：${draft.name.trim()}`
        : "新增 Codex 配置"
      : profileTitle(activeProfile);
  const description =
    editorMode === "create"
      ? "填写 API 连接信息"
      : "";

  return (
    <ConfigFormDetailCard
      title={title}
      description={description}
      notice={detailNotice}
      onSave={onSave}
      submitLabel={
        isConnectionEditable
          ? editorMode === "create"
            ? "保存配置"
            : "保存修改"
          : "保存上下文偏好"
      }
      submitDisabled={actionBusy === "save-codex-profile" || !canSave}
      secondaryAction={
        editorMode === "create" ? (
          <button
            className="ghost-button"
            disabled={actionBusy === "save-codex-profile"}
            type="button"
            onClick={() => onCancelCreate()}
          >
            取消
          </button>
        ) : activeProfile?.deletable ? (
          <button
            className="danger-button"
            disabled={Boolean(deleteTargetID) || actionBusy !== ""}
            type="button"
            onClick={() => onDeleteTargetChange(activeProfile.id)}
          >
            删除配置
          </button>
        ) : null
      }
    >
      {isConnectionEditable ? (
        <div className="form-grid stack-top">
          <label className="field form-grid-span-2">
            <span>
              名称 <em className="field-required">*</em>
            </span>
            <input
              required
              value={draft.name}
              placeholder="例如：研发代理"
              onChange={(event) =>
                onDraftChange((current) => ({
                  ...current,
                  name: event.target.value,
                }))
              }
            />
          </label>

          <label className="field">
            <span>
              端点地址 <em className="field-required">*</em>
            </span>
            <input
              required
              value={draft.baseURL}
              placeholder="例如：https://api.example.com/v1"
              onChange={(event) =>
                onDraftChange((current) => ({
                  ...current,
                  baseURL: event.target.value,
                }))
              }
            />
          </label>

          <label className="field">
            <span>
              API Key{" "}
              {editorMode === "create" ? <em className="field-required">*</em> : null}
            </span>
            <input
              autoComplete="new-password"
              placeholder="输入新的 API Key"
              type="password"
              value={draft.apiKey}
              onChange={(event) =>
                onDraftChange((current) => ({
                  ...current,
                  apiKey: event.target.value,
                }))
              }
            />
          </label>

          <label className="field">
            <span>主模型</span>
            <input
              value={draft.model}
              placeholder="例如：gpt-5.5"
              onChange={(event) =>
                onDraftChange((current) => ({
                  ...current,
                  model: event.target.value,
                }))
              }
            />
          </label>

          <label className="field">
            <span>审阅模型</span>
            <input
              value={draft.reviewModel}
              placeholder="留空时跟随主模型"
              onChange={(event) =>
                onDraftChange((current) => ({
                  ...current,
                  reviewModel: event.target.value,
                }))
              }
            />
          </label>

          <label className="field">
            <span>子代理模型</span>
            <input
              value={draft.subagentModel}
              placeholder="留空时子代理跟随主模型"
              onChange={(event) =>
                onDraftChange((current) => ({
                  ...current,
                  subagentModel: event.target.value,
                }))
              }
            />
          </label>

          <label className="field">
            <span>
              推理强度 <em className="field-required">*</em>
            </span>
            <input
              aria-label="推理强度"
              list="codex-reasoning-options"
              placeholder="例如：high"
              value={draft.reasoningEffort}
              onChange={(event) =>
                onDraftChange((current) => ({
                  ...current,
                  reasoningEffort: event.target.value,
                }))
              }
            />
            <datalist id="codex-reasoning-options">
              {codexReasoningOptions.map((value) => (
                <option key={value} value={value}>
                  {value}
                </option>
              ))}
            </datalist>
          </label>
          <label className="field">
            <span>视觉能力</span>
            <input
              type="checkbox"
              aria-label="主模型支持直接看图"
              checked={draft.visionSupported}
              onChange={(event) =>
                onDraftChange((current) => ({
                  ...current,
                  visionSupported: event.target.checked,
                }))
              }
            />
            <span className="field-hint">
              勾选后该 profile 的主模型视为支持直接看图，不再注入 describe_image 图片描述辅助工具。
            </span>
          </label>
          <label className="field form-grid-span-2 stack-top">
            <span>指令 / 角色提示词（可选）</span>
            <textarea
              aria-label="指令 / 角色提示词（可选）"
              maxLength={codexInstructionMaxChars}
              placeholder="用于预设代理的角色与行为，留空不注入"
              rows={6}
              value={draft.instruction}
              onChange={(event) =>
                onDraftChange((current) => ({
                  ...current,
                  instruction: event.target.value,
                }))
              }
            />
            <span className="field-hint">
              {draft.instruction.length}/{codexInstructionMaxChars}
            </span>
          </label>
        </div>
      ) : (
        <div className="hero-card">
          <h3>{profileKindLabel(activeProfile)}</h3>
          <p>{readOnlyProfileDescription(activeProfile)}</p>
        </div>
      )}
      <label className="field form-grid-span-2 stack-top">
        <span className="sr-only">上下文大小</span>
        <select
          aria-label="上下文大小"
          disabled={editorMode !== "create" && !activeProfile?.contextEditable}
          value={draft.contextMode}
          onChange={(event) =>
            onDraftChange((current) => ({
              ...current,
              contextMode: event.target.value,
            }))
          }
        >
          <option value="codex_default">跟随 Codex</option>
          <option value="price_guard_272k">272K（费用优先）</option>
          <option value="extended_1m">1M（长上下文）</option>
        </select>
        <span className="field-hint">
          {contextStatusDescription(draft.contextMode, activeProfile)}
        </span>
      </label>
    </ConfigFormDetailCard>
  );
}

function createEmptyDraft(): CodexProfileDraft {
  return {
    name: "",
    baseURL: "",
    apiKey: "",
    model: "",
    reviewModel: "",
    subagentModel: "",
    instruction: "",
    reasoningEffort: "",
    visionSupported: false,
    contextMode: codexContextModeDefault,
  };
}

function createDraftFromProfile(profile: CodexProfileSummary): CodexProfileDraft {
  return {
    name: profileTitle(profile),
    baseURL: profile.baseURL?.trim() || "",
    apiKey: "",
    model: profile.model?.trim() || "",
    reviewModel: profile.reviewModel?.trim() || "",
    subagentModel: profile.subagentModel?.trim() || "",
    instruction: profile.instruction?.trim() || "",
    reasoningEffort: normalizeCodexReasoningEffort(profile.reasoningEffort),
    visionSupported: profile.visionSupported ?? false,
    contextMode: contextMode(profile),
  };
}

function validateDraft(
  draft: CodexProfileDraft,
  editorMode: EditorMode,
  activeProfile: CodexProfileSummary | null,
): string {
  if (editorMode !== "create" && activeProfile && !activeProfile.editable) {
    return "";
  }
  const nameError = requiredProfileFieldMessage(draft.name, "名称");
  if (nameError) {
    return nameError;
  }
  const baseURLError = requiredProfileFieldMessage(draft.baseURL, "端点地址");
  if (baseURLError) {
    return baseURLError;
  }
  const apiKeyError =
    editorMode === "create" ? requiredProfileFieldMessage(draft.apiKey, "API Key") : "";
  if (apiKeyError) {
    return apiKeyError;
  }
  const modelError = requiredProfileFieldMessage(draft.model, "主模型");
  if (modelError) {
    return modelError;
  }
  const reasoningError = requiredProfileFieldMessage(draft.reasoningEffort, "推理强度");
  if (reasoningError) {
    return reasoningError;
  }
  const instructionError = maxProfileTextLengthMessage(
    draft.instruction,
    codexInstructionMaxChars,
    "指令",
  );
  if (instructionError) {
    return instructionError;
  }
  return "";
}

function buildCreatePayload(draft: CodexProfileDraft): CodexProfileWriteRequest {
  return {
    name: draft.name.trim(),
    baseURL: draft.baseURL.trim(),
    apiKey: draft.apiKey,
    model: draft.model.trim(),
    reviewModel: draft.reviewModel.trim(),
    subagentModel: draft.subagentModel.trim(),
    instruction: draft.instruction.trim(),
    reasoningEffort: normalizeCodexReasoningEffort(draft.reasoningEffort),
    visionSupported: draft.visionSupported,
  };
}

function buildUpdatePayload(draft: CodexProfileDraft): CodexProfileWriteRequest {
  const payload: CodexProfileWriteRequest = {
    name: draft.name.trim(),
    baseURL: draft.baseURL.trim(),
    model: draft.model.trim(),
    reviewModel: draft.reviewModel.trim(),
    subagentModel: draft.subagentModel.trim(),
    instruction: draft.instruction.trim(),
    reasoningEffort: normalizeCodexReasoningEffort(draft.reasoningEffort),
    visionSupported: draft.visionSupported,
  };
  if (draft.apiKey) {
    payload.apiKey = draft.apiKey;
  }
  return payload;
}

function profileTitle(profile: CodexProfileSummary | null): string {
  if (!profile) {
    return "当前配置";
  }
  return profile.name?.trim() || "未命名配置";
}

function profileCardSummary(profile: CodexProfileSummary): string {
  const parts = [
    profileKindLabel(profile),
    profile.available ? "" : statusLabel(profile.statusCode),
    profile.baseURL?.trim() || "",
    profile.model?.trim() ? `模型 ${profile.model.trim()}` : "",
    normalizeCodexReasoningEffort(profile.reasoningEffort)
      ? `推理 ${normalizeCodexReasoningEffort(profile.reasoningEffort)}`
      : "",
    contextLabel(contextMode(profile)),
  ].filter(Boolean);
  return parts.join(" · ");
}

function profileKindLabel(profile: CodexProfileSummary | null): string {
  switch (profile?.kind) {
    case "native":
      return "本机默认";
    case "oauth":
      return "ChatGPT 登录";
    case "api":
      return "API";
    default:
      return "配置";
  }
}

function readOnlyProfileDescription(profile: CodexProfileSummary | null): string {
  if (profile?.kind === "oauth") {
    return "连接身份由 Codex 登录管理。";
  }
  return "连接身份由本机 Codex 管理。";
}

function statusLabel(statusCode?: string): string {
  switch (statusCode) {
    case "oauth_missing":
      return "未检测到登录";
    case "oauth_probe_unknown":
      return "登录状态暂未确认";
    case "oauth_deployment_unsupported":
      return "当前登录部署暂不支持";
    case "codex_capability_unsupported":
      return "当前 Codex 版本暂不支持";
    case "profile_catalog_degraded":
      return "配置暂不可用";
    default:
      return statusCode ? "暂不可用" : "";
  }
}

function contextLabel(mode: string): string {
  switch (mode) {
    case "price_guard_272k":
      return "272K";
    case "extended_1m":
      return "1M";
    default:
      return "跟随 Codex";
  }
}

function contextMode(profile: CodexProfileSummary): string {
  return profile.contextPreference?.mode || codexContextModeDefault;
}

function contextStatusDescription(
  mode: string,
  profile: CodexProfileSummary | null,
): string {
  if (mode === "codex_default") {
    return "不覆盖 Codex 默认上下文。";
  }
  if (mode === "price_guard_272k") {
    return "按费用优先请求 272K；这不是单次请求费用硬上限。";
  }
  if (profile?.contextStatus === "context_preference_clamped") {
    return profile.effectiveContextWindow
      ? `目标模型限制为约 ${formatContextWindow(profile.effectiveContextWindow)}。`
      : "目标模型限制了可用上下文。";
  }
  if (profile?.effectiveContextWindow) {
    return `已观察到约 ${formatContextWindow(profile.effectiveContextWindow)} 可用上下文。`;
  }
  return "新会话开始后确认实际生效值；可能受模型上限影响。";
}

function formatContextWindow(value: number): string {
  if (!Number.isFinite(value) || value <= 0) {
    return "";
  }
  if (value >= 1000000) {
    return `${(value / 1000000).toFixed(value % 1000000 === 0 ? 0 : 1)}M`;
  }
  return `${Math.round(value / 1000)}K`;
}

function describeCodexProfileError(error: unknown): string {
  if (error instanceof APIRequestError) {
    switch (error.code) {
      case "codex_profile_name_required":
        return "请填写名称。";
      case "codex_profile_base_url_required":
        return "请填写端点地址。";
      case "codex_profile_api_key_required":
        return "请填写 API Key。";
      case "codex_profile_reasoning_effort_invalid":
        return "推理强度不可用，请重新选择。";
      case "codex_profile_reserved_name":
        return "这个名称不能使用，请换一个名字。";
      case "duplicate_codex_profile_name":
        return "这个名称已经存在，请换一个名字。";
      case "codex_profile_read_only":
        return "这个配置的连接身份不能直接修改。";
      case "codex_profile_not_found":
        return "当前配置已经不存在，请重新读取后再试。";
      case "profile_revision_required":
      case "profile_preference_revision_required":
        return "页面状态已过期，请重新读取后再保存。";
      case "profile_revision_conflict":
      case "profile_preference_revision_conflict":
        return "这个配置已被其他窗口修改，请重新读取后再保存。";
      case "codex_profile_in_use":
        return "这个配置仍在使用中，请先切换或结束相关会话。";
      default:
        break;
    }
  }
  return formatError(error);
}

function safeReferenceLabel(reference: CodexProfileReference): string {
  const kind = referenceKindLabel(reference.kind);
  const name = sanitizeReferenceName(reference.name);
  const reason = referenceReasonLabel(reference.reason);
  return [kind, name, reason].filter(Boolean).join(" · ");
}

function referenceKindLabel(value?: string): string {
  switch (value?.trim()) {
    case "active_instance":
    case "session":
    case "thread":
      return "会话";
    case "queued_request":
    case "queue":
      return "队列";
    case "workspace":
      return "工作区";
    default:
      return "使用中";
  }
}

function referenceReasonLabel(value?: string): string {
  switch (value?.trim()) {
    case "profile_in_use":
    case "active_session":
      return "正在使用";
    case "pending_request":
      return "等待处理";
    default:
      return "";
  }
}

function sanitizeReferenceName(value?: string): string {
  const text = value?.trim() || "";
  if (!text || text.includes("/") || text.includes("\\") || text.includes(":")) {
    return "";
  }
  return text;
}

function normalizeCodexReasoningEffort(value: string | undefined): string {
  return value?.trim() ?? "";
}
