import type { Ref } from 'react';
import { cn } from '@/shared/lib/utils';

// FrostWindow is the window surface: one frosted sheet over the wallpaper
// that content sits on directly, without panels of its own. A clip path set
// on it through ref (never on a parent, which would cut its blur off from the
// wallpaper) can cut a floating sidebar out of it.
export function FrostWindow({ className, ref }: { className?: string; ref?: Ref<HTMLDivElement> }) {
  return <div ref={ref} aria-hidden className={cn('frost', className)} />;
}
