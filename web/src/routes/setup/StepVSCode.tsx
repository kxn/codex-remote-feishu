import type { VSCodeDetectResponse } from "../../lib/types";
import type { VSCodeUsageScenario } from "../shared/helpers";

export function StepVSCode(props: {
  vscode: VSCodeDetectResponse | null;
  vscodeError: string;
  vscodeScenario: VSCodeUsageScenario | null;
  vscodeBundleDetected: boolean;
  onVSCodeScenarioChange: (scenario: VSCodeUsageScenario) => void;
}) {
  const { vscode, vscodeError, vscodeScenario, vscodeBundleDetected, onVSCodeScenarioChange } = props;

  if (vscodeError) {
    return <div className="notice-banner warn">VS Code 检测暂时不可用：{vscodeError}</div>;
  }
  if (!vscode) {
    return <div className="notice-banner warn">当前还没拿到 VS Code 检测结果，请先刷新状态后再继续。</div>;
  }

  return (
    <div className="wizard-step-layout">
      <div className="manifest-block" style={{ marginBottom: '1.5rem', background: '#fff' }}>
        <h4>不使用 VS Code 可以直接跳过</h4>
        <p style={{ color: 'var(--text-soft)', marginTop: '0.4rem' }}>
          这一步只在你准备使用 VS Code 里的 Codex 时才需要处理。不用 VS Code 的话，可以直接点底部的“跳过 VS Code”。
        </p>
      </div>

      {vscode?.sshSession ? (
        <>
          <div className="manifest-block" style={{ marginBottom: '1.5rem', background: '#fff' }}>
            <h4>检测到你正在远程机器上完成设置</h4>
            <p>如果你准备在这台远程机器上配合 VS Code 使用，继续这一步就可以了。</p>
          </div>
          <div className="manifest-block" style={{ background: '#fff' }}>
            <h4>推荐操作</h4>
            <p>我们会把 VS Code 在这台机器上的使用入口接好，方便你后面直接从 VS Code 进入。</p>
          </div>
          {!vscodeBundleDetected && (
            <div className="notice-banner warn" style={{ marginTop: '1.5rem' }}>
              还没检测到这台机器上的 VS Code 扩展。请先在这台机器上打开一次远程 VS Code 窗口，并确保 Codex 扩展已经安装，然后再回来继续。
            </div>
          )}
        </>
      ) : (
        <>
          <div className="manifest-block" style={{ marginBottom: '1.5rem', background: '#fff' }}>
            <h4>你以后主要怎么使用 VS Code 里的 Codex？</h4>
            <p style={{ color: 'var(--text-soft)' }}>先确认当前这台机器以后会不会拿来配合 VS Code 使用。</p>
          </div>
          <div className="choice-card-list" role="radiogroup" aria-label="VS Code 使用场景">
            <label className={`choice-card${vscodeScenario === "current_machine" ? " selected" : ""}`} style={{ cursor: 'pointer', padding: '1rem', border: '1px solid var(--border)', borderRadius: '1rem', background: vscodeScenario === "current_machine" ? 'var(--accent-light)' : '#fff' }}>
              <input type="radio" style={{ marginRight: '1rem' }} checked={vscodeScenario === "current_machine"} onChange={() => onVSCodeScenarioChange("current_machine")} />
              <div style={{ display: 'inline-block', verticalAlign: 'top' }}>
                <strong style={{ display: 'block' }}>要在当前这台机器上使用</strong>
                <p style={{ margin: 0, marginTop: '0.3rem', color: 'var(--text-soft)' }}>无论是本地打开，还是通过远程连接打开，只要会在这台机器上用，就选这个。</p>
              </div>
            </label>
            <label className={`choice-card${vscodeScenario === "remote_only" ? " selected" : ""}`} style={{ cursor: 'pointer', padding: '1rem', border: '1px solid var(--border)', borderRadius: '1rem', background: vscodeScenario === "remote_only" ? 'var(--accent-light)' : '#fff' }}>
              <input type="radio" style={{ marginRight: '1rem' }} checked={vscodeScenario === "remote_only"} onChange={() => onVSCodeScenarioChange("remote_only")} />
              <div style={{ display: 'inline-block', verticalAlign: 'top' }}>
                <strong style={{ display: 'block' }}>主要去别的 SSH 机器上使用</strong>
                <p style={{ margin: 0, marginTop: '0.3rem', color: 'var(--text-soft)' }}>当前机器先不用处理，等你真正要用的那台机器安装好后，再去那边处理。</p>
              </div>
            </label>
          </div>
          
          {vscodeScenario === "current_machine" && !vscodeBundleDetected && (
            <div className="notice-banner warn" style={{ marginTop: '1.5rem' }}>
              还没检测到这台机器上的 VS Code 扩展安装。请先在这台机器上打开一次 VS Code，并确保 Codex 扩展已经安装，然后再回来继续。
            </div>
          )}
        </>
      )}
    </div>
  );
}
