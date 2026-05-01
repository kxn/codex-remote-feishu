import {
  useEffect,
  useState,
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

type NoticeTone = "good" | "warn" | "danger";

type DetailNotice = {
  tone: NoticeTone;
  message: string;
};

type EditorMode = "built-in" | "edit" | "create";

type ClaudeProfileDraft = {
  name: string;
  baseURL: string;
  authToken: string;
  model: string;
  smallModel: string;
};

type ClaudeProfileSectionProps = {
  profiles: ClaudeProfileSummary[];
  loadError: string;
  setProfiles: Dispatch<SetStateAction<ClaudeProfileSummary[]>>;
  onReload: () => Promise<void>;
};

const newClaudeProfileID = "new-claude-profile";

export function ClaudeProfileSection(props: ClaudeProfileSectionProps) {
  const { profiles, loadError, setProfiles, onReload } = props;
  const [activeProfileID, setActiveProfileID] = useState("");
  const [editorMode, setEditorMode] = useState<EditorMode>("create");
  const [draft, setDraft] = useState<ClaudeProfileDraft>(createEmptyDraft());
  const [detailNotice, setDetailNotice] = useState<DetailNotice | null>(null);
  const [actionBusy, setActionBusy] = useState("");
  const [deleteTargetID, setDeleteTargetID] = useState<string | null>(null);

  useEffect(() => {
    if (profiles.length === 0) {
      if (editorMode !== "create") {
        setActiveProfileID(newClaudeProfileID);
        setEditorMode("create");
        setDraft(createEmptyDraft());
      }
      return;
    }
    if (activeProfileID === newClaudeProfileID && editorMode === "create") {
      return;
    }
    const fallbackProfile = profiles[0];
    const activeProfile =
      profiles.find((profile) => profile.id === activeProfileID) ?? fallbackProfile;
    if (!activeProfile) {
      return;
    }
    if (activeProfile.id !== activeProfileID) {
      setActiveProfileID(activeProfile.id);
    }
    if (activeProfile.builtIn) {
      setEditorMode("built-in");
      setDraft(createEmptyDraft());
      return;
    }
    setEditorMode("edit");
    setDraft(createDraftFromProfile(activeProfile));
  }, [activeProfileID, editorMode, profiles]);

  const activeProfile =
    profiles.find((profile) => profile.id === activeProfileID) ?? null;

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
        selectPersistedProfile(response.profile);
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
        buildUpdatePayload(draft, activeProfile),
      );
      setProfiles((current) =>
        appendOrReplaceProfile(current, response.profile, activeProfile.id),
      );
      selectPersistedProfile(response.profile);
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
      const nextSelected = nextProfiles[0] ?? null;
      if (nextSelected) {
        if (nextSelected.builtIn) {
          setActiveProfileID(nextSelected.id);
          setEditorMode("built-in");
          setDraft(createEmptyDraft());
        } else {
          selectPersistedProfile(nextSelected);
        }
      } else {
        startCreateBlank();
      }
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

  function selectPersistedProfile(profile: ClaudeProfileSummary) {
    setActiveProfileID(profile.id);
    setEditorMode(profile.builtIn ? "built-in" : "edit");
    setDraft(profile.builtIn ? createEmptyDraft() : createDraftFromProfile(profile));
  }

  function startCreateBlank() {
    setActiveProfileID(newClaudeProfileID);
    setEditorMode("create");
    setDraft(createEmptyDraft());
    setDetailNotice(null);
  }

  function cancelCreate() {
    const fallbackProfile = profiles[0] ?? null;
    if (!fallbackProfile) {
      startCreateBlank();
      return;
    }
    if (fallbackProfile.builtIn) {
      setActiveProfileID(fallbackProfile.id);
      setEditorMode("built-in");
      setDraft(createEmptyDraft());
      setDetailNotice(null);
      return;
    }
    selectPersistedProfile(fallbackProfile);
    setDetailNotice(null);
  }

  function handleProfileSelect(profile: ClaudeProfileSummary) {
    setDetailNotice(null);
    if (profile.builtIn) {
      setActiveProfileID(profile.id);
      setEditorMode("built-in");
      setDraft(createEmptyDraft());
      return;
    }
    selectPersistedProfile(profile);
  }

  if (loadError && profiles.length === 0) {
    return (
      <section className="panel">
        <div className="step-stage-head">
          <h2>Claude 配置</h2>
          <p>管理不同的 Claude 连接配置。</p>
        </div>
        <div className="empty-state error">
          <strong>当前还不能读取 Claude 配置</strong>
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
          <h2>Claude 配置</h2>
          <p>管理不同的 Claude 连接配置。</p>
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
            {profiles.map((profile) => (
              <button
                key={profile.id}
                className={`profile-list-button${activeProfileID === profile.id ? " active" : ""}`}
                type="button"
                onClick={() => handleProfileSelect(profile)}
              >
                <div className="profile-list-head">
                  <strong>{profileTitle(profile)}</strong>
                  <span className="robot-tag">
                    {profile.builtIn ? "默认" : "自定义"}
                  </span>
                </div>
                <p>{profileCardSummary(profile)}</p>
              </button>
            ))}
            <button
              className={`profile-list-button${activeProfileID === newClaudeProfileID ? " active" : ""}`}
              type="button"
              onClick={() => startCreateBlank()}
            >
              <div className="profile-list-head">
                <strong>新增配置</strong>
                <span className="robot-tag">新增</span>
              </div>
              <p>创建一套新的 Claude 连接配置</p>
            </button>
          </div>
          {renderDetailCard({
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
        </div>
      </section>

      {deleteTargetID ? (
        <div className="modal-backdrop" role="presentation">
          <div
            className="modal-card"
            role="dialog"
            aria-modal="true"
            aria-labelledby="delete-claude-profile-title"
          >
            <h3 id="delete-claude-profile-title">确认删除 Claude 配置</h3>
            <p className="modal-copy">
              删除后将移除“
              {profileTitle(
                profiles.find((profile) => profile.id === deleteTargetID) ?? null,
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
                disabled={actionBusy === "delete-claude-profile"}
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
  activeProfile: ClaudeProfileSummary | null;
  deleteTargetID: string | null;
  detailNotice: DetailNotice | null;
  draft: ClaudeProfileDraft;
  editorMode: EditorMode;
  onCancelCreate: () => void;
  onDeleteTargetChange: (value: string | null) => void;
  onDraftChange: Dispatch<SetStateAction<ClaudeProfileDraft>>;
  onSave: (event: FormEvent<HTMLFormElement>) => void;
  onStartCreate: () => void;
};

function renderDetailCard(props: DetailCardProps) {
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
      <section className="panel">
        <div className="step-stage-head">
          <h2>{profileTitle(activeProfile)}</h2>
          <p>使用这台机器当前可用的 Claude 设置。</p>
        </div>
        {detailNotice ? (
          <div className={`notice-banner ${detailNotice.tone}`}>
            {detailNotice.message}
          </div>
        ) : null}
        <div className="completed-card profile-hero-card">
          <h3>系统默认配置</h3>
          <p>如需使用其他端点或模型，请新增配置。</p>
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
        : "新增 Claude 配置"
      : profileTitle(activeProfile);
  const description =
    editorMode === "create"
      ? "填写新的 Claude 连接配置。"
      : "修改后保存会更新当前配置。";

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
        </div>

        <div className="button-row">
          <button
            className="primary-button"
            disabled={actionBusy === "save-claude-profile"}
            type="submit"
          >
            {editorMode === "create" ? "保存配置" : "保存修改"}
          </button>
          {editorMode === "create" ? (
            <button
              className="ghost-button"
              disabled={actionBusy === "save-claude-profile"}
              type="button"
              onClick={() => onCancelCreate()}
            >
              取消
            </button>
          ) : (
            <>
              <button
                className="danger-button"
                disabled={Boolean(deleteTargetID) || actionBusy === "delete-claude-profile"}
                type="button"
                onClick={() => onDeleteTargetChange(activeProfile?.id ?? null)}
              >
                删除配置
              </button>
            </>
          )}
        </div>
      </form>
    </section>
  );
}

function createEmptyDraft(): ClaudeProfileDraft {
  return {
    name: "",
    baseURL: "",
    authToken: "",
    model: "",
    smallModel: "",
  };
}

function createDraftFromProfile(profile: ClaudeProfileSummary): ClaudeProfileDraft {
  return {
    name: profileTitle(profile),
    baseURL: profile.baseURL?.trim() || "",
    authToken: "",
    model: profile.model?.trim() || "",
    smallModel: profile.smallModel?.trim() || "",
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
  };
}

function buildUpdatePayload(
  draft: ClaudeProfileDraft,
  profile: ClaudeProfileSummary,
): ClaudeProfileWriteRequest {
  const payload: ClaudeProfileWriteRequest = {
    name: draft.name.trim(),
    baseURL: draft.baseURL.trim(),
    model: draft.model.trim(),
    smallModel: draft.smallModel.trim(),
  };
  const authToken = optionalString(draft.authToken);
  if (authToken) {
    payload.authToken = authToken;
  }
  void profile;
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
