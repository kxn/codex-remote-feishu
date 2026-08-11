export type ProfileListItem = {
  id: string;
};

export function appendOrReplaceProfileItem<T extends ProfileListItem>(
  items: T[],
  item: T,
  previousID = item.id,
): T[] {
  const nextItems = items
    .filter((current) => current.id !== previousID || current.id === item.id)
    .map((current) => (current.id === item.id ? item : current));
  if (nextItems.some((current) => current.id === item.id)) {
    return nextItems;
  }
  return [...nextItems, item];
}

export function removeProfileItem<T extends ProfileListItem>(items: T[], targetID: string): T[] {
  return items.filter((item) => item.id !== targetID);
}

export function requiredProfileFieldMessage(value: string, label: string): string {
  const normalizedLabel = label.trim();
  const separator = /^[A-Za-z0-9]/.test(normalizedLabel) ? " " : "";
  return value.trim() ? "" : `请填写${separator}${normalizedLabel}。`;
}

export function maxProfileTextLengthMessage(value: string, maxChars: number, label: string): string {
  return value.length > maxChars ? `${label}最多 ${maxChars} 字符。` : "";
}
