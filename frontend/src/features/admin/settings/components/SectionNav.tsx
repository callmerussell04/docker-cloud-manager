import { useEffect, useRef } from 'react';
import { cn } from '@/shared/lib/utils';

export interface SectionNavItem<Id extends string> {
  id: Id;
  title: string;
  /** What the count beside the title means: settings changed, refused, or found by a search. */
  mark?: { kind: 'changed' | 'invalid' | 'found'; count: number; label: string };
}

/**
 * The sections of the settings page as a plain list down the left of the
 * content, on the frost, one section shown at a time; on compact content a
 * strip of pills that scrolls sideways. Each entry counts its unsaved or
 * refused settings, or while searching its matches.
 */
export function SectionNav<Id extends string>({ items, active, onSelect, label, compact }: {
  items: Array<SectionNavItem<Id>>;
  /** The section shown; null while a search shows matches from every section. */
  active: Id | null;
  onSelect: (id: Id) => void;
  label: string;
  compact: boolean;
}) {
  const currentRef = useRef<HTMLButtonElement>(null);
  // The strip keeps the shown section in view, as one opened from a link.
  useEffect(() => {
    if (compact) currentRef.current?.scrollIntoView({ block: 'nearest', inline: 'nearest' });
  }, [compact, active]);

  return (
    <nav aria-label={label}>
      <ul className={cn(compact ? 'flex gap-1' : 'grid gap-px')}>
        {items.map((item) => {
          const current = item.id === active;
          return (
            <li key={item.id} className={cn(compact && 'shrink-0')}>
              <button
                ref={current ? currentRef : undefined}
                type="button"
                aria-current={current ? 'true' : undefined}
                onClick={() => onSelect(item.id)}
                className={cn(
                  'flex h-[34px] w-full items-center justify-between gap-2.5 whitespace-nowrap text-left transition-colors',
                  compact ? 'rounded-full px-3.5' : 'rounded-[10px] px-2.5',
                  current
                    ? 'bg-lift font-semibold text-label shadow-[var(--lift-shadow)]'
                    : 'font-medium text-label-2 hover:bg-fill hover:text-label',
                )}
              >
                <span className="truncate">{item.title}</span>
                {item.mark && <SectionCount {...item.mark} />}
              </button>
            </li>
          );
        })}
      </ul>
    </nav>
  );
}

function SectionCount({ kind, count, label }: NonNullable<SectionNavItem<string>['mark']>) {
  if (kind === 'found') {
    return <span aria-label={label} title={label} className="text-xs tabular-nums text-label-3">{count}</span>;
  }
  return (
    <span
      aria-label={label}
      title={label}
      className={cn(
        'grid h-[18px] min-w-[18px] shrink-0 place-items-center rounded-full px-[5px] text-[11px] font-bold tabular-nums text-on-accent',
        kind === 'invalid' ? 'bg-red' : 'bg-accent-fill',
      )}
    >
      {count}
    </span>
  );
}
