import { useState } from 'react';
import type { Scope } from '@/shared/api/scope';
import { useT } from '@/shared/i18n';
import { ConfirmDialog } from '@/shared/ui/ConfirmDialog';
import { isBuildActive, type BuildData } from '../model';
import { useCancelBuild, useDeleteBuild } from '../queries';

// useBuildActions holds a build's Cancel (while it is queued or running) and
// Delete (once it has finished), each confirmed, for its row and its page.
export function useBuildActions(scope: Scope, build: BuildData, onDeleted?: () => void) {
  const t = useT();
  const [confirm, setConfirm] = useState<'cancel' | 'delete' | null>(null);
  const cancelMutation = useCancelBuild(scope);
  const deleteMutation = useDeleteBuild(scope);
  const active = isBuildActive(build.status);
  const name = build.tag || build.id;

  const dialogs = (
    <ConfirmDialog
      isOpen={confirm !== null}
      title={confirm === 'cancel' ? t('images.cancelBuildConfirm', { id: name }) : t('images.deleteBuildConfirm', { id: name })}
      message={confirm === 'delete' ? t('images.deleteBuildConsequence') : undefined}
      confirmLabel={confirm === 'cancel' ? t('images.cancelBuild') : t('common.delete')}
      isLoading={cancelMutation.isPending || deleteMutation.isPending}
      variant={confirm === 'cancel' ? 'primary' : 'danger'}
      onCancel={() => setConfirm(null)}
      onConfirm={() => {
        if (confirm === 'cancel') cancelMutation.mutate(build.id, { onSuccess: () => setConfirm(null) });
        else deleteMutation.mutate(build.id, { onSuccess: () => { setConfirm(null); onDeleted?.(); } });
      }}
    />
  );

  return {
    canCancel: active,
    canDelete: !active,
    cancelPending: cancelMutation.isPending,
    deletePending: deleteMutation.isPending,
    requestCancel: () => setConfirm('cancel'),
    requestDelete: () => setConfirm('delete'),
    dialogs,
  };
}
