import { cn } from '@/shared/lib/utils';

// FrostWindow is the window surface: one frosted sheet over the wallpaper
// that content sits on directly, without panels of its own.
export function FrostWindow({ className }: { className?: string }) {
  return <div aria-hidden className={cn('frost', className)} />;
}
