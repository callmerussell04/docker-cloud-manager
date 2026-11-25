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

// Pills is a glass group of buttons of which one is chosen, for a toolbar's
// filters or tabs. The chosen pill is lifted; each pill is a toggle button,
// pressed while chosen.
export function Pills<T extends string>({ label, items, value, onChange, dense = false }: PillsProps<T>) {
  return (
    <Glass role="group" aria-label={label} className="inline-flex h-10 shrink-0 gap-0.5 rounded-full p-1">
      {items.map((item) => {
        const chosen = item.id === value;
        return (
          <button
            key={item.id}
            type="button"
            aria-pressed={chosen}
            onClick={() => onChange(item.id)}
            className={cn(
              'inline-flex h-8 items-center whitespace-nowrap rounded-full transition-colors',
              dense ? 'px-2.5' : 'px-3.5',
              chosen ? 'bg-lift font-semibold text-label shadow-[var(--lift-shadow)]' : 'font-medium text-label-2 hover:text-label',
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
