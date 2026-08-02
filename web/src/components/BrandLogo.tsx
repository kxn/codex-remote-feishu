import { relativeLocalPath } from "../lib/paths";

export function BrandLogo(props: { className?: string }) {
  const { className } = props;
  return (
    <img
      className={className}
      src={relativeLocalPath("/branding/codex-remote-logo.svg")}
      alt=""
      aria-hidden="true"
    />
  );
}
