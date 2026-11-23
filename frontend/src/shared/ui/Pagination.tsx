import { useEffect } from 'react';
import { ChevronLeft, ChevronRight } from 'lucide-react';
import { Button } from './Button';
import { useT } from '@/shared/i18n';

interface PaginationProps {
  currentPage: number;
  totalItems: number;
  pageSize: number;
  onPageChange: (page: number) => void;
}

export function Pagination({ currentPage, totalItems, pageSize, onPageChange }: PaginationProps) {
  const totalPages = Math.ceil(totalItems / pageSize);
  const t = useT();
  const lastPage = Math.max(totalPages, 1);

  // A page past the end, as after deleting the last row of the last page,
  // returns to the last page that exists.
  useEffect(() => {
    if (currentPage > lastPage) onPageChange(lastPage);
  }, [currentPage, lastPage, onPageChange]);

  if (totalPages <= 1) return null;

  return (
    <div className="flex flex-col gap-3 px-2.5 py-3 sm:flex-row sm:items-center sm:justify-between">
      <div className="text-label-2">
        {t('pagination.summary', {
          from: (currentPage - 1) * pageSize + 1,
          to: Math.min(currentPage * pageSize, totalItems),
          total: totalItems,
        })}
      </div>
      <div className="flex items-center justify-end gap-2">
        <Button
          variant="secondary"
          className="p-2 h-9 w-9"
          disabled={currentPage === 1}
          aria-label={t('pagination.previous')}
          title={t('pagination.previous')}
          onClick={() => onPageChange(currentPage - 1)}
        >
          <ChevronLeft className="w-4 h-4" />
        </Button>
        <span className="w-10 text-center font-medium tabular-nums text-label">
          {currentPage}
        </span>
        <Button
          variant="secondary"
          className="p-2 h-9 w-9"
          disabled={currentPage >= totalPages}
          aria-label={t('pagination.next')}
          title={t('pagination.next')}
          onClick={() => onPageChange(currentPage + 1)}
        >
          <ChevronRight className="w-4 h-4" />
        </Button>
      </div>
    </div>
  );
}
