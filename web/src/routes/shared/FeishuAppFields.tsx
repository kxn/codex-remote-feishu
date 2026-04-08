type SharedNotice = {
  tone: "good" | "warn" | "danger";
  message: string;
};

type FeishuAppFieldValues = {
  name: string;
  appId: string;
  appSecret: string;
};

type FeishuAppFieldsProps = {
  className?: string;
  notices?: SharedNotice[];
  values: FeishuAppFieldValues;
  readOnly?: boolean;
  hasSecret?: boolean;
  nameLabel: string;
  namePlaceholder: string;
  secretPlaceholderWithExisting?: string;
  secretPlaceholderWithoutExisting?: string;
  nameFieldClassName?: string;
  appIDFieldClassName?: string;
  appIDHintClassName?: string;
  secretFieldClassName?: string;
  onNameChange: (value: string) => void;
  onAppIDChange: (value: string) => void;
  onAppSecretChange: (value: string) => void;
};

export function FeishuAppFields({
  className,
  notices = [],
  values,
  readOnly = false,
  hasSecret = false,
  nameLabel,
  namePlaceholder,
  secretPlaceholderWithExisting = "留空表示保留当前 secret",
  secretPlaceholderWithoutExisting = "secret_xxx",
  nameFieldClassName = "field",
  appIDFieldClassName = "field",
  appIDHintClassName = "form-hint",
  secretFieldClassName = "field",
  onNameChange,
  onAppIDChange,
  onAppSecretChange,
}: FeishuAppFieldsProps) {
  return (
    <div className={className}>
      {notices.map((notice) => (
        <div key={`${notice.tone}:${notice.message}`} className={`notice-banner ${notice.tone}`}>
          {notice.message}
        </div>
      ))}
      <label className={nameFieldClassName}>
        <span>{nameLabel}</span>
        <input value={values.name} placeholder={namePlaceholder} disabled={readOnly} onChange={(event) => onNameChange(event.target.value)} />
      </label>
      <label className={appIDFieldClassName}>
        <span>App ID</span>
        <input value={values.appId} placeholder="cli_xxx" disabled={readOnly} onChange={(event) => onAppIDChange(event.target.value)} />
      </label>
      <p className={appIDHintClassName}>改成另一个 App ID 等于切换到另一个机器人身份，旧飞书会话不会自动迁移。</p>
      <label className={secretFieldClassName}>
        <span>App Secret</span>
        <input
          type="password"
          value={values.appSecret}
          placeholder={hasSecret ? secretPlaceholderWithExisting : secretPlaceholderWithoutExisting}
          disabled={readOnly}
          onChange={(event) => onAppSecretChange(event.target.value)}
        />
      </label>
    </div>
  );
}
