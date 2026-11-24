import { Undo2 } from 'lucide-react';
import { cn } from '@/shared/lib/utils';
import { Button } from './Button';
import { Glass } from './Glass';
import { useShellLayout } from './shellLayout';

interface SaveBarProps {
  // message says what is unsaved, such as how many changes; invalid shows it
  // in red when something must be fixed before saving.
  message: string;
  invalid?: boolean;
  discardLabel: string;
  saveLabel: string;
  onDiscard: () => void;
  saving?: boolean;
  // label names the bar for assistive technology.
  label: string;
}

// SaveBar is the floating glass bar of a form with unsaved changes: what is
// unsaved, Discard and Save. It floats at the bottom centre of the page (above
// the phone tab bar) and is rendered only while there is something to save,
// inside the form, whose submit its Save button is.
export function SaveBar({ message, invalid = false, discardLabel, saveLabel, onDiscard, saving = false, label }: SaveBarProps) {
  const { tabBar } = useShellLayout();
  return (
    <div
      role="region"
      aria-label={label}
      className={cn(
        'pointer-events-none absolute inset-x-3 z-10 flex justify-center',
        tabBar ? 'bottom-[96px]' : 'bottom-5',
      )}
    >
      <Glass
        material="dense"
        className={cn(
          'pointer-events-auto flex max-w-full items-center gap-2 py-1.5 pl-4 pr-1.5 animate-in fade-in slide-in-from-bottom-2',
          tabBar ? 'rounded-[24px]' : 'rounded-full',
        )}
      >
        <span aria-hidden className={cn('h-[7px] w-[7px] shrink-0 rounded-full', invalid ? 'bg-red' : 'bg-accent-fill')} />
        <p
          aria-live="polite"
          // On a phone the message may take two lines rather than be cut off.
          className={cn('min-w-0 pr-1 font-medium', tabBar ? 'line-clamp-2 text-[12.5px] leading-tight' : 'truncate', invalid ? 'text-red-text' : 'text-label')}
        >
          {message}
        </p>
        <Button
          type="button"
          variant="ghost"
          onClick={onDiscard}
          disabled={saving}
          // On a phone Discard is its icon alone, so the message keeps its room.
          aria-label={tabBar ? discardLabel : undefined}
          title={tabBar ? discardLabel : undefined}
          className={cn('h-9 shrink-0 rounded-full', tabBar ? 'w-9 px-0' : 'px-3')}
        >
          <Undo2 aria-hidden className={cn('h-3.5 w-3.5', !tabBar && 'mr-1.5')} />
          {!tabBar && discardLabel}
        </Button>
        <Button type="submit" isLoading={saving} className="h-9 shrink-0 rounded-full px-4 font-semibold">
          {saveLabel}
        </Button>
      </Glass>
    </div>
  );
}
