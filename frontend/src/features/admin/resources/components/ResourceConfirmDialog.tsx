import { ConfirmDialog } from '@/shared/ui/ConfirmDialog';
import { useT } from '@/shared/i18n';

// A confirmation an all-users table asks for before it runs an action.
export interface ConfirmRequest {
  message: string;
  run: () => void;
  confirmLabel?: string;
  variant?: 'danger' | 'secondary';
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
      title={t('confirm.title')}
      message={request?.message || ''}
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
