export const time = (value?: string | null) =>
  value
    ? new Intl.DateTimeFormat("zh-CN", {
        timeZone: "Asia/Shanghai",
        hour: "2-digit",
        minute: "2-digit",
        second: "2-digit",
        hour12: false,
      }).format(new Date(value))
    : "—";
export const dateTime = (value?: string | null) =>
  value
    ? new Intl.DateTimeFormat("zh-CN", {
        timeZone: "Asia/Shanghai",
        month: "2-digit",
        day: "2-digit",
        hour: "2-digit",
        minute: "2-digit",
        hour12: false,
      }).format(new Date(value))
    : "—";
export const bytes = (value: number) =>
  value >= 1073741824
    ? `${(value / 1073741824).toFixed(2)} GiB`
    : value >= 1048576
      ? `${(value / 1048576).toFixed(1)} MiB`
      : value >= 1024
        ? `${(value / 1024).toFixed(1)} KiB`
        : `${value} B`;
export const channelOrder = new Intl.Collator("en", { numeric: true });
