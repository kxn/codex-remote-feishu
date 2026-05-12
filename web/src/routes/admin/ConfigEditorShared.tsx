import {
  useEffect,
  useState,
  type Dispatch,
  type FormEvent,
  type ReactNode,
  type SetStateAction,
} from "react";

export type NoticeTone = "good" | "warn" | "danger";

export type DetailNotice = {
  tone: NoticeTone;
  message: string;
};

export type EditorMode = "built-in" | "edit" | "create";

type ConfigEditorItem = {
  id: string;
  builtIn?: boolean;
};

type UseConfigEditorSectionOptions<TItem extends ConfigEditorItem, TDraft> = {
  items: TItem[];
  newItemID: string;
  createEmptyDraft: () => TDraft;
  createDraftFromItem: (item: TItem) => TDraft;
};

export type ConfigEditorSectionState<TItem extends ConfigEditorItem, TDraft> = {
  activeItem: TItem | null;
  activeItemID: string;
  actionBusy: string;
  deleteTargetID: string | null;
  detailNotice: DetailNotice | null;
  draft: TDraft;
  editorMode: EditorMode;
  setActionBusy: Dispatch<SetStateAction<string>>;
  setDeleteTargetID: Dispatch<SetStateAction<string | null>>;
  setDetailNotice: Dispatch<SetStateAction<DetailNotice | null>>;
  setDraft: Dispatch<SetStateAction<TDraft>>;
  applyNextItems: (items: TItem[]) => void;
  cancelCreate: () => void;
  handleItemSelect: (item: TItem) => void;
  selectPersistedItem: (item: TItem) => void;
  startCreateBlank: () => void;
};

export function useConfigEditorSection<TItem extends ConfigEditorItem, TDraft>(
  options: UseConfigEditorSectionOptions<TItem, TDraft>,
): ConfigEditorSectionState<TItem, TDraft> {
  const { items, newItemID, createEmptyDraft, createDraftFromItem } = options;
  const [activeItemID, setActiveItemID] = useState("");
  const [editorMode, setEditorMode] = useState<EditorMode>("create");
  const [draft, setDraft] = useState<TDraft>(createEmptyDraft);
  const [detailNotice, setDetailNotice] = useState<DetailNotice | null>(null);
  const [actionBusy, setActionBusy] = useState("");
  const [deleteTargetID, setDeleteTargetID] = useState<string | null>(null);

  useEffect(() => {
    if (items.length === 0) {
      if (editorMode !== "create") {
        setActiveItemID(newItemID);
        setEditorMode("create");
        setDraft(createEmptyDraft());
      }
      return;
    }
    if (activeItemID === newItemID && editorMode === "create") {
      return;
    }
    const fallbackItem = items[0];
    const activeItem = items.find((item) => item.id === activeItemID) ?? fallbackItem;
    if (!activeItem) {
      return;
    }
    if (activeItem.id !== activeItemID) {
      setActiveItemID(activeItem.id);
    }
    if (activeItem.builtIn) {
      setEditorMode("built-in");
      setDraft(createEmptyDraft());
      return;
    }
    setEditorMode("edit");
    setDraft(createDraftFromItem(activeItem));
  }, [activeItemID, createDraftFromItem, createEmptyDraft, editorMode, items, newItemID]);

  const activeItem = items.find((item) => item.id === activeItemID) ?? null;

  function selectPersistedItem(item: TItem) {
    setActiveItemID(item.id);
    setEditorMode(item.builtIn ? "built-in" : "edit");
    setDraft(item.builtIn ? createEmptyDraft() : createDraftFromItem(item));
  }

  function startCreateBlank() {
    setActiveItemID(newItemID);
    setEditorMode("create");
    setDraft(createEmptyDraft());
    setDetailNotice(null);
  }

  function cancelCreate() {
    const fallbackItem = items[0] ?? null;
    if (!fallbackItem) {
      startCreateBlank();
      return;
    }
    if (fallbackItem.builtIn) {
      setActiveItemID(fallbackItem.id);
      setEditorMode("built-in");
      setDraft(createEmptyDraft());
      setDetailNotice(null);
      return;
    }
    selectPersistedItem(fallbackItem);
    setDetailNotice(null);
  }

  function handleItemSelect(item: TItem) {
    setDetailNotice(null);
    if (item.builtIn) {
      setActiveItemID(item.id);
      setEditorMode("built-in");
      setDraft(createEmptyDraft());
      return;
    }
    selectPersistedItem(item);
  }

  function applyNextItems(nextItems: TItem[]) {
    const nextSelected = nextItems[0] ?? null;
    if (!nextSelected) {
      startCreateBlank();
      return;
    }
    if (nextSelected.builtIn) {
      setActiveItemID(nextSelected.id);
      setEditorMode("built-in");
      setDraft(createEmptyDraft());
      return;
    }
    selectPersistedItem(nextSelected);
  }

  return {
    activeItem,
    activeItemID,
    actionBusy,
    deleteTargetID,
    detailNotice,
    draft,
    editorMode,
    setActionBusy,
    setDeleteTargetID,
    setDetailNotice,
    setDraft,
    applyNextItems,
    cancelCreate,
    handleItemSelect,
    selectPersistedItem,
    startCreateBlank,
  };
}

type ConfigSectionShellProps<TItem extends ConfigEditorItem> = {
  sectionTitle: string;
  sectionDescription: string;
  emptyLoadErrorTitle: string;
  loadError: string;
  onReload: () => Promise<void> | void;
  items: TItem[];
  activeItemID: string;
  newItemID: string;
  onItemSelect: (item: TItem) => void;
  onStartCreate: () => void;
  getItemTitle: (item: TItem) => string;
  getItemSummary: (item: TItem) => string;
  detailCard: ReactNode;
};

export function ConfigSectionShell<TItem extends ConfigEditorItem>(
  props: ConfigSectionShellProps<TItem>,
) {
  const {
    sectionTitle,
    sectionDescription,
    emptyLoadErrorTitle,
    loadError,
    onReload,
    items,
    activeItemID,
    newItemID,
    onItemSelect,
    onStartCreate,
    getItemTitle,
    getItemSummary,
    detailCard,
  } = props;

  if (loadError && items.length === 0) {
    return (
      <section className="panel">
        <div className="step-stage-head">
          <h2>{sectionTitle}</h2>
          <p>{sectionDescription}</p>
        </div>
        <div className="empty-state error">
          <strong>{emptyLoadErrorTitle}</strong>
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
    <section className="panel">
      <div className="step-stage-head">
        <h2>{sectionTitle}</h2>
        <p>{sectionDescription}</p>
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
          {items.map((item) => (
            <button
              key={item.id}
              className={`profile-list-button${activeItemID === item.id ? " active" : ""}`}
              type="button"
              onClick={() => onItemSelect(item)}
            >
              <div className="profile-list-head">
                <strong>{getItemTitle(item)}</strong>
                <span className="robot-tag">
                  {item.builtIn ? "默认" : "自定义"}
                </span>
              </div>
              <p>{getItemSummary(item)}</p>
            </button>
          ))}
          <button
            className={`profile-list-button${activeItemID === newItemID ? " active" : ""}`}
            type="button"
            onClick={() => onStartCreate()}
          >
            <div className="profile-list-head">
              <strong>新增配置</strong>
              <span className="robot-tag">新增</span>
            </div>
            <p>新建配置</p>
          </button>
        </div>
        {detailCard}
      </div>
    </section>
  );
}

type ConfigBuiltInDetailCardProps = {
  title: string;
  description: string;
  notice: DetailNotice | null;
  heroTitle: string;
  heroDescription: string;
  startCreateLabel: string;
  onStartCreate: () => void;
};

export function ConfigBuiltInDetailCard(props: ConfigBuiltInDetailCardProps) {
  const {
    title,
    description,
    notice,
    heroTitle,
    heroDescription,
    startCreateLabel,
    onStartCreate,
  } = props;

  return (
    <section className="panel">
      <div className="step-stage-head">
        <h2>{title}</h2>
        <p>{description}</p>
      </div>
      {notice ? (
        <div className={`notice-banner ${notice.tone}`}>{notice.message}</div>
      ) : null}
      <div className="completed-card profile-hero-card">
        <h3>{heroTitle}</h3>
        <p>{heroDescription}</p>
      </div>
      <div className="button-row">
        <button
          className="primary-button"
          type="button"
          onClick={() => onStartCreate()}
        >
          {startCreateLabel}
        </button>
      </div>
    </section>
  );
}

type ConfigFormDetailCardProps = {
  title: string;
  description: string;
  notice: DetailNotice | null;
  onSave: (event: FormEvent<HTMLFormElement>) => void;
  submitLabel: string;
  submitDisabled: boolean;
  secondaryAction: ReactNode;
  children: ReactNode;
};

export function ConfigFormDetailCard(props: ConfigFormDetailCardProps) {
  const {
    title,
    description,
    notice,
    onSave,
    submitLabel,
    submitDisabled,
    secondaryAction,
    children,
  } = props;

  return (
    <section className="panel">
      <div className="step-stage-head">
        <h2>{title}</h2>
        <p>{description}</p>
      </div>
      {notice ? (
        <div className={`notice-banner ${notice.tone}`}>{notice.message}</div>
      ) : null}
      <form noValidate onSubmit={onSave}>
        {children}
        <div className="button-row">
          <button
            className="primary-button"
            disabled={submitDisabled}
            type="submit"
          >
            {submitLabel}
          </button>
          {secondaryAction}
        </div>
      </form>
    </section>
  );
}

type ConfigDeleteConfirmModalProps<TItem extends ConfigEditorItem> = {
  targetID: string | null;
  items: TItem[];
  dialogTitle: string;
  confirmDisabled: boolean;
  getItemTitle: (item: TItem | null) => string;
  onCancel: () => void;
  onConfirm: () => void;
};

export function ConfigDeleteConfirmModal<TItem extends ConfigEditorItem>(
  props: ConfigDeleteConfirmModalProps<TItem>,
) {
  const {
    targetID,
    items,
    dialogTitle,
    confirmDisabled,
    getItemTitle,
    onCancel,
    onConfirm,
  } = props;

  if (!targetID) {
    return null;
  }

  const targetItem = items.find((item) => item.id === targetID) ?? null;
  const titleID = dialogTitle.replace(/\s+/g, "-").toLowerCase();

  return (
    <div className="modal-backdrop" role="presentation">
      <div
        className="modal-card"
        role="dialog"
        aria-modal="true"
        aria-labelledby={titleID}
      >
        <h3 id={titleID}>{dialogTitle}</h3>
        <p className="modal-copy">
          删除后将移除“
          {getItemTitle(targetItem)}
          ”，此操作不可恢复。
        </p>
        <div className="modal-actions">
          <button
            className="ghost-button"
            type="button"
            onClick={() => onCancel()}
          >
            取消
          </button>
          <button
            className="danger-button"
            type="button"
            disabled={confirmDisabled}
            onClick={() => onConfirm()}
          >
            确认删除
          </button>
        </div>
      </div>
    </div>
  );
}
