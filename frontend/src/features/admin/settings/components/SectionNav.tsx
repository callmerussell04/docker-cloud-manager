import { Select } from '@/shared/ui/Select';
import type { TFunction } from '@/shared/i18n';
import { cn } from '@/shared/lib/utils';

export interface SectionNavItem<Id extends string> {
  id: Id;
  title: string;
  /** Settings of the section with unsaved changes. */
  changed: number;
  /** Settings of the section whose value was refused. */
  invalid: number;
}

/**
 * The sections of the settings page: a list beside the section on a wide
 * screen and a selector above it on a narrow one. Each entry shows how many of
 * its settings are changed or refused, so nothing unsaved is out of sight.
 */
export function SectionNav<Id extends string>({ items, active, onSelect, t }: {
  items: Array<SectionNavItem<Id>>;
  active: Id | null;
  onSelect: (id: Id) => void;
  t: TFunction;
}) {
  const status = (item: SectionNavItem<Id>) => {
    if (item.invalid) return t('admin.settings.sectionInvalid', { count: item.invalid });
    if (item.changed) return t('admin.settings.sectionChanged', { count: item.changed });
    return '';
  };

  return (
    <nav aria-label={t('admin.settings.sectionsLabel')}>
      <Select
        className="lg:hidden"
        aria-label={t('admin.settings.sectionsLabel')}
        value={active ?? ''}
        onChange={(event) => onSelect(event.target.value as Id)}
      >
        {active === null && <option value="" />}
        {items.map((item) => (
          <option key={item.id} value={item.id}>
            {status(item) ? `${item.title} (${status(item)})` : item.title}
          </option>
        ))}
      </Select>
      <ul className="hidden space-y-1 lg:block">
        {items.map((item) => (
          <li key={item.id}>
            <button
              type="button"
              aria-current={item.id === active ? 'page' : undefined}
              onClick={() => onSelect(item.id)}
              className={cn(
                'flex w-full items-center justify-between gap-2 rounded-xl px-3 py-2 text-left text-sm font-medium transition-colors focus:outline-none focus-visible:ring-2 focus-visible:ring-accent-fill',
                item.id === active
                  ? 'bg-accent-fill text-on-accent shadow'
                  : 'text-slate-700 hover:bg-white/60 dark:text-slate-300 dark:hover:bg-slate-800/60',
              )}
            >
              <span className="truncate">{item.title}</span>
              {(item.invalid > 0 || item.changed > 0) && (
                <span
                  title={status(item)}
                  aria-label={status(item)}
                  className={cn(
                    'inline-flex h-5 min-w-5 shrink-0 items-center justify-center rounded-full px-1.5 text-xs font-semibold',
                    item.invalid
                      ? 'bg-red-600 text-white'
                      : item.id === active
                        ? 'bg-on-accent text-accent'
                        : 'bg-fill text-label-2',
                  )}
                >
                  {item.invalid || item.changed}
                </span>
              )}
            </button>
          </li>
        ))}
      </ul>
    </nav>
  );
}
