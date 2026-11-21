// Package workpractices installs optional, versioned methods shipped with the
// product. Installing a library never adopts a method for a project.
package workpractices

import (
	"context"
	"embed"
	"errors"
	"fmt"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
	"time"
)

//go:embed library/*.md
var library embed.FS

const ScopeID = "shipped-practice-library"
const Version = "1"

type method struct {
	id, purpose, applicability string
	inputs, outputs            []string
}

var methods = []method{
	{"code-review", "围绕实际变化做代码评审", "有明确代码或方案版本需要独立检查", []string{"任务要求", "待审查版本", "参与约定"}, []string{"有证据的问题及检查边界", "对应版本的评审记录"}},
	{"verification", "用证据验证结果", "交付前验证行为，或修复和恢复后的复核", []string{"期待行为", "变更版本", "已有验证证据"}, []string{"实际检查结果", "尚未验证的具体边界"}},
	{"integration", "协作后的集成检查", "多项独立成果需要接成实际可用流程", []string{"各项交付及版本", "接口与依赖", "共享修改面"}, []string{"集成结果", "入口与异常流程的验证证据"}},
	{"delegation", "分工、反馈和责任交接", "负责人协调多个角色或工作需要移交", []string{"明确结果", "任务要求及依赖", "当前参与约定"}, []string{"真实分工记录", "成果、问题和责任交接"}},
	{"materials", "管理共享资料和交付", "需要维护长期资料或把成果交给人", []string{"资料用途", "已登记入口", "需要人注意的内容"}, []string{"稳定资料入口", "有版本和保存证据的交付"}},
}

func Install(ctx context.Context, store *workstore.Store) error {
	return store.Within(ctx, func(tx *workstore.Tx) error {
		scope, err := tx.GetScope(ScopeID)
		if errors.Is(err, workstore.ErrNotFound) {
			err = tx.PutScope(work.WorkScope{ID: ScopeID, Kind: work.ScopeLibrary, Revision: 1}, 0)
		} else if err == nil && scope.Kind != work.ScopeLibrary {
			return fmt.Errorf("shipped library scope is owned by another purpose")
		}
		if err != nil {
			return err
		}
		for _, m := range methods {
			body, err := library.ReadFile("library/" + m.id + ".md")
			if err != nil {
				return err
			}
			definition := work.PracticeDefinition{
				StorageScopeID: ScopeID, Owner: work.KnowledgeOwner{Kind: work.KnowledgeGlobal}, Revision: 1,
				PracticeRef: work.PracticeRef{ID: "builtin." + m.id, Version: Version}, Purpose: m.purpose, Applicability: m.applicability,
				Exclusions: "可选方法，不强制所有项目采用；项目的明确要求和当前参与约定优先。",
				Inputs:     m.inputs, Steps: []string{"核对本次范围与条件", "按正文选择适用步骤", "保存实际结果和未完成事项"}, Outputs: m.outputs,
				Escalation: "缺少必要信息、发现重要体验变化或无法确认外部结果时，向负责人或当前约定的决定者说明。",
			}
			if err = tx.InstallBuiltinPractice(definition, body, time.Now().UTC()); err != nil {
				return fmt.Errorf("install %s@%s: %w", definition.ID, definition.Version, err)
			}
		}
		return nil
	})
}
