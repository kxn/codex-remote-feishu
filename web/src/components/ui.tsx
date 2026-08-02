import {
  type AnchorHTMLAttributes,
  type ButtonHTMLAttributes,
  type PropsWithChildren,
  type ReactNode,
} from "react";
import { BrandLogo } from "./BrandLogo";

export type NoticeTone = "good" | "warn" | "danger";
export type ButtonTone = "primary" | "secondary" | "ghost" | "danger";
export type StatusTone = NoticeTone | "idle" | "neutral";

export function BrandLockup(props: { subtitle: string; compact?: boolean }) {
  return (
    <div className={props.compact ? "brand brand-compact" : "brand"}>
      <BrandLogo className="brand-mark" />
      <div>
        <div className="brand-name">Codex Remote</div>
        <div className="brand-sub">{props.subtitle}</div>
      </div>
    </div>
  );
}

export function AppButton(
  props: ButtonHTMLAttributes<HTMLButtonElement> & {
    tone?: ButtonTone;
  },
) {
  const { className, tone = "secondary", ...rest } = props;
  return <button className={joinClassNames("btn", `btn-${tone}`, className)} {...rest} />;
}

export function AppLink(
  props: AnchorHTMLAttributes<HTMLAnchorElement> & {
    tone?: Exclude<ButtonTone, "danger">;
  },
) {
  const { className, tone = "ghost", ...rest } = props;
  return <a className={joinClassNames("btn", `btn-${tone}`, className)} {...rest} />;
}

export function Card(
  props: PropsWithChildren<{
    title?: string;
    description?: string;
    className?: string;
    actions?: ReactNode;
  }>,
) {
  const { actions, children, className, description, title } = props;
  return (
    <section className={joinClassNames("card", className)}>
      {title || description || actions ? (
        <div className="card-head">
          <div>
            {title ? <h3>{title}</h3> : null}
            {description ? <p className="card-sub">{description}</p> : null}
          </div>
          {actions ? <div className="card-actions">{actions}</div> : null}
        </div>
      ) : null}
      {children}
    </section>
  );
}

export function Notice(
  props: PropsWithChildren<{
    tone: NoticeTone;
    title?: string;
    className?: string;
  }>,
) {
  return (
    <div className={joinClassNames("notice", props.tone, props.className)}>
      {props.title ? <strong>{props.title}</strong> : null}
      {props.children}
    </div>
  );
}

export function Toast(props: { tone: NoticeTone; message: string }) {
  return (
    <div className={`toast ${props.tone}`} role="status">
      {props.message}
    </div>
  );
}

export function StatusDot(props: { tone?: StatusTone }) {
  return <span className={`dot ${props.tone ?? "idle"}`} aria-hidden="true" />;
}

export function Badge(props: PropsWithChildren<{ tone?: NoticeTone | "neutral" }>) {
  return <span className={`badge ${props.tone ?? "neutral"}`}>{props.children}</span>;
}

export function LoadingLine(props: { children: ReactNode }) {
  return (
    <div className="loading-line">
      <span className="spinner" />
      {props.children}
    </div>
  );
}

export function EmptyState(
  props: PropsWithChildren<{
    tone?: "default" | "error";
    title?: string;
  }>,
) {
  return (
    <div className={joinClassNames("empty-state", props.tone === "error" ? "error" : "")}>
      {props.title ? <strong>{props.title}</strong> : null}
      {props.children}
    </div>
  );
}

export function ConfirmModal(
  props: PropsWithChildren<{
    open: boolean;
    title: string;
    description: string;
    confirmLabel: string;
    confirmTone?: "primary" | "danger";
    confirmDisabled?: boolean;
    onCancel: () => void;
    onConfirm: () => void;
  }>,
) {
  if (!props.open) {
    return null;
  }
  const titleID = props.title.replace(/\s+/g, "-").toLowerCase();
  return (
    <div className="modal-backdrop" role="presentation">
      <div className="modal-card" role="dialog" aria-modal="true" aria-labelledby={titleID}>
        <h3 id={titleID}>{props.title}</h3>
        <p className="modal-copy">{props.description}</p>
        {props.children}
        <div className="modal-actions">
          <AppButton type="button" tone="secondary" onClick={props.onCancel}>
            取消
          </AppButton>
          <AppButton
            type="button"
            tone={props.confirmTone === "danger" ? "danger" : "primary"}
            disabled={props.confirmDisabled}
            onClick={props.onConfirm}
          >
            {props.confirmLabel}
          </AppButton>
        </div>
      </div>
    </div>
  );
}

function joinClassNames(...values: Array<string | undefined | false>): string {
  return values.filter(Boolean).join(" ");
}
