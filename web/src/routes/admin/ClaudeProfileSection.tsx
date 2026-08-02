import {
  type Dispatch,
  type FormEvent,
  type SetStateAction,
} from "react";
import { APIRequestError, formatError, requestVoid, sendJSON, sendJSONWithHeaders } from "../../lib/api";
import type {
  ClaudeProfileResponse,
  ClaudeProfileSummary,
  ClaudeProfileWriteRequest,
  CodexContextPreferenceResponse,
} from "../../lib/types";
import {
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
  contextMode: string;
};

type ClaudeProfileSectionProps = {
  profiles: ClaudeProfileSummary[];
  loadError: string;
  setProfiles: Dispatch<SetStateAction<ClaudeProfileSummary[]>>;
  onReload: () => Promise<void>;
};

const newClaudeProfileID = "new-claude-profile";
const claudeReasoningOptions = ["low", "medium", "high", "max"] as const;
const claudeContextModeDefault = "default";
const claudeContextModeExtended = "extended_1m";

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
    const validationError = validateDraft(draft, editorMode);
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

      if (!activeProfile) {
        setDetailNotice({
          tone: "danger",
          message: "当前配置不能直接编辑，请重新选择后再试。",
        });
        return;
      }

      let nextProfile = activeProfile;
      if (!activeProfile.builtIn) {
        const response = await sendJSON<ClaudeProfileResponse>(
          `/api/admin/claude/profiles/${encodeURIComponent(activeProfile.id)}`,
          "PUT",
          buildUpdatePayload(draft),
        );
        nextProfile = response.profile;
      }
      if (draft.contextMode !== contextMode(nextProfile)) {
        nextProfile = await saveContextPreference(nextProfile, draft.contextMode);
      }
      setProfiles((current) =>
        appendOrReplaceProfile(current, nextProfile, activeProfile.id),
      );
      selectPersistedItem(nextProfile);
      setDetailNotice({
        tone: "good",
        message: activeProfile.builtIn ? "上下文偏好已保存。" : "Claude 配置已保存。",
      });
    } catch (error) {
      setDetailNotice({
        tone: "danger",
        message: `保存没有完成：${describeClaudeProfileError(error)}`,
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
        sectionTitle="Claude"
        sectionDescription="管理 Claude 连接与上下文偏好"
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
        newItemTitle="新增配置"
        newItemSummary="新建 Claude 配置"
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

  async function saveContextPreference(
    profile: ClaudeProfileSummary,
    mode: string,
  ): Promise<ClaudeProfileSummary> {
    const preference = profile.contextPreference;
    if (!preference) {
      throw new APIRequestError(428, "If-Match is required", "profile_preference_revision_required");
    }
    const response = await sendJSONWithHeaders<CodexContextPreferenceResponse>(
      `/api/admin/claude/profiles/${encodeURIComponent(profile.id)}/context-preference`,
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

  const title =
    editorMode === "create"
      ? draft.name.trim()
        ? `新增配置：${draft.name.trim()}`
        : "新增 Claude 配置"
      : profileTitle(activeProfile);
  const isBuiltIn = editorMode === "built-in";

  return (
    <ConfigFormDetailCard
      title={title}
      description={
        isBuiltIn
          ? "系统默认的 Claude 连接"
          : editorMode === "create"
            ? "填写连接信息"
            : ""
      }
      notice={detailNotice}
      onSave={onSave}
      submitLabel={isBuiltIn ? "保存上下文偏好" : editorMode === "create" ? "保存配置" : "保存修改"}
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
        ) : !isBuiltIn ? (
          <button
            className="danger-button"
            disabled={Boolean(deleteTargetID) || actionBusy === "delete-claude-profile"}
            type="button"
            onClick={() => onDeleteTargetChange(activeProfile?.id ?? null)}
          >
            删除配置
          </button>
        ) : null
      }
    >
      {isBuiltIn ? (
        <div className="completed-card profile-hero-card">
          <h3>系统默认配置</h3>
          <p>内建默认开启后使用 Sonnet 1M。</p>
        </div>
      ) : null}
      {!isBuiltIn ? <div className="form-grid" style={{ marginTop: "1rem" }}>
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
      </div> : null}
      <label className="field form-grid-span-2" style={{ marginTop: "1rem" }}>
        <span>上下文大小</span>
        <span className="checkbox-line">
          <input
            aria-label="使用 1M 上下文"
            checked={draft.contextMode === claudeContextModeExtended}
            type="checkbox"
            onChange={(event) =>
              onDraftChange((current) => ({
                ...current,
                contextMode: event.target.checked
                  ? claudeContextModeExtended
                  : claudeContextModeDefault,
              }))
            }
          />
          <span>使用 1M 上下文</span>
        </span>
      </label>
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
    contextMode: claudeContextModeDefault,
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
    contextMode: contextMode(profile),
  };
}

function validateDraft(draft: ClaudeProfileDraft, editorMode: string): string {
  if (editorMode === "built-in") {
    return "";
  }
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

function contextMode(profile: ClaudeProfileSummary): string {
  return profile.contextPreference?.mode || claudeContextModeDefault;
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
    return contextMode(profile) === claudeContextModeExtended
      ? "本机默认配置 · 1M"
      : "本机默认配置";
  }
  return [
    profile.baseURL?.trim() || "自定义连接配置",
    contextMode(profile) === claudeContextModeExtended ? "1M" : "",
  ].filter(Boolean).join(" · ");
}

function describeClaudeProfileError(error: unknown): string {
  if (error instanceof APIRequestError) {
    switch (error.code) {
      case "profile_preference_revision_required":
        return "页面状态已过期，请重新读取后再保存。";
      case "profile_preference_revision_conflict":
        return "上下文偏好已被其他窗口修改，请重新读取后再保存。";
      default:
        break;
    }
  }
  return formatError(error);
}
