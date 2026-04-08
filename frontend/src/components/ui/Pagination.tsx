import { ChevronLeft, ChevronRight } from 'lucide-react';
import { Button } from './Button';

interface PaginationProps {
  currentPage: number;
  totalItems: number;
  pageSize: number;
  onPageChange: (page: number) => void;
}

export function Pagination({ currentPage, totalItems, pageSize, onPageChange }: PaginationProps) {
  const totalPages = Math.ceil(totalItems / pageSize);

  if (totalPages <= 1) return null;

  return (
    <div className="flex items-center justify-between px-4 py-3 border-t border-white/20 dark:border-slate-700/50">
      <div className="text-sm text-slate-500 dark:text-slate-400">
        Показано <span className="font-medium text-slate-900 dark:text-slate-100">{(currentPage - 1) * pageSize + 1}</span> — <span className="font-medium text-slate-900 dark:text-slate-100">{Math.min(currentPage * pageSize, totalItems)}</span> из <span className="font-medium text-slate-900 dark:text-slate-100">{totalItems}</span>
      </div>
      <div className="flex items-center gap-2">
        <Button
          variant="secondary"
          className="p-2 h-9 w-9"
          disabled={currentPage === 1}
          onClick={() => onPageChange(currentPage - 1)}
        >
          <ChevronLeft className="w-4 h-4" />
        </Button>
        <span className="text-sm font-medium text-slate-700 dark:text-slate-300 w-10 text-center">
          {currentPage}
        </span>
        <Button
          variant="secondary"
          className="p-2 h-9 w-9"
          disabled={currentPage === totalPages}
          onClick={() => onPageChange(currentPage + 1)}
        >
          <ChevronRight className="w-4 h-4" />
        </Button>
      </div>
    </div>
  );
}