import type { PreviewDriveStatusResponse } from "../../lib/types";

export const newAppID = "__new__";

export type AppDraft = {
  isNew: boolean;
  id: string;
  name: string;
  appId: string;
  appSecret: string;
  enabled: boolean;
};

export type Notice = {
  tone: "good" | "warn" | "danger";
  message: string;
};

export type PreviewMap = Record<string, PreviewDriveStatusResponse>;

export type WizardRow = {
  label: string;
  done: boolean;
  timestamp?: string;
};
