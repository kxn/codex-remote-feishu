import type { RuntimeRequirementsDetectResponse } from "../../lib/types";
import { StatusBadge } from "../../components/ui";

function runtimeRequirementStatusTone(status: string): "neutral" | "good" | "warn" | "danger" {
  switch (status) {
    case "pass": return "good";
    case "warn": return "warn";
    case "fail": return "danger";
    default: return "neutral";
  }
}

function runtimeRequirementStatusLabel(status: string): string {
  switch (status) {
    case "pass": return "通过";
    case "warn": return "注意";
    case "fail": return "阻断";
    default: return "信息";
  }
}

export function StepEnvCheck(props: {
  runtimeRequirements: RuntimeRequirementsDetectResponse | null;
  runtimeRequirementsError: string;
}) {
  const { runtimeRequirements, runtimeRequirementsError } = props;

  if (runtimeRequirementsError) {
    return <div className="notice-banner warn">环境检查暂时不可用：{runtimeRequirementsError}</div>;
  }
  if (!runtimeRequirements) {
    return <div className="notice-banner warn">当前还没拿到环境检查结果，请先刷新状态后再继续。</div>;
  }

  const isReady = runtimeRequirements.ready;
  const isWarn = runtimeRequirements.checks.some((check) => check.status === "warn");
  const bannerTone = isReady ? (isWarn ? "warn" : "good") : "danger";

  return (
    <div className="wizard-step-layout">
      <div className={`notice-banner ${bannerTone}`} style={{ marginBottom: '1.5rem', fontWeight: 500 }}>
        {runtimeRequirements.summary}
      </div>
      <div className="manifest-block" style={{ marginBottom: '1.5rem', background: '#fff' }}>
        <h4>先确认这台机器能不能继续安装</h4>
        <p style={{ color: 'var(--text-soft)', marginTop: '0.4rem' }}>
          先看当前环境是否已经准备好。如果有明显依赖问题，会直接下发阻断项。
        </p>
      </div>
      <div className="checkbox-card-list">
        {runtimeRequirements.checks.map((check) => (
          <div key={check.id} className="checkbox-card" style={{ display: 'flex', flexDirection: 'row', alignItems: 'flex-start', padding: '1.2rem', border: '1px solid var(--border)', borderRadius: '1rem', background: '#fff' }}>
            <div style={{ marginRight: '1rem' }}>
              <StatusBadge value={runtimeRequirementStatusLabel(check.status)} tone={runtimeRequirementStatusTone(check.status)} />
            </div>
            <div>
              <strong style={{ display: 'block', marginBottom: '0.3rem', fontSize: '1rem' }}>{check.title}</strong>
              <p style={{ margin: 0, color: 'var(--text-soft)', fontSize: '0.9rem' }}>{check.summary}</p>
            </div>
          </div>
        ))}
      </div>
    </div>
  );
}
