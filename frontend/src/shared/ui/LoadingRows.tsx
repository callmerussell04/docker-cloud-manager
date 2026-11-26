import { cn } from '@/shared/lib/utils';
import { GhostRows, type GhostShape } from './GhostRows';

interface LoadingRowsProps extends GhostShape {
  rows?: number;
  // dot leads each row with a status dot's placeholder.
  dot?: boolean;
  className?: string;
}

// LoadingRows is the first load of a list or a panel: placeholder rows with
// a light sweeping across them, in the list's own grid (template) or the
// panel's shape (columns), so the page does not jump when the data arrives.
export function LoadingRows({ rows, dot = true, className, ...shape }: LoadingRowsProps) {
  return (
    <div className={cn(className)}>
      <GhostRows tone="loading" rows={rows} dot={dot} {...shape} />
    </div>
  );
}
