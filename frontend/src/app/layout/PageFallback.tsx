import { Loader2 } from 'lucide-react';
import { cn } from '@/shared/lib/utils';

interface PageFallbackProps {
  fullScreen?: boolean;
}

// PageFallback is shown while a lazily loaded page's code downloads.
export function PageFallback({ fullScreen = false }: PageFallbackProps) {
  return (
    <div className={cn('flex items-center justify-center', fullScreen ? 'min-h-screen' : 'py-24')} role="status">
      <Loader2 className="h-8 w-8 animate-spin text-label-3" />
    </div>
  );
}
