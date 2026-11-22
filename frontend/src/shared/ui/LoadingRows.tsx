import { useT } from '@/shared/i18n';
import { cn } from '@/shared/lib/utils';

interface LoadingRowsProps {
  rows?: number;
  // columns is the relative width of each column after the leading dot, so
  // the placeholders take the list's own shape.
  columns?: number[];
  // dot leads each row with a status dot's placeholder.
  dot?: boolean;
  className?: string;
}

// LoadingRows is the first load of a list: pulsing placeholder rows in the
// list's shape, so the page does not jump when the data arrives.
export function LoadingRows({ rows = 5, columns = [1.5, 0.8, 0.5], dot = true, className }: LoadingRowsProps) {
  const t = useT();
  const template = [dot ? '10px' : null, ...columns.map((width) => `${width}fr`)].filter(Boolean).join(' ');
  return (
    <div role="status" aria-busy="true" aria-label={t('common.loading')} className={cn('grid gap-1', className)}>
      {Array.from({ length: rows }, (_, row) => (
        <div
          key={row}
          className="grid h-11 animate-pulse items-center gap-[18px] px-2.5"
          style={{ gridTemplateColumns: template, animationDelay: `${row * 120}ms` }}
        >
          {dot && <span className="h-[9px] w-[9px] rounded-full bg-fill-hover" />}
          {columns.map((_, column) => (
            <span
              key={column}
              className="h-2.5 rounded-full bg-fill-hover"
              style={{ width: column === 0 ? `${90 - ((row * 17) % 40)}%` : '70%' }}
            />
          ))}
        </div>
      ))}
    </div>
  );
}
