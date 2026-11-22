import type { ReactNode } from 'react';
import { X } from 'lucide-react';
import { cn } from '@/shared/lib/utils';
import { Dialog } from './Dialog';
import { useT } from '@/shared/i18n';

interface ModalProps {
  isOpen: boolean;
  onClose: () => void;
  title?: string;
  className?: string;
  children: ReactNode;
}

// Modal is a Dialog with a title bar and a close button, for forms.
export function Modal({ isOpen, onClose, title, children, className }: ModalProps) {
  const t = useT();

  return (
    <Dialog isOpen={isOpen} onClose={onClose} label={title} className={cn('max-w-lg rounded-2xl', className)}>
      <div className="flex shrink-0 items-center justify-between border-b border-separator px-6 py-4">
        {title && <h2 className="truncate pr-4 text-xl font-semibold tracking-tight">{title}</h2>}
        <button
          type="button"
          onClick={onClose}
          className="ml-auto shrink-0 rounded-full p-1.5 transition-colors hover:bg-fill"
          aria-label={t('common.closeModal')}
          title={t('common.closeModal')}
        >
          <X className="h-5 w-5 opacity-70" />
        </button>
      </div>
      <div className="min-h-0 flex-1 overflow-y-auto p-6">
        {children}
      </div>
    </Dialog>
  );
}
