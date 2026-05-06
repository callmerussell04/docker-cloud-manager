import { type HTMLAttributes, useEffect } from 'react';
import { createPortal } from 'react-dom';
import { X } from 'lucide-react';
import { cn } from '@/lib/utils';
import { Card } from './Card';
import { useT } from '@/lib/i18n';

interface ModalProps extends HTMLAttributes<HTMLDivElement> {
  isOpen: boolean;
  onClose: () => void;
  title?: string;
}

export function Modal({ isOpen, onClose, title, children, className, ...props }: ModalProps) {
  const t = useT();

  useEffect(() => {
    const previousOverflow = document.body.style.overflow;

    if (isOpen) {
      document.body.style.overflow = 'hidden';
    }

    return () => {
      document.body.style.overflow = previousOverflow;
    };
  }, [isOpen]);

  useEffect(() => {
    if (!isOpen) return;

    const handleKeyDown = (event: KeyboardEvent) => {
      if (event.key === 'Escape') {
        onClose();
      }
    };

    window.addEventListener('keydown', handleKeyDown);
    return () => window.removeEventListener('keydown', handleKeyDown);
  }, [isOpen, onClose]);

  if (!isOpen) return null;

  return createPortal(
    <div className="fixed inset-0 z-[100] flex items-center justify-center p-4" role="dialog" aria-modal="true" aria-label={title}>
      <div 
        className="absolute inset-0 bg-slate-900/20 dark:bg-black/40 backdrop-blur-sm animate-in fade-in"
        onClick={onClose}
      />
      
      <Card 
        className={cn(
          "relative z-[101] flex max-h-[calc(100vh-2rem)] w-full max-w-lg flex-col overflow-hidden p-0 shadow-2xl animate-in zoom-in-95 slide-in-from-bottom-4",
          className
        )}
        {...props}
      >
        <div className="flex shrink-0 items-center justify-between border-b border-slate-200/70 px-6 py-4 dark:border-slate-700/60">
          {title && <h2 className="text-xl font-semibold tracking-tight pr-4 truncate">{title}</h2>}
          <button
            onClick={onClose}
            className="p-1.5 rounded-full hover:bg-slate-200/50 dark:hover:bg-slate-800/50 transition-colors shrink-0"
            aria-label={t('common.closeModal')}
          >
            <X className="w-5 h-5 opacity-70" />
          </button>
        </div>
        <div className="min-h-0 flex-1 overflow-y-auto p-6">
          {children}
        </div>
      </Card>
    </div>,
    document.body
  );
}
