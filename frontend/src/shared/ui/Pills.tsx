import { useLayoutEffect, useRef, useState } from 'react';
import { cn } from '@/shared/lib/utils';
import { Glass } from './Glass';

export interface PillItem<T extends string> {
  id: T;
  label: string;
  // count follows the label, as how many rows a filter shows.
  count?: number;
}

interface PillsProps<T extends string> {
  // label names the group for assistive technology.
  label: string;
  items: readonly PillItem<T>[];
  value: T;
  onChange: (value: T) => void;
  // dense spaces the pills tighter, so a group of many fits a narrow
  // toolbar's row.
  dense?: boolean;
}

// The lift's place under the chosen pill, in the group's own pixels.
interface LiftBox {
  left: number;
  width: number;
}

// useSlidingLift measures the chosen pill so one lift can sit beneath it,
// and follows the pills as their widths change (a count grows, the language
// changes). The lift's CSS transition slides it to a new choice; a lift that
// first appears takes no transition, so it is placed without sliding in.
// The group has to stay mounted for the slide: a page that switches tabs
// keeps one group and changes its value.
function useSlidingLift(chosenIndex: number) {
  const groupRef = useRef<HTMLDivElement>(null);
  const [box, setBox] = useState<LiftBox | null>(null);

  useLayoutEffect(() => {
    const group = groupRef.current;
    if (!group) return;
    const measure = () => {
      const chosen = group.querySelectorAll<HTMLElement>('[data-pill]')[chosenIndex];
      setBox(chosen ? { left: chosen.offsetLeft, width: chosen.offsetWidth } : null);
    };
    measure();
    const observer = new ResizeObserver(measure);
    for (const pill of group.querySelectorAll('[data-pill]')) observer.observe(pill);
    return () => observer.disconnect();
  }, [chosenIndex]);

  return { groupRef, box };
}

// Pills is a glass group of buttons of which one is chosen, for a toolbar's
// filters or tabs. The chosen pill is lifted, and the lift slides to a new
// choice; each pill is a toggle button, pressed while chosen.
export function Pills<T extends string>({ label, items, value, onChange, dense = false }: PillsProps<T>) {
  const chosenIndex = items.findIndex((item) => item.id === value);
  const { groupRef, box } = useSlidingLift(chosenIndex);
  return (
    <Glass ref={groupRef} role="group" aria-label={label} className="inline-flex h-10 shrink-0 gap-0.5 rounded-full p-1">
      {box && (
        <span
          aria-hidden
          className="pointer-events-none absolute top-1 h-8 rounded-full bg-lift shadow-[var(--lift-shadow)] transition-[transform,width] duration-300 ease-[cubic-bezier(0.32,0.72,0,1)]"
          style={{ left: 0, width: box.width, transform: `translateX(${box.left}px)` }}
        />
      )}
      {items.map((item) => {
        const chosen = item.id === value;
        return (
          <button
            key={item.id}
            type="button"
            data-pill
            aria-pressed={chosen}
            onClick={() => onChange(item.id)}
            className={cn(
              'relative inline-flex h-8 items-center whitespace-nowrap rounded-full transition-colors',
              dense ? 'px-2.5' : 'px-3.5',
              chosen ? 'font-semibold text-label' : 'font-medium text-label-2 hover:text-label',
            )}
          >
            {item.label}
            {item.count !== undefined && <span className="ml-1.5 font-medium tabular-nums text-label-3">{item.count}</span>}
          </button>
        );
      })}
    </Glass>
  );
}
