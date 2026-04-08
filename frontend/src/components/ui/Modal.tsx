import { type HTMLAttributes, useEffect } from 'react';
import { X } from 'lucide-react';
import { cn } from '@/lib/utils';
import { Card } from './Card';

interface ModalProps extends HTMLAttributes<HTMLDivElement> {
  isOpen: boolean;
  onClose: () => void;
  title?: string;
}

export function Modal({ isOpen, onClose, title, children, className, ...props }: ModalProps) {
  useEffect(() => {
    if (isOpen) {
      document.body.style.overflow = 'hidden';
    } else {
      document.body.style.overflow = 'unset';
    }
    return () => {
      document.body.style.overflow = 'unset';
    };
  }, [isOpen]);

  if (!isOpen) return null;

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center">
      <div 
        className="absolute inset-0 bg-slate-900/20 dark:bg-black/40 backdrop-blur-sm animate-in fade-in"
        onClick={onClose}
      />
      
      <Card 
        className={cn(
          "relative z-50 w-full max-w-lg p-6 shadow-2xl animate-in zoom-in-95 slide-in-from-bottom-4 mx-4",
          className
        )}
        {...props}
      >
        <div className="flex items-center justify-between mb-6">
          {title && <h2 className="text-xl font-semibold tracking-tight">{title}</h2>}
          <button
            onClick={onClose}
            className="p-1.5 rounded-full hover:bg-slate-200/50 dark:hover:bg-slate-800/50 transition-colors"
          >
            <X className="w-5 h-5 opacity-70" />
          </button>
        </div>
        {children}
      </Card>
    </div>
  );
}