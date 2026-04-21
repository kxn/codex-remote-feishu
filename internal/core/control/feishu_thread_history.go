package control

import "time"

type FeishuThreadHistoryViewMode string

const (
	FeishuThreadHistoryViewList   FeishuThreadHistoryViewMode = "list"
	FeishuThreadHistoryViewDetail FeishuThreadHistoryViewMode = "detail"
)

type FeishuThreadHistoryTurnOption struct {
	TurnID   string
	Label    string
	MetaText string
	Current  bool
}

type FeishuThreadHistoryTurnDetail struct {
	TurnID      string
	Ordinal     int
	Status      string
	ErrorText   string
	Inputs      []string
	Outputs     []string
	ReturnPage  int
	PrevTurnID  string
	NextTurnID  string
	UpdatedText string
}

// FeishuThreadHistoryView is the UI-owned read model for the /history list and
// detail card flow.
type FeishuThreadHistoryView struct {
	PickerID         string
	MessageID        string
	Mode             FeishuThreadHistoryViewMode
	Title            string
	ThreadID         string
	ThreadLabel      string
	TurnCount        int
	Page             int
	TotalPages       int
	PageStart        int
	PageEnd          int
	CurrentTurnLabel string
	SelectedTurnID   string
	TurnOptions      []FeishuThreadHistoryTurnOption
	Detail           *FeishuThreadHistoryTurnDetail
	Loading          bool
	LoadingText      string
	NoticeCode       string
	NoticeText       string
	NoticeSections   []FeishuCardTextSection
	Hint             string
	CreatedAt        time.Time
	ExpiresAt        time.Time
}
