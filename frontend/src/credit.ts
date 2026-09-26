const steps = [0, 3, 4, 6, 8, 9, 12];

const labels: Record<number, string> = {
  0: "0",
  3: "¼",
  4: "⅓",
  6: "½",
  8: "⅔",
  9: "¾",
  12: "1",
};

export function creditTwelfths(focusSeconds: number, plannedSeconds: number): number {
  if (plannedSeconds <= 0 || focusSeconds <= 0) return 0;
  if (focusSeconds >= plannedSeconds) return 12;
  let best = 0;
  let bestDist = Infinity;
  for (const step of steps) {
    const dist = Math.abs(focusSeconds * 12 - step * plannedSeconds);
    if (dist <= bestDist) {
      best = step;
      bestDist = dist;
    }
  }
  return best;
}

export function creditLabel(twelfths: number): string {
  return labels[twelfths] ?? `${twelfths}/12`;
}

export function formatCreditSum(value: number): string {
  const rounded = Math.round(value * 100) / 100;
  return rounded.toLocaleString("ru-RU", { maximumFractionDigits: 2 });
}

export function formatDuration(seconds: number): string {
  const total = Math.max(0, Math.round(seconds));
  const m = Math.floor(total / 60);
  const s = total % 60;
  return `${m}:${String(s).padStart(2, "0")}`;
}
