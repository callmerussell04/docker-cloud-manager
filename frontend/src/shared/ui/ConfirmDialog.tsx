import { useId } from 'react';
import { Button } from '@/shared/ui/Button';
import { Dialog } from '@/shared/ui/Dialog';
import { useT } from '@/shared/i18n';

interface ConfirmDialogProps {
  isOpen: boolean;
  // title is the question, naming the action and what it acts on.
  title: string;
  // message says what the action costs; a confirmation that deletes data
  // that cannot be recovered names what is lost.
  message?: string;
  confirmLabel: string;
  onCancel: () => void;
  onConfirm: () => void;
  isLoading?: boolean;
  // danger (a red action) for anything destructive, primary otherwise.
  variant?: 'danger' | 'primary';
}

// ConfirmDialog is the small glass dialog every destructive action asks
// through: the question, what it costs, then Cancel and the action. Focus
// starts on Cancel, and it never asks for a name to be typed.
export function ConfirmDialog({ isOpen, title, message, confirmLabel, onCancel, onConfirm, isLoading, variant = 'danger' }: ConfirmDialogProps) {
  const t = useT();
  const messageId = useId();

  return (
    <Dialog isOpen={isOpen} onClose={onCancel} label={title} describedBy={message ? messageId : undefined} role="alertdialog" className="max-w-[420px] gap-2.5 rounded-3xl p-6">
      <h2 className="text-lg font-bold leading-snug tracking-tight">{title}</h2>
      {message && <p id={messageId} className="leading-normal text-label-2">{message}</p>}
      <div className="mt-2 flex flex-wrap justify-end gap-2">
        <Button type="button" variant="secondary" className="rounded-full" onClick={onCancel} data-autofocus>{t('common.cancel')}</Button>
        <Button type="button" variant={variant} className="rounded-full" onClick={onConfirm} isLoading={isLoading}>{confirmLabel}</Button>
      </div>
    </Dialog>
  );
}
