import { ChevronRight } from 'lucide-react';
import { useMemo, type ReactNode } from 'react';
import { useT } from '@/shared/i18n';
import { useElementWidth } from '@/shared/lib/hooks';
import type { ListSort } from '@/shared/lib/listState';
import { cn, formatBytes } from '@/shared/lib/utils';
import { List, ListHead, ListRow, type ListColumn } from '@/shared/ui/List';
import { ShellLayoutContext, useShellLayout } from '@/shared/ui/shellLayout';
import { Sparkline } from '@/shared/ui/Sparkline';
import type { UsageSortKey, UserUsageReportItem } from '../model';
import { numberValue, percentValue } from './reportFormat';

// The table's grid, shared by its head and rows; on compact content a row is
// the name over its figures, beside the trend.
const template = 'minmax(150px,1.5fr) minmax(64px,1fr) 84px 84px 60px 84px 64px';
const compactTemplate = 'minmax(0,1fr) 72px';
// The columns need this much room; a narrower table turns its rows compact,
// whatever the content area's width, as the side column can narrow it.
export const usersTableMinWidth = 680;

interface UsersTableProps {
  users: readonly UserUsageReportItem[];
  sort: ListSort<UsageSortKey>;
  onSort: (key: UsageSortKey) => void;
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
export function UsersTable({ users, sort, onSort, colorOf, trendOf, openUser, onToggle, detail }: UsersTableProps) {
  const t = useT();
  const shellLayout = useShellLayout();
  const [measure, width] = useElementWidth<HTMLDivElement>(usersTableMinWidth);
  const compact = shellLayout.rowsCompact || width < usersTableMinWidth;
  const layout = useMemo(() => ({ ...shellLayout, rowsCompact: compact }), [shellLayout, compact]);
  const bytes = (value: number) => formatBytes(value, 1);
  const columns: ListColumn<UsageSortKey>[] = [
    { key: 'user', label: t('admin.reports.column.user') },
    { key: 'trend', label: t('admin.reports.column.trend') },
    { key: 'reserved', label: t('admin.reports.column.reserved') },
    { key: 'used', label: t('admin.reports.column.used') },
    { key: 'cpu', label: t('admin.reports.column.cpu') },
    { key: 'disk', label: t('admin.reports.column.disk') },
    { key: 'actions', label: t('admin.reports.column.actions') },
  ];

  return (
    <div ref={measure} className="min-w-0">
      <ShellLayoutContext.Provider value={layout}>
        <ListHead columns={columns} template={template} sort={sort} onSort={onSort} />
        <List label={t('admin.reports.usersList')}>
          {users.map((user) => {
            const name = user.owner_username || user.owner_id;
            const open = openUser === user.owner_id;
            const detailId = `report-user-${user.owner_id}`;
            const trend = trendOf(user.owner_id);
            const sparkline = (
              <Sparkline series={trend} max={Math.max(0, ...trend)} color={colorOf(user.owner_id)} className="mt-0 h-6" />
            );
            return (
              <div key={user.owner_id} role="listitem" aria-label={name}>
                <ListRow template={compact ? compactTemplate : template} onOpen={() => onToggle(user.owner_id)} inItem>
                  <div className="flex min-w-0 items-center gap-2">
                    <span aria-hidden className="h-2.5 w-2.5 shrink-0 rounded-[3px]" style={{ background: colorOf(user.owner_id) }} />
                    <div className="min-w-0">
                      <button
                        type="button"
                        aria-expanded={open}
                        aria-controls={open ? detailId : undefined}
                        onClick={(event) => {
                          event.stopPropagation();
                          onToggle(user.owner_id);
                        }}
                        className="flex max-w-full items-center gap-1 rounded font-semibold text-label"
                      >
                        <span className="truncate">{name}</span>
                        <ChevronRight
                          aria-hidden
                          className={cn('h-3.5 w-3.5 shrink-0 text-label-3 transition-transform', open && 'rotate-90')}
                          strokeWidth={2.2}
                        />
                      </button>
                      <div className="truncate text-[12px] text-label-2">
                        {compact
                          ? t('admin.reports.compactFigures', { reserved: bytes(user.reserved_memory_bytes), used: bytes(user.memory_usage_bytes) })
                          : t('admin.reports.running', { running: user.containers_running, total: user.containers_total })}
                      </div>
                    </div>
                  </div>
                  {compact ? (
                    sparkline
                  ) : (
                    <>
                      {sparkline}
                      <span className="tabular-nums text-label">{bytes(user.reserved_memory_bytes)}</span>
                      <span className="tabular-nums text-label-2">{bytes(user.memory_usage_bytes)}</span>
                      <span className="tabular-nums text-label-2">{percentValue(user.cpu_percent)}</span>
                      <span className="tabular-nums text-label-2">{bytes(user.total_disk_bytes)}</span>
                      <span className="tabular-nums text-label-2">{numberValue(user.actions_total)}</span>
                    </>
                  )}
                </ListRow>
                {open && (
                  <div id={detailId} role="region" aria-label={name} className="px-1 pb-6 pt-3 sm:pl-9">
                    {detail}
                  </div>
                )}
              </div>
            );
          })}
        </List>
      </ShellLayoutContext.Provider>
    </div>
  );
}
