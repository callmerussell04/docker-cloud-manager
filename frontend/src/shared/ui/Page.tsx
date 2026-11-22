import type { ReactNode } from 'react';
import { cn } from '@/shared/lib/utils';

interface PageProps {
  // toolbar is the page's Toolbar, floating over the top.
  toolbar: ReactNode;
  className?: string;
  children: ReactNode;
}

// Page fills the shell's content area: the toolbar floats over the top and
// the content scrolls beneath it, fading out as it passes under.
export function Page({ toolbar, className, children }: PageProps) {
  return (
    <div className="relative h-full">
      {toolbar}
      <div className="toolbar-fade h-full overflow-y-auto px-3 pb-8 pt-[76px]">
        <div className={cn('space-y-6', className)}>{children}</div>
      </div>
    </div>
  );
}
