import type { AutostartDetectResponse } from "../../lib/types";

export function StepAutostart(props: {
  autostart: AutostartDetectResponse | null;
  autostartError: string;
  autostartSummary: string;
}) {
  const { autostart, autostartError, autostartSummary } = props;

  if (autostartError) {
    return <div className="notice-banner warn" style={{marginBottom: '1rem'}}>自动启动状态暂时不可用：{autostartError}</div>;
  }
  if (!autostart) {
    return <div className="notice-banner warn" style={{marginBottom: '1rem'}}>当前还没拿到自动启动检测结果，请先刷新状态后再继续。</div>;
  }

  if (!autostart.supported) {
    return (
      <div className="wizard-step-layout">
        <div className="manifest-block" style={{ background: '#fff' }}>
          <h4>当前平台暂不支持自动启动</h4>
          <p style={{ color: 'var(--text-soft)' }}>这一步先不需要处理。你仍然可以继续完成后面的安装和使用。</p>
        </div>
      </div>
    );
  }

  return (
    <div className="wizard-step-layout">
      <div className="manifest-block" style={{ marginBottom: '1.5rem', background: '#fff' }}>
        <h4>当前平台支持自动启动</h4>
        <p style={{ color: 'var(--text-soft)', marginTop: '0.4rem' }}>
          启用后，这台机器在你登录当前账号后会自动启动，不需要每次手动打开。
        </p>
      </div>

      <div className="manifest-block" style={{ background: '#fff' }}>
        <h4>当前状态</h4>
        <div style={{ padding: '0.8rem', background: 'var(--surface-muted)', borderRadius: '0.8rem', margin: '0.8rem 0' }}>
          <strong style={{ color: 'var(--accent-strong)' }}>{autostartSummary}</strong>
        </div>
        <ul className="wizard-bullet-list">
          <li style={{ color: 'var(--text-soft)' }}>这一步只处理当前登录用户的自动启动。</li>
          <li style={{ color: 'var(--text-soft)' }}>你也可以先跳过，后面回到管理页再启用。</li>
        </ul>
      </div>

      {autostart.warning ? <div className="notice-banner warn" style={{marginTop: '1rem'}}>自动启动检测提示：{autostart.warning}</div> : null}
      {autostart.lingerHint ? <div className="notice-banner neutral" style={{marginTop: '1rem'}}>{autostart.lingerHint}</div> : null}
    </div>
  );
}
