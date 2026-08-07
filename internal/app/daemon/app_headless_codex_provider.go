package daemon

import (
	"context"

	"github.com/kxn/codex-remote-feishu/internal/app/codexprofile"
	"github.com/kxn/codex-remote-feishu/internal/codexcatalog"
	"github.com/kxn/codex-remote-feishu/internal/config"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

func codexHeadlessLaunchProblem(err error, defaults agentproto.ErrorInfo) agentproto.ErrorInfo {
	problem := agentproto.ErrorInfoFromError(err, defaults)
	code := codexprofile.RuntimeErrorCode(err)
	if code == "" {
		return problem
	}
	problem.Code = code
	problem.Retryable = false
	problem.Details = codexprofile.RuntimeErrorStage(err)
	switch code {
	case codexprofile.ErrorProfileDefinitionIncomplete:
		problem.Message = "Codex Profile 缺少必需的模型或推理配置，请先完成配置。"
	case codexprofile.ErrorProfileSecretMissing:
		problem.Message = "Codex Profile 缺少可用的 API Key，请先更新 Profile。"
	case codexprofile.ErrorOAuthMissing:
		problem.Message = "本机 Codex 当前没有可用的 ChatGPT 登录，请先重新登录。"
	case codexprofile.ErrorOAuthProbeUnknown:
		problem.Message = "暂时无法确认本机 Codex 的 ChatGPT 登录状态，请刷新状态后再试。"
	case codexprofile.ErrorOAuthDeploymentUnsupported:
		problem.Message = "当前 ChatGPT 登录使用了暂不支持的自定义部署，请改用本机默认或官方部署。"
	case codexprofile.ErrorCodexCapabilityUnsupported:
		problem.Message = "当前 Codex 版本不支持所需的 Profile 隔离能力，请先升级 Codex。"
	case codexprofile.ErrorCodexBinaryUnavailable:
		problem.Message = "找不到可用的 Codex 可执行文件，或 Codex 启动失败，请检查 Codex 安装与运行环境。"
	case codexprofile.ErrorCodexProbeTimeout:
		problem.Message = "Codex 能力探测超时，请稍后重试或重启服务。"
	case codexprofile.ErrorCodexProbeUnavailable:
		problem.Message = "暂时无法完成 Codex 能力探测，请稍后重试。"
	case codexprofile.ErrorCodexProbeContractMismatch:
		problem.Message = "Codex app-server 返回的协议契约与预期不一致，暂时无法确认兼容性。"
	case codexprofile.ErrorManagedModelCatalogMissing:
		problem.Message = "当前运行目录无法准备 Codex 模型目录，请检查服务安装状态后再试。"
	case codexprofile.ErrorProfileRevisionUnavailable:
		problem.Message = "这个任务引用的 Codex Profile 版本已经不可用，请重新选择 Profile。"
	}
	return problem.Normalize()
}

func (a *App) applyCodexHeadlessProviderConfig(baseEnv, baseArgs []string, backend agentproto.Backend, providerID string) ([]string, []string, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	env, args, _, err := a.applyCodexHeadlessProviderConfigLocked(baseEnv, baseArgs, backend, providerID, nil)
	return env, args, err
}

func (a *App) applyCodexHeadlessProviderConfigLocked(baseEnv, baseArgs []string, backend agentproto.Backend, providerID string, admissionRef *state.CodexAdmissionRef) ([]string, []string, *codexprofile.RuntimeProjection, error) {
	env := append([]string{}, baseEnv...)
	args := append([]string{}, baseArgs...)
	if agentproto.NormalizeBackend(backend) != agentproto.BackendCodex {
		return env, args, nil, nil
	}

	loaded, err := a.loadAdminConfig()
	if err != nil {
		return nil, nil, nil, err
	}
	profileID := state.CodexProfileIDFromLegacyProviderID(providerID)
	profileRevision := uint64(1)
	ref := state.NormalizeCodexAdmissionRef(admissionRef)
	if ref != nil {
		profileID = ref.ProfileRef.ID
		profileRevision = ref.ProfileRef.Revision
	}
	var oauthState *state.CodexOAuthProfileState
	if profileID != config.CodexNativeProfileID && profileID != config.CodexOAuthProfileID {
		index := config.IndexOfCodexAPIProfile(loaded.Config.Codex.Profiles, profileID)
		if index < 0 {
			return nil, nil, nil, &codexprofile.RuntimeError{Code: codexprofile.ErrorProfileRevisionUnavailable}
		}
		if ref == nil {
			profile, ok := config.CurrentCodexAPIProfile(loaded.Config.Codex.Profiles[index])
			if !ok {
				return nil, nil, nil, &codexprofile.RuntimeError{Code: codexprofile.ErrorProfileRevisionUnavailable}
			}
			profileRevision = profile.Revision
		}
	} else if profileID == config.CodexOAuthProfileID {
		if err := a.ensureCodexOAuthProfileForLaunchLocked(context.Background()); err != nil {
			return nil, nil, nil, err
		}
	}
	capabilitySet := a.effectiveCodexRuntimeCapabilitySetLocked()
	capabilityErrorCode := a.effectiveCodexRuntimeCapabilityErrorCodeLocked()
	capabilityErrorStage := a.effectiveCodexRuntimeCapabilityErrorStageLocked()
	if profileID == config.CodexOAuthProfileID {
		profile, ok := a.codexOAuthProfileState.current()
		if !ok {
			return nil, nil, nil, &codexprofile.RuntimeError{Code: codexprofile.ErrorOAuthProbeUnknown}
		}
		if ref == nil {
			profileRevision = profile.Revision
		}
		oauthState = &profile
	}
	preferenceStore, err := a.profileContextPreferenceStore()
	if err != nil {
		return nil, nil, nil, err
	}
	effectiveRef := state.CodexAdmissionRef{
		ProfileRef: state.CodexProfileRef{ID: profileID, Revision: profileRevision},
	}
	if ref != nil {
		effectiveRef.ContextPreferenceRef = ref.ContextPreferenceRef
	} else {
		preference, ok := preferenceStore.CodexCurrent(profileID)
		if !ok {
			return nil, nil, nil, &codexprofile.RuntimeError{Code: codexprofile.ErrorProfileRevisionUnavailable}
		}
		effectiveRef.ContextPreferenceRef = state.CodexContextPreferenceRef{ProfileID: profileID, Revision: preference.Revision}
	}
	nativeEvidence, reservedProviderIDs, nativeProviderEnvKeys, nativeConfigProbeFailed := a.effectiveCodexNativeConnectionLocked()
	resolver := codexprofile.RuntimeResolver{
		APIProfiles: loaded.Config.Codex.Profiles,
		Preference: func(requested state.CodexContextPreferenceRef) (state.ProfileContextPreference, bool) {
			return preferenceStore.CodexRevision(requested.ProfileID, requested.Revision)
		},
		Native:                  nativeEvidence,
		ReservedProviderIDs:     reservedProviderIDs,
		NativeProviderEnvKeys:   nativeProviderEnvKeys,
		NativeConfigProbeFailed: nativeConfigProbeFailed,
		OAuthState:              oauthState,
		CapabilitySet:           capabilitySet,
		CapabilityErrorCode:     capabilityErrorCode,
		CapabilityErrorStage:    capabilityErrorStage,
		ManagedModelCatalogDir:  codexcatalog.ManagedModelCatalogDir(a.headlessRuntime.Paths.StateDir),
	}
	projection, err := resolver.Resolve(effectiveRef)
	if err != nil {
		return nil, nil, nil, err
	}
	if err := codexprofile.EnsureLaunchManagedFiles(projection.Launch); err != nil {
		return nil, nil, nil, err
	}
	env, args = codexprofile.ApplyLaunchMaterial(env, args, projection.Launch)
	env = config.UpsertEnvValue(env, codexprofile.CodexRuntimeResolvedEnv, "1")
	return env, args, &projection, nil
}
