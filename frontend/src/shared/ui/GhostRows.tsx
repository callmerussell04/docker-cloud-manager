import type { ReactNode } from 'react';
import { useT } from '@/shared/i18n';
import { cn } from '@/shared/lib/utils';
import { ghostBarWidth, ghostRowsMask, gridTracks } from './ghostRows';
import { useShellLayout } from './shellLayout';

// GhostTone is what placeholder rows stand for: a list still loading (full
// strength, a light sweeping across), a list with nothing in it yet (faint,
// fading out), or a list that could not be loaded (faint and grey).
export type GhostTone = 'loading' | 'empty' | 'failed';

export interface GhostShape {
  // template is the list's own grid (its listLayout), so the bars sit under
  // the column labels; the compact one on compact rows.
  template?: string;
  // compactTemplate is the grid of compact rows; without it compact rows
  // take the template too.
  compactTemplate?: string;
  // columns, without a template, are the relative widths of a placeholder
  // that is not a list's rows (a panel, a section).
  columns?: number[];
}

interface GhostRowsProps extends GhostShape {
  tone: GhostTone;
  rows?: number;
  // dot leads each row with a status dot's placeholder.
  dot?: boolean;
  // children is the message shown over the rows, centred near their top.
  children?: ReactNode;
}

// GhostRows is a list's shape without its data: placeholder rows on the
// list's own grid, with what to say about it over them.
export function GhostRows({ tone, template, compactTemplate, columns, rows, dot = true, children }: GhostRowsProps) {
  const t = useT();
  const compact = useShellLayout().rowsCompact;
  const loading = tone === 'loading';
  const count = rows ?? (loading ? 5 : 4);
  const grid = compact && compactTemplate ? compactTemplate : template;

  const bar = (key: number, width: string, row: number, className?: string) => (
    <span
      key={key}
      className={cn('block h-2.5 rounded-full', loading ? 'ghost-shimmer bg-fill-hover' : 'bg-fill', className)}
      style={{ width, animationDelay: loading ? `${row * 90}ms` : undefined }}
    />
  );

  const listRows = grid && (() => {
    const tracks = gridTracks(grid);
    return Array.from({ length: count }, (_, row) => (
      <div
        key={row}
        className={cn('list-row relative grid items-center', compact ? 'min-h-[62px] gap-2 py-2.5 pl-3.5 pr-1' : 'h-[54px] gap-[18px] pl-4 pr-2.5')}
        style={{ gridTemplateColumns: grid }}
      >
        {tracks.map((_, cell) => {
          if (cell === 0) {
            return (
              <span key={cell} className="flex min-w-0 items-center gap-3">
                {dot && <span className={cn('h-2 w-2 shrink-0 rounded-full', loading ? 'bg-fill-hover' : 'bg-fill')} />}
                <span className="grid min-w-0 flex-1 gap-2">
                  {bar(0, `${ghostBarWidth(row, 0)}%`, row, 'h-3')}
                  {compact && bar(1, `${ghostBarWidth(row + 2, 1)}%`, row)}
                </span>
              </span>
            );
          }
          // The last column is the row's age with its actions; on compact rows
          // it is only the actions, with nothing to stand in for.
          if (cell === tracks.length - 1) {
            return compact ? <span key={cell} /> : <span key={cell} className="flex justify-end pr-8">{bar(0, '40px', row)}</span>;
          }
          return <span key={cell}>{bar(0, `${ghostBarWidth(row, cell)}%`, row)}</span>;
        })}
      </div>
    ));
  })();

  const shapeRows = !grid && (() => {
    const widths = columns ?? [1.5, 0.8, 0.5];
    const shape = [dot ? '10px' : null, ...widths.map((width) => `${width}fr`)].filter(Boolean).join(' ');
    return Array.from({ length: count }, (_, row) => (
      <div key={row} className="grid h-11 items-center gap-[18px] px-2.5" style={{ gridTemplateColumns: shape }}>
        {dot && <span className={cn('h-[9px] w-[9px] rounded-full', loading ? 'bg-fill-hover' : 'bg-fill')} />}
        {widths.map((_, cell) => bar(cell, `${ghostBarWidth(row, cell) + 10}%`, row))}
      </div>
    ));
  })();

  return (
    <div className="relative" {...(loading ? { role: 'status', 'aria-busy': true, 'aria-label': t('common.loading') } : {})}>
      <div
        aria-hidden
        className={cn('pointer-events-none select-none', tone === 'failed' && 'opacity-60 grayscale')}
        style={loading ? undefined : ghostRowsMask(children != null)}
      >
        {listRows || shapeRows}
      </div>
      {children && <div className="absolute inset-x-0 top-6 flex justify-center px-4">{children}</div>}
    </div>
  );
}
