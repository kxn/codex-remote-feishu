import type {
  FeishuAppAutoConfigRequirementStatus,
  FeishuAppSummary,
} from "../../lib/types";
import {
  groupAutoConfigRequirements,
  type AutoConfigRequirementRow,
} from "./feishuAutoConfig";

type AutoConfigRequirementListProps = {
  title: string;
  requirements: FeishuAppAutoConfigRequirementStatus[];
  tone: "warn" | "danger";
  consoleLinks?: FeishuAppSummary["consoleLinks"];
  onCopy: (row: AutoConfigRequirementRow) => void;
};

export function AutoConfigRequirementList({
  title,
  requirements,
  tone,
  consoleLinks,
  onCopy,
}: AutoConfigRequirementListProps) {
  if (requirements.length === 0) {
    return null;
  }
  const rows = groupAutoConfigRequirements(requirements);
  return (
    <div className="req-group">
      <div className={`req-group-title ${tone}`}>{title}</div>
      <ul className="requirement-list">
        {rows.map((item) => {
          const consoleURL = autoConfigRequirementConsoleURL(item, consoleLinks);
          return (
            <li key={item.key} className="requirement-row">
              <div className="requirement-main">
                <span className={`badge ${tone === "danger" ? "danger" : "warn"}`}>
                  {item.meta}
                </span>
                <div className="requirement-name-row">
                  <strong className="mono">{item.label}</strong>
                  <button
                    className="ghost-button"
                    type="button"
                    aria-label={`复制${item.label}`}
                    title={`复制${item.label}`}
                    onClick={() => onCopy(item)}
                  >
                    <span aria-hidden="true">⧉</span>
                  </button>
                </div>
              </div>
              <div className="requirement-impact">
                {item.impacts.length > 0 ? item.impacts.join("、") : "基础配置"}
              </div>
              <div className="requirement-action">
                {consoleURL ? (
                  <a
                    className="inline-link"
                    href={consoleURL}
                    rel="noreferrer"
                    target="_blank"
                  >
                    去后台配置
                  </a>
                ) : null}
              </div>
            </li>
          );
        })}
      </ul>
    </div>
  );
}

function autoConfigRequirementConsoleURL(
  row: AutoConfigRequirementRow,
  consoleLinks?: FeishuAppSummary["consoleLinks"],
): string {
  if (!consoleLinks) {
    return "";
  }
  switch (row.kind) {
    case "scope":
      return consoleLinks.auth || "";
    case "event":
      return consoleLinks.events || "";
    case "callback":
      return consoleLinks.callback || "";
    default:
      return consoleLinks.bot || "";
  }
}
