package daemon

import surfaceresume "github.com/kxn/codex-remote-feishu/internal/app/daemon/surfaceresume"

const (
	surfaceResumeStateVersion = surfaceresume.StateVersion
	surfaceResumeStateFile    = surfaceresume.StateFileName
)

type SurfaceResumeEntry = surfaceresume.Entry
type HeadlessRestoreHint = surfaceresume.HeadlessRestoreHint
type surfaceResumeState = surfaceresume.StateFile
type surfaceResumeStore = surfaceresume.Store

func newSurfaceResumeStore(path string) *surfaceResumeStore {
	return surfaceresume.NewStore(path)
}

func surfaceResumeStatePath(stateDir string) string {
	return surfaceresume.StatePath(stateDir)
}

func loadSurfaceResumeStore(path string) (*surfaceResumeStore, error) {
	return surfaceresume.LoadStore(path)
}

func normalizeSurfaceResumeEntry(entry SurfaceResumeEntry) (SurfaceResumeEntry, bool) {
	return surfaceresume.NormalizeEntry(entry)
}

func sameSurfaceResumeEntryContent(left, right SurfaceResumeEntry) bool {
	return surfaceresume.SameEntryContent(left, right)
}

func canonicalizeSurfaceResumeEntries(entries map[string]SurfaceResumeEntry) (map[string]SurfaceResumeEntry, bool) {
	return surfaceresume.CanonicalizeEntries(entries)
}

func normalizeResumeThreadTitle(title, threadID, threadCWD, workspaceKey string) string {
	return surfaceresume.NormalizeThreadTitle(title, threadID, threadCWD, workspaceKey)
}

func storedResumeThreadTitle(snapshotTitle, threadID, threadCWD, workspaceKey, threadName string) string {
	return surfaceresume.StoredThreadTitle(snapshotTitle, threadID, threadCWD, workspaceKey, threadName)
}

func normalizeHeadlessRestoreHint(hint HeadlessRestoreHint) (HeadlessRestoreHint, bool) {
	return surfaceresume.NormalizeHeadlessRestoreHint(hint)
}
