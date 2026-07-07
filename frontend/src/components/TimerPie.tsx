interface TimerPieProps {
  size: number;
  fraction: number;
  color: string;
}

export default function TimerPie({ size, fraction, color }: TimerPieProps) {
  const clamped = Math.min(1, Math.max(0, fraction));
  const c = size / 2;
  const r = c - 2;
  const angle = clamped * 2 * Math.PI;
  const endX = c + r * Math.sin(angle);
  const endY = c - r * Math.cos(angle);
  const largeArc = clamped > 0.5 ? 1 : 0;
  return (
    <svg width={size} height={size}>
      <circle cx={c} cy={c} r={r} fill={color} opacity={0.16} />
      {clamped >= 0.999 ? (
        <circle cx={c} cy={c} r={r} fill={color} />
      ) : clamped > 0.001 ? (
        <path d={`M ${c} ${c} L ${c} ${c - r} A ${r} ${r} 0 ${largeArc} 1 ${endX} ${endY} Z`} fill={color} />
      ) : null}
    </svg>
  );
}
