interface DayProgressProps {
  blocks: number[];
  completed: number;
  focusActive: boolean;
}

export default function DayProgress({ blocks, completed, focusActive }: DayProgressProps) {
  let offset = 0;
  return (
    <div className="flex flex-wrap items-center justify-center gap-x-5 gap-y-2 px-1">
      {blocks.map((size, blockIdx) => {
        const start = offset;
        offset += size;
        return (
          <div key={blockIdx} className="flex items-center gap-1.5 sm:gap-2">
            {Array.from({ length: size }, (_, i) => {
              const idx = start + i;
              const done = idx < completed;
              const current = focusActive && idx === completed;
              return (
                <div
                  key={i}
                  className={
                    done
                      ? "h-3.5 w-3.5 rounded-full bg-primary"
                      : current
                        ? "h-3.5 w-3.5 rounded-full border-2 border-primary bg-primary/30 animate-pulse"
                        : "h-3.5 w-3.5 rounded-full bg-primary/20"
                  }
                />
              );
            })}
          </div>
        );
      })}
    </div>
  );
}
