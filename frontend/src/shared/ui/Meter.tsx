import { cn } from '@/shared/lib/utils';
import { meterParts } from './meter';

export interface MeterPart {
  value: number;
  // label names the part in the legend, with its amount.
  label: string;
  // className fills the part's segment and its legend swatch.
  className: string;
}

interface MeterProps {
  parts: readonly MeterPart[];
  total: number;
  // freeLabel names the empty track in the legend; omitted, it is unnamed.
  freeLabel?: string;
  className?: string;
}

// Meter is a capacity bar: parts of one whole in order with a 2 px gap
// between them, the free part as the empty track, and a legend naming every
// part. The legend carries the meaning, so the bar is hidden from assistive
// technology.
export function Meter({ parts, total, freeLabel, className }: MeterProps) {
  const laid = meterParts(parts, total);
  const shown = laid.parts.filter((part) => part.percent > 0);
  return (
    <div className={cn('grid gap-2', className)}>
      <div aria-hidden className="flex h-2 gap-[2px] overflow-hidden rounded-full">
        {shown.map((part, index) => (
          <span key={index} className={cn('h-full rounded-[1px] first:rounded-l-full last:rounded-r-full', part.className)} style={{ width: `${part.percent}%` }} />
        ))}
        {laid.freePercent > 0 && <span className="h-full min-w-0 flex-1 rounded-r-full bg-fill first:rounded-l-full" />}
      </div>
      <ul className="flex flex-wrap gap-x-4 gap-y-1 text-xs text-label-2">
        {parts.map((part, index) => (
          <li key={index} className="inline-flex items-center gap-1.5">
            <span aria-hidden className={cn('h-2 w-2 rounded-full', part.className)} />
            {part.label}
          </li>
        ))}
        {freeLabel && (
          <li className="inline-flex items-center gap-1.5">
            <span aria-hidden className="h-2 w-2 rounded-full bg-fill-hover" />
            {freeLabel}
          </li>
        )}
      </ul>
    </div>
  );
}
