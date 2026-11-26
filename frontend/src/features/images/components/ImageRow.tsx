import { useState } from 'react';
import { Copy, Hammer, Play, Trash2 } from 'lucide-react';
import { getServerErrorMessage } from '@/shared/api/apiError';
import type { Scope } from '@/shared/api/scope';
import { useT } from '@/shared/i18n';
import { ageOf } from '@/shared/lib/age';
import { useNowSeconds } from '@/shared/lib/hooks';
import { joinNames } from '@/shared/lib/names';
import { cn, formatBytes } from '@/shared/lib/utils';
import { ConfirmDialog } from '@/shared/ui/ConfirmDialog';
import { ListRow, RowEnd, RowMenu, RowQuickAction } from '@/shared/ui/List';
import { MenuItem, MenuNote, MenuSeparator } from '@/shared/ui/Menu';
import { useNotificationStore } from '@/shared/ui/notificationStore';
import { useShellLayout } from '@/shared/ui/shellLayout';
import { Dot } from '@/shared/ui/StatusDot';
import {
  imageDeleteBlock,
  imageNameLines,
  imageStatusLabel,
  imageTone,
  imageUsersLabel,
  isImageAvailable,
  type ImageData,
} from '../model';
import { useDeleteImage } from '../queries';
import { imageListLayout } from './listLayout';

interface ImageRowProps {
  image: ImageData;
  // onRun opens the create container dialog with the image; left out where
  // the image is not the user's own to run.
  onRun?: (image: ImageData) => void;
  // onBuildAgain opens the build dialog for the image's tag; left out while
  // builds are off.
  onBuildAgain?: (image: ImageData) => void;
  // scope is whose list the row is in: the user's own (the default) or every
  // user's, which the admin view shows with each row's owner.
  scope?: Scope;
  // owner names the row's owner, in a column after the name; given only in
  // the admin view.
  owner?: string;
}

// ImageRow is an image's line in the list: its repository and tag with the
// registry path beneath, its status with the failure after it, the
// containers using it, its size and its age. An image has no page, so the
// row opens nothing; its menu holds Run a container, Build again, Copy the
// tag and Delete, disabled with the reason while a container uses the image
// or it is not ready. Compact rows are the name, then status, size and age.
export function ImageRow({ image, onRun, onBuildAgain, scope = 'own', owner }: ImageRowProps) {
  const t = useT();
  const compact = useShellLayout().rowsCompact;
  const now = useNowSeconds(60_000);
  const notify = useNotificationStore((state) => state.notify);
  const [isConfirmOpen, setIsConfirmOpen] = useState(false);
  const deleteMutation = useDeleteImage(scope);

  const { name, path } = imageNameLines(image.tag);
  const failure = getServerErrorMessage(image.last_error, t);
  const statusLabel = imageStatusLabel(t, image.status);
  const age = ageOf(image.created_at, now);
  const ageLabel = t(`common.age.${age.unit}`, { count: age.count });
  const users = imageUsersLabel(t, image.used_by);
  const deleteBlock = imageDeleteBlock(image);
  const deleteReason = deleteBlock === 'inUse'
    ? t('images.deleteBlockedInUse', { names: joinNames(image.used_by) })
    : deleteBlock === 'failed' ? t('images.deleteBlockedFailed')
      : deleteBlock === 'busy' ? t('images.deleteBlockedBusy') : undefined;
  const available = isImageAvailable(image.status);
  const size = image.size_bytes > 0 ? formatBytes(image.size_bytes, 1) : '';
  const detail = failure ? <span className="text-red-text">{failure}</span> : null;

  const copyTag = () => {
    if (!navigator.clipboard) {
      notify(t('images.copyFailed'), 'error');
      return;
    }
    navigator.clipboard.writeText(image.tag).then(
      () => notify(t('images.tagCopied'), 'success'),
      () => notify(t('images.copyFailed'), 'error'),
    );
  };

  const menu = (
    <RowMenu label={t('images.rowActions', { name: image.tag })} sheetTitle={name}>
      {onRun && <MenuItem icon={Play} disabled={!available} onSelect={() => onRun(image)}>{t('images.run')}</MenuItem>}
      {onBuildAgain && <MenuItem icon={Hammer} onSelect={() => onBuildAgain(image)}>{t('images.buildAgain')}</MenuItem>}
      <MenuItem icon={Copy} onSelect={copyTag}>{t('images.copyTag')}</MenuItem>
      <MenuSeparator />
      <MenuItem icon={Trash2} danger disabled={!!deleteBlock || deleteMutation.isPending} title={deleteReason} onSelect={() => setIsConfirmOpen(true)}>
        {t('common.delete')}
      </MenuItem>
      {deleteReason && <MenuNote>{deleteReason}</MenuNote>}
    </RowMenu>
  );

  const confirm = (
    <ConfirmDialog
      isOpen={isConfirmOpen}
      title={t('images.deleteConfirm', { name: image.tag })}
      message={t('images.deleteConsequence')}
      confirmLabel={t('common.delete')}
      isLoading={deleteMutation.isPending}
      onCancel={() => setIsConfirmOpen(false)}
      onConfirm={() => deleteMutation.mutate(image.id, { onSuccess: () => setIsConfirmOpen(false) })}
    />
  );

  const quick = onRun && <RowQuickAction icon={Play} label={t('images.run')} disabled={!available} onClick={() => onRun(image)} />;

  return (
    <ListRow template={compact ? imageListLayout.compact : owner !== undefined ? imageListLayout.owned : imageListLayout.wide}>
      <div className="flex min-w-0 items-center gap-3.5">
        <Dot tone={imageTone(image.status)} />
        <div className="min-w-0 flex-1">
          <div className={cn('truncate font-semibold', compact ? 'text-[15px]' : 'text-[13.5px]')} title={image.tag}>
            {name}
          </div>
          {compact ? (
            <div className="mt-0.5 truncate text-[12.5px] text-label-2">
              <span className="text-label">{statusLabel}</span>
              {detail && <> · {detail}</>}
              {owner && ` · ${owner}`}
              {size && ` · ${size}`}
              {' · '}
              {ageLabel}
            </div>
          ) : (
            path && <div className="truncate text-[11.5px] text-label-3" title={path}>{path}</div>
          )}
        </div>
      </div>
      {!compact && (
        <>
          {owner !== undefined && <div className="truncate text-label-2" title={owner}>{owner}</div>}
          <div className="truncate" title={failure || statusLabel}>
            {statusLabel}
            {detail && <> · {detail}</>}
          </div>
          <div className={cn('truncate', !users && 'text-label-3')} title={joinNames(image.used_by) || undefined}>
            {users ?? t('images.notUsed')}
          </div>
          <div className="truncate tabular-nums">{size}</div>
        </>
      )}
      <RowEnd age={ageLabel} quick={quick} menu={<>{menu}{confirm}</>} />
    </ListRow>
  );
}
