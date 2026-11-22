import { Search, Users } from 'lucide-react';
import type { UseQueryResult } from '@tanstack/react-query';

import { Card, CardContent, CardHeader, CardTitle } from '@/shared/ui/Card';
import { Pagination } from '@/shared/ui/Pagination';
import { useT } from '@/shared/i18n';
import { formatBytes } from '@/shared/lib/utils';
import type { UserUsageReportItem, UserUsageReportResponse } from '../model';
import { numberValue, percentValue, userPageSize } from './reportFormat';
import { EmptyState } from '@/shared/ui/EmptyState';
import { ErrorState } from '@/shared/ui/ErrorState';

interface UserUsageTableProps {
  query: UseQueryResult<UserUsageReportResponse>;
  search: string;
  onSearchChange: (value: string) => void;
  sort: string;
  onSortChange: (value: string) => void;
  page: number;
  onPageChange: (page: number) => void;
  onSelect: (user: UserUsageReportItem) => void;
}

/** The users ranked by usage; selecting a row opens that user's chart. */
export function UserUsageTable({ query, search, onSearchChange, sort, onSortChange, page, onPageChange, onSelect }: UserUsageTableProps) {
  const t = useT();

  return (
    <Card>
      <CardHeader className="space-y-3">
        <div className="flex flex-col justify-between gap-3 lg:flex-row lg:items-center">
          <CardTitle className="flex items-center gap-2 text-lg">
            <Users className="h-5 w-5 text-sky-500" />
            {t('admin.reports.topUsers')}
          </CardTitle>
          <div className="grid w-full grid-cols-1 gap-3 sm:grid-cols-[minmax(220px,1fr)_220px] lg:w-auto">
            <div className="relative">
              <Search className="absolute left-3 top-1/2 h-4 w-4 -translate-y-1/2 text-slate-400" />
              <input
                value={search}
                onChange={(event) => onSearchChange(event.target.value)}
                placeholder={t('common.search')}
                className="h-10 w-full rounded-lg border border-slate-200 bg-white pl-9 pr-3 text-sm dark:border-slate-700 dark:bg-slate-900"
              />
            </div>
            <select value={sort} onChange={(event) => onSortChange(event.target.value)} className="h-10 rounded-lg border border-slate-200 bg-white px-3 text-sm dark:border-slate-700 dark:bg-slate-900">
              <option value="disk">{t('admin.reports.sort.disk')}</option>
              <option value="actual_memory">{t('admin.reports.sort.actualMemory')}</option>
              <option value="reserved_memory">{t('admin.reports.sort.reservedMemory')}</option>
              <option value="cpu">{t('admin.reports.sort.cpu')}</option>
              <option value="resources">{t('admin.reports.sort.resources')}</option>
              <option value="actions">{t('admin.reports.sort.actions')}</option>
              <option value="containers">{t('admin.reports.sort.containers')}</option>
            </select>
          </div>
        </div>
      </CardHeader>
      <CardContent>
        <div className="overflow-x-auto">
          <table className="w-full text-sm">
            <thead className="text-left text-slate-500 dark:text-slate-400">
              <tr>
                <th className="py-2 pr-3">{t('common.user')}</th>
                <th className="py-2 pr-3">{t('admin.reports.actualMemory')}</th>
                <th className="py-2 pr-3">{t('admin.reports.reservedMemory')}</th>
                <th className="py-2 pr-3">{t('admin.reports.cpu')}</th>
                <th className="py-2 pr-3">{t('admin.reports.disk')}</th>
                <th className="py-2 pr-3">{t('admin.reports.containers')}</th>
                <th className="py-2 pr-3">{t('admin.reports.volumes')}</th>
                <th className="py-2 pr-3">{t('admin.reports.images')}</th>
                <th className="py-2 pr-3">{t('admin.reports.builds')}</th>
                <th className="py-2 pr-3">{t('admin.reports.projects')}</th>
                <th className="py-2 pr-3">{t('admin.reports.actions')}</th>
              </tr>
            </thead>
            <tbody>
              {!query.isError && (query.data?.users ?? []).map((item) => {
                return (
                  <tr
                    key={item.owner_id}
                    onClick={() => onSelect(item)}
                    className="cursor-pointer border-t border-slate-100 hover:bg-slate-50 dark:border-slate-800 dark:hover:bg-slate-800/60"
                  >
                    <td className="py-3 pr-3 font-medium text-slate-900 dark:text-slate-100">{item.owner_username || item.owner_id}</td>
                    <td className="py-3 pr-3">{formatBytes(item.memory_usage_bytes)}</td>
                    <td className="py-3 pr-3">{formatBytes(item.reserved_memory_bytes)}</td>
                    <td className="py-3 pr-3">{percentValue(item.cpu_percent)}</td>
                    <td className="py-3 pr-3">{formatBytes(item.total_disk_bytes)}</td>
                    <td className="py-3 pr-3">{item.containers_running}/{item.containers_total}</td>
                    <td className="py-3 pr-3">{numberValue(item.volumes_total)}</td>
                    <td className="py-3 pr-3">{numberValue(item.images_total)}</td>
                    <td className="py-3 pr-3">{numberValue(item.builds_total)}</td>
                    <td className="py-3 pr-3">{numberValue(item.projects_total)}</td>
                    <td className="py-3 pr-3">{numberValue(item.actions_total)}</td>
                  </tr>
                );
              })}
            </tbody>
          </table>
          {query.isError && (
            <div className="py-6">
              <ErrorState
                title={t('admin.reports.usersLoadFailed')}
                onRetry={() => query.refetch()}
                isRetrying={query.isFetching}
              />
            </div>
          )}
          {!query.isError && !query.isLoading && (query.data?.users.length ?? 0) === 0 && (
            <EmptyState title={t('admin.reports.emptyUsers')} />
          )}
        </div>
      </CardContent>
      {!query.isError && <Pagination currentPage={page} totalItems={query.data?.total_count ?? 0} pageSize={userPageSize} onPageChange={onPageChange} />}
    </Card>
  );
}
