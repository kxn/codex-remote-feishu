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
type TokenMode = "keep" | "replace" | "clear";

type ClaudeProfileDraft = {
  name: string;
  authMode: string;
  baseURL: string;
  authToken: string;
  model: string;
  smallModel: string;
  tokenMode: TokenMode;
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
    const validationError = validateDraft(draft, editorMode, activeProfile);
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
      setProfiles((current) => appendOrReplaceProfile(current, response.profile));
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

  function startCopyFrom(profile: ClaudeProfileSummary) {
    setActiveProfileID(newClaudeProfileID);
    setEditorMode("create");
    setDraft(createCopyDraft(profile));
    setDetailNotice({
      tone: "good",
      message: "已带入可见字段。你可以补充新的 Token，也可以先留空保存。",
    });
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
                    {profile.builtIn
                      ? "系统"
                      : profile.authMode === "auth_token"
                        ? "专用 Token"
                        : "沿用当前 Claude"}
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
            onStartCopy: () => {
              if (activeProfile && !activeProfile.builtIn) {
                startCopyFrom(activeProfile);
              }
            },
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
  onStartCopy: () => void;
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
    onStartCopy,
    onStartCreate,
  } = props;

  if (editorMode === "built-in") {
    return (
      <section className="panel">
        <div className="step-stage-head">
          <h2>{profileTitle(activeProfile)}</h2>
          <p>这个配置会沿用当前 Claude 在本机上的默认认证、端点和模型设置。</p>
        </div>
        {detailNotice ? (
          <div className={`notice-banner ${detailNotice.tone}`}>
            {detailNotice.message}
          </div>
        ) : null}
        <div className="completed-card profile-hero-card">
          <h3>系统默认配置</h3>
          <p>如果你想指定专用端点、认证 Token 或模型，请新建一个自定义配置。</p>
        </div>
        <dl className="definition-list">
          <div>
            <dt>认证方式</dt>
            <dd>沿用当前 Claude</dd>
          </div>
          <div>
            <dt>端点</dt>
            <dd>跟随当前 Claude</dd>
          </div>
          <div>
            <dt>主模型</dt>
            <dd>跟随当前 Claude</dd>
          </div>
          <div>
            <dt>轻量模型</dt>
            <dd>跟随当前 Claude</dd>
          </div>
        </dl>
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
      ? "填写一套新的 Claude 连接配置。"
      : "修改这个配置时，旧 Token 不会再次回显。";
  const showTokenFields = draft.authMode === "auth_token";
  const hasSavedToken = Boolean(activeProfile?.hasAuthToken);
  const shouldShowReplacementInput =
    showTokenFields &&
    (editorMode === "create" ||
      !hasSavedToken ||
      draft.tokenMode === "replace");

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
      <form onSubmit={onSave}>
        <div className="form-grid" style={{ marginTop: "1rem" }}>
          <label className="field form-grid-span-2">
            <span>配置名称</span>
            <input
              value={draft.name}
              placeholder="例如：研发代理"
              onChange={(event) =>
                onDraftChange((current) => ({
                  ...current,
                  name: event.target.value,
                }))
              }
            />
            <span className="form-hint">留空时会自动生成一个名称。</span>
          </label>

          <div className="field form-grid-span-2">
            <span>认证方式</span>
            <div className="choice-card-list">
              <label
                className={`choice-card${draft.authMode === "inherit" ? " selected" : ""}`}
              >
                <input
                  checked={draft.authMode === "inherit"}
                  name="claude-auth-mode"
                  type="radio"
                  value="inherit"
                  onChange={() =>
                    onDraftChange((current) => ({
                      ...current,
                      authMode: "inherit",
                      tokenMode: "keep",
                    }))
                  }
                />
                <div>
                  <strong>沿用当前 Claude</strong>
                  <p>继续使用本机 Claude 已有的认证和默认端点。</p>
                </div>
              </label>
              <label
                className={`choice-card${draft.authMode === "auth_token" ? " selected" : ""}`}
              >
                <input
                  checked={draft.authMode === "auth_token"}
                  name="claude-auth-mode"
                  type="radio"
                  value="auth_token"
                  onChange={() =>
                    onDraftChange((current) => ({
                      ...current,
                      authMode: "auth_token",
                      tokenMode:
                        editorMode === "edit" && activeProfile?.hasAuthToken
                          ? "keep"
                          : "replace",
                    }))
                  }
                />
                <div>
                  <strong>使用专用 Token</strong>
                  <p>给这个配置保存独立的端点地址和认证 Token。</p>
                </div>
              </label>
            </div>
          </div>

          {showTokenFields ? (
            <>
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
              <div className="field">
                <span>Token 状态</span>
                <div className="soft-card-v2 profile-token-card">
                  <strong>{describeTokenStatus(editorMode, activeProfile, draft)}</strong>
                  <p>{describeTokenHint(editorMode, activeProfile, draft)}</p>
                </div>
              </div>
            </>
          ) : null}

          {showTokenFields && editorMode === "edit" && hasSavedToken ? (
            <div className="field form-grid-span-2">
              <span>Token 处理方式</span>
              <div className="choice-card-list">
                <label
                  className={`choice-card${draft.tokenMode === "keep" ? " selected" : ""}`}
                >
                  <input
                    checked={draft.tokenMode === "keep"}
                    name="claude-token-mode"
                    type="radio"
                    value="keep"
                    onChange={() =>
                      onDraftChange((current) => ({
                        ...current,
                        tokenMode: "keep",
                        authToken: "",
                      }))
                    }
                  />
                  <div>
                    <strong>保持现状</strong>
                    <p>继续使用已保存的 Token，不会再次回显旧值。</p>
                  </div>
                </label>
                <label
                  className={`choice-card${draft.tokenMode === "replace" ? " selected" : ""}`}
                >
                  <input
                    checked={draft.tokenMode === "replace"}
                    name="claude-token-mode"
                    type="radio"
                    value="replace"
                    onChange={() =>
                      onDraftChange((current) => ({
                        ...current,
                        tokenMode: "replace",
                        authToken: "",
                      }))
                    }
                  />
                  <div>
                    <strong>替换 Token</strong>
                    <p>保存后用一组新的 Token 覆盖旧值。</p>
                  </div>
                </label>
                <label
                  className={`choice-card${draft.tokenMode === "clear" ? " selected" : ""}`}
                >
                  <input
                    checked={draft.tokenMode === "clear"}
                    name="claude-token-mode"
                    type="radio"
                    value="clear"
                    onChange={() =>
                      onDraftChange((current) => ({
                        ...current,
                        tokenMode: "clear",
                        authToken: "",
                      }))
                    }
                  />
                  <div>
                    <strong>清除已保存 Token</strong>
                    <p>保存后会移除这个配置当前保存的 Token。</p>
                  </div>
                </label>
              </div>
            </div>
          ) : null}

          {shouldShowReplacementInput ? (
            <label className="field form-grid-span-2">
              <span>认证 Token</span>
              <input
                autoComplete="new-password"
                placeholder="保存时写入，之后不会再次回显"
                type="password"
                value={draft.authToken}
                onChange={(event) =>
                  onDraftChange((current) => ({
                    ...current,
                    authToken: event.target.value,
                  }))
                }
              />
              <span className="form-hint">
                这个字段只会在保存时写入，页面不会显示旧 Token。
              </span>
            </label>
          ) : null}

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
                className="secondary-button"
                disabled={actionBusy === "save-claude-profile"}
                type="button"
                onClick={() => onStartCopy()}
              >
                复制为新配置
              </button>
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
    authMode: "inherit",
    baseURL: "",
    authToken: "",
    model: "",
    smallModel: "",
    tokenMode: "replace",
  };
}

function createDraftFromProfile(profile: ClaudeProfileSummary): ClaudeProfileDraft {
  return {
    name: profileTitle(profile),
    authMode: profile.authMode?.trim() || "inherit",
    baseURL: profile.baseURL?.trim() || "",
    authToken: "",
    model: profile.model?.trim() || "",
    smallModel: profile.smallModel?.trim() || "",
    tokenMode: profile.hasAuthToken ? "keep" : "replace",
  };
}

function createCopyDraft(profile: ClaudeProfileSummary): ClaudeProfileDraft {
  return {
    name: `${profileTitle(profile)} 副本`,
    authMode: profile.authMode?.trim() || "inherit",
    baseURL: profile.baseURL?.trim() || "",
    authToken: "",
    model: profile.model?.trim() || "",
    smallModel: profile.smallModel?.trim() || "",
    tokenMode: "replace",
  };
}

function validateDraft(
  draft: ClaudeProfileDraft,
  mode: EditorMode,
  profile: ClaudeProfileSummary | null,
): string {
  void draft;
  void mode;
  void profile;
  return "";
}

function buildCreatePayload(draft: ClaudeProfileDraft): ClaudeProfileWriteRequest {
  if (draft.authMode === "auth_token") {
    return {
      name: optionalString(draft.name),
      authMode: "auth_token",
      baseURL: optionalString(draft.baseURL),
      authToken: optionalString(draft.authToken),
      model: draft.model.trim(),
      smallModel: draft.smallModel.trim(),
    };
  }
  return {
    name: optionalString(draft.name),
    authMode: "inherit",
    model: draft.model.trim(),
    smallModel: draft.smallModel.trim(),
  };
}

function buildUpdatePayload(
  draft: ClaudeProfileDraft,
  profile: ClaudeProfileSummary,
): ClaudeProfileWriteRequest {
  const payload: ClaudeProfileWriteRequest = {
    name: optionalString(draft.name),
    authMode: draft.authMode === "auth_token" ? "auth_token" : "inherit",
    model: draft.model.trim(),
    smallModel: draft.smallModel.trim(),
  };
  if (draft.authMode === "auth_token") {
    payload.baseURL = draft.baseURL.trim();
    if (draft.tokenMode === "replace") {
      payload.authToken = optionalString(draft.authToken);
    }
    if (draft.tokenMode === "clear") {
      payload.clearAuthToken = true;
    }
    return payload;
  }
  payload.baseURL = "";
  if (profile.hasAuthToken) {
    payload.clearAuthToken = true;
  }
  return payload;
}

function appendOrReplaceProfile(
  profiles: ClaudeProfileSummary[],
  profile: ClaudeProfileSummary,
): ClaudeProfileSummary[] {
  const nextProfiles = profiles.map((current) =>
    current.id === profile.id ? profile : current,
  );
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
    return "沿用当前 Claude 的本地设置";
  }
  if (profile.authMode === "auth_token") {
    return profile.baseURL?.trim() || "已切到专用 Token，但还没有保存端点";
  }
  return "沿用当前 Claude 的认证和端点";
}

function describeTokenStatus(
  mode: EditorMode,
  profile: ClaudeProfileSummary | null,
  draft: ClaudeProfileDraft,
): string {
  if (draft.authMode !== "auth_token") {
    return "当前不会保存专用 Token";
  }
  if (mode === "create") {
    return draft.authToken.trim() ? "准备写入新的 Token" : "还没有填写 Token";
  }
  if (draft.tokenMode === "replace") {
    return draft.authToken.trim() ? "准备替换为新的 Token" : "正在等待新的 Token";
  }
  if (draft.tokenMode === "clear") {
    return "保存后会清除当前 Token";
  }
  return profile?.hasAuthToken ? "将继续使用已保存 Token" : "当前还没有保存 Token";
}

function describeTokenHint(
  mode: EditorMode,
  profile: ClaudeProfileSummary | null,
  draft: ClaudeProfileDraft,
): string {
  if (draft.authMode !== "auth_token") {
    return "如果以后需要独立端点和认证，再切到专用 Token。";
  }
  if (mode === "create") {
    return "这个字段只会在保存时写入，之后不会再次回显。";
  }
  if (draft.tokenMode === "replace") {
    return "保存后会用新的 Token 覆盖旧值。";
  }
  if (draft.tokenMode === "clear") {
    return "保存后会移除当前 Token；需要时可以稍后再补上新的 Token。";
  }
  return profile?.hasAuthToken
    ? "页面不会显示旧 Token，但保存时会继续沿用它。"
    : "当前还没有保存 Token，请填写后再保存。";
}
