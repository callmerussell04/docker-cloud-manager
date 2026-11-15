import type { ReactNode } from 'react';
import { RefreshCcw } from 'lucide-react';

import { EmptyState } from '@/shared/ui/EmptyState';
import { ErrorState } from '@/shared/ui/ErrorState';
import { Pagination } from '@/shared/ui/Pagination';
import { cn } from '@/shared/lib/utils';

interface ResourceTableProps {
  layout: { minWidth: string; grid: string };
  columns: ReactNode;
  // isLoading is the first load of a page; a background refresh keeps the rows.
  isLoading: boolean;
  isFetching: boolean;
  error: { title: string; message: string; onRetry: () => void } | null;
  empty: { icon: ReactNode; title: string; description: string };
  itemCount: number;
  page: number;
  pageSize: number;
  totalItems?: number;
  onPageChange: (page: number) => void;
  children: ReactNode;
}

/** The frame of an all-users table: header, loading, error and empty states, rows and pagination. */
export function ResourceTable({
  layout,
  columns,
  isLoading,
  isFetching,
  error,
  empty,
  itemCount,
  page,
  pageSize,
  totalItems,
  onPageChange,
  children,
}: ResourceTableProps) {
  return (
    <div className="bg-white/40 dark:bg-slate-900/40 backdrop-blur-xl border border-white/50 dark:border-slate-700/50 rounded-2xl overflow-hidden flex-1 flex flex-col relative">
      <div className="overflow-x-auto flex-1">
        <div className={cn("flex flex-col h-full", layout.minWidth)}>
          <div className={cn("grid items-center gap-4 p-4 border-b border-white/20 dark:border-slate-700/50 bg-slate-50/50 dark:bg-slate-800/50 text-sm font-medium text-slate-500", layout.grid)}>
            {columns}
          </div>

          <div className="flex flex-col flex-1">
            {isLoading ? (
              <div className="p-12 flex justify-center opacity-50"><RefreshCcw className="w-8 h-8 animate-spin" /></div>
            ) : error ? (
              <div className="p-4">
                <ErrorState title={error.title} message={error.message} onRetry={error.onRetry} isRetrying={isFetching} />
              </div>
            ) : itemCount === 0 ? (
              <EmptyState icon={empty.icon} title={empty.title} description={empty.description} />
            ) : (
              children
            )}
          </div>

          <div className="mt-auto shrink-0 bg-slate-50/50 dark:bg-slate-800/50 rounded-b-2xl">
            {totalItems !== undefined && <Pagination currentPage={page} pageSize={pageSize} totalItems={totalItems} onPageChange={onPageChange} />}
          </div>
        </div>
      </div>
    </div>
  );
}
