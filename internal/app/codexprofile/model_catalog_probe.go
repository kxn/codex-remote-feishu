package codexprofile

import (
	"bufio"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"io"
	"os"
	"strings"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/adapter/codex"
	"github.com/kxn/codex-remote-feishu/internal/app/appserverargs"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/execlaunch"
)

type ModelCatalogProbeOptions struct {
	BinaryPath string
	Env        []string
	// Args are the complete app-server launch arguments, including app-server.
	// Empty uses app-server --listen=stdio://. Profile authentication is supplied
	// by the caller's resolved launch material; it is never inherited implicitly.
	Args                       []string
	Version                    string
	Cursor                     string
	Limit                      int
	ExpectedAccountFingerprint string
	ExpectedPlanType           string
}

type ModelCatalogProbeObservation struct {
	Snapshot           agentproto.ModelCatalogSnapshot `json:"snapshot"`
	EvidenceID         string                          `json:"evidenceId"`
	AccountFingerprint string                          `json:"accountFingerprint,omitempty"`
	PlanType           string                          `json:"planType,omitempty"`
}

// RunModelCatalogProbe starts a disposable private app-server and performs only
// initialize/initialized/model/list, with account/read when OAuth identity is
// required. It does not create or resume any thread.
func RunModelCatalogProbe(ctx context.Context, options ModelCatalogProbeOptions) (ModelCatalogProbeObservation, error) {
	fail := func(code, stage string) (ModelCatalogProbeObservation, error) {
		return ModelCatalogProbeObservation{}, &OAuthProbeError{Code: code, Stage: stage}
	}
	if strings.TrimSpace(options.BinaryPath) == "" {
		return fail(ErrorCodexBinaryUnavailable, "model_catalog_launch")
	}
	args := append([]string{}, options.Args...)
	if len(args) == 0 {
		args = []string{"app-server", "--listen=stdio://"}
	}
	match, ok := appserverargs.Find(args)
	if !ok || match.Mode != appserverargs.ModeCodex {
		return fail(ErrorCodexProbeContractMismatch, "model_catalog_args")
	}
	args, err := appserverargs.PrivateStdioArgs(args)
	if err != nil {
		return fail(ErrorCodexProbeContractMismatch, "model_catalog_args")
	}
	workDir, err := os.MkdirTemp("", "codex-remote-model-catalog-*")
	if err != nil {
		return fail(ErrorCodexProbeUnavailable, "model_catalog_workdir")
	}
	defer os.RemoveAll(workDir)
	childCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	cmd := execlaunch.CommandContext(childCtx, options.BinaryPath, args...)
	cmd.Dir = workDir
	cmd.Env = appserverargs.PrivateStdioEnv(options.Env)
	cmd.Stderr = io.Discard
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return fail(ErrorCodexProbeUnavailable, "model_catalog_stdin")
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		_ = stdin.Close()
		return fail(ErrorCodexProbeUnavailable, "model_catalog_stdout")
	}
	if err = cmd.Start(); err != nil {
		_ = stdin.Close()
		_ = stdout.Close()
		return fail(ErrorCodexProbeUnavailable, "model_catalog_start")
	}
	observation, probeErr := runModelCatalogProbeSession(childCtx, stdout, stdin, options)
	expired := childCtx.Err() != nil
	_ = stdin.Close()
	cancel()
	_ = cmd.Wait()
	if expired {
		return fail(ErrorCodexProbeTimeout, "model_catalog_wait")
	}
	if probeErr != nil {
		return ModelCatalogProbeObservation{}, probeErr
	}
	return observation, nil
}

func runModelCatalogProbeSession(ctx context.Context, reader io.Reader, writer io.Writer, options ModelCatalogProbeOptions) (ModelCatalogProbeObservation, error) {
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 4096), 4*1024*1024)
	frames := OAuthProbeFrames(options.Version)
	if _, err := exchangeOAuthProbeFrame(ctx, scanner, writer, frames[0], "model_catalog_initialize"); err != nil {
		return ModelCatalogProbeObservation{}, err
	}
	if err := writeOAuthProbeFrame(writer, frames[1]); err != nil {
		return ModelCatalogProbeObservation{}, &OAuthProbeError{Code: ErrorCodexProbeUnavailable, Stage: "model_catalog_initialized"}
	}
	accountFingerprint, planType := "", ""
	if options.ExpectedAccountFingerprint != "" {
		accountResult, err := exchangeOAuthProbeFrame(ctx, scanner, writer, frames[3], "model_catalog_account")
		if err != nil {
			return ModelCatalogProbeObservation{}, err
		}
		evidence, err := decodeOAuthProbeEvidence(map[string]any{"config": map[string]any{"chatgpt_base_url": OfficialChatGPTBaseURL}}, accountResult, map[string]any{"authMethod": "chatgpt"})
		if err != nil {
			return ModelCatalogProbeObservation{}, err
		}
		account := ClassifyOAuthProbe(evidence)
		if account.Status != OAuthProbeStatusDetected || account.AccountFingerprint == "" || account.AccountFingerprint != options.ExpectedAccountFingerprint || (options.ExpectedPlanType != "" && account.PlanType != options.ExpectedPlanType) {
			return ModelCatalogProbeObservation{}, &OAuthProbeError{Code: ErrorCodexProbeContractMismatch, Stage: "model_catalog_account_changed"}
		}
		accountFingerprint, planType = account.AccountFingerprint, account.PlanType
	}
	nonce := make([]byte, 20)
	if _, err := rand.Read(nonce); err != nil {
		return ModelCatalogProbeObservation{}, &OAuthProbeError{Code: ErrorCodexProbeUnavailable, Stage: "model_catalog_identity"}
	}
	evidence := "model-catalog-probe-" + hex.EncodeToString(nonce)
	limit := options.Limit
	if limit <= 0 {
		limit = 100
	}
	translator := codex.NewTranslator(evidence)
	payloads, err := translator.TranslateCommand(agentproto.Command{CommandID: evidence, Kind: agentproto.CommandModelList, ModelList: agentproto.ModelListCommand{Cursor: options.Cursor, Limit: limit}})
	if err != nil || len(payloads) != 1 {
		return ModelCatalogProbeObservation{}, &OAuthProbeError{Code: ErrorCodexProbeContractMismatch, Stage: "model_catalog_request"}
	}
	var frame OAuthProbeFrame
	if err = json.Unmarshal(payloads[0], &frame); err != nil {
		return ModelCatalogProbeObservation{}, err
	}
	result, err := exchangeOAuthProbeFrame(ctx, scanner, writer, frame, "model_catalog_list")
	if err != nil {
		return ModelCatalogProbeObservation{}, err
	}
	if _, ok := result["data"].([]any); !ok {
		return ModelCatalogProbeObservation{}, &OAuthProbeError{Code: ErrorCodexProbeContractMismatch, Stage: "model_catalog_data"}
	}
	raw, err := json.Marshal(map[string]any{"id": frame.ID, "result": result})
	if err != nil {
		return ModelCatalogProbeObservation{}, err
	}
	parsed, err := translator.ObserveServer(raw)
	if err != nil {
		return ModelCatalogProbeObservation{}, &OAuthProbeError{Code: ErrorCodexProbeContractMismatch, Stage: "model_catalog_response"}
	}
	for _, event := range parsed.Events {
		if event.Kind == agentproto.EventModelCatalogUpdated && event.ModelCatalog != nil {
			return ModelCatalogProbeObservation{Snapshot: *event.ModelCatalog, EvidenceID: evidence, AccountFingerprint: accountFingerprint, PlanType: planType}, nil
		}
	}
	return ModelCatalogProbeObservation{}, &OAuthProbeError{Code: ErrorCodexProbeContractMismatch, Stage: "model_catalog_response"}
}
