import {
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
import {
  ConfigBuiltInDetailCard,
  ConfigDeleteConfirmModal,
  ConfigFormDetailCard,
  ConfigSectionShell,
  type ConfigEditorSectionState,
  useConfigEditorSection,
} from "./ConfigEditorShared";

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
  const editor = useConfigEditorSection<CodexProviderSummary, CodexProviderDraft>({
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
        selectPersistedItem(response.provider);
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
      selectPersistedItem(response.provider);
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
      applyNextItems(nextProviders);
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

  return (
    <>
      <ConfigSectionShell
        sectionTitle="Codex Provider"
        sectionDescription="Codex 连接配置"
        emptyLoadErrorTitle="当前还不能读取 Codex Provider"
        loadError={loadError}
        onReload={onReload}
        items={providers}
        activeItemID={activeItemID}
        newItemID={newCodexProviderID}
        onItemSelect={handleItemSelect}
        onStartCreate={startCreateBlank}
        getItemTitle={providerTitle}
        getItemSummary={providerCardSummary}
        detailCard={renderCodexProviderDetailCard({
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
      />

      <ConfigDeleteConfirmModal
        targetID={deleteTargetID}
        items={providers}
        dialogTitle="确认删除 Codex Provider"
        confirmDisabled={actionBusy === "delete-codex-provider"}
        getItemTitle={providerTitle}
        onCancel={() => setDeleteTargetID(null)}
        onConfirm={() => void handleDelete()}
      />
    </>
  );
}

type CodexDetailCardProps = Pick<
  ConfigEditorSectionState<CodexProviderSummary, CodexProviderDraft>,
  "actionBusy" | "deleteTargetID" | "detailNotice" | "draft" | "editorMode"
> & {
  activeProvider: CodexProviderSummary | null;
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
    onStartCreate,
  } = props;

  if (editorMode === "built-in") {
    return (
      <ConfigBuiltInDetailCard
        title={providerTitle(activeProvider)}
        description="系统默认的 Codex 连接"
        notice={detailNotice}
        heroTitle="系统默认配置"
        heroDescription="如需使用其他端点，请新增配置。"
        startCreateLabel="新增自定义配置"
        onStartCreate={onStartCreate}
      />
    );
  }

  const title =
    editorMode === "create"
      ? draft.name.trim()
        ? `新增配置：${draft.name.trim()}`
        : "新增 Codex Provider"
      : providerTitle(activeProvider);

  return (
    <ConfigFormDetailCard
      title={title}
      description={editorMode === "create" ? "填写连接信息" : ""}
      notice={detailNotice}
      onSave={onSave}
      submitLabel={editorMode === "create" ? "保存配置" : "保存修改"}
      submitDisabled={actionBusy === "save-codex-provider"}
      secondaryAction={
        editorMode === "create" ? (
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
        )
      }
    >
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
    </ConfigFormDetailCard>
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
