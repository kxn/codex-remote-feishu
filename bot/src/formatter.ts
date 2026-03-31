export const DEFAULT_FEISHU_MESSAGE_LIMIT = 30_000;

export interface FormatFeishuMessageOptions {
  sessionName?: string;
  content: string;
  codeBlock?: boolean;
  language?: string;
  maxLength?: number;
}

export function formatSessionTag(sessionName: string): string {
  return `[${sessionName}]`;
}

export function formatCodeBlock(content: string, language?: string): string {
  const languageTag = language ?? "";
  return `\`\`\`${languageTag}\n${content}\n\`\`\``;
}

export function formatFeishuMessageChunks(
  options: FormatFeishuMessageOptions,
): string[] {
  if (options.content.length === 0) {
    return [];
  }

  const maxLength = Math.max(1, options.maxLength ?? DEFAULT_FEISHU_MESSAGE_LIMIT);

  if (options.codeBlock) {
    return formatCodeBlockChunks(options, maxLength);
  }

  return formatPlainTextChunks(options, maxLength);
}

function formatPlainTextChunks(
  options: FormatFeishuMessageOptions,
  maxLength: number,
): string[] {
  const prefix = options.sessionName ? `${formatSessionTag(options.sessionName)} ` : "";
  const chunkBudget = Math.max(1, maxLength - prefix.length);

  return splitContent(options.content, chunkBudget).map((chunk) => `${prefix}${chunk}`);
}

function formatCodeBlockChunks(
  options: FormatFeishuMessageOptions,
  maxLength: number,
): string[] {
  const prefix = options.sessionName ? `${formatSessionTag(options.sessionName)}\n` : "";
  const language = options.language ?? "";
  const fencePrefix = `\`\`\`${language}\n`;
  const fenceSuffix = "\n```";
  const chunkBudget = Math.max(
    1,
    maxLength - prefix.length - fencePrefix.length - fenceSuffix.length,
  );

  return splitContent(options.content, chunkBudget).map(
    (chunk) => `${prefix}${fencePrefix}${chunk}${fenceSuffix}`,
  );
}

function splitContent(content: string, chunkBudget: number): string[] {
  const chunks: string[] = [];
  let cursor = 0;

  while (cursor < content.length) {
    const remaining = content.slice(cursor);
    if (remaining.length <= chunkBudget) {
      chunks.push(remaining);
      break;
    }

    const splitIndex = chooseSplitIndex(remaining, chunkBudget);
    chunks.push(remaining.slice(0, splitIndex));
    cursor += splitIndex;
  }

  return chunks;
}

function chooseSplitIndex(content: string, chunkBudget: number): number {
  if (content.length <= chunkBudget) {
    return content.length;
  }

  const searchWindow = content.slice(0, chunkBudget + 1);
  const newlineIndex = searchWindow.lastIndexOf("\n");
  if (newlineIndex > chunkBudget / 2) {
    return newlineIndex + 1;
  }

  const spaceIndex = Math.max(
    searchWindow.lastIndexOf(" "),
    searchWindow.lastIndexOf("\t"),
  );
  if (spaceIndex > chunkBudget / 2) {
    return spaceIndex + 1;
  }

  return chunkBudget;
}
