import { type KeyboardEvent as ReactKeyboardEvent, type ReactNode, useEffect, useRef } from 'react';
import { createPortal } from 'react-dom';
import { X } from 'lucide-react';
import { useT } from '@/shared/i18n';
import { cn } from '@/shared/lib/utils';
import { Glass } from './Glass';

interface DialogProps {
  isOpen: boolean;
  onClose: () => void;
  // label names the dialog for assistive technology.
  label?: string;
  // describedBy is the id of the text that says what the dialog is about.
  describedBy?: string;
  // alertdialog for a confirmation that interrupts, dialog otherwise.
  role?: 'dialog' | 'alertdialog';
  className?: string;
  children: ReactNode;
}

const focusableSelector = 'a[href], button:not([disabled]), input:not([disabled]), select:not([disabled]), textarea:not([disabled]), [tabindex]:not([tabindex="-1"])';

// The open dialogs, innermost last: Escape closes only the innermost.
const openDialogs: object[] = [];

// Dialog is a dense Glass panel portalled to the body over a scrim. Focus
// moves into it on open (to an element marked data-autofocus, else the
// panel), Tab stays inside it, Escape or a click on the scrim closes it, and
// focus returns to where it was when it closes.
export function Dialog({ isOpen, onClose, label, describedBy, role = 'dialog', className, children }: DialogProps) {
  const dialogRef = useRef<HTMLDivElement>(null);
  const onCloseRef = useRef(onClose);
  useEffect(() => {
    onCloseRef.current = onClose;
  }, [onClose]);

  useEffect(() => {
    if (!isOpen) return;
    const token = {};
    openDialogs.push(token);
    const previousOverflow = document.body.style.overflow;
    document.body.style.overflow = 'hidden';
    const previous = document.activeElement instanceof HTMLElement ? document.activeElement : null;
    const dialog = dialogRef.current;
    if (dialog && !dialog.contains(document.activeElement)) {
      (dialog.querySelector<HTMLElement>('[data-autofocus]') ?? dialog).focus();
    }
    const handleKeyDown = (event: KeyboardEvent) => {
      if (event.key !== 'Escape' || event.defaultPrevented || openDialogs[openDialogs.length - 1] !== token) return;
      onCloseRef.current();
    };
    window.addEventListener('keydown', handleKeyDown);
    return () => {
      window.removeEventListener('keydown', handleKeyDown);
      openDialogs.splice(openDialogs.indexOf(token), 1);
      document.body.style.overflow = previousOverflow;
      previous?.focus();
    };
  }, [isOpen]);

  const keepFocusInside = (event: ReactKeyboardEvent<HTMLDivElement>) => {
    const dialog = dialogRef.current;
    if (event.key !== 'Tab' || !dialog) return;
    const focusable = dialog.querySelectorAll<HTMLElement>(focusableSelector);
    if (focusable.length === 0) {
      event.preventDefault();
      return;
    }
    const first = focusable[0];
    const last = focusable[focusable.length - 1];
    const active = document.activeElement;
    if (event.shiftKey && (active === first || active === dialog)) {
      event.preventDefault();
      last.focus();
    } else if (!event.shiftKey && active === last) {
      event.preventDefault();
      first.focus();
    }
  };

  if (!isOpen) return null;

  return createPortal(
    <div
      ref={dialogRef}
      tabIndex={-1}
      onKeyDown={keepFocusInside}
      className="fixed inset-0 z-[100] flex items-center justify-center p-3 outline-none sm:p-4"
      role={role}
      aria-modal="true"
      aria-label={label}
      aria-describedby={describedBy}
    >
      <div className="absolute inset-0 bg-scrim animate-in fade-in" onClick={onClose} />
      <Glass
        material="dense"
        className={cn('relative z-[101] flex max-h-[calc(100vh-2rem)] w-full flex-col overflow-hidden animate-in fade-in zoom-in-95', className)}
      >
        {children}
      </Glass>
    </div>,
    document.body,
  );
}

interface DialogHeaderProps {
  title: string;
  // titleId lets the dialog's form be labelled by the title.
  titleId?: string;
  onClose: () => void;
}

// DialogHeader is a form dialog's title with a round close button.
export function DialogHeader({ title, titleId, onClose }: DialogHeaderProps) {
  const t = useT();
  return (
    <div className="flex shrink-0 items-center justify-between gap-3 px-6 pb-2 pt-5">
      <h2 id={titleId} className="truncate text-lg font-bold tracking-tight">{title}</h2>
      <button
        type="button"
        onClick={onClose}
        className="-mr-2 inline-flex h-8 w-8 shrink-0 items-center justify-center rounded-full text-label-2 transition-colors hover:bg-fill hover:text-label"
        aria-label={t('common.close')}
        title={t('common.close')}
      >
        <X aria-hidden className="h-[18px] w-[18px]" />
      </button>
    </div>
  );
}

// DialogFooter is a form dialog's row of buttons under a hairline, ending
// with its action.
export function DialogFooter({ children }: { children: ReactNode }) {
  return <div className="flex shrink-0 items-center gap-2 border-t border-separator px-6 py-4">{children}</div>;
}
