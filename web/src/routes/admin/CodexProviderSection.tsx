import {
  useEffect,
  useState,
  type Dispatch,
  type FormEvent,
  type SetStateAction,
} from "react";
import {
  APIRequestError,
  formatError,
  requestVoid,
  sendJSON,
} from "../../lib/api";
import type {
  CodexProviderResponse,
  CodexProviderSummary,
  CodexProviderWriteRequest,
} from "../../lib/types";

type NoticeTone = "good" | "warn" | "danger";

type DetailNotice = {
  tone: NoticeTone;
  message: string;
};

type EditorMode = "built-in" | "edit" | "create";

type CodexProviderDraft = {
  name: string;
  baseURL: string;
  apiKey: string;
  model: string;
  reasoningEffort: string;
};

type CodexProviderSectionProps = {
  providers: CodexProviderSummary[];
  loadError: string;
  setProviders: Dispatch<SetStateAction<CodexProviderSummary[]>>;
  onReload: () => Promise<void>;
};

const newCodexProviderID = "new-codex-provider";
const codexReasoningOptions = ["low", "medium", "high", "xhigh"] as const;

export function CodexProviderSection(props: CodexProviderSectionProps) {
  const { providers, loadError, setProviders, onReload } = props;
  const [activeProviderID, setActiveProviderID] = useState("");
  const [editorMode, setEditorMode] = useState<EditorMode>("create");
  const [draft, setDraft] = useState<CodexProviderDraft>(createEmptyDraft());
  const [detailNotice, setDetailNotice] = useState<DetailNotice | null>(null);
  const [actionBusy, setActionBusy] = useState("");
  const [deleteTargetID, setDeleteTargetID] = useState<string | null>(null);

  useEffect(() => {
    if (providers.length === 0) {
      if (editorMode !== "create") {
        setActiveProviderID(newCodexProviderID);
        setEditorMode("create");
        setDraft(createEmptyDraft());
      }
      return;
    }
    if (activeProviderID === newCodexProviderID && editorMode === "create") {
      return;
    }
    const fallbackProvider = providers[0];
    const activeProvider =
      providers.find((provider) => provider.id === activeProviderID) ?? fallbackProvider;
    if (!activeProvider) {
      return;
    }
    if (activeProvider.id !== activeProviderID) {
      setActiveProviderID(activeProvider.id);
    }
    if (activeProvider.builtIn) {
      setEditorMode("built-in");
      setDraft(createEmptyDraft());
      return;
    }
    setEditorMode("edit");
    setDraft(createDraftFromProvider(activeProvider));
  }, [activeProviderID, editorMode, providers]);

  const activeProvider =
    providers.find((provider) => provider.id === activeProviderID) ?? null;

  async function handleSave(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    const validationError = validateDraft(draft);
    if (validationError) {
      setDetailNotice({ tone: "warn", message: validationError });
      return;
    }

    setActionBusy("save-codex-provider");
    setDetailNotice(null);
    try {
      if (editorMode === "create") {
        const response = await sendJSON<CodexProviderResponse>(
          "/api/admin/codex/providers",
          "POST",
          buildCreatePayload(draft),
        );
        setProviders((current) => appendOrReplaceProvider(current, response.provider));
        selectPersistedProvider(response.provider);
        setDetailNotice({ tone: "good", message: "Codex Provider 已创建。" });
        return;
      }

      if (!activeProvider || activeProvider.builtIn) {
        setDetailNotice({
          tone: "danger",
          message: "当前配置不能直接编辑，请重新选择后再试。",
        });
        return;
      }

      const response = await sendJSON<CodexProviderResponse>(
        `/api/admin/codex/providers/${encodeURIComponent(activeProvider.id)}`,
        "PUT",
        buildUpdatePayload(draft),
      );
      setProviders((current) =>
        appendOrReplaceProvider(current, response.provider, activeProvider.id),
      );
      selectPersistedProvider(response.provider);
      setDetailNotice({ tone: "good", message: "Codex Provider 已保存。" });
    } catch (error) {
      setDetailNotice({
        tone: "danger",
        message: `保存没有完成：${describeCodexProviderError(error)}`,
      });
    } finally {
      setActionBusy("");
    }
  }

  async function handleDelete() {
    if (!deleteTargetID) {
      return;
    }

    const provider = providers.find((item) => item.id === deleteTargetID) ?? null;
    if (!provider || provider.builtIn) {
      setDeleteTargetID(null);
      setDetailNotice({
        tone: "warn",
        message: "系统默认配置不能删除。",
      });
      return;
    }

    setActionBusy("delete-codex-provider");
    setDetailNotice(null);
    try {
      await requestVoid(
        `/api/admin/codex/providers/${encodeURIComponent(deleteTargetID)}`,
        {
          method: "DELETE",
        },
      );
      const nextProviders = removeProvider(providers, deleteTargetID);
      setProviders(nextProviders);
      setDeleteTargetID(null);
      const nextSelected = nextProviders[0] ?? null;
      if (nextSelected) {
        if (nextSelected.builtIn) {
          setActiveProviderID(nextSelected.id);
          setEditorMode("built-in");
          setDraft(createEmptyDraft());
        } else {
          selectPersistedProvider(nextSelected);
        }
      } else {
        startCreateBlank();
      }
      setDetailNotice({ tone: "good", message: "Codex Provider 已删除。" });
    } catch (error) {
      setDetailNotice({
        tone: "danger",
        message: `删除没有完成：${describeCodexProviderError(error)}`,
      });
    } finally {
      setActionBusy("");
    }
  }

  function selectPersistedProvider(provider: CodexProviderSummary) {
    setActiveProviderID(provider.id);
    setEditorMode(provider.builtIn ? "built-in" : "edit");
    setDraft(provider.builtIn ? createEmptyDraft() : createDraftFromProvider(provider));
  }

  function startCreateBlank() {
    setActiveProviderID(newCodexProviderID);
    setEditorMode("create");
    setDraft(createEmptyDraft());
    setDetailNotice(null);
  }

  function cancelCreate() {
    const fallbackProvider = providers[0] ?? null;
    if (!fallbackProvider) {
      startCreateBlank();
      return;
    }
    if (fallbackProvider.builtIn) {
      setActiveProviderID(fallbackProvider.id);
      setEditorMode("built-in");
      setDraft(createEmptyDraft());
      setDetailNotice(null);
      return;
    }
    selectPersistedProvider(fallbackProvider);
    setDetailNotice(null);
  }

  function handleProviderSelect(provider: CodexProviderSummary) {
    setDetailNotice(null);
    if (provider.builtIn) {
      setActiveProviderID(provider.id);
      setEditorMode("built-in");
      setDraft(createEmptyDraft());
      return;
    }
    selectPersistedProvider(provider);
  }

  if (loadError && providers.length === 0) {
    return (
      <section className="panel">
        <div className="step-stage-head">
          <h2>Codex Provider</h2>
          <p>Codex 连接配置</p>
        </div>
        <div className="empty-state error">
          <strong>当前还不能读取 Codex Provider</strong>
          <p>{loadError}</p>
          <div className="button-row">
            <button
              className="secondary-button"
              type="button"
              onClick={() => void onReload()}
            >
              重新读取
            </button>
          </div>
        </div>
      </section>
    );
  }

  return (
    <>
      <section className="panel">
        <div className="step-stage-head">
          <h2>Codex Provider</h2>
          <p>Codex 连接配置</p>
        </div>
        {loadError ? (
          <div className="notice-banner warn">
            <div className="inline-status-row">
              <span>{loadError}</span>
              <button
                className="ghost-button"
                type="button"
                onClick={() => void onReload()}
              >
                重新读取
              </button>
            </div>
          </div>
        ) : null}
        <div className="profile-layout" style={{ marginTop: "1rem" }}>
          <div className="profile-list">
            {providers.map((provider) => (
              <button
                key={provider.id}
                className={`profile-list-button${activeProviderID === provider.id ? " active" : ""}`}
                type="button"
                onClick={() => handleProviderSelect(provider)}
              >
                <div className="profile-list-head">
                  <strong>{providerTitle(provider)}</strong>
                  <span className="robot-tag">
                    {provider.builtIn ? "默认" : "自定义"}
                  </span>
                </div>
                <p>{providerCardSummary(provider)}</p>
              </button>
            ))}
            <button
              className={`profile-list-button${activeProviderID === newCodexProviderID ? " active" : ""}`}
              type="button"
              onClick={() => startCreateBlank()}
            >
              <div className="profile-list-head">
                <strong>新增配置</strong>
                <span className="robot-tag">新增</span>
              </div>
              <p>新建配置</p>
            </button>
          </div>
          {renderDetailCard({
            actionBusy,
            activeProvider,
            deleteTargetID,
            detailNotice,
            draft,
            editorMode,
            onCancelCreate: cancelCreate,
            onDeleteTargetChange: setDeleteTargetID,
            onDraftChange: setDraft,
            onSave: (event) => void handleSave(event),
            onStartCreate: startCreateBlank,
          })}
        </div>
      </section>

      {deleteTargetID ? (
        <div className="modal-backdrop" role="presentation">
          <div
            className="modal-card"
            role="dialog"
            aria-modal="true"
            aria-labelledby="delete-codex-provider-title"
          >
            <h3 id="delete-codex-provider-title">确认删除 Codex Provider</h3>
            <p className="modal-copy">
              删除后将移除“
              {providerTitle(
                providers.find((provider) => provider.id === deleteTargetID) ?? null,
              )}
              ”，此操作不可恢复。
            </p>
            <div className="modal-actions">
              <button
                className="ghost-button"
                type="button"
                onClick={() => setDeleteTargetID(null)}
              >
                取消
              </button>
              <button
                className="danger-button"
                type="button"
                disabled={actionBusy === "delete-codex-provider"}
                onClick={() => void handleDelete()}
              >
                确认删除
              </button>
            </div>
          </div>
        </div>
      ) : null}
    </>
  );
}

type DetailCardProps = {
  actionBusy: string;
  activeProvider: CodexProviderSummary | null;
  deleteTargetID: string | null;
  detailNotice: DetailNotice | null;
  draft: CodexProviderDraft;
  editorMode: EditorMode;
  onCancelCreate: () => void;
  onDeleteTargetChange: (value: string | null) => void;
  onDraftChange: Dispatch<SetStateAction<CodexProviderDraft>>;
  onSave: (event: FormEvent<HTMLFormElement>) => void;
  onStartCreate: () => void;
};

function renderDetailCard(props: DetailCardProps) {
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
    onStartCreate,
  } = props;

  if (editorMode === "built-in") {
    return (
      <section className="panel">
        <div className="step-stage-head">
          <h2>{providerTitle(activeProvider)}</h2>
          <p>系统默认的 Codex 连接</p>
        </div>
        {detailNotice ? (
          <div className={`notice-banner ${detailNotice.tone}`}>
            {detailNotice.message}
          </div>
        ) : null}
        <div className="completed-card profile-hero-card">
          <h3>系统默认配置</h3>
          <p>如需使用其他端点，请新增配置。</p>
        </div>
        <div className="button-row">
          <button
            className="primary-button"
            type="button"
            onClick={() => onStartCreate()}
          >
            新增自定义配置
          </button>
        </div>
      </section>
    );
  }

  const title =
    editorMode === "create"
      ? draft.name.trim()
        ? `新增配置：${draft.name.trim()}`
        : "新增 Codex Provider"
      : providerTitle(activeProvider);
  const description =
    editorMode === "create"
      ? "填写连接信息"
      : "";

  return (
    <section className="panel">
      <div className="step-stage-head">
        <h2>{title}</h2>
        <p>{description}</p>
      </div>
      {detailNotice ? (
        <div className={`notice-banner ${detailNotice.tone}`}>
          {detailNotice.message}
        </div>
      ) : null}
      <form noValidate onSubmit={onSave}>
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
              placeholder="例如：https://proxy.internal/v1"
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
              API Key <em className="field-required">*</em>
            </span>
            <input
              autoComplete="new-password"
              placeholder="输入 API Key"
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
            <span>默认模型</span>
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
            <span>默认推理强度</span>
            <select
              value={draft.reasoningEffort}
              onChange={(event) =>
                onDraftChange((current) => ({
                  ...current,
                  reasoningEffort: event.target.value,
                }))
              }
            >
              <option value="">不设置</option>
              {codexReasoningOptions.map((value) => (
                <option key={value} value={value}>
                  {value}
                </option>
              ))}
            </select>
          </label>
        </div>

        <div className="button-row">
          <button
            className="primary-button"
            disabled={actionBusy === "save-codex-provider"}
            type="submit"
          >
            {editorMode === "create" ? "保存配置" : "保存修改"}
          </button>
          {editorMode === "create" ? (
            <button
              className="ghost-button"
              disabled={actionBusy === "save-codex-provider"}
              type="button"
              onClick={() => onCancelCreate()}
            >
              取消
            </button>
          ) : (
            <button
              className="danger-button"
              disabled={Boolean(deleteTargetID) || actionBusy === "delete-codex-provider"}
              type="button"
              onClick={() => onDeleteTargetChange(activeProvider?.id ?? null)}
            >
              删除配置
            </button>
          )}
        </div>
      </form>
    </section>
  );
}

function createEmptyDraft(): CodexProviderDraft {
  return {
    name: "",
    baseURL: "",
    apiKey: "",
    model: "",
    reasoningEffort: "",
  };
}

function createDraftFromProvider(provider: CodexProviderSummary): CodexProviderDraft {
  return {
    name: providerTitle(provider),
    baseURL: provider.baseURL?.trim() || "",
    apiKey: "",
    model: provider.model?.trim() || "",
    reasoningEffort: normalizeCodexReasoningEffort(provider.reasoningEffort),
  };
}

function validateDraft(draft: CodexProviderDraft): string {
  if (!draft.name.trim()) {
    return "请填写名称。";
  }
  if (!draft.baseURL.trim()) {
    return "请填写端点地址。";
  }
  if (!draft.apiKey.trim()) {
    return "请填写 API Key。";
  }
  return "";
}

function buildCreatePayload(draft: CodexProviderDraft): CodexProviderWriteRequest {
  return {
    name: draft.name.trim(),
    baseURL: draft.baseURL.trim(),
    apiKey: draft.apiKey.trim(),
    model: draft.model.trim(),
    reasoningEffort: normalizeCodexReasoningEffort(draft.reasoningEffort),
  };
}

function buildUpdatePayload(draft: CodexProviderDraft): CodexProviderWriteRequest {
  const payload: CodexProviderWriteRequest = {
    name: draft.name.trim(),
    baseURL: draft.baseURL.trim(),
    model: draft.model.trim(),
    reasoningEffort: normalizeCodexReasoningEffort(draft.reasoningEffort),
  };
  const apiKey = optionalString(draft.apiKey);
  if (apiKey) {
    payload.apiKey = apiKey;
  }
  return payload;
}

function appendOrReplaceProvider(
  providers: CodexProviderSummary[],
  provider: CodexProviderSummary,
  previousID = provider.id,
): CodexProviderSummary[] {
  const nextProviders = providers
    .filter((current) => current.id !== previousID || current.id === provider.id)
    .map((current) => (current.id === provider.id ? provider : current));
  if (nextProviders.some((current) => current.id === provider.id)) {
    return nextProviders;
  }
  return [...providers, provider];
}

function removeProvider(
  providers: CodexProviderSummary[],
  targetID: string,
): CodexProviderSummary[] {
  return providers.filter((provider) => provider.id !== targetID);
}

function optionalString(value: string): string | undefined {
  const trimmed = value.trim();
  return trimmed ? trimmed : undefined;
}

function providerTitle(provider: CodexProviderSummary | null): string {
  if (!provider) {
    return "当前配置";
  }
  const name = provider.name?.trim();
  if (name) {
    return name;
  }
  if (provider.builtIn || provider.id === "default") {
    return "系统默认";
  }
  return "未命名配置";
}

function providerCardSummary(provider: CodexProviderSummary): string {
  if (provider.builtIn) {
    return "本机默认配置";
  }
  const parts = [
    provider.baseURL?.trim() || "",
    provider.model?.trim() ? `模型 ${provider.model.trim()}` : "",
    normalizeCodexReasoningEffort(provider.reasoningEffort)
      ? `推理 ${normalizeCodexReasoningEffort(provider.reasoningEffort)}`
      : "",
  ].filter(Boolean);
  if (parts.length === 0) {
    return "自定义连接配置";
  }
  return parts.join(" · ");
}

function describeCodexProviderError(error: unknown): string {
  if (error instanceof APIRequestError) {
    switch (error.code) {
      case "codex_provider_name_required":
        return "请填写名称。";
      case "codex_provider_base_url_required":
        return "请填写端点地址。";
      case "codex_provider_api_key_required":
        return "请填写 API Key。";
      case "codex_provider_reasoning_effort_invalid":
        return "默认推理强度不可用，请重新选择。";
      case "codex_provider_reserved_name":
        return "这个名称不能使用，请换一个名字。";
      case "duplicate_codex_provider_name":
        return "这个名称已经存在，请换一个名字。";
      case "codex_provider_read_only":
        return "系统默认配置不能直接修改。";
      case "codex_provider_not_found":
        return "当前配置已经不存在，请重新选择后再试。";
      default:
        break;
    }
  }
  return formatError(error);
}

function normalizeCodexReasoningEffort(value: string | undefined): string {
  const trimmed = value?.trim().toLowerCase() ?? "";
  return codexReasoningOptions.includes(trimmed as (typeof codexReasoningOptions)[number])
    ? trimmed
    : "";
}
