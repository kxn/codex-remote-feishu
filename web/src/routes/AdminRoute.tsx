import { useEffect, useMemo, useState } from "react";
import { requestJSON, sendJSON } from "../lib/api";
import type {
  BootstrapState,
  ClaudeProfilesResponse,
  ClaudeProfileSummary,
  FeishuAppsResponse,
  FeishuAppSummary,
  ImageStagingCleanupResponse,
  ImageStagingStatusResponse,
  LogsStorageCleanupResponse,
  LogsStorageStatusResponse,
  PreviewDriveCleanupResponse,
  PreviewDriveStatusResponse,
} from "../lib/types";
import { OnboardingFlowSurface } from "./shared/onboarding-flow";
import { ClaudeProfileSection } from "./admin/ClaudeProfileSection";

type NoticeTone = "good" | "warn" | "danger";

type DetailNotice = {
  tone: NoticeTone;
  message: string;
};

const newRobotID = "new";

export function AdminRoute() {
  const [loading, setLoading] = useState(true);
  const [loadError, setLoadError] = useState("");
  const [bootstrap, setBootstrap] = useState<BootstrapState | null>(null);
  const [apps, setApps] = useState<FeishuAppSummary[]>([]);
  const [selectedRobotID, setSelectedRobotID] = useState(newRobotID);
  const [detailNotice, setDetailNotice] = useState<DetailNotice | null>(null);
  const [claudeProfiles, setClaudeProfiles] = useState<ClaudeProfileSummary[]>(
    [],
  );
  const [claudeProfilesError, setClaudeProfilesError] = useState("");
  const [imageStaging, setImageStaging] =
    useState<ImageStagingStatusResponse | null>(null);
  const [imageStagingError, setImageStagingError] = useState("");
  const [logsStorage, setLogsStorage] = useState<LogsStorageStatusResponse | null>(
    null,
  );
  const [logsStorageError, setLogsStorageError] = useState("");
  const [previewMap, setPreviewMap] = useState<
    Record<string, PreviewDriveStatusResponse>
  >({});
  const [previewError, setPreviewError] = useState("");
  const [actionBusy, setActionBusy] = useState("");
  const [deleteTargetID, setDeleteTargetID] = useState<string | null>(null);

  const selectedApp = useMemo(
    () => apps.find((app) => app.id === selectedRobotID) ?? null,
    [apps, selectedRobotID],
  );
  const versionTitle = buildAdminPageTitle(bootstrap);
  const previewSummary = useMemo(() => {
    return Object.values(previewMap).reduce(
      (accumulator, item) => {
        accumulator.fileCount += item.summary.fileCount;
        accumulator.bytes += item.summary.estimatedBytes;
        return accumulator;
      },
      { fileCount: 0, bytes: 0 },
    );
  }, [previewMap]);

  useEffect(() => {
    document.title = versionTitle;
  }, [versionTitle]);

  useEffect(() => {
    void loadAdminPage().catch(() => {
      setLoadError("当前页面暂时无法读取状态，请刷新后重试。");
      setLoading(false);
    });
  }, []);

  async function loadAdminPage(options?: { preferredRobotID?: string }) {
    setLoading(true);
    setLoadError("");

    const [bootstrapState, appList, claudeProfilesResult, imageResult, logsResult] =
      await Promise.all([
        requestJSON<BootstrapState>("/api/admin/bootstrap-state"),
        requestJSON<FeishuAppsResponse>("/api/admin/feishu/apps"),
        safeRequest<ClaudeProfilesResponse>("/api/admin/claude/profiles"),
        safeRequest<ImageStagingStatusResponse>("/api/admin/storage/image-staging"),
        safeRequest<LogsStorageStatusResponse>("/api/admin/storage/logs"),
      ]);

    const previewResults = await Promise.allSettled(
      appList.apps.map(async (app) => {
        const data = await requestJSON<PreviewDriveStatusResponse>(
          `/api/admin/storage/preview-drive/${encodeURIComponent(app.id)}`,
        );
        return [app.id, data] as const;
      }),
    );

    const previews: Record<string, PreviewDriveStatusResponse> = {};
    let previewFailed = false;
    previewResults.forEach((result) => {
      if (result.status === "fulfilled") {
        previews[result.value[0]] = result.value[1];
        return;
      }
      previewFailed = true;
    });

    const nextSelectedRobotID =
      appList.apps.find((app) => app.id === options?.preferredRobotID)?.id ||
      appList.apps.find((app) => app.id === selectedRobotID)?.id ||
      appList.apps[0]?.id ||
      newRobotID;

    setBootstrap(bootstrapState);
    setApps(appList.apps);
    setSelectedRobotID(nextSelectedRobotID);
    setClaudeProfiles(claudeProfilesResult.data?.profiles || []);
    setClaudeProfilesError(claudeProfilesResult.error);
    setImageStaging(imageResult.data);
    setImageStagingError(imageResult.error);
    setLogsStorage(logsResult.data);
    setLogsStorageError(logsResult.error);
    setPreviewMap(previews);
    setPreviewError(previewFailed ? "部分预览文件状态暂时没有读取成功。" : "");
    setLoading(false);
  }

  async function deleteRobot() {
    if (!deleteTargetID) {
      return;
    }
    setActionBusy("delete-robot");
    try {
      await sendJSON<unknown>(
        `/api/admin/feishu/apps/${encodeURIComponent(deleteTargetID)}`,
        "DELETE",
      );
      setDeleteTargetID(null);
      setDetailNotice({ tone: "good", message: "机器人已删除。" });
      await loadAdminPage();
    } catch {
      setDetailNotice({ tone: "danger", message: "当前还不能删除机器人，请稍后重试。" });
    } finally {
      setActionBusy("");
    }
  }

  async function cleanupImageStaging() {
    setActionBusy("cleanup-image");
    try {
      const response = await sendJSON<ImageStagingCleanupResponse>(
        "/api/admin/storage/image-staging/cleanup",
        "POST",
      );
      setImageStaging((current) =>
        current
          ? {
              ...current,
              fileCount: response.remainingFileCount,
              totalBytes: response.remainingBytes,
            }
          : current,
      );
      setImageStagingError("");
    } catch {
      setImageStagingError("图片清理没有完成，请稍后重试。");
    } finally {
      setActionBusy("");
    }
  }

  async function cleanupLogsStorage() {
    setActionBusy("cleanup-logs");
    try {
      const response = await sendJSON<LogsStorageCleanupResponse>(
        "/api/admin/storage/logs/cleanup",
        "POST",
      );
      setLogsStorage((current) =>
        current
          ? {
              ...current,
              fileCount: response.remainingFileCount,
              totalBytes: response.remainingBytes,
            }
          : current,
      );
      setLogsStorageError("");
    } catch {
      setLogsStorageError("日志清理没有完成，请稍后重试。");
    } finally {
      setActionBusy("");
    }
  }

  async function cleanupPreviewDrive() {
    if (apps.length === 0) {
      return;
    }
    setActionBusy("cleanup-preview");
    try {
      const results = await Promise.allSettled(
        apps.map((app) =>
          sendJSON<PreviewDriveCleanupResponse>(
            `/api/admin/storage/preview-drive/${encodeURIComponent(app.id)}/cleanup`,
            "POST",
          ),
        ),
      );
      const nextMap: Record<string, PreviewDriveStatusResponse> = { ...previewMap };
      let failed = false;
      results.forEach((result) => {
        if (result.status !== "fulfilled") {
          failed = true;
          return;
        }
        nextMap[result.value.gatewayId] = {
          gatewayId: result.value.gatewayId,
          name: result.value.name,
          summary: result.value.result.summary,
        };
      });
      setPreviewMap(nextMap);
      setPreviewError(failed ? "部分预览文件暂时没有清理成功。" : "");
    } catch {
      setPreviewError("预览文件清理没有完成，请稍后重试。");
    } finally {
      setActionBusy("");
    }
  }

  function renderRobotDetail() {
    if (!selectedApp) {
      return (
        <>
          {detailNotice ? (
            <div className={`notice-banner ${detailNotice.tone}`}>
              {detailNotice.message}
            </div>
          ) : null}
          <OnboardingFlowSurface
            mode="admin"
            connectOnly
            connectOnlyTitle="新增机器人"
            connectOnlyDescription="选择扫码创建或手动输入，连接验证通过后会自动加入机器人列表。"
            onConnectedApp={async (appID) => {
              setDetailNotice(null);
              await loadAdminPage({ preferredRobotID: appID });
            }}
          />
        </>
      );
    }

    return (
      <section className="panel">
        <div className="step-stage-head">
          <h2>{selectedApp.name || "未命名机器人"}</h2>
          <p>当前机器人 onboarding 与补救流程。</p>
        </div>
        <dl className="definition-list">
          <div>
            <dt>App ID</dt>
            <dd>{selectedApp.appId || "未填写"}</dd>
          </div>
          <div>
            <dt>连接状态</dt>
            <dd>{describeConnectionState(selectedApp)}</dd>
          </div>
          <div>
            <dt>启用状态</dt>
            <dd>{selectedApp.enabled ? "已启用" : "未启用"}</dd>
          </div>
          <div>
            <dt>最近验证</dt>
            <dd>{selectedApp.verifiedAt ? formatTimestamp(selectedApp.verifiedAt) : "暂未验证"}</dd>
          </div>
        </dl>
        {selectedApp.runtimeApply?.pending ? (
          <div className="notice-banner warn">
            当前机器人还在同步设置，请稍后刷新状态后再继续操作。
          </div>
        ) : null}
        {detailNotice ? (
          <div className={`notice-banner ${detailNotice.tone}`}>
            {detailNotice.message}
          </div>
        ) : null}
        <OnboardingFlowSurface
          mode="admin"
          preferredAppID={selectedApp.id}
          onContextRefresh={(appID) => loadAdminPage({ preferredRobotID: appID || selectedApp.id })}
        />
        <div className="button-row">
          <button
            className="danger-button"
            type="button"
            disabled={Boolean(selectedApp.readOnly)}
            onClick={() => setDeleteTargetID(selectedApp.id)}
          >
            删除机器人
          </button>
        </div>
        {selectedApp.readOnly ? (
          <p className="support-copy">当前机器人由运行环境提供，不能在这里删除。</p>
        ) : null}
      </section>
    );
  }

  if (loading) {
    return (
      <div className="product-page">
        <header className="product-topbar">
          <h1>{versionTitle}</h1>
          <p>管理机器人、Claude 配置与本地存储。</p>
        </header>
        <section className="panel">
          <div className="empty-state">
            <div className="loading-dot" />
            <span>正在读取最新状态</span>
          </div>
        </section>
      </div>
    );
  }

  if (loadError) {
    return (
      <div className="product-page">
        <header className="product-topbar">
          <h1>{versionTitle}</h1>
          <p>管理机器人、Claude 配置与本地存储。</p>
        </header>
        <section className="panel">
          <div className="empty-state error">
            <strong>当前页面暂时无法打开</strong>
            <p>{loadError}</p>
            <div className="button-row">
              <button
                className="secondary-button"
                type="button"
                onClick={() => void loadAdminPage()}
              >
                重新加载
              </button>
            </div>
          </div>
        </section>
      </div>
    );
  }

  return (
    <div className="product-page">
      <header className="product-topbar">
        <h1>{versionTitle}</h1>
        <p>管理机器人、Claude 配置与本地存储。</p>
      </header>

      <section className="panel">
        <div className="step-stage-head">
          <h2>机器人管理</h2>
          <p>查看所有机器人并处理需要关注的状态。</p>
        </div>
        <div className="robot-layout" style={{ marginTop: "1rem" }}>
          <div className="robot-list">
            {apps.map((app) => (
              <button
                key={app.id}
                className={`robot-list-button${selectedRobotID === app.id ? " active" : ""}`}
                type="button"
                onClick={() => {
                  setDetailNotice(null);
                  setSelectedRobotID(app.id);
                }}
              >
                <div className="robot-list-head">
                  <strong>{app.name || "未命名机器人"}</strong>
                  {app.runtimeApply?.pending ? <span className="robot-tag warn">同步中</span> : null}
                </div>
                <p>{app.appId || "未填写 App ID"}</p>
              </button>
            ))}
            <button
              className={`robot-list-button${selectedRobotID === newRobotID ? " active" : ""}`}
              type="button"
              onClick={() => {
                setDetailNotice(null);
                setSelectedRobotID(newRobotID);
              }}
            >
              <div className="robot-list-head">
                <strong>新增机器人</strong>
                <span className="robot-tag">新增</span>
              </div>
              <p>点击开始接入</p>
            </button>
          </div>
          {renderRobotDetail()}
        </div>
      </section>

      <ClaudeProfileSection
        loadError={claudeProfilesError}
        profiles={claudeProfiles}
        setProfiles={setClaudeProfiles}
        onReload={async () => {
          await loadAdminPage({ preferredRobotID: selectedRobotID });
        }}
      />

      <section className="panel">
        <div className="step-stage-head">
          <h2>存储管理</h2>
          <p>查看占用并按需清理旧文件。</p>
        </div>
        <div className="soft-grid" style={{ marginTop: "1rem" }}>
          <article className="soft-card-v2">
            <h4>预览文件</h4>
            <p>
              {formatFileSummary(previewSummary.fileCount, previewSummary.bytes)}
            </p>
            {previewError ? <div className="notice-banner warn">{previewError}</div> : null}
            <div className="button-row">
              <button
                className="secondary-button"
                type="button"
                disabled={actionBusy === "cleanup-preview" || apps.length === 0}
                onClick={() => void cleanupPreviewDrive()}
              >
                清理旧预览
              </button>
            </div>
          </article>
          <article className="soft-card-v2">
            <h4>图片暂存</h4>
            <p>
              {formatFileSummary(
                imageStaging?.fileCount || 0,
                imageStaging?.totalBytes || 0,
              )}
            </p>
            {imageStagingError ? (
              <div className="notice-banner warn">{imageStagingError}</div>
            ) : null}
            <div className="button-row">
              <button
                className="secondary-button"
                type="button"
                disabled={actionBusy === "cleanup-image"}
                onClick={() => void cleanupImageStaging()}
              >
                清理旧图片
              </button>
            </div>
          </article>
          <article className="soft-card-v2">
            <h4>日志文件</h4>
            <p>
              {formatFileSummary(
                logsStorage?.fileCount || 0,
                logsStorage?.totalBytes || 0,
              )}
            </p>
            {logsStorageError ? (
              <div className="notice-banner warn">{logsStorageError}</div>
            ) : null}
            <div className="button-row">
              <button
                className="secondary-button"
                type="button"
                disabled={actionBusy === "cleanup-logs"}
                onClick={() => void cleanupLogsStorage()}
              >
                清理一天前日志
              </button>
            </div>
          </article>
        </div>
      </section>

      {deleteTargetID ? (
        <div className="modal-backdrop" role="presentation">
          <div
            className="modal-card"
            role="dialog"
            aria-modal="true"
            aria-labelledby="delete-robot-title"
          >
            <h3 id="delete-robot-title">确认删除机器人</h3>
            <p className="modal-copy">
              删除后将移除“
              {apps.find((app) => app.id === deleteTargetID)?.name || "当前机器人"}
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
                disabled={actionBusy === "delete-robot"}
                onClick={() => void deleteRobot()}
              >
                确认删除
              </button>
            </div>
          </div>
        </div>
      ) : null}
    </div>
  );
}

async function safeRequest<T>(path: string) {
  try {
    return {
      data: await requestJSON<T>(path),
      error: "",
    };
  } catch {
    return {
      data: null,
      error: "暂时没有读取成功，请稍后重试。",
    };
  }
}

function buildAdminPageTitle(bootstrap: BootstrapState | null): string {
  const name = bootstrap?.product.name?.trim() || "Codex Remote Feishu";
  const version = bootstrap?.product.version?.trim();
  return version ? `${name} ${version} 管理` : `${name} 管理`;
}

function describeConnectionState(app: FeishuAppSummary): string {
  switch (app.status?.state) {
    case "connected":
      return "连接正常";
    case "disabled":
      return "已停用";
    case "error":
      return "需要处理";
    default:
      return "待确认";
  }
}

function formatBytes(value: number): string {
  if (value <= 0) {
    return "0 B";
  }
  const units = ["B", "KB", "MB", "GB", "TB"];
  let current = value;
  let index = 0;
  while (current >= 1024 && index < units.length - 1) {
    current /= 1024;
    index += 1;
  }
  return `${current >= 100 || index === 0 ? current.toFixed(0) : current.toFixed(1)} ${units[index]}`;
}

function formatFileSummary(fileCount: number, bytes: number): string {
  return `${fileCount} 个文件，约 ${formatBytes(bytes)}`;
}

function formatTimestamp(value: string): string {
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) {
    return "暂不可用";
  }
  return date.toLocaleString();
}
