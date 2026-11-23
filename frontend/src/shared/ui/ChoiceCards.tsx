import { useRef, type KeyboardEvent } from 'react';
import type { LucideIcon } from 'lucide-react';
import { cn } from '@/shared/lib/utils';

export interface ChoiceCard<T extends string> {
  id: T;
  icon: LucideIcon;
  title: string;
  // description says what choosing it means, before it is chosen.
  description: string;
}

interface ChoiceCardsProps<T extends string> {
  // label names the choice for assistive technology.
  label: string;
  items: readonly ChoiceCard<T>[];
  value: T;
  onChange: (value: T) => void;
}

const nextKeys = new Set(['ArrowRight', 'ArrowDown']);
const previousKeys = new Set(['ArrowLeft', 'ArrowUp']);

// ChoiceCards is one choice among a few cards, each with an icon, a title and
// a line explaining its consequence: a radio group with one Tab stop, the
// chosen card, arrow keys that move and choose, and Enter that submits the
// form around it.
export function ChoiceCards<T extends string>({ label, items, value, onChange }: ChoiceCardsProps<T>) {
  const refs = useRef<(HTMLButtonElement | null)[]>([]);

  const move = (event: KeyboardEvent<HTMLButtonElement>, index: number) => {
    // Enter submits the form around the cards, as on a native radio button.
    if (event.key === 'Enter' && event.currentTarget.form) {
      event.preventDefault();
      event.currentTarget.form.requestSubmit();
      return;
    }
    const step = nextKeys.has(event.key) ? 1 : previousKeys.has(event.key) ? -1 : 0;
    if (!step) return;
    event.preventDefault();
    const next = (index + step + items.length) % items.length;
    onChange(items[next].id);
    refs.current[next]?.focus();
  };

  return (
    <div role="radiogroup" aria-label={label} className="grid gap-2.5 sm:grid-cols-2">
      {items.map((item, index) => {
        const chosen = item.id === value;
        const Icon = item.icon;
        return (
          <button
            key={item.id}
            ref={(element) => {
              refs.current[index] = element;
            }}
            type="button"
            role="radio"
            aria-checked={chosen}
            tabIndex={chosen ? 0 : -1}
            onClick={() => onChange(item.id)}
            onKeyDown={(event) => move(event, index)}
            className={cn(
              'flex flex-col items-start gap-1 rounded-2xl border p-4 text-left transition-colors',
              chosen ? 'border-accent-fill bg-lift shadow-[var(--lift-shadow)]' : 'border-separator hover:bg-fill',
            )}
          >
            <Icon aria-hidden className={cn('mb-1 h-[18px] w-[18px]', chosen ? 'text-accent' : 'text-label-2')} />
            <span className="font-semibold text-label">{item.title}</span>
            <span className="text-xs leading-snug text-label-2">{item.description}</span>
          </button>
        );
      })}
    </div>
  );
}
