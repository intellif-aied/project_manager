export const BUSINESS_TIME_ZONE = "Asia/Shanghai";

type DateValue = string | number | Date | null | undefined;

function parseDate(value: DateValue) {
  if (value === null || value === undefined || value === "") return null;
  const parsed = value instanceof Date ? new Date(value.getTime()) : new Date(value);
  return Number.isNaN(parsed.getTime()) ? null : parsed;
}

function businessParts(value: DateValue, includeTime: boolean) {
  const parsed = parseDate(value);
  if (!parsed) return null;
  const formatter = new Intl.DateTimeFormat("en-US", {
    timeZone: BUSINESS_TIME_ZONE,
    year: "numeric",
    month: "2-digit",
    day: "2-digit",
    ...(includeTime
      ? { hour: "2-digit", minute: "2-digit", second: "2-digit", hourCycle: "h23" as const }
      : {})
  });
  return Object.fromEntries(
    formatter
      .formatToParts(parsed)
      .filter((part) => part.type !== "literal")
      .map((part) => [part.type, part.value])
  );
}

export function businessDateKey(value: DateValue = new Date()) {
  const parts = businessParts(value, false);
  if (!parts) return "-";
  return `${parts.year}-${parts.month}-${parts.day}`;
}

export function formatBusinessDateTime(value: DateValue) {
  const parts = businessParts(value, true);
  if (!parts) return "-";
  return `${parts.year}-${parts.month}-${parts.day} ${parts.hour}:${parts.minute}`;
}

export function formatBusinessMonthDayTime(value: DateValue) {
  const parts = businessParts(value, true);
  if (!parts) return "-";
  return `${parts.month}-${parts.day} ${parts.hour}:${parts.minute}`;
}
