import { useLayoutEffect, useMemo, useRef, useState, type CSSProperties } from 'react';
import { Link } from 'react-router-dom';
import { useT } from '@/shared/i18n';
import { cn, formatBytes } from '@/shared/lib/utils';
import { blockContent, memoryMapRects, type BlockContent, type MemoryHolder, type MemoryMapPart } from '../model';

// MapHolder is a container on the map: what it holds, its name, the page it
// opens, and its project's series colour (none for a standalone container).
export interface MapHolder extends MemoryHolder {
  name: string;
  to: string;
  series?: number;
}

// LegendEntry names a colour on the map: a project, or Standalone.
export interface LegendEntry {
  key: string;
  label: string;
  series?: number;
}


// The map's shape before it is measured, and the gap between blocks.
const initialSize = { width: 800, height: 400 };
const gap = 3;

const tint = (series?: number) => (series ? `var(--series-${series})` : 'var(--label-3)');

function useSize() {
  const ref = useRef<HTMLDivElement>(null);
  const [size, setSize] = useState(initialSize);
  useLayoutEffect(() => {
    const element = ref.current;
    if (!element) return;
    const measure = () => setSize({ width: element.clientWidth, height: element.clientHeight });
    measure();
    const observer = new ResizeObserver(measure);
    observer.observe(element);
    return () => observer.disconnect();
  }, []);
  return { ref, size };
}

// MemoryMapHead says how much of the memory quota is reserved and how much is
// free, beside the legend of the map's colours.
export function MemoryMapHead({ usedBytes, quotaBytes, legend, other }: { usedBytes: number; quotaBytes: number; legend: readonly LegendEntry[]; other: boolean }) {
  const t = useT();
  const free = Math.max(quotaBytes - usedBytes, 0);
  return (
    <div className="flex flex-wrap items-end justify-between gap-x-6 gap-y-3">
      <div className="min-w-0">
        <h2 className="text-[17px] font-semibold tracking-[-0.01em] text-label">
          {t('dashboard.memory.reserved', { used: formatBytes(usedBytes, 1), quota: formatBytes(quotaBytes, 1) })}
        </h2>
        <p className="text-label-2">{t('dashboard.memory.free', { free: formatBytes(free, 1) })}</p>
      </div>
      <ul className="flex flex-wrap gap-x-3.5 gap-y-1 text-xs text-label-2">
        {legend.map((entry) => (
          <li key={entry.key} className="inline-flex items-center gap-1.5">
            <span aria-hidden className="h-2.5 w-2.5 rounded-[3px]" style={{ background: tint(entry.series) }} />
            {entry.label}
          </li>
        ))}
        {other && (
          <li className="inline-flex items-center gap-1.5">
            <span aria-hidden className="h-2.5 w-2.5 rounded-[3px] bg-fill-hover" />
            {t('dashboard.memory.other')}
          </li>
        )}
        <li className="inline-flex items-center gap-1.5">
          <span aria-hidden className="memory-free h-2.5 w-2.5 rounded-[3px]" />
          {t('dashboard.memory.freeLegend')}
        </li>
      </ul>
    </div>
  );
}

// MemoryMap shows the memory quota as a treemap: a block per container that
// holds memory, sized by its reservation and coloured by its Compose project
// (standalone ones in gray), then what the map does not list, then the free
// part, hatched, in a region of its own (memoryMapRects keeps every block
// visible). A block opens its container; a block too small for its name
// shows its amount alone, and its tooltip and accessible name say both.
// The parts come from memoryMapParts.
export function MemoryMap({ parts }: { parts: readonly MemoryMapPart<MapHolder>[] }) {
  const t = useT();
  const { ref, size } = useSize();
  const rects = useMemo(
    () => memoryMapRects(parts.map((part) => part.bytes), parts.findIndex((part) => part.kind === 'free'), size.width, size.height),
    [parts, size],
  );

  return (
    <div ref={ref} role="group" aria-label={t('dashboard.memory.map')} className="relative h-[min(52vh,440px)] min-h-[240px]">
      {parts.map((part, index) => {
        const rect = rects[index];
        if (rect.width <= 0 || rect.height <= 0) return null;
        const place: CSSProperties = {
          left: rect.x + gap / 2,
          top: rect.y + gap / 2,
          width: Math.max(rect.width - gap, 0),
          height: Math.max(rect.height - gap, 0),
        };
        const amount = formatBytes(part.bytes, part.kind === 'holder' ? 0 : 1);
        // A block with room for text is padded around it. One too small for
        // any text has none, so it is drawn at its rectangle's size: padding
        // would make the browser draw it wider or taller than its rectangle,
        // over its neighbours (a sliver of memory became a 24 px block over
        // the free part).
        const blockClass = (content: BlockContent) =>
          cn('absolute flex flex-col justify-end gap-0.5 overflow-hidden rounded-[16px] text-left', content === 'none' ? 'p-0' : 'px-3 py-2.5');

        if (part.kind === 'holder') {
          const { holder } = part;
          const content = blockContent(rect.width, rect.height, holder.name);
          const blockLabel = t('dashboard.memory.block', { name: holder.name, amount });
          return (
            <Link
              key={holder.id}
              to={holder.to}
              aria-label={blockLabel}
              title={blockLabel}
              className={cn(blockClass(content), 'memory-block font-semibold text-on-accent')}
              style={{ ...place, ['--tint' as string]: tint(holder.series) }}
            >
              {content === 'name' && <span className="truncate">{holder.name}</span>}
              {content !== 'none' && <span className="truncate font-medium tabular-nums opacity-85">{amount}</span>}
            </Link>
          );
        }

        const label = part.kind === 'free' ? t('dashboard.memory.freeBlock', { amount }) : t('dashboard.memory.otherBlock', { amount });
        const content = blockContent(rect.width, rect.height, label);
        return (
          <div
            key={part.kind}
            title={label}
            className={cn(
              blockClass(content),
              'font-[550] text-label-2',
              part.kind === 'free' ? 'memory-free items-center justify-center' : 'bg-fill-hover',
            )}
            style={place}
          >
            {content !== 'none' && <span className="truncate tabular-nums">{content === 'name' ? label : amount}</span>}
          </div>
        );
      })}
    </div>
  );
}
