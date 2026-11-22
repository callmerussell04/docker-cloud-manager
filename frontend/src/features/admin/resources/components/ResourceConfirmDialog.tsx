import { ConfirmDialog } from '@/shared/ui/ConfirmDialog';
import { useT } from '@/shared/i18n';

// A confirmation an all-users table asks for before it runs an action.
export interface ConfirmRequest {
  // title is the question; message, when set, says what the action costs.
  title: string;
  message?: string;
  run: () => void;
  confirmLabel?: string;
  variant?: 'danger' | 'primary';
}

interface ResourceConfirmDialogProps {
  request: ConfirmRequest | null;
  isLoading: boolean;
  onClose: () => void;
}

export function ResourceConfirmDialog({ request, isLoading, onClose }: ResourceConfirmDialogProps) {
  const t = useT();

  return (
    <ConfirmDialog
      isOpen={!!request}
      title={request?.title || ''}
      message={request?.message}
      confirmLabel={request?.confirmLabel || t('common.delete')}
      variant={request?.variant || 'danger'}
      isLoading={isLoading}
      onCancel={onClose}
      onConfirm={() => {
        request?.run();
        onClose();
      }}
    />
  );
}
