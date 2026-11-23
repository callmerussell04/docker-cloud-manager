import type { ReactNode } from 'react';
import { cn } from '@/shared/lib/utils';
import { useShellLayout } from './shellLayout';

interface PageProps {
  // toolbar is the page's Toolbar, floating over the top.
  toolbar: ReactNode;
  // head is a list's column labels (ListHead), fixed under the toolbar while
  // the rows scroll beneath it.
  head?: ReactNode;
  // fill gives the content the whole height below the toolbar instead of a
  // scroll area, for a page whose parts scroll on their own, as the
  // container page's console and side column do.
  fill?: boolean;
  className?: string;
  children: ReactNode;
}

// Page fills the shell's content area: the toolbar floats over the top and
// the content scrolls beneath it, fading out as it passes under. The scroll
// area starts below the toolbar however many rows it has (see .page in
// app/index.css), and ends above the phone tab bar.
export function Page({ toolbar, head, fill = false, className, children }: PageProps) {
  const { tabBar } = useShellLayout();
  return (
    <div className="page relative h-full">
      {toolbar}
      {head != null && head !== false && <div className="list-head-slot absolute inset-x-3 z-[5]">{head}</div>}
      <div
        className={cn(
          'page-scroll toolbar-fade h-full px-3',
          fill ? 'overflow-hidden' : 'overflow-y-auto',
          tabBar ? 'pb-[104px]' : fill ? 'pb-3.5' : 'pb-8',
        )}
      >
        <div className={cn(fill ? 'h-full' : 'space-y-6', className)}>{children}</div>
      </div>
    </div>
  );
}
