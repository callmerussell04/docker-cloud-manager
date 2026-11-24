import type { CSSProperties, KeyboardEvent, ReactNode } from 'react';
import { ChevronDown, ChevronUp, MoreHorizontal, type LucideIcon } from 'lucide-react';
import { useT } from '@/shared/i18n';
import type { ListSort } from '@/shared/lib/listState';
import { useSizeClass } from '@/shared/lib/useSizeClass';
import { cn } from '@/shared/lib/utils';
import { Menu } from './Menu';
import { useShellLayout } from './shellLayout';

// The list primitives every list page uses: plain column labels that sort,
// rows that open on click or Enter, and each row's actions. A list is rows on
// the frost, never a boxed table. A page gives the head to Page's `head`, so
// it stays fixed under the toolbar while rows scroll beneath it, and lays out
// head and rows on the same grid columns.

export interface ListColumn<K extends string> {
  key: K;
  label: string;
  // end aligns the label with a right-aligned last column, as an age.
  end?: boolean;
}

interface ListHeadProps<K extends string> {
  columns: readonly ListColumn<K>[];
  // template is the grid the rows use, so each label sits over its column.
  template: string;
  // sort and onSort make each label sort the list; without them the labels
  // are plain, for a list in a fixed order such as a log.
  sort?: ListSort<K>;
  onSort?: (key: K) => void;
}

// ListHead is a list's column labels; each sorts the list, and again reverses
// it. Compact rows have no columns, so it shows nothing then.
export function ListHead<K extends string>({ columns, template, sort, onSort }: ListHeadProps<K>) {
  const t = useT();
  if (useShellLayout().rowsCompact) return null;
  return (
    <div className="grid h-[34px] items-center gap-[18px] pl-4 pr-2.5 shadow-[inset_0_-1px_0_var(--separator)]" style={{ gridTemplateColumns: template }}>
      {columns.map((column, index) => {
        if (!sort || !onSort) {
          return (
            <span
              key={column.key}
              className={cn('truncate text-[11.5px] font-[650] text-label-2', index === 0 && 'pl-[22px]', column.end && 'pr-[38px] text-right')}
            >
              {column.label}
            </span>
          );
        }
        const current = sort.key === column.key;
        const Arrow = sort.ascending ? ChevronUp : ChevronDown;
        return (
          <button
            key={column.key}
            type="button"
            onClick={() => onSort(column.key)}
            className={cn(
              'inline-flex min-w-0 items-center gap-[3px] text-[11.5px] font-[650] hover:text-label',
              current ? 'text-label' : 'text-label-2',
              index === 0 && 'pl-[22px]',
              column.end && 'justify-end pr-[38px]',
            )}
          >
            <span className="truncate">{column.label}</span>
            {current && (
              <>
                <Arrow aria-hidden className="h-[13px] w-[13px] shrink-0" strokeWidth={2.4} />
                <span className="sr-only">, {t(sort.ascending ? 'common.sortedAscending' : 'common.sortedDescending')}</span>
              </>
            )}
          </button>
        );
      })}
    </div>
  );
}

interface ListProps {
  label: string;
  children: ReactNode;
}

// List holds a list's rows.
export function List({ label, children }: ListProps) {
  return (
    <div role="list" aria-label={label} className="flex flex-col">
      {children}
    </div>
  );
}

interface ListRowProps {
  // template is the grid of the row's cells; ListHead takes the same one.
  template: string;
  // onOpen opens the row's item; leave it out for an item with no page, whose
  // row is then not a target and its menu holds everything.
  onOpen?: () => void;
  // inItem marks a row that its own list item holds together with what it
  // opens below it, as a row that opens in place; the row is then a group.
  inItem?: boolean;
  children: ReactNode;
}

// ListRow is one line of a list. The whole row opens its item, on a click or
// on Enter while it is focused; links and buttons inside do their own thing.
// Compact rows grow to fit two lines.
export function ListRow({ template, onOpen, inItem = false, children }: ListRowProps) {
  const compact = useShellLayout().rowsCompact;
  const onKeyDown = (event: KeyboardEvent<HTMLDivElement>) => {
    if (!onOpen || event.target !== event.currentTarget) return;
    if (event.key === 'Enter' || event.key === ' ') {
      event.preventDefault();
      onOpen();
    }
  };
  return (
    <div
      role={inItem ? 'group' : 'listitem'}
      tabIndex={onOpen ? 0 : undefined}
      onClick={onOpen}
      onKeyDown={onOpen && onKeyDown}
      style={{ gridTemplateColumns: template } as CSSProperties}
      className={cn(
        'list-row group/row relative grid items-center rounded-[13px] focus-visible:outline-offset-[-2px]',
        onOpen && 'cursor-pointer hover:bg-fill',
        compact ? 'min-h-[62px] gap-2 py-2.5 pl-3.5 pr-1' : 'h-[54px] gap-[18px] pl-4 pr-2.5',
      )}
    >
      {children}
    </div>
  );
}

interface RowEndProps {
  // age is the row's last column, which the quick actions cover while the row
  // is hovered or focused.
  age?: ReactNode;
  // quick holds RowQuickActions, shown only on wide windows.
  quick?: ReactNode;
  // menu is the row's RowMenu, always present.
  menu: ReactNode;
}

// RowEnd is a row's last cell: its age, and its actions. The menu holds every
// action and is always there, faint until the row is hovered on a wide window
// where a pointer is likely; there the most used actions also appear over the
// age. On a compact row the cell is the menu alone.
export function RowEnd({ age, quick, menu }: RowEndProps) {
  const compact = useShellLayout().rowsCompact;
  const showQuick = useSizeClass() === 'wide' && !compact && quick != null;
  // Clicks inside, including those in the menu and dialogs it opens, which
  // React passes up through their portals, never open the row.
  const actions = (
    <div className={cn('flex items-center gap-0.5', !compact && 'absolute right-0')} onClick={(event) => event.stopPropagation()}>
      {showQuick && (
        <div className="flex gap-0.5 opacity-0 group-focus-within/row:opacity-100 group-hover/row:opacity-100">{quick}</div>
      )}
      {menu}
    </div>
  );
  if (compact) return actions;
  return (
    <div className="relative flex h-full items-center justify-end pr-8">
      {age != null && (
        <span
          className={cn(
            'truncate pr-1.5 tabular-nums text-label-2',
            showQuick && 'group-focus-within/row:opacity-0 group-hover/row:opacity-0',
          )}
        >
          {age}
        </span>
      )}
      {actions}
    </div>
  );
}

interface RowQuickActionProps {
  icon: LucideIcon;
  // label is the button's accessible name and tooltip.
  label: string;
  onClick: () => void;
  disabled?: boolean;
}

// RowQuickAction is one of the actions a hovered row shows, icon only.
export function RowQuickAction({ icon: Icon, label, onClick, disabled }: RowQuickActionProps) {
  return (
    <button
      type="button"
      aria-label={label}
      title={label}
      disabled={disabled}
      onClick={onClick}
      className="flex h-7 w-7 items-center justify-center rounded-lg text-label-2 hover:bg-fill-hover hover:text-label disabled:pointer-events-none disabled:opacity-30"
    >
      <Icon className="h-[15px] w-[15px]" strokeWidth={1.8} />
    </button>
  );
}

interface RowMenuProps {
  // label is the menu's and its button's accessible name.
  label: string;
  // sheetTitle heads the menu as a sheet on a phone, as the row's name.
  sheetTitle: string;
  children: ReactNode;
}

// RowMenu is the menu of every action on a row: faint until the row is
// hovered on a wide window, at full strength where nothing hovers.
export function RowMenu({ label, sheetTitle, children }: RowMenuProps) {
  const compact = useShellLayout().rowsCompact;
  const faint = useSizeClass() === 'wide' && !compact;
  return (
    <Menu
      label={label}
      sheetTitle={sheetTitle}
      iconOnly
      trigger={<MoreHorizontal className="h-[17px] w-[17px]" />}
      triggerClassName={cn(
        'flex items-center justify-center text-label-2 hover:bg-fill-hover hover:text-label aria-expanded:bg-fill-hover aria-expanded:text-label',
        compact ? 'h-10 w-10 rounded-full' : 'h-7 w-7 rounded-lg',
        faint && 'opacity-45 group-focus-within/row:opacity-100 group-hover/row:opacity-100 aria-expanded:opacity-100',
      )}
    >
      {children}
    </Menu>
  );
}
