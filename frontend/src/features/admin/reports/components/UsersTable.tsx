import { ChevronDown, ChevronRight, ChevronUp } from 'lucide-react';
import type { ReactNode } from 'react';
import { useT } from '@/shared/i18n';
import { useElementWidth } from '@/shared/lib/hooks';
import type { ListSort } from '@/shared/lib/listState';
import { cn, formatBytes } from '@/shared/lib/utils';
import { useShellLayout } from '@/shared/ui/shellLayout';
import type { UsageSortKey, UserUsageReportItem } from '../model';
import { numberValue, percentValue } from './reportFormat';

// The table's grid, shared by its head and rows; a compact row is the user
// and their memory reserved.
const template = 'minmax(0,1.4fr) 120px repeat(5,minmax(64px,0.6fr))';
const compactTemplate = 'minmax(0,1fr) 80px';
// The columns need this much room; a narrower table turns its rows compact,
// whatever the content area's width, as the side column can narrow it.
export const usersTableMinWidth = 700;

// Trend is a user's figure over the range as a plain line in their colour,
// scaled between its own lowest and highest reading so its shape shows.
function Trend({ values, color }: { values: readonly number[]; color: string }) {
  if (values.length < 2) return <span aria-hidden />;
  const max = Math.max(...values);
  const min = Math.min(...values);
  const points = values.map((value, index) => `${((index / (values.length - 1)) * 100).toFixed(2)},${(22 - ((value - min) / (max - min || 1)) * 20).toFixed(2)}`);
  return (
    <svg aria-hidden className="block h-6 w-full" viewBox="0 0 100 24" preserveAspectRatio="none">
      <path d={`M${points.join(' L')}`} fill="none" stroke={color} strokeWidth={2} strokeLinejoin="round" vectorEffect="non-scaling-stroke" />
    </svg>
  );
}

interface UsersTableProps {
  users: readonly UserUsageReportItem[];
  sort: ListSort<UsageSortKey>;
  onSort: (key: UsageSortKey) => void;
  // rangeLabel names the range in the trend's heading.
  rangeLabel: string;
  // colorOf is each user's series colour, the same in every report.
  colorOf: (ownerId: string) => string;
  // trendOf is each user's memory reserved over the range, oldest first.
  trendOf: (ownerId: string) => readonly number[];
  openUser: string | null;
  onToggle: (ownerId: string) => void;
  // detail is the open user's charts and actions, shown under their row.
  detail: ReactNode;
}

// UsersTable lists the users with their latest figures in the range and the
// trend of their memory reserved, each column sorting it. A row opens that
// user's charts and recent actions beneath it.
export function UsersTable({ users, sort, onSort, rangeLabel, colorOf, trendOf, openUser, onToggle, detail }: UsersTableProps) {
  const t = useT();
  const [measure, width] = useElementWidth<HTMLDivElement>(usersTableMinWidth);
  const compact = useShellLayout().rowsCompact || width < usersTableMinWidth;
  const bytes = (value: number) => formatBytes(value, 1);
  const columns: { key: UsageSortKey; label: string }[] = [
    { key: 'user', label: t('admin.reports.column.user') },
    { key: 'trend', label: t('admin.reports.column.trendOf', { range: rangeLabel }) },
    { key: 'reserved', label: t('admin.reports.column.reserved') },
    { key: 'used', label: t('admin.reports.column.used') },
    { key: 'cpu', label: t('admin.reports.column.cpu') },
    { key: 'disk', label: t('admin.reports.column.disk') },
    { key: 'actions', label: t('admin.reports.column.actions') },
  ];
  const Arrow = sort.ascending ? ChevronUp : ChevronDown;

  return (
    <div ref={measure} className="min-w-0">
      {!compact && (
        <div className="grid min-h-[34px] items-center gap-4 px-3.5 shadow-[inset_0_-1px_0_var(--separator)]" style={{ gridTemplateColumns: template }}>
          {columns.map((column) => {
            const current = sort.key === column.key;
            return (
              <button
                key={column.key}
                type="button"
                onClick={() => onSort(column.key)}
                className={cn('inline-flex min-w-0 items-center gap-[3px] text-left text-[11.5px] font-[650] hover:text-label', current ? 'text-label' : 'text-label-2')}
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
      )}
      <ul aria-label={t('admin.reports.usersList')} className="grid">
        {users.map((user, index) => {
          const name = user.owner_username || user.owner_id;
          const open = openUser === user.owner_id;
          const detailId = `report-user-${user.owner_id}`;
          const color = colorOf(user.owner_id);
          return (
            <li key={user.owner_id} className={cn(index > 0 && 'border-t border-separator')}>
              <button
                type="button"
                aria-expanded={open}
                aria-controls={open ? detailId : undefined}
                onClick={() => onToggle(user.owner_id)}
                className="grid min-h-[50px] w-full items-center gap-4 rounded-[10px] px-3.5 py-1.5 text-left tabular-nums hover:bg-tile focus-visible:outline-offset-[-2px]"
                style={{ gridTemplateColumns: compact ? compactTemplate : template }}
              >
                <span className="flex min-w-0 items-center gap-2">
                  <ChevronRight aria-hidden className={cn('h-3.5 w-3.5 shrink-0 text-label-3 transition-transform', open && 'rotate-90')} />
                  <span aria-hidden className="h-2.5 w-2.5 shrink-0 rounded-[3px]" style={{ background: color }} />
                  <span className="shrink-0 font-semibold text-label">{name}</span>
                  <span className="truncate text-[12px] text-label-2">
                    {t('admin.reports.running', { running: user.containers_running, total: user.containers_total })}
                  </span>
                </span>
                {compact ? (
                  <span className="text-right text-label">{bytes(user.reserved_memory_bytes)}</span>
                ) : (
                  <>
                    <Trend values={trendOf(user.owner_id)} color={color} />
                    <span className="text-label">{bytes(user.reserved_memory_bytes)}</span>
                    <span className="text-label-2">{bytes(user.memory_usage_bytes)}</span>
                    <span className="text-label">{percentValue(user.cpu_percent)}</span>
                    <span className="text-label">{bytes(user.total_disk_bytes)}</span>
                    <span className="text-label-2">{numberValue(user.actions_total)}</span>
                  </>
                )}
              </button>
              {open && (
                <div id={detailId} role="region" aria-label={name} className="px-3.5 pb-[18px] pt-2">
                  {detail}
                </div>
              )}
            </li>
          );
        })}
      </ul>
    </div>
  );
}
