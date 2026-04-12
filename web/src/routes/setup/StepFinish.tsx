import type { FeishuAppSummary } from "../../lib/types";

export function StepFinish(props: {
  activeApp: FeishuAppSummary | null;
  autostartSummary: string;
  vscodeSummary: string;
}) {
  const { activeApp, autostartSummary, vscodeSummary } = props;

  return (
    <div className="wizard-step-layout">
      <div className="manifest-block" style={{ marginBottom: '1.5rem', background: '#fff' }}>
        <h4>已经可以开始第一次对话了</h4>
        <ul className="wizard-bullet-list">
          <li>推荐先在飞书里打开这次刚处理好的飞书应用。</li>
          <li>先给它发一条测试消息，确认单聊和按钮交互都已经正常。</li>
          <li>如果你的工作台已经能看到该应用，也可以直接从工作台进入。</li>
        </ul>
      </div>
      <div className="wizard-summary-grid">
        <div className="wizard-summary-card">
          <strong style={{ color: 'var(--text-soft)', fontSize: '0.9rem' }}>当前飞书应用</strong>
          <p style={{ marginTop: '0.4rem', fontSize: '1.1rem', fontWeight: 500 }}>{activeApp?.name || activeApp?.appId || "未命名应用"}</p>
        </div>
        <div className="wizard-summary-card">
          <strong style={{ color: 'var(--text-soft)', fontSize: '0.9rem' }}>基础对话与交互</strong>
          <p style={{ marginTop: '0.4rem', fontSize: '1.1rem', fontWeight: 500, color: 'var(--accent-strong)' }}>已经完成，可以开始正常对话。</p>
        </div>
        <div className="wizard-summary-card">
          <strong style={{ color: 'var(--text-soft)', fontSize: '0.9rem' }}>自动启动</strong>
          <p style={{ marginTop: '0.4rem', fontSize: '1.1rem', fontWeight: 500 }}>{autostartSummary}</p>
        </div>
        <div className="wizard-summary-card">
          <strong style={{ color: 'var(--text-soft)', fontSize: '0.9rem' }}>VS Code</strong>
          <p style={{ marginTop: '0.4rem', fontSize: '1.1rem', fontWeight: 500 }}>{vscodeSummary}</p>
        </div>
      </div>
    </div>
  );
}
