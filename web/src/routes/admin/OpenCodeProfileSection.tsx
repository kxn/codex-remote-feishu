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
  OpenCodeProfileReference,
  OpenCodeProfileReferencesResponse,
  OpenCodeProfileResponse,
  OpenCodeProfileSummary,
  OpenCodeProfileWriteRequest,
} from "../../lib/types";
import {
  ConfigBuiltInDetailCard,
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

type OpenCodeProfileDraft = {
  name: string;
  providerType: string;
  baseURL: string;
  apiKey: string;
  model: string;
  smallModel: string;
  subagentModel: string;
  instruction: string;
  reasoningEffort: string;
};

type OpenCodeProfileSectionProps = {
  profiles: OpenCodeProfileSummary[];
  loadError: string;
  setProfiles: Dispatch<SetStateAction<OpenCodeProfileSummary[]>>;
  onReload: () => Promise<void>;
};

const newOpenCodeProfileID = "new-opencode-profile";
const openCodeProviderTypeOpenAICompatibleChat = "openai_compatible_chat";
const openCodeProviderTypeGoogleGemini = "google_gemini";
const openCodeReasoningOptions = ["low", "medium", "high", "xhigh"] as const;
const openCodeInstructionMaxChars = 16000;

export function OpenCodeProfileSection(props: OpenCodeProfileSectionProps) {
  const { profiles, loadError, setProfiles, onReload } = props;
  const [deleteReferences, setDeleteReferences] = useState<OpenCodeProfileReference[]>([]);
  const editor = useConfigEditorSection<OpenCodeProfileSummary, OpenCodeProfileDraft>({
    items: profiles,
    newItemID: newOpenCodeProfileID,
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
    const validationError = validateDraft(draft, editorMode);
    if (validationError) {
      setDetailNotice({ tone: "warn", message: validationError });
      return;
    }

    setActionBusy("save-opencode-profile");
    setDetailNotice(null);
    try {
      if (editorMode === "create") {
        const response = await sendJSON<OpenCodeProfileResponse>(
          "/api/admin/opencode/profiles",
          "POST",
          buildCreatePayload(draft),
        );
        setProfiles((current) => appendOrReplaceProfileItem(current, response.profile));
        selectPersistedItem(response.profile);
        setDetailNotice({ tone: "good", message: "OpenCode 配置已创建。" });
        return;
      }

      if (!activeProfile || activeProfile.readOnly || activeProfile.builtIn) {
        setDetailNotice({
          tone: "warn",
          message: "这个配置不能直接编辑。",
        });
        return;
      }

      const response = await sendJSONWithHeaders<OpenCodeProfileResponse>(
        `/api/admin/opencode/profiles/${encodeURIComponent(activeProfile.id)}`,
        "PUT",
        buildUpdatePayload(draft),
        { "If-Match": activeProfile.etag ?? "" },
      );
      const nextProfile = response.profile;
      setProfiles((current) =>
        appendOrReplaceProfileItem(current, nextProfile, activeProfile.id),
      );
      selectPersistedItem(nextProfile);
      setDetailNotice({ tone: "good", message: "OpenCode 配置已保存。" });
    } catch (error) {
      setDetailNotice({
        tone: "danger",
        message: `保存没有完成：${describeOpenCodeProfileError(error)}`,
      });
    } finally {
      setActionBusy("");
    }
  }

  async function handleStartDelete(profileID: string | null) {
    if (!profileID) {
      return;
    }
    setActionBusy("load-opencode-profile-references");
    setDetailNotice(null);
    try {
      const response = await requestJSON<OpenCodeProfileReferencesResponse>(
        `/api/admin/opencode/profiles/${encodeURIComponent(profileID)}/references`,
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
    if (!profile || profile.builtIn || profile.readOnly) {
      setDeleteTargetID(null);
      setDetailNotice({
        tone: "warn",
        message: "这个配置不能删除。",
      });
      return;
    }

    setActionBusy("delete-opencode-profile");
    setDetailNotice(null);
    try {
      await requestVoid(
        `/api/admin/opencode/profiles/${encodeURIComponent(deleteTargetID)}`,
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
      setDetailNotice({ tone: "good", message: "OpenCode 配置已删除。" });
    } catch (error) {
      setDetailNotice({
        tone: "danger",
        message: `删除没有完成：${describeOpenCodeProfileError(error)}`,
      });
    } finally {
      setActionBusy("");
    }
  }

  return (
    <>
      <ConfigSectionShell
        sectionTitle="OpenCode"
        sectionDescription="管理 OpenCode API 配置"
        emptyLoadErrorTitle="当前还不能读取 OpenCode 配置"
        loadError={loadError}
        onReload={onReload}
        items={profiles}
        activeItemID={activeItemID}
        newItemID={newOpenCodeProfileID}
        onItemSelect={handleItemSelect}
        onStartCreate={startCreateBlank}
        getItemTitle={profileTitle}
        getItemSummary={profileCardSummary}
        getItemTag={profileTag}
        newItemTitle="新增配置"
        newItemSummary="新建 API 配置"
        detailCard={renderOpenCodeProfileDetailCard({
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
        dialogTitle="确认删除 OpenCode 配置"
        confirmDisabled={actionBusy === "delete-opencode-profile"}
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
}

type OpenCodeDetailCardProps = Pick<
  ConfigEditorSectionState<OpenCodeProfileSummary, OpenCodeProfileDraft>,
  "actionBusy" | "deleteTargetID" | "detailNotice" | "draft" | "editorMode"
> & {
  activeProfile: OpenCodeProfileSummary | null;
  onCancelCreate: () => void;
  onDeleteTargetChange: (value: string | null) => void;
  onDraftChange: Dispatch<SetStateAction<OpenCodeProfileDraft>>;
  onSave: (event: FormEvent<HTMLFormElement>) => void;
  onStartCreate: () => void;
};

function renderOpenCodeProfileDetailCard(props: OpenCodeDetailCardProps) {
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
    onStartCreate,
  } = props;

  if (editorMode === "built-in" || activeProfile?.readOnly) {
    return (
      <ConfigBuiltInDetailCard
        title={profileTitle(activeProfile)}
        description="系统默认的 OpenCode 连接"
        notice={detailNotice}
        heroTitle="系统默认配置"
        heroDescription="使用本机 OpenCode 当前配置。"
        startCreateLabel="新增 OpenCode 配置"
        onStartCreate={onStartCreate}
      />
    );
  }

  const title =
    editorMode === "create"
      ? draft.name.trim()
        ? `新增配置：${draft.name.trim()}`
        : "新增 OpenCode 配置"
      : profileTitle(activeProfile);
  const description = editorMode === "create" ? "填写 API 连接信息" : "";

  return (
    <ConfigFormDetailCard
      title={title}
      description={description}
      notice={detailNotice}
      onSave={onSave}
      submitLabel={editorMode === "create" ? "保存配置" : "保存修改"}
      submitDisabled={actionBusy === "save-opencode-profile"}
      secondaryAction={
        editorMode === "create" ? (
          <button
            className="ghost-button"
            disabled={actionBusy === "save-opencode-profile"}
            type="button"
            onClick={() => onCancelCreate()}
          >
            取消
          </button>
        ) : (
          <button
            className="danger-button"
            disabled={Boolean(deleteTargetID) || actionBusy !== ""}
            type="button"
            onClick={() => onDeleteTargetChange(activeProfile?.id ?? null)}
          >
            删除配置
          </button>
        )
      }
    >
      <div className="form-grid stack-top">
        <label className="field">
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
            协议类型 <em className="field-required">*</em>
          </span>
          <select
            required
            aria-label="协议类型"
            value={normalizeOpenCodeProviderType(draft.providerType)}
            onChange={(event) =>
              onDraftChange((current) => ({
                ...current,
                providerType: event.target.value,
              }))
            }
          >
            <option value={openCodeProviderTypeOpenAICompatibleChat}>OpenAI 兼容</option>
            <option value={openCodeProviderTypeGoogleGemini}>Gemini</option>
          </select>
        </label>

        <label className="field">
          <span>
            端点地址{" "}
            {normalizeOpenCodeProviderType(draft.providerType) ===
            openCodeProviderTypeOpenAICompatibleChat ? (
              <em className="field-required">*</em>
            ) : null}
          </span>
          <input
            required={
              normalizeOpenCodeProviderType(draft.providerType) ===
              openCodeProviderTypeOpenAICompatibleChat
            }
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
            required={editorMode === "create"}
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
          <span>
            主模型 <em className="field-required">*</em>
          </span>
          <input
            aria-label="主模型"
            required
            value={draft.model}
            placeholder="例如：kimi-k2"
            onChange={(event) =>
              onDraftChange((current) => ({
                ...current,
                model: event.target.value,
              }))
            }
          />
        </label>

        <label className="field">
          <span>轻量模型</span>
          <input
            value={draft.smallModel}
            placeholder="留空时跟随主模型"
            onChange={(event) =>
              onDraftChange((current) => ({
                ...current,
                smallModel: event.target.value,
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
          <span>推理强度</span>
          <input
            aria-label="推理强度"
            list="opencode-reasoning-options"
            placeholder="例如：high"
            value={draft.reasoningEffort}
            onChange={(event) =>
              onDraftChange((current) => ({
                ...current,
                reasoningEffort: event.target.value,
              }))
            }
          />
          <datalist id="opencode-reasoning-options">
            {openCodeReasoningOptions.map((value) => (
              <option key={value} value={value}>
                {value}
              </option>
            ))}
          </datalist>
        </label>

        <label className="field form-grid-span-2 stack-top">
          <span>指令 / 角色提示词（可选）</span>
          <textarea
            aria-label="指令 / 角色提示词（可选）"
            maxLength={openCodeInstructionMaxChars}
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
            {draft.instruction.length}/{openCodeInstructionMaxChars}
          </span>
        </label>
      </div>
    </ConfigFormDetailCard>
  );
}

function createEmptyDraft(): OpenCodeProfileDraft {
  return {
    name: "",
    providerType: openCodeProviderTypeOpenAICompatibleChat,
    baseURL: "",
    apiKey: "",
    model: "",
    smallModel: "",
    subagentModel: "",
    instruction: "",
    reasoningEffort: "",
  };
}

function createDraftFromProfile(profile: OpenCodeProfileSummary): OpenCodeProfileDraft {
  return {
    name: profileTitle(profile),
    providerType: normalizeOpenCodeProviderType(profile.providerType),
    baseURL: profile.baseURL?.trim() || "",
    apiKey: "",
    model: profile.model?.trim() || "",
    smallModel: profile.smallModel?.trim() || "",
    subagentModel: profile.subagentModel?.trim() || "",
    instruction: profile.instruction?.trim() || "",
    reasoningEffort: normalizeOpenCodeReasoningEffort(profile.reasoningEffort),
  };
}

function validateDraft(draft: OpenCodeProfileDraft, editorMode: EditorMode): string {
  if (editorMode === "built-in") {
    return "";
  }
  const nameError = requiredProfileFieldMessage(draft.name, "名称");
  if (nameError) {
    return nameError;
  }
  const baseURLError = requiredProfileFieldMessage(draft.baseURL, "端点地址");
  if (
    normalizeOpenCodeProviderType(draft.providerType) ===
      openCodeProviderTypeOpenAICompatibleChat &&
    baseURLError
  ) {
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
  const instructionError = maxProfileTextLengthMessage(
    draft.instruction,
    openCodeInstructionMaxChars,
    "指令",
  );
  if (instructionError) {
    return instructionError;
  }
  return "";
}

function buildCreatePayload(draft: OpenCodeProfileDraft): OpenCodeProfileWriteRequest {
  return {
    name: draft.name.trim(),
    providerType: normalizeOpenCodeProviderType(draft.providerType),
    baseURL: draft.baseURL.trim(),
    apiKey: draft.apiKey,
    model: draft.model.trim(),
    smallModel: draft.smallModel.trim(),
    subagentModel: draft.subagentModel.trim(),
    instruction: draft.instruction.trim(),
    reasoningEffort: normalizeOpenCodeReasoningEffort(draft.reasoningEffort),
  };
}

function buildUpdatePayload(draft: OpenCodeProfileDraft): OpenCodeProfileWriteRequest {
  const payload: OpenCodeProfileWriteRequest = {
    name: draft.name.trim(),
    providerType: normalizeOpenCodeProviderType(draft.providerType),
    baseURL: draft.baseURL.trim(),
    model: draft.model.trim(),
    smallModel: draft.smallModel.trim(),
    subagentModel: draft.subagentModel.trim(),
    instruction: draft.instruction.trim(),
    reasoningEffort: normalizeOpenCodeReasoningEffort(draft.reasoningEffort),
  };
  const apiKey = optionalString(draft.apiKey);
  if (apiKey) {
    payload.apiKey = apiKey;
  }
  return payload;
}

function optionalString(value: string): string | undefined {
  const trimmed = value.trim();
  return trimmed ? trimmed : undefined;
}

function normalizeOpenCodeProviderType(value: string | undefined): string {
  switch (value?.trim()) {
    case openCodeProviderTypeGoogleGemini:
      return openCodeProviderTypeGoogleGemini;
    default:
      return openCodeProviderTypeOpenAICompatibleChat;
  }
}

function normalizeOpenCodeReasoningEffort(value: string | undefined): string {
  return value?.trim() ?? "";
}

function profileTitle(profile: OpenCodeProfileSummary | null): string {
  if (!profile) {
    return "当前配置";
  }
  return profile.name?.trim() || "未命名配置";
}

function profileCardSummary(profile: OpenCodeProfileSummary): string {
  if (profile.builtIn) {
    return "本机默认配置";
  }
  const parts = [
    profile.available ? "" : statusLabel(profile.statusCode),
    profile.baseURL?.trim() || openCodeProviderTypeLabel(profile.providerType),
    profile.model?.trim() ? `模型 ${profile.model.trim()}` : "",
    profile.smallModel?.trim() ? `轻量 ${profile.smallModel.trim()}` : "",
    profile.subagentModel?.trim() ? `子代理 ${profile.subagentModel.trim()}` : "",
    normalizeOpenCodeReasoningEffort(profile.reasoningEffort)
      ? `推理 ${normalizeOpenCodeReasoningEffort(profile.reasoningEffort)}`
      : "",
  ].filter(Boolean);
  return parts.join(" · ");
}

function profileTag(profile: OpenCodeProfileSummary): string {
  return profile.builtIn ? "默认" : "API";
}

function openCodeProviderTypeLabel(value: string | undefined): string {
  switch (normalizeOpenCodeProviderType(value)) {
    case openCodeProviderTypeGoogleGemini:
      return "Gemini";
    default:
      return "OpenAI 兼容";
  }
}

function statusLabel(statusCode?: string): string {
  switch (statusCode) {
    case "profile_definition_incomplete":
      return "配置未完整";
    case "profile_secret_missing":
      return "缺少 API Key";
    default:
      return statusCode ? "暂不可用" : "";
  }
}

function describeOpenCodeProfileError(error: unknown): string {
  if (error instanceof APIRequestError) {
    switch (error.code) {
      case "profile_revision_required":
        return "页面状态已过期，请重新读取后再保存。";
      case "profile_revision_conflict":
        return "这个配置已被其他窗口修改，请重新读取后再保存。";
      case "profile_not_found":
        return "当前配置已经不存在，请重新读取后再试。";
      case "opencode_profile_read_only":
        return "这个配置不能直接修改。";
      case "profile_in_use":
        return "这个配置仍在使用中，请先切换或结束相关会话。";
      case "invalid_opencode_profile":
        return "请检查配置内容。";
      default:
        break;
    }
  }
  return formatError(error);
}

function safeReferenceLabel(reference: OpenCodeProfileReference): string {
  const kind = referenceKindLabel(reference.kind);
  const name = sanitizeReferenceName(reference.name);
  return [kind, name].filter(Boolean).join(" · ");
}

function referenceKindLabel(value?: string): string {
  switch (value?.trim()) {
    case "active_instance":
    case "surface_desired":
    case "surface_actual":
      return "会话";
    case "pending_headless":
      return "待启动";
    case "queue_item":
    case "active_queue_item":
      return "队列";
    case "bot_default":
      return "机器人";
    default:
      return "使用中";
  }
}

function sanitizeReferenceName(value?: string): string {
  const text = value?.trim() || "";
  if (!text || text.includes("/") || text.includes("\\") || text.includes(":")) {
    return "";
  }
  return text;
}
