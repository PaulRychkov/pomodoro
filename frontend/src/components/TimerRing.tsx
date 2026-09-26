interface TimerRingProps {
  size: number;
  stroke: number;
  fraction: number;
  color: string;
  trackColor?: string;
  responsive?: boolean;
  children?: React.ReactNode;
}

export default function TimerRing({
  size,
  stroke,
  fraction,
  color,
  trackColor = "#E2E8F0",
  responsive = false,
  children,
}: TimerRingProps) {
  const radius = (size - stroke) / 2;
  const circumference = 2 * Math.PI * radius;
  const clamped = Math.min(1, Math.max(0, fraction));
  const style = responsive
    ? { width: "100%", maxWidth: size, aspectRatio: "1 / 1" }
    : { width: size, height: size };
  return (
    <div className="relative" style={style}>
      <svg width="100%" height="100%" viewBox={`0 0 ${size} ${size}`} className="-rotate-90">
        <circle cx={size / 2} cy={size / 2} r={radius} fill="none" stroke={trackColor} strokeWidth={stroke} />
        <circle
          cx={size / 2}
          cy={size / 2}
          r={radius}
          fill="none"
          stroke={color}
          strokeWidth={stroke}
          strokeLinecap="round"
          strokeDasharray={circumference}
          strokeDashoffset={circumference * (1 - clamped)}
          style={{ transition: "stroke-dashoffset 0.3s linear" }}
        />
      </svg>
      <div className="absolute inset-0 flex flex-col items-center justify-center">{children}</div>
    </div>
  );
}
