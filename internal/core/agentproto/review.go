package agentproto

import "strings"

type ReviewDelivery string

const (
	ReviewDeliveryInline   ReviewDelivery = "inline"
	ReviewDeliveryDetached ReviewDelivery = "detached"
)

func NormalizeReviewDelivery(value ReviewDelivery) ReviewDelivery {
	switch strings.ToLower(strings.TrimSpace(string(value))) {
	case "inline":
		return ReviewDeliveryInline
	case "detached":
		return ReviewDeliveryDetached
	default:
		return ""
	}
}

type ReviewTargetKind string

const (
	ReviewTargetKindUncommittedChanges ReviewTargetKind = "uncommitted_changes"
	ReviewTargetKindBaseBranch         ReviewTargetKind = "base_branch"
	ReviewTargetKindCommit             ReviewTargetKind = "commit"
	ReviewTargetKindCustom             ReviewTargetKind = "custom"
)

func NormalizeReviewTargetKind(value ReviewTargetKind) ReviewTargetKind {
	switch strings.ToLower(strings.TrimSpace(string(value))) {
	case "uncommitted_changes", "uncommitted", "working_tree", "working-tree":
		return ReviewTargetKindUncommittedChanges
	case "base_branch", "base-branch", "branch":
		return ReviewTargetKindBaseBranch
	case "commit":
		return ReviewTargetKindCommit
	case "custom", "instructions":
		return ReviewTargetKindCustom
	default:
		return ""
	}
}

type ReviewTarget struct {
	Kind         ReviewTargetKind `json:"kind,omitempty"`
	Branch       string           `json:"branch,omitempty"`
	CommitSHA    string           `json:"commitSha,omitempty"`
	CommitTitle  string           `json:"commitTitle,omitempty"`
	Instructions string           `json:"instructions,omitempty"`
}

func (t ReviewTarget) Normalized() ReviewTarget {
	t.Kind = NormalizeReviewTargetKind(t.Kind)
	t.Branch = strings.TrimSpace(t.Branch)
	t.CommitSHA = strings.TrimSpace(t.CommitSHA)
	t.CommitTitle = strings.TrimSpace(t.CommitTitle)
	t.Instructions = strings.TrimSpace(t.Instructions)
	return t
}

func (t ReviewTarget) Valid() bool {
	t = t.Normalized()
	switch t.Kind {
	case ReviewTargetKindUncommittedChanges:
		return true
	case ReviewTargetKindBaseBranch:
		return t.Branch != ""
	case ReviewTargetKindCommit:
		return t.CommitSHA != ""
	case ReviewTargetKindCustom:
		return t.Instructions != ""
	default:
		return false
	}
}

type ReviewRequest struct {
	Delivery ReviewDelivery `json:"delivery,omitempty"`
	Target   ReviewTarget   `json:"target,omitempty"`
}

func (r ReviewRequest) Normalized() ReviewRequest {
	r.Delivery = NormalizeReviewDelivery(r.Delivery)
	r.Target = r.Target.Normalized()
	return r
}
