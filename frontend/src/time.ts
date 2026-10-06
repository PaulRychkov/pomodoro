const pad = (n: number) => String(n).padStart(2, "0");

export function fmtClock(minutes: number): string {
  if (minutes === 1440) return "24:00";
  const m = ((Math.floor(minutes) % 1440) + 1440) % 1440;
  return `${pad(Math.floor(m / 60))}:${pad(m % 60)}`;
}

export function parseClock(text: string): number | null {
  const m = /^(\d{1,2}):(\d{2})/.exec(text);
  if (!m) return null;
  const h = Number(m[1]);
  const min = Number(m[2]);
  if (h > 23 || min > 59) return null;
  return h * 60 + min;
}
