import { cn } from '@/shared/lib/utils';

interface SparklineProps {
  series: readonly number[];
  // max is the top of the scale; the bottom is zero.
  max: number;
  // slots is how many points the full width holds. Given, the series is
  // laid from the right, so the newest point stays at the right edge while
  // the history fills; omitted, the series spans the width.
  slots?: number;
  // guides draws quarter lines and a baseline, shown even before there is a
  // line to draw.
  guides?: boolean;
  // color is the line's colour, a series token; the first series by default.
  color?: string;
  className?: string;
}

// The plot's height in viewBox units, and the room kept above it so the
// line's stroke is not cut at the top.
const height = 30;
const headroom = 2;
const quarters = [0.25, 0.5, 0.75];

const yOf = (share: number) => height - share * (height - headroom);

// Sparkline draws a series as a small filled line in a chart series (the
// first unless given), scaled from zero to max. It draws no line until there are two points to
// join, so a history that has just started shows no invented line.
export function Sparkline({ series, max, slots, guides = false, color = 'var(--series-1)', className }: SparklineProps) {
  const drawn = series.length >= 2 && max > 0;
  if (!drawn && !guides) return null;
  const span = Math.max(slots ?? series.length, series.length, 2) - 1;
  const step = 100 / span;
  const start = 100 - (series.length - 1) * step;
  const points = series.map((value, index) => {
    const x = start + index * step;
    const y = yOf(Math.min(Math.max(value, 0), max) / max);
    return `${x.toFixed(2)},${y.toFixed(2)}`;
  });
  return (
    <svg className={cn('mt-2.5 block h-10 w-full', className)} viewBox={`0 0 100 ${height}`} preserveAspectRatio="none" aria-hidden>
      {guides &&
        [...quarters, 0].map((share) => (
          <line key={share} x1="0" x2="100" y1={yOf(share)} y2={yOf(share)} stroke="var(--separator)" strokeWidth={1} vectorEffect="non-scaling-stroke" />
        ))}
      {drawn && (
        <>
          <path d={`M${start.toFixed(2)},${height} L${points.join(' L')} L100,${height} Z`} fill={`color-mix(in srgb, ${color} 18%, transparent)`} />
          <path d={`M${points.join(' L')}`} fill="none" stroke={color} strokeWidth={1.6} strokeLinejoin="round" vectorEffect="non-scaling-stroke" />
        </>
      )}
    </svg>
  );
}
