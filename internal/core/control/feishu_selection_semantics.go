package control

import "strings"

type FeishuSelectionSemantics struct {
	PromptKind          SelectionPromptKind
	Layout              string
	ViewMode            string
	Title               string
	ContextTitle        string
	ContextText         string
	ContextKey          string
	HiddenEntriesNotice string
}

func DeriveFeishuSelectionSemantics(view FeishuSelectionView) FeishuSelectionSemantics {
	semantics := FeishuSelectionSemantics{
		PromptKind: view.PromptKind,
	}
	switch {
	case view.Instance != nil && view.PromptKind == SelectionPromptAttachInstance:
		semantics.Layout = "vscode_instance_list"
		semantics.Title = "在线 VS Code 实例"
		if view.Instance.Current != nil {
			semantics.ContextTitle = "当前实例"
			semantics.ContextText = strings.TrimSpace(view.Instance.Current.ContextText)
		}
	case view.Workspace != nil && view.PromptKind == SelectionPromptAttachWorkspace:
		semantics.Layout = "grouped_attach_workspace"
		semantics.Title = "工作区列表"
		semantics.ViewMode = "paged"
		if view.Workspace.Current != nil {
			semantics.ContextTitle = "当前工作区"
			semantics.ContextText = feishuWorkspaceSelectionContextText(view.Workspace.Current.WorkspaceLabel, view.Workspace.Current.AgeText)
			semantics.ContextKey = strings.TrimSpace(view.Workspace.Current.WorkspaceKey)
		}
	case view.Thread != nil && view.PromptKind == SelectionPromptUseThread:
		semantics.ViewMode = string(view.Thread.Mode)
		switch view.Thread.Mode {
		case FeishuThreadSelectionNormalGlobalRecent, FeishuThreadSelectionNormalGlobalAll:
			semantics.Layout = "workspace_grouped_useall"
			semantics.Title = "全部会话"
		case FeishuThreadSelectionNormalScopedRecent:
			semantics.Title = "最近会话"
		case FeishuThreadSelectionNormalScopedAll:
			semantics.Title = "当前工作区全部会话"
		case FeishuThreadSelectionNormalWorkspaceView:
			semantics.Layout = "workspace_grouped_useall"
			if view.Thread.Workspace != nil {
				workspaceLabel := strings.TrimSpace(view.Thread.Workspace.WorkspaceLabel)
				if workspaceLabel == "" {
					workspaceLabel = strings.TrimSpace(view.Thread.Workspace.WorkspaceKey)
				}
				if workspaceLabel == "" {
					workspaceLabel = "工作区"
				}
				semantics.Title = workspaceLabel + " 全部会话"
				semantics.ContextKey = strings.TrimSpace(view.Thread.Workspace.WorkspaceKey)
			}
		case FeishuThreadSelectionVSCodeRecent:
			semantics.Layout = "vscode_instance_threads"
			semantics.Title = "最近会话"
		case FeishuThreadSelectionVSCodeAll, FeishuThreadSelectionVSCodeScopedAll:
			semantics.Layout = "vscode_instance_threads"
			semantics.Title = "当前实例全部会话"
		}
		if view.Thread.CurrentWorkspace != nil {
			semantics.ContextTitle = "当前工作区"
			semantics.ContextKey = strings.TrimSpace(view.Thread.CurrentWorkspace.WorkspaceKey)
			line := strings.TrimSpace(view.Thread.CurrentWorkspace.WorkspaceLabel)
			if age := strings.TrimSpace(view.Thread.CurrentWorkspace.AgeText); age != "" {
				line += " · " + age
			}
			semantics.ContextText = strings.Join([]string{line, "同工作区内切换请直接用 /use"}, "\n")
		}
		if view.Thread.CurrentInstance != nil {
			semantics.ContextTitle = "当前实例"
			semantics.ContextText = strings.TrimSpace(view.Thread.CurrentInstance.Label)
			if status := strings.TrimSpace(view.Thread.CurrentInstance.Status); status != "" {
				semantics.ContextText += " · " + status
			}
		}
		if selectionViewUsesHiddenDisabledThreadHint(view.Thread) {
			semantics.HiddenEntriesNotice = "已省略当前不可切换的会话。"
		}
	case view.KickThread != nil && view.PromptKind == SelectionPromptKickThread:
		semantics.Layout = "kick_thread_confirm"
		semantics.Title = "强踢当前会话？"
	}
	return semantics
}

func selectionViewUsesHiddenDisabledThreadHint(view *FeishuThreadSelectionView) bool {
	if view == nil {
		return false
	}
	switch view.Mode {
	case FeishuThreadSelectionVSCodeRecent, FeishuThreadSelectionVSCodeAll, FeishuThreadSelectionVSCodeScopedAll:
	default:
		return false
	}
	for _, entry := range view.Entries {
		if entry.Disabled && !entry.Current {
			return true
		}
	}
	return false
}

func feishuWorkspaceSelectionContextText(label, ageText string) string {
	label = strings.TrimSpace(label)
	parts := []string{label}
	if age := strings.TrimSpace(ageText); age != "" {
		parts[0] += " · " + age
	}
	parts = append(parts, "同工作区内继续工作可 /use，或直接发送文本（也可 /new）")
	return strings.Join(parts, "\n")
}

func FormatFeishuWorkspaceSelectionMetaText(ageText string, hasVSCodeActivity, busy, unavailable, recoverableOnly bool) string {
	parts := make([]string, 0, 2)
	if age := strings.TrimSpace(ageText); age != "" {
		parts = append(parts, age)
	}
	switch {
	case busy:
		parts = append(parts, "当前被其他飞书会话接管")
	case recoverableOnly:
		parts = append(parts, "后台可恢复")
	case unavailable:
		parts = append(parts, "当前暂不可接管")
	case hasVSCodeActivity:
		parts = append(parts, "有 VS Code 活动")
	}
	if len(parts) == 0 {
		return "可接管"
	}
	return strings.Join(parts, " · ")
}
