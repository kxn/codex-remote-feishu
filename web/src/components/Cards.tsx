import type { ReactNode } from "react";
import { StatusBadge } from "./ui";

export function ActionCard(props: {
  selected?: boolean;
  title: string;
  description: string;
  onClick?: () => void;
  children?: ReactNode;
}) {
  return (
    <label className={`choice-card${props.selected ? " selected" : ""}`} onClick={props.onClick}>
      {props.children}
      <div>
        <strong>{props.title}</strong>
        <p>{props.description}</p>
      </div>
    </label>
  );
}

export function CapabilityCard(props: {
  title: string;
  description: string;
  status: "passed" | "pending" | "deferred";
  actionNode?: ReactNode;
}) {
  const tone = props.status === "passed" ? "good" : props.status === "pending" ? "danger" : "neutral";
  const label = props.status === "passed" ? "已通过" : props.status === "pending" ? "未处理" : "已跳过";
  return (
    <div className="info-card capability-card" style={{ display: 'flex', flexDirection: 'row', justifyContent: 'space-between', alignItems: 'center' }}>
      <div>
        <strong>{props.title}</strong>
        <p>{props.description}</p>
        <div style={{ marginTop: '0.5rem' }}>
           <StatusBadge value={label} tone={tone} />
        </div>
      </div>
      <div>
        {props.actionNode}
      </div>
    </div>
  );
}
