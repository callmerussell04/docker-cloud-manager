// Sparkline draws a series as a small filled line in the first chart series, scaled from
// zero to max. It draws nothing until there are two points to join, so a
// history that has just started shows no invented line.
export function Sparkline({ series, max }: { series: readonly number[]; max: number }) {
  if (series.length < 2 || max <= 0) return null;
  const points = series.map((value, index) => {
    const x = (index / (series.length - 1)) * 100;
    const y = 30 - (Math.min(Math.max(value, 0), max) / max) * 28;
    return `${x.toFixed(2)},${y.toFixed(2)}`;
  });
  return (
    <svg className="mt-2.5 block h-10 w-full" viewBox="0 0 100 30" preserveAspectRatio="none" aria-hidden>
      <path d={`M0,30 L${points.join(' L')} L100,30 Z`} fill="color-mix(in srgb, var(--series-1) 18%, transparent)" />
      <path d={`M${points.join(' L')}`} fill="none" stroke="var(--series-1)" strokeWidth={1.6} vectorEffect="non-scaling-stroke" />
    </svg>
  );
}
