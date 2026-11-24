import { useState, type KeyboardEvent, type PointerEvent } from 'react';
import { useElementWidth } from '@/shared/lib/hooks';
import { cn } from '@/shared/lib/utils';
import { guideShares, nearestPoint, niceScale, spreadLabels, stackBands, type ScaleKind } from './stackedArea';

export interface ChartSeries {
  id: string;
  // label names the series in the legend, beside its band and in the hover.
  label: string;
  // color is a CSS colour, a series token: var(--series-1).
  color: string;
  // values are the series' figures at each of the chart's times.
  values: readonly number[];
}

interface StackedAreaChartProps {
  // label is the chart's accessible name.
  label: string;
  // times are the points' times (Unix seconds), oldest first; from and to
  // are the ends of the time axis.
  times: readonly number[];
  from: number;
  to: number;
  // series are stacked in order, the first at the bottom.
  series: readonly ChartSeries[];
  format: (value: number) => string;
  // scale says what the figures are, so the scale's top is a round figure in
  // their unit; plain by default.
  scale?: ScaleKind;
  // formatTick writes the scale's figures; format is used when omitted.
  formatTick?: (value: number) => string;
  formatTime: (time: number) => string;
  // formatMoment writes the time under the pointer; formatTime when omitted.
  formatMoment?: (time: number) => string;
  height?: number;
  // endLabels names each band at the chart's right end.
  endLabels?: boolean;
  // totalLabel adds a total to the hover, for a stack of several series.
  totalLabel?: string;
  // legend names each series under the chart; a chart whose title names its
  // one series leaves it off.
  legend?: boolean;
  className?: string;
}

const left = 44;
const top = 8;
const bottom = 22;
const labelGap = 13;

// StackedAreaChart draws series stacked over time as solid bands parted by a
// fine line, with the scale on the left, each band's name at its right end
// (endLabels), a crosshair that
// lists every value at the time under the pointer (or chosen with the arrow
// keys while the chart has focus), and a legend. It takes the width of its
// container. Series colours are the series tokens; in light mode three of
// them are faint against the frost, so names, never colour alone, tell the
// bands apart.
export function StackedAreaChart({
  label,
  times,
  from,
  to,
  series,
  format,
  scale = 'plain',
  formatTick = format,
  formatTime,
  formatMoment = formatTime,
  height = 240,
  endLabels = false,
  totalLabel,
  legend = true,
  className,
}: StackedAreaChartProps) {
  const [measure, width] = useElementWidth<HTMLDivElement>(600);
  const [hover, setHover] = useState<number | null>(null);

  const labelRoom = endLabels ? Math.min(120, Math.max(72, width * 0.22)) : 10;
  const plotWidth = Math.max(width - left - labelRoom, 40);
  const plotHeight = Math.max(height - top - bottom, 20);
  const span = Math.max(to - from, 1);
  const x = (time: number) => left + ((time - from) / span) * plotWidth;
  const bands = stackBands(series.map((item) => item.values));
  const totals = times.map((_, index) => bands[bands.length - 1]?.upper[index] ?? 0);
  const ceiling = niceScale(Math.max(0, ...totals), scale);
  const y = (value: number) => top + (1 - value / ceiling) * plotHeight;
  // Four times along the axis: the ends and two between.
  const timeTicks = [0, 1 / 3, 2 / 3, 1].map((share) => from + share * span);
  const guides = guideShares(ceiling, scale, height >= 180).map((share) => share * ceiling);

  // A lone point is drawn a few pixels wide so it shows.
  const xs = times.map(x);
  const pad = times.length === 1 ? 3 : 0;
  const edge = (values: readonly number[]) =>
    xs.flatMap((at, index) => (pad ? [`${at - pad},${y(values[index])}`, `${at + pad},${y(values[index])}`] : [`${at},${y(values[index])}`]));
  const bandPath = (lower: readonly number[], upper: readonly number[]) =>
    xs.length === 0 ? '' : `M${edge(upper).join(' L')} L${edge(lower).reverse().join(' L')} Z`;

  const last = times.length - 1;
  // Labels, and the hover's rows, read top to bottom: the reverse of the
  // stack. Each band's label sits by the middle of its right end.
  const endLabelOrder = [...series.keys()].reverse();
  const labelY = endLabels && last >= 0
    ? spreadLabels(endLabelOrder.map((index) => y((bands[index].lower[last] + bands[index].upper[last]) / 2)), labelGap, top + 4, top + plotHeight)
    : [];

  const onPointerMove = (event: PointerEvent<SVGRectElement>) => {
    const box = event.currentTarget.getBoundingClientRect();
    const share = (event.clientX - box.left) / box.width;
    const index = nearestPoint(times, from + share * span);
    setHover(index >= 0 ? index : null);
  };
  const onKeyDown = (event: KeyboardEvent<HTMLDivElement>) => {
    if (times.length === 0) return;
    if (event.key === 'ArrowLeft' || event.key === 'ArrowRight') {
      event.preventDefault();
      const step = event.key === 'ArrowLeft' ? -1 : 1;
      setHover((current) => Math.min(last, Math.max(0, (current ?? last) + step)));
    } else if (event.key === 'Home' || event.key === 'End') {
      event.preventDefault();
      setHover(event.key === 'Home' ? 0 : last);
    } else if (event.key === 'Escape') {
      setHover(null);
    }
  };

  const tipLeft = hover !== null ? Math.min(Math.max(xs[hover] + 12, 0), Math.max(width - 210, 0)) : 0;
  const tipFlipped = hover !== null && xs[hover] + 12 + 200 > width;

  return (
    <div className={cn('min-w-0', className)}>
      <div
        ref={measure}
        role="group"
        aria-label={label}
        tabIndex={times.length > 0 ? 0 : undefined}
        onKeyDown={onKeyDown}
        onFocus={() => setHover((current) => current ?? (times.length > 0 ? last : null))}
        onBlur={() => setHover(null)}
        className="relative rounded-lg focus-visible:outline-offset-4"
      >
        <svg width={width} height={height} className="block overflow-visible" aria-hidden>
          {guides.map((value) => (
            <g key={value}>
              <line x1={left} x2={left + plotWidth} y1={y(value)} y2={y(value)} stroke="var(--separator)" strokeWidth={1} />
              <text x={left - 8} y={y(value) + 4} textAnchor="end" className="fill-label-2 text-[11px] tabular-nums">
                {value === 0 ? '0' : formatTick(value)}
              </text>
            </g>
          ))}
          {timeTicks.map((time, index) => (
            <text
              key={index}
              x={x(time)}
              y={height - 6}
              textAnchor={index === 0 ? 'start' : index === timeTicks.length - 1 ? 'end' : 'middle'}
              className="fill-label-2 text-[11px] tabular-nums"
            >
              {formatTime(time)}
            </text>
          ))}
          {/* Bands are parted by a line of the window's own colour. */}
          {bands.map((band, index) => (
            <path
              key={series[index].id}
              d={bandPath(band.lower, band.upper)}
              fill={series[index].color}
              fillOpacity={0.92}
              stroke="var(--solid-window)"
              strokeWidth={series.length > 1 ? 2 : 0}
              strokeLinejoin="round"
            />
          ))}
          {endLabels &&
            endLabelOrder.map((seriesIndex, order) => (
              <text key={series[seriesIndex].id} x={left + plotWidth + 8} y={labelY[order] + 4} className="fill-label text-[12px] font-semibold">
                {truncate(series[seriesIndex].label, Math.floor((labelRoom - 8) / 7))}
              </text>
            ))}
          {hover !== null && (
            <line x1={xs[hover]} x2={xs[hover]} y1={top} y2={top + plotHeight} stroke="var(--label)" strokeWidth={1} strokeDasharray="3 3" />
          )}
          <rect
            x={left}
            y={0}
            width={plotWidth}
            height={top + plotHeight}
            fill="transparent"
            onPointerMove={onPointerMove}
            onPointerLeave={() => setHover(null)}
          />
        </svg>
        {hover !== null && (
          <div
            aria-live="polite"
            className="pointer-events-none absolute top-2 z-[2] w-[200px] rounded-xl bg-glass-dense px-3 py-2.5 text-[12.5px] shadow-[var(--lift-shadow)] backdrop-blur-xl"
            style={{ left: tipFlipped ? Math.max(xs[hover] - 212, 0) : tipLeft }}
          >
            <div className="mb-1.5 font-semibold text-label-2">{formatMoment(times[hover])}</div>
            {endLabelOrder.map((index) => (
              <div key={series[index].id} className="flex items-center gap-2 py-0.5 text-label">
                <span aria-hidden className="h-2.5 w-2.5 shrink-0 rounded-[3px]" style={{ background: series[index].color }} />
                <span className="min-w-0 flex-1 truncate">{series[index].label}</span>
                <b className="font-semibold tabular-nums">{format(series[index].values[hover] ?? 0)}</b>
              </div>
            ))}
            {totalLabel && (
              <div className="mt-1 flex justify-between border-t border-separator pt-1.5 font-semibold text-label">
                <span>{totalLabel}</span>
                <span className="tabular-nums">{format(totals[hover])}</span>
              </div>
            )}
          </div>
        )}
      </div>
      {legend && (
        <ul className="mt-2 flex flex-wrap gap-x-4 gap-y-1.5 text-[12.5px] text-label-2" style={{ marginLeft: left }}>
          {series.map((item) => (
            <li key={item.id} className="inline-flex min-w-0 items-center gap-1.5">
              <span aria-hidden className="h-2.5 w-2.5 shrink-0 rounded-[3px]" style={{ background: item.color }} />
              <span className="truncate">{item.label}</span>
            </li>
          ))}
        </ul>
      )}
    </div>
  );
}

function truncate(text: string, length: number) {
  return text.length > length ? `${text.slice(0, Math.max(length - 1, 1))}…` : text;
}
