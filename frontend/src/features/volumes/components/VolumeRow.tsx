import { useState } from 'react';
import { Trash2 } from 'lucide-react';
import { getServerErrorMessage } from '@/shared/api/apiError';
import { useT } from '@/shared/i18n';
import { ageOf } from '@/shared/lib/age';
import { useNowSeconds } from '@/shared/lib/hooks';
import { cn, formatBytes } from '@/shared/lib/utils';
import { ConfirmDialog } from '@/shared/ui/ConfirmDialog';
import { ListRow, RowEnd, RowMenu } from '@/shared/ui/List';
import { MenuItem, MenuNote } from '@/shared/ui/Menu';
import { useShellLayout } from '@/shared/ui/shellLayout';
import { Dot } from '@/shared/ui/StatusDot';
import {
  volumeDeleteBlock,
  volumeDeleteConsequence,
  volumeStatusLabel,
  volumeTone,
  volumeUserNames,
  volumeUsersLabel,
  type VolumeData,
} from '../model';
import { useDeleteVolume } from '../queries';
import { volumeListLayout } from './listLayout';

interface VolumeRowProps {
  volume: VolumeData;
}

// VolumeRow is a volume's line in the list: its name, its status with the
// failure after it, the space it takes of its size, the containers using it
// and its age. A volume has no page, so the row opens nothing; its menu holds
// Delete, disabled with the reason while containers use the volume or a
// worker is still creating or deleting it. Compact rows are the name, then
// status, size and age.
export function VolumeRow({ volume }: VolumeRowProps) {
  const t = useT();
  const compact = useShellLayout().rowsCompact;
  const now = useNowSeconds(60_000);
  const [isConfirmOpen, setIsConfirmOpen] = useState(false);
  const deleteMutation = useDeleteVolume('own');

  const failure = getServerErrorMessage(volume.last_error, t);
  const statusLabel = volumeStatusLabel(t, volume.status);
  const age = ageOf(volume.created_at, now);
  const ageLabel = t(`common.age.${age.unit}`, { count: age.count });
  const users = volumeUsersLabel(t, volume.used_by);
  const deleteBlock = volumeDeleteBlock(volume);

  const measured = volume.usage_observed_at > 0;
  const overSize = measured && volume.size_bytes > 0 && volume.used_bytes > volume.size_bytes;
  const sizeLabel = measured
    ? t('volumes.usageOfSize', { used: formatBytes(volume.used_bytes, 1), size: formatBytes(volume.size_bytes, 1) })
    : formatBytes(volume.size_bytes, 1);
  const sizeTitle = [
    overSize ? t('volumes.overSize') : measured ? null : t('volumes.notMeasured'),
    volume.size_enforced ? null : t('volumes.sizeMeasuredHint'),
  ].filter(Boolean).join(' ');

  const size = (
    <span title={sizeTitle} className={cn('tabular-nums', overSize && 'text-red-text')}>{sizeLabel}</span>
  );
  const detail = failure ? <span className="text-red-text">{failure}</span> : null;

  const menu = (
    <RowMenu label={t('volumes.rowActions', { name: volume.name })} sheetTitle={volume.name}>
      <MenuItem icon={Trash2} danger disabled={!!deleteBlock || deleteMutation.isPending} onSelect={() => setIsConfirmOpen(true)}>
        {t('common.delete')}
      </MenuItem>
      {deleteBlock === 'inUse' && (
        <MenuNote>{t('volumes.deleteBlockedInUse', { names: volumeUserNames(volume.used_by) })}</MenuNote>
      )}
      {deleteBlock === 'busy' && <MenuNote>{t('volumes.deleteBlockedBusy')}</MenuNote>}
    </RowMenu>
  );

  const confirm = (
    <ConfirmDialog
      isOpen={isConfirmOpen}
      title={t('volumes.deleteConfirm', { name: volume.name })}
      message={volumeDeleteConsequence(t, volume)}
      confirmLabel={t('common.delete')}
      isLoading={deleteMutation.isPending}
      onCancel={() => setIsConfirmOpen(false)}
      onConfirm={() => deleteMutation.mutate(volume.id, { onSuccess: () => setIsConfirmOpen(false) })}
    />
  );

  return (
    <ListRow template={compact ? volumeListLayout.compact : volumeListLayout.wide}>
      <div className="flex min-w-0 items-center gap-3.5">
        <Dot tone={volumeTone(volume.status)} />
        <div className="min-w-0 flex-1">
          <div className={cn('truncate font-semibold', compact ? 'text-[15px]' : 'text-[13.5px]')} title={volume.name}>
            {volume.name}
          </div>
          {compact && (
            <div className="mt-0.5 truncate text-[12.5px] text-label-2">
              <span className="text-label">{statusLabel}</span>
              {detail && <> · {detail}</>}
              {' · '}
              {size}
              {' · '}
              {ageLabel}
            </div>
          )}
        </div>
      </div>
      {!compact && (
        <>
          <div className="truncate" title={failure || statusLabel}>
            {statusLabel}
            {detail && <> · {detail}</>}
          </div>
          <div className="truncate">{size}</div>
          <div className={cn('truncate', !users && 'text-label-3')} title={volumeUserNames(volume.used_by) || undefined}>
            {users ?? t('volumes.notUsed')}
          </div>
        </>
      )}
      <RowEnd age={ageLabel} menu={<>{menu}{confirm}</>} />
    </ListRow>
  );
}
