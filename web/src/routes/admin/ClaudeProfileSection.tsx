import {
  type Dispatch,
  type FormEvent,
  type SetStateAction,
} from "react";
import { formatError, requestVoid, sendJSON } from "../../lib/api";
import type {
  ClaudeProfileResponse,
  ClaudeProfileSummary,
  ClaudeProfileWriteRequest,
} from "../../lib/types";
import {
  ConfigBuiltInDetailCard,
  ConfigDeleteConfirmModal,
  ConfigFormDetailCard,
  ConfigSectionShell,
  type ConfigEditorSectionState,
  useConfigEditorSection,
} from "./ConfigEditorShared";

type ClaudeProfileDraft = {
  name: string;
  baseURL: string;
  authToken: string;
  model: string;
  smallModel: string;
  reasoningEffort: string;
};

type ClaudeProfileSectionProps = {
  profiles: ClaudeProfileSummary[];
  loadError: string;
  setProfiles: Dispatch<SetStateAction<ClaudeProfileSummary[]>>;
  onReload: () => Promise<void>;
};

const newClaudeProfileID = "new-claude-profile";
const claudeReasoningOptions = ["low", "medium", "high", "max"] as const;

export function ClaudeProfileSection(props: ClaudeProfileSectionProps) {
  const { profiles, loadError, setProfiles, onReload } = props;
  const editor = useConfigEditorSection<ClaudeProfileSummary, ClaudeProfileDraft>({
    items: profiles,
    newItemID: newClaudeProfileID,
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
    const validationError = validateDraft(draft);
    if (validationError) {
      setDetailNotice({ tone: "warn", message: validationError });
      return;
    }

    setActionBusy("save-claude-profile");
    setDetailNotice(null);
    try {
      if (editorMode === "create") {
        const response = await sendJSON<ClaudeProfileResponse>(
          "/api/admin/claude/profiles",
          "POST",
          buildCreatePayload(draft),
        );
        setProfiles((current) => appendOrReplaceProfile(current, response.profile));
        selectPersistedItem(response.profile);
        setDetailNotice({ tone: "good", message: "Claude 配置已创建。" });
        return;
      }

      if (!activeProfile || activeProfile.builtIn) {
        setDetailNotice({
          tone: "danger",
          message: "当前配置不能直接编辑，请重新选择后再试。",
        });
        return;
      }

      const response = await sendJSON<ClaudeProfileResponse>(
        `/api/admin/claude/profiles/${encodeURIComponent(activeProfile.id)}`,
        "PUT",
        buildUpdatePayload(draft),
      );
      setProfiles((current) =>
        appendOrReplaceProfile(current, response.profile, activeProfile.id),
      );
      selectPersistedItem(response.profile);
      setDetailNotice({ tone: "good", message: "Claude 配置已保存。" });
    } catch (error) {
      setDetailNotice({
        tone: "danger",
        message: `保存没有完成：${formatError(error)}`,
      });
    } finally {
      setActionBusy("");
    }
  }

  async function handleDelete() {
    if (!deleteTargetID) {
      return;
    }

    const profile = profiles.find((item) => item.id === deleteTargetID) ?? null;
    if (!profile || profile.builtIn) {
      setDeleteTargetID(null);
      setDetailNotice({
        tone: "warn",
        message: "系统默认配置不能删除。",
      });
      return;
    }

    setActionBusy("delete-claude-profile");
    setDetailNotice(null);
    try {
      await requestVoid(
        `/api/admin/claude/profiles/${encodeURIComponent(deleteTargetID)}`,
        {
          method: "DELETE",
        },
      );
      const nextProfiles = removeProfile(profiles, deleteTargetID);
      setProfiles(nextProfiles);
      setDeleteTargetID(null);
      applyNextItems(nextProfiles);
      setDetailNotice({ tone: "good", message: "Claude 配置已删除。" });
    } catch (error) {
      setDetailNotice({
        tone: "danger",
        message: `删除没有完成：${formatError(error)}`,
      });
    } finally {
      setActionBusy("");
    }
  }

  return (
    <>
      <ConfigSectionShell
        sectionTitle="Claude 配置"
        sectionDescription="Claude 连接配置"
        emptyLoadErrorTitle="当前还不能读取 Claude 配置"
        loadError={loadError}
        onReload={onReload}
        items={profiles}
        activeItemID={activeItemID}
        newItemID={newClaudeProfileID}
        onItemSelect={handleItemSelect}
        onStartCreate={startCreateBlank}
        getItemTitle={profileTitle}
        getItemSummary={profileCardSummary}
        detailCard={renderClaudeProfileDetailCard({
          actionBusy,
          activeProfile,
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
        items={profiles}
        dialogTitle="确认删除 Claude 配置"
        confirmDisabled={actionBusy === "delete-claude-profile"}
        getItemTitle={profileTitle}
        onCancel={() => setDeleteTargetID(null)}
        onConfirm={() => void handleDelete()}
      />
    </>
  );
}

type ClaudeDetailCardProps = Pick<
  ConfigEditorSectionState<ClaudeProfileSummary, ClaudeProfileDraft>,
  "actionBusy" | "deleteTargetID" | "detailNotice" | "draft" | "editorMode"
> & {
  activeProfile: ClaudeProfileSummary | null;
  onCancelCreate: () => void;
  onDeleteTargetChange: (value: string | null) => void;
  onDraftChange: Dispatch<SetStateAction<ClaudeProfileDraft>>;
  onSave: (event: FormEvent<HTMLFormElement>) => void;
  onStartCreate: () => void;
};

function renderClaudeProfileDetailCard(props: ClaudeDetailCardProps) {
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

  if (editorMode === "built-in") {
    return (
      <ConfigBuiltInDetailCard
        title={profileTitle(activeProfile)}
        description="系统默认的 Claude 连接"
        notice={detailNotice}
        heroTitle="系统默认配置"
        heroDescription="如需使用其他端点或模型，请新增配置。"
        startCreateLabel="新增自定义配置"
        onStartCreate={onStartCreate}
      />
    );
  }

  const title =
    editorMode === "create"
      ? draft.name.trim()
        ? `新增配置：${draft.name.trim()}`
        : "新增 Claude 配置"
      : profileTitle(activeProfile);

  return (
    <ConfigFormDetailCard
      title={title}
      description={editorMode === "create" ? "填写连接信息" : ""}
      notice={detailNotice}
      onSave={onSave}
      submitLabel={editorMode === "create" ? "保存配置" : "保存修改"}
      submitDisabled={actionBusy === "save-claude-profile"}
      secondaryAction={
        editorMode === "create" ? (
          <button
            className="ghost-button"
            disabled={actionBusy === "save-claude-profile"}
            type="button"
            onClick={() => onCancelCreate()}
          >
            取消
          </button>
        ) : (
          <button
            className="danger-button"
            disabled={Boolean(deleteTargetID) || actionBusy === "delete-claude-profile"}
            type="button"
            onClick={() => onDeleteTargetChange(activeProfile?.id ?? null)}
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
          <span>端点地址</span>
          <input
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
          <span>认证 Token</span>
          <input
            autoComplete="new-password"
            placeholder="输入认证 Token"
            type="password"
            value={draft.authToken}
            onChange={(event) =>
              onDraftChange((current) => ({
                ...current,
                authToken: event.target.value,
              }))
            }
          />
        </label>

        <label className="field">
          <span>主模型</span>
          <input
            value={draft.model}
            placeholder="留空时跟随 Claude 默认"
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
            placeholder="留空时跟随 Claude 默认"
            onChange={(event) =>
              onDraftChange((current) => ({
                ...current,
                smallModel: event.target.value,
              }))
            }
          />
        </label>
        <label className="field">
          <span>推理强度</span>
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
            {claudeReasoningOptions.map((value) => (
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

function createEmptyDraft(): ClaudeProfileDraft {
  return {
    name: "",
    baseURL: "",
    authToken: "",
    model: "",
    smallModel: "",
    reasoningEffort: "",
  };
}

function createDraftFromProfile(profile: ClaudeProfileSummary): ClaudeProfileDraft {
  return {
    name: profileTitle(profile),
    baseURL: profile.baseURL?.trim() || "",
    authToken: "",
    model: profile.model?.trim() || "",
    smallModel: profile.smallModel?.trim() || "",
    reasoningEffort: normalizeClaudeReasoningEffort(profile.reasoningEffort),
  };
}

function validateDraft(draft: ClaudeProfileDraft): string {
  if (!draft.name.trim()) {
    return "请填写名称。";
  }
  return "";
}

function buildCreatePayload(draft: ClaudeProfileDraft): ClaudeProfileWriteRequest {
  return {
    name: draft.name.trim(),
    baseURL: draft.baseURL.trim(),
    authToken: optionalString(draft.authToken),
    model: draft.model.trim(),
    smallModel: draft.smallModel.trim(),
    reasoningEffort: normalizeClaudeReasoningEffort(draft.reasoningEffort),
  };
}

function buildUpdatePayload(draft: ClaudeProfileDraft): ClaudeProfileWriteRequest {
  const payload: ClaudeProfileWriteRequest = {
    name: draft.name.trim(),
    baseURL: draft.baseURL.trim(),
    model: draft.model.trim(),
    smallModel: draft.smallModel.trim(),
    reasoningEffort: normalizeClaudeReasoningEffort(draft.reasoningEffort),
  };
  const authToken = optionalString(draft.authToken);
  if (authToken) {
    payload.authToken = authToken;
  }
  return payload;
}

function appendOrReplaceProfile(
  profiles: ClaudeProfileSummary[],
  profile: ClaudeProfileSummary,
  previousID = profile.id,
): ClaudeProfileSummary[] {
  const nextProfiles = profiles
    .filter((current) => current.id !== previousID || current.id === profile.id)
    .map((current) => (current.id === profile.id ? profile : current));
  if (nextProfiles.some((current) => current.id === profile.id)) {
    return nextProfiles;
  }
  return [...profiles, profile];
}

function removeProfile(
  profiles: ClaudeProfileSummary[],
  targetID: string,
): ClaudeProfileSummary[] {
  return profiles.filter((profile) => profile.id !== targetID);
}

function optionalString(value: string): string | undefined {
  const trimmed = value.trim();
  return trimmed ? trimmed : undefined;
}

function normalizeClaudeReasoningEffort(value: string | undefined): string {
  const trimmed = value?.trim().toLowerCase() ?? "";
  return claudeReasoningOptions.includes(trimmed as (typeof claudeReasoningOptions)[number])
    ? trimmed
    : "";
}

function profileTitle(profile: ClaudeProfileSummary | null): string {
  if (!profile) {
    return "当前配置";
  }
  const name = profile.name?.trim();
  if (name) {
    return name;
  }
  if (profile.builtIn || profile.id === "default") {
    return "默认";
  }
  return "未命名配置";
}

function profileCardSummary(profile: ClaudeProfileSummary): string {
  if (profile.builtIn) {
    return "本机默认配置";
  }
  return profile.baseURL?.trim() || "自定义连接配置";
}
