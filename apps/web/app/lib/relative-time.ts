const relative = new Intl.RelativeTimeFormat("zh-CN", { numeric: "auto" });

export function formatRelative(value: string): string {
  const date = new Date(value);
  if (Number.isNaN(date.valueOf())) return value;
  const seconds = Math.round((date.valueOf() - Date.now()) / 1000);
  const minutes = Math.round(seconds / 60);
  if (Math.abs(minutes) < 60) return relative.format(minutes, "minute");
  const hours = Math.round(minutes / 60);
  if (Math.abs(hours) < 24) return relative.format(hours, "hour");
  const days = Math.round(hours / 24);
  if (Math.abs(days) < 30) return relative.format(days, "day");
  const months = Math.round(days / 30);
  if (Math.abs(months) < 12) return relative.format(months, "month");
  return relative.format(Math.round(months / 12), "year");
}
