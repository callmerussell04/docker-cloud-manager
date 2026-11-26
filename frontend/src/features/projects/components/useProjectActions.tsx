import { useState } from 'react';
import { Play, Square, type LucideIcon } from 'lucide-react';
import type { Scope } from '@/shared/api/scope';
import { useT, type TranslationKey } from '@/shared/i18n';
import { ConfirmDialog } from '@/shared/ui/ConfirmDialog';
import { blockedProjectActions, type ProjectData } from '../model';
import { useProjectAction } from '../queries';

// What each confirmation asks and what its action is called. Canceling a
// deployment removes the containers it created, so it confirms in red too.
const confirmations: Record<'cancel' | 'delete', { title: TranslationKey; message: TranslationKey; action: TranslationKey }> = {
  cancel: { title: 'projects.cancelConfirm', message: 'projects.cancelConsequence', action: 'projects.cancel' },
  delete: { title: 'projects.deleteConfirm', message: 'projects.deleteConsequence', action: 'common.delete' },
};

export interface ProjectControl {
  icon: LucideIcon;
  label: string;
  disabled: boolean;
  run: () => void;
}

// useProjectActions gives a project's row and its page the same controls:
// the primary one, which is Cancel deployment while that is possible and
// Start or Stop otherwise (Stop while it runs), and Delete, each asking first
// where it should, with the confirmations to render beside them. onDeleted
// runs once Core accepts the delete.
export function useProjectActions(scope: Scope, project: ProjectData, onDeleted?: () => void) {
  const t = useT();
  const mutation = useProjectAction(scope);
  const [confirm, setConfirm] = useState<'cancel' | 'delete' | null>(null);
  const blocked = blockedProjectActions(project.status);
  const pending = mutation.isPending;
  const running = project.status === 'running';

  const canCancel = !blocked.cancel;
  const primary: ProjectControl = canCancel
    ? { icon: Square, label: t('projects.cancel'), disabled: pending, run: () => setConfirm('cancel') }
    : running
      ? { icon: Square, label: t('containers.stop'), disabled: blocked.stop || pending, run: () => mutation.mutate({ id: project.id, action: 'stop' }) }
      : { icon: Play, label: t('containers.start'), disabled: blocked.start || pending, run: () => mutation.mutate({ id: project.id, action: 'start' }) };

  const shown = confirm ? confirmations[confirm] : confirmations.delete;
  const dialogs = (
    <ConfirmDialog
      isOpen={confirm !== null}
      title={t(shown.title, { name: project.name })}
      message={t(shown.message)}
      confirmLabel={t(shown.action)}
      isLoading={pending}
      onCancel={() => setConfirm(null)}
      onConfirm={() => {
        if (!confirm) return;
        mutation.mutate(
          { id: project.id, action: confirm },
          {
            onSuccess: () => {
              setConfirm(null);
              if (confirm === 'delete') onDeleted?.();
            },
          },
        );
      }}
    />
  );

  return {
    primary,
    // canCancel says the primary control is Cancel deployment.
    canCancel,
    deleteDisabled: blocked.delete || pending,
    requestDelete: () => setConfirm('delete'),
    dialogs,
  };
}
