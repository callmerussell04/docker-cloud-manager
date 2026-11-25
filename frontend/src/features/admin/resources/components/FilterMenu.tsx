import { ChevronDown, type LucideIcon } from 'lucide-react';
import { cn } from '@/shared/lib/utils';
import { Glass } from '@/shared/ui/Glass';
import { Menu, MenuGroup, MenuItem } from '@/shared/ui/Menu';

export interface FilterOption<V extends string> {
  id: V;
  label: string;
  // count follows the label in the menu, as how many rows the choice shows.
  count?: number;
}

// FilterAlert is a count the capsule calls out while the filter shows
// everything, such as the containers that need attention; label is its
// accessible name.
export interface FilterAlert {
  count: number;
  label: string;
}

interface FilterMenuProps<V extends string> {
  // label names the filter: the menu's name and the capsule's accessible
  // name.
  label: string;
  icon: LucideIcon;
  value: V;
  // chosen is the capsule's text for the value; count follows it.
  chosen: string;
  count?: number;
  options: readonly FilterOption<V>[];
  onChange: (value: V) => void;
  // narrowed marks a choice other than everything, in the accent, on the
  // round button.
  narrowed: boolean;
  alert?: FilterAlert;
  // round shows the icon alone, as a round button for a narrow toolbar's
  // top row; the chosen value is then its accessible name and tooltip.
  round?: boolean;
}

// FilterMenu is a toolbar glass capsule that narrows a list to one choice:
// it names the choice (and its count) and opens a menu of choices, the
// chosen one marked, each with its count.
export function FilterMenu<V extends string>({ label, icon: Icon, value, chosen, count, options, onChange, narrowed, alert, round = false }: FilterMenuProps<V>) {
  const alerting = alert && alert.count > 0 ? alert : undefined;
  const trigger = round ? (
    <span className="relative flex">
      <Icon aria-hidden className={cn('h-[17px] w-[17px]', narrowed ? 'text-accent' : 'text-label')} strokeWidth={narrowed ? 2.3 : 1.9} />
      {alerting && <span aria-hidden className="absolute -right-1 -top-1 h-2 w-2 rounded-full bg-orange" />}
    </span>
  ) : (
    <>
      <Icon aria-hidden className="h-[15px] w-[15px] shrink-0 text-label-2" strokeWidth={1.9} />
      <span className="sr-only">{label}: </span>
      <span className="max-w-[140px] truncate">{chosen}</span>
      {count !== undefined && <span className="tabular-nums text-label-3">{count}</span>}
      {alerting && (
        <span aria-hidden className="rounded-full bg-orange px-1.5 text-[11px] font-semibold leading-[17px] tabular-nums text-white">
          {alerting.count}
        </span>
      )}
      <ChevronDown aria-hidden className="h-[14px] w-[14px] shrink-0 text-label-2" strokeWidth={2.2} />
    </>
  );
  const name = [round ? `${label}: ${chosen}${count !== undefined ? ` ${count}` : ''}` : label, alerting?.label].filter(Boolean).join(', ');
  return (
    <Glass className="shrink-0 rounded-full">
      <Menu
        label={name}
        iconOnly={round}
        sheetTitle={label}
        side="below-end"
        trigger={trigger}
        triggerClassName={cn(
          'flex h-10 items-center rounded-full font-medium text-label transition-colors hover:bg-lift aria-expanded:bg-lift',
          round ? 'w-10 justify-center' : 'gap-1.5 pl-3 pr-2.5',
        )}
      >
        <MenuGroup label={label}>
          {options.map((option) => (
            <MenuItem key={option.id} checked={value === option.id} onSelect={() => onChange(option.id)}>
              <span className="flex items-baseline justify-between gap-4">
                <span className="truncate">{option.label}</span>
                {option.count !== undefined && (
                  <span className="tabular-nums text-label-3 group-hover:text-on-accent group-focus-visible:text-on-accent">{option.count}</span>
                )}
              </span>
            </MenuItem>
          ))}
        </MenuGroup>
      </Menu>
    </Glass>
  );
}
