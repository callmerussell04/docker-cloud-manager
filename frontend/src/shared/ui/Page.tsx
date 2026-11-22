import type { ReactNode } from 'react';
import { cn } from '@/shared/lib/utils';
import { useShellLayout } from './shellLayout';

interface PageProps {
  // toolbar is the page's Toolbar, floating over the top.
  toolbar: ReactNode;
  className?: string;
  children: ReactNode;
}

// Page fills the shell's content area: the toolbar floats over the top and
// the content scrolls beneath it, fading out as it passes under. The scroll
// area starts below the toolbar however many rows it has (see .page in
// app/index.css), and ends above the phone tab bar.
export function Page({ toolbar, className, children }: PageProps) {
  const { tabBar } = useShellLayout();
  return (
    <div className="page relative h-full">
      {toolbar}
      <div className={cn('page-scroll toolbar-fade h-full overflow-y-auto px-3', tabBar ? 'pb-[104px]' : 'pb-8')}>
        <div className={cn('space-y-6', className)}>{children}</div>
      </div>
    </div>
  );
}
