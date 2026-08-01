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

type CodexProviderDraft = {
  name: string;
  baseURL: string;
  apiKey: string;
  model: string;
  reviewModel: string;
  reasoningEffort: string;
  contextMode: string;
};

type CodexProviderSectionProps = {
  providers: CodexProfileSummary[];
  loadError: string;
  setProviders: Dispatch<SetStateAction<CodexProfileSummary[]>>;
  onReload: () => Promise<void>;
};

const newCodexProviderID = "new-codex-profile";
const codexReasoningOptions = ["low", "medium", "high", "xhigh"] as const;
const codexContextModeDefault = "codex_default";

export function CodexProviderSection(props: CodexProviderSectionProps) {
  const { providers, loadError, setProviders, onReload } = props;
  const [deleteReferences, setDeleteReferences] = useState<CodexProfileReference[]>([]);
  const editor = useConfigEditorSection<CodexProfileSummary, CodexProviderDraft>({
    items: providers,
    newItemID: newCodexProviderID,
    createEmptyDraft,
    createDraftFromItem: createDraftFromProvider,
  });
  const {
    activeItem: activeProvider,
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
    const validationError = validateDraft(draft, editorMode, activeProvider);
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
        setProviders((current) => appendOrReplaceProvider(current, nextProfile));
        selectPersistedItem(nextProfile);
        setDetailNotice({ tone: "good", message: "Codex Profile 已创建。" });
        return;
      }

      if (!activeProvider) {
        setDetailNotice({
          tone: "danger",
          message: "当前 Profile 不存在，请重新选择后再试。",
        });
        return;
      }

      let nextProfile = activeProvider;
      const connectionEditable = activeProvider.kind === "api" && activeProvider.editable;
      if (connectionEditable) {
        nextProfile = (
          await sendJSONWithHeaders<CodexProfileResponse>(
            `/api/admin/codex/profiles/${encodeURIComponent(activeProvider.id)}`,
            "PUT",
            buildUpdatePayload(draft),
            { "If-Match": activeProvider.etag ?? "" },
          )
        ).profile;
      }
      if (activeProvider.contextEditable && draft.contextMode !== contextMode(activeProvider)) {
        nextProfile = await saveContextPreference(nextProfile, draft.contextMode);
      }
      setProviders((current) =>
        appendOrReplaceProvider(current, nextProfile, activeProvider.id),
      );
      selectPersistedItem(nextProfile);
      setDetailNotice({
        tone: "good",
        message: connectionEditable ? "Codex Profile 已保存。" : "上下文偏好已保存。",
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

    const provider = providers.find((item) => item.id === deleteTargetID) ?? null;
    if (!provider || !provider.deletable) {
      setDeleteTargetID(null);
      setDetailNotice({
        tone: "warn",
        message: "这个 Profile 不能删除。",
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
          headers: { "If-Match": provider.etag ?? "" },
        },
      );
      const nextProviders = removeProvider(providers, deleteTargetID);
      setProviders(nextProviders);
      setDeleteTargetID(null);
      setDeleteReferences([]);
      applyNextItems(nextProviders);
      setDetailNotice({ tone: "good", message: "Codex Profile 已删除。" });
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
        sectionTitle="Codex Profile"
        sectionDescription="管理 Codex 连接与上下文偏好"
        emptyLoadErrorTitle="当前还不能读取 Codex Profile"
        loadError={loadError}
        onReload={onReload}
        items={providers}
        activeItemID={activeItemID}
        newItemID={newCodexProviderID}
        onItemSelect={handleItemSelect}
        onStartCreate={startCreateBlank}
        getItemTitle={providerTitle}
        getItemSummary={providerCardSummary}
        getItemTag={providerKindLabel}
        newItemTitle="新增 Profile"
        newItemSummary="新建 API Profile"
        detailCard={renderCodexProviderDetailCard({
          actionBusy,
          activeProvider,
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
        items={providers}
        dialogTitle="确认删除 Codex Profile"
        confirmDisabled={actionBusy === "delete-codex-profile"}
        getItemTitle={providerTitle}
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
  ConfigEditorSectionState<CodexProfileSummary, CodexProviderDraft>,
  "actionBusy" | "deleteTargetID" | "detailNotice" | "draft" | "editorMode"
> & {
  activeProvider: CodexProfileSummary | null;
  onCancelCreate: () => void;
  onDeleteTargetChange: (value: string | null) => void;
  onDraftChange: Dispatch<SetStateAction<CodexProviderDraft>>;
  onSave: (event: FormEvent<HTMLFormElement>) => void;
  onStartCreate: () => void;
};

function renderCodexProviderDetailCard(props: CodexDetailCardProps) {
  const {
    actionBusy,
    activeProvider,
    deleteTargetID,
    detailNotice,
    draft,
    editorMode,
    onCancelCreate,
    onDeleteTargetChange,
    onDraftChange,
    onSave,
  } = props;

  const isConnectionEditable = editorMode === "create" || Boolean(activeProvider?.editable);
  const canSave = isConnectionEditable || Boolean(activeProvider?.contextEditable);
  const title =
    editorMode === "create"
      ? draft.name.trim()
        ? `新增 Profile：${draft.name.trim()}`
        : "新增 Codex Profile"
      : providerTitle(activeProvider);
  const description =
    editorMode === "create"
      ? "填写 API 连接信息"
      : isConnectionEditable
        ? "连接信息和上下文偏好"
        : activeProvider?.contextEditable
          ? "连接身份不可编辑，上下文偏好可单独调整。"
          : "连接身份和上下文偏好不可编辑。";

  return (
    <ConfigFormDetailCard
      title={title}
      description={description}
      notice={detailNotice}
      onSave={onSave}
      submitLabel={
        isConnectionEditable
          ? editorMode === "create"
            ? "保存 Profile"
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
        ) : activeProvider?.deletable ? (
          <button
            className="danger-button"
            disabled={Boolean(deleteTargetID) || actionBusy !== ""}
            type="button"
            onClick={() => onDeleteTargetChange(activeProvider.id)}
          >
            删除 Profile
          </button>
        ) : null
      }
    >
      {isConnectionEditable ? (
        <div className="form-grid" style={{ marginTop: "1rem" }}>
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
              placeholder="例如：gpt-5.4"
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
        </div>
      ) : (
        <div className="completed-card profile-hero-card">
          <h3>{providerKindLabel(activeProvider)}</h3>
          <p>{readOnlyProfileDescription(activeProvider)}</p>
        </div>
      )}
      <label className="field form-grid-span-2" style={{ marginTop: "1rem" }}>
        <span>上下文大小</span>
        <select
          aria-label="上下文大小"
          disabled={editorMode !== "create" && !activeProvider?.contextEditable}
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
          {contextStatusDescription(draft.contextMode, activeProvider)}
        </span>
      </label>
    </ConfigFormDetailCard>
  );
}

function createEmptyDraft(): CodexProviderDraft {
  return {
    name: "",
    baseURL: "",
    apiKey: "",
    model: "",
    reviewModel: "",
    reasoningEffort: "",
    contextMode: codexContextModeDefault,
  };
}

function createDraftFromProvider(provider: CodexProfileSummary): CodexProviderDraft {
  return {
    name: providerTitle(provider),
    baseURL: provider.baseURL?.trim() || "",
    apiKey: "",
    model: provider.model?.trim() || "",
    reviewModel: provider.reviewModel?.trim() || "",
    reasoningEffort: normalizeCodexReasoningEffort(provider.reasoningEffort),
    contextMode: contextMode(provider),
  };
}

function validateDraft(
  draft: CodexProviderDraft,
  editorMode: EditorMode,
  activeProvider: CodexProfileSummary | null,
): string {
  if (editorMode !== "create" && activeProvider && !activeProvider.editable) {
    return "";
  }
  if (!draft.name.trim()) {
    return "请填写名称。";
  }
  if (!draft.baseURL.trim()) {
    return "请填写端点地址。";
  }
  if (editorMode === "create" && !draft.apiKey.trim()) {
    return "请填写 API Key。";
  }
  if (!draft.model.trim()) {
    return "请填写主模型。";
  }
  if (!draft.reasoningEffort.trim()) {
    return "请填写推理强度。";
  }
  return "";
}

function buildCreatePayload(draft: CodexProviderDraft): CodexProfileWriteRequest {
  return {
    name: draft.name.trim(),
    baseURL: draft.baseURL.trim(),
    apiKey: draft.apiKey,
    model: draft.model.trim(),
    reviewModel: draft.reviewModel.trim(),
    reasoningEffort: normalizeCodexReasoningEffort(draft.reasoningEffort),
  };
}

function buildUpdatePayload(draft: CodexProviderDraft): CodexProfileWriteRequest {
  const payload: CodexProfileWriteRequest = {
    name: draft.name.trim(),
    baseURL: draft.baseURL.trim(),
    model: draft.model.trim(),
    reviewModel: draft.reviewModel.trim(),
    reasoningEffort: normalizeCodexReasoningEffort(draft.reasoningEffort),
  };
  if (draft.apiKey) {
    payload.apiKey = draft.apiKey;
  }
  return payload;
}

function appendOrReplaceProvider(
  providers: CodexProfileSummary[],
  provider: CodexProfileSummary,
  previousID = provider.id,
): CodexProfileSummary[] {
  const nextProviders = providers
    .filter((current) => current.id !== previousID || current.id === provider.id)
    .map((current) => (current.id === provider.id ? provider : current));
  if (nextProviders.some((current) => current.id === provider.id)) {
    return nextProviders;
  }
  return [...nextProviders, provider];
}

function removeProvider(
  providers: CodexProfileSummary[],
  targetID: string,
): CodexProfileSummary[] {
  return providers.filter((provider) => provider.id !== targetID);
}

function providerTitle(provider: CodexProfileSummary | null): string {
  if (!provider) {
    return "当前 Profile";
  }
  return provider.name?.trim() || "未命名 Profile";
}

function providerCardSummary(provider: CodexProfileSummary): string {
  const parts = [
    providerKindLabel(provider),
    provider.available ? "" : statusLabel(provider.statusCode),
    provider.baseURL?.trim() || "",
    provider.model?.trim() ? `模型 ${provider.model.trim()}` : "",
    normalizeCodexReasoningEffort(provider.reasoningEffort)
      ? `推理 ${normalizeCodexReasoningEffort(provider.reasoningEffort)}`
      : "",
    contextLabel(contextMode(provider)),
  ].filter(Boolean);
  return parts.join(" · ");
}

function providerKindLabel(provider: CodexProfileSummary | null): string {
  switch (provider?.kind) {
    case "native":
      return "本机默认";
    case "oauth":
      return "ChatGPT 登录";
    case "api":
      return "API";
    default:
      return "Profile";
  }
}

function readOnlyProfileDescription(provider: CodexProfileSummary | null): string {
  if (provider?.kind === "oauth") {
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
      return "Profile 暂不可用";
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

function contextMode(provider: CodexProfileSummary): string {
  return provider.contextPreference?.mode || codexContextModeDefault;
}

function contextStatusDescription(
  mode: string,
  provider: CodexProfileSummary | null,
): string {
  if (mode === "codex_default") {
    return "不覆盖 Codex 默认上下文。";
  }
  if (mode === "price_guard_272k") {
    return "按费用优先请求 272K；这不是单次请求费用硬上限。";
  }
  if (provider?.contextStatus === "context_preference_clamped") {
    return provider.effectiveContextWindow
      ? `目标模型限制为约 ${formatContextWindow(provider.effectiveContextWindow)}。`
      : "目标模型限制了可用上下文。";
  }
  if (provider?.effectiveContextWindow) {
    return `已观察到约 ${formatContextWindow(provider.effectiveContextWindow)} 可用上下文。`;
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
      case "codex_provider_name_required":
        return "请填写名称。";
      case "codex_profile_base_url_required":
      case "codex_provider_base_url_required":
        return "请填写端点地址。";
      case "codex_profile_api_key_required":
      case "codex_provider_api_key_required":
        return "请填写 API Key。";
      case "codex_profile_reasoning_effort_invalid":
      case "codex_provider_reasoning_effort_invalid":
        return "推理强度不可用，请重新选择。";
      case "codex_profile_reserved_name":
      case "codex_provider_reserved_name":
        return "这个名称不能使用，请换一个名字。";
      case "duplicate_codex_profile_name":
      case "duplicate_codex_provider_name":
        return "这个名称已经存在，请换一个名字。";
      case "codex_profile_read_only":
        return "这个 Profile 的连接身份不能直接修改。";
      case "codex_profile_not_found":
      case "codex_provider_not_found":
        return "当前 Profile 已经不存在，请重新读取后再试。";
      case "profile_revision_required":
      case "profile_preference_revision_required":
        return "页面状态已过期，请重新读取后再保存。";
      case "profile_revision_conflict":
      case "profile_preference_revision_conflict":
        return "这个 Profile 已被其他窗口修改，请重新读取后再保存。";
      case "codex_profile_in_use":
        return "这个 Profile 仍在使用中，请先切换或结束相关会话。";
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
