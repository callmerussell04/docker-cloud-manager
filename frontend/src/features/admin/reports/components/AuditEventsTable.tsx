import { Activity, Search } from 'lucide-react';
import type { UseQueryResult } from '@tanstack/react-query';

import { Card, CardContent, CardHeader, CardTitle } from '@/shared/ui/Card';
import { Pagination } from '@/shared/ui/Pagination';
import { dateLocale, useLocale, useT } from '@/shared/i18n';
import { auditDetails, type AuditEventsResponse } from '../model';
import { auditActionLabel, auditActions, auditPageSize, auditResourceTypes } from './reportFormat';

export interface AuditFilterValues {
  search: string;
  outcome: string;
  action: string;
  resourceType: string;
}

interface AuditEventsTableProps {
  query: UseQueryResult<AuditEventsResponse>;
  filters: AuditFilterValues;
  onFilterChange: (name: keyof AuditFilterValues, value: string) => void;
  page: number;
  onPageChange: (page: number) => void;
}

/** The audit log with its filters. */
export function AuditEventsTable({ query, filters, onFilterChange, page, onPageChange }: AuditEventsTableProps) {
  const t = useT();
  const locale = useLocale();

  return (
    <Card>
      <CardHeader className="space-y-3">
        <CardTitle className="flex items-center gap-2 text-lg">
          <Activity className="h-5 w-5 text-indigo-500" />
          {t('admin.reports.auditEventsTable')}
        </CardTitle>
        <div className="grid grid-cols-1 gap-3 md:grid-cols-[1fr_160px_180px_160px]">
          <div className="relative">
            <Search className="absolute left-3 top-1/2 h-4 w-4 -translate-y-1/2 text-slate-400" />
            <input
              value={filters.search}
              onChange={(event) => onFilterChange('search', event.target.value)}
              placeholder={t('common.search')}
              className="h-10 w-full rounded-lg border border-slate-200 bg-white pl-9 pr-3 text-sm dark:border-slate-700 dark:bg-slate-900"
            />
          </div>
          <select value={filters.outcome} onChange={(event) => onFilterChange('outcome', event.target.value)} className="h-10 rounded-lg border border-slate-200 bg-white px-3 text-sm dark:border-slate-700 dark:bg-slate-900">
            <option value="">{t('admin.reports.allOutcomes')}</option>
            <option value="success">{t('admin.reports.outcome.success')}</option>
            <option value="failure">{t('admin.reports.outcome.failure')}</option>
          </select>
          <select value={filters.action} onChange={(event) => onFilterChange('action', event.target.value)} className="h-10 rounded-lg border border-slate-200 bg-white px-3 text-sm dark:border-slate-700 dark:bg-slate-900">
            <option value="">{t('admin.reports.allActions')}</option>
            {auditActions.map((action) => (
              <option key={action} value={action}>{auditActionLabel(t, action)}</option>
            ))}
          </select>
          <select value={filters.resourceType} onChange={(event) => onFilterChange('resourceType', event.target.value)} className="h-10 rounded-lg border border-slate-200 bg-white px-3 text-sm dark:border-slate-700 dark:bg-slate-900">
            <option value="">{t('admin.reports.allResources')}</option>
            {auditResourceTypes.map((resourceType) => (
              <option key={resourceType} value={resourceType}>{resourceType}</option>
            ))}
          </select>
        </div>
      </CardHeader>
      <CardContent>
        <div className="overflow-x-auto">
          <table className="w-full text-sm">
            <thead className="text-left text-slate-500 dark:text-slate-400">
              <tr>
                <th className="py-2 pr-3">{t('admin.reports.time')}</th>
                <th className="py-2 pr-3">{t('common.user')}</th>
                <th className="py-2 pr-3">{t('admin.reports.action')}</th>
                <th className="py-2 pr-3">{t('common.status')}</th>
                <th className="py-2 pr-3">{t('admin.reports.resource')}</th>
                <th className="py-2 pr-3">{t('admin.reports.details')}</th>
              </tr>
            </thead>
            <tbody>
              {(query.data?.events ?? []).map((event) => {
                const details = auditDetails(event);
                return (
                  <tr key={event.id} className="align-top border-t border-slate-100 dark:border-slate-800">
                    <td className="whitespace-nowrap py-3 pr-3">{new Date(event.occurred_at * 1000).toLocaleString(dateLocale(locale))}</td>
                    <td className="py-3 pr-3">{event.actor_username || event.actor_user_id || 'system'}</td>
                    <td className="py-3 pr-3 font-medium" title={event.action}>{auditActionLabel(t, event.action)}</td>
                    <td className="py-3 pr-3">{event.outcome}</td>
                    <td className="py-3 pr-3">{event.resource_name || event.resource_id || event.resource_type}</td>
                    <td className="min-w-64 py-3 pr-3">
                      {details.length > 0 ? (
                        <div className="flex flex-wrap gap-1.5">
                          {details.map((detail) => (
                            <span key={`${event.id}-${detail.key}`} className="max-w-72 truncate rounded border border-slate-200 px-2 py-1 text-xs text-slate-600 dark:border-slate-700 dark:text-slate-300" title={`${detail.key}: ${detail.value}`}>
                              {detail.key}: {detail.value}
                            </span>
                          ))}
                        </div>
                      ) : (
                        <span className="text-slate-400">-</span>
                      )}
                    </td>
                  </tr>
                );
              })}
            </tbody>
          </table>
          {!query.isLoading && (query.data?.events.length ?? 0) === 0 && (
            <div className="py-10 text-center text-slate-500">{t('admin.reports.emptyAudit')}</div>
          )}
        </div>
      </CardContent>
      <Pagination currentPage={page} totalItems={query.data?.total_count ?? 0} pageSize={auditPageSize} onPageChange={onPageChange} />
    </Card>
  );
}
