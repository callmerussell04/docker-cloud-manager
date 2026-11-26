import { Gauge, PowerOff, RotateCcw, SquarePen, UserX } from 'lucide-react';
import { useT } from '@/shared/i18n';
import { cn, formatBytes } from '@/shared/lib/utils';
import { ListRow, RowEnd, RowMenu, RowQuickAction } from '@/shared/ui/List';
import { MenuItem, MenuSeparator } from '@/shared/ui/Menu';
import { useShellLayout } from '@/shared/ui/shellLayout';
import { Dot } from '@/shared/ui/StatusDot';
import { userRoleLabel, userStatusLabel, userTone, type AdminUser } from '../model';
import { userListLayout } from './listLayout';

const MIB = 1024 * 1024;

interface UserRowProps {
  user: AdminUser;
  onEdit: (user: AdminUser) => void;
  onQuota: (user: AdminUser) => void;
  // onDeactivate deactivates an active user, or stops the workloads that
  // remain of a deactivated one; it asks first.
  onDeactivate: (user: AdminUser) => void;
  onActivate: (user: AdminUser) => void;
  // busy disables the account actions while one is being sent.
  busy: boolean;
}

// UserRow is an account's line in the users list: its name with the email and
// source beneath, its role, its status and its quota, custom or default, with
// the figures. The row opens the edit dialog; its menu holds Edit, Quota and
// then Deactivate, or for a deactivated user Activate and Stop workloads. A
// wide window shows Edit and Quota on the hovered row. Compact rows are the
// name, then status, role and quota kind.
export function UserRow({ user, onEdit, onQuota, onDeactivate, onActivate, busy }: UserRowProps) {
  const t = useT();
  const compact = useShellLayout().rowsCompact;
  const active = user.status === 'active';
  const role = userRoleLabel(t, user.role);
  const status = userStatusLabel(t, user.status);
  const quotaKind = user.quota_custom ? t('admin.users.quotaCustom') : t('admin.users.quotaDefault');
  const figures = t('admin.users.quotaFigures', {
    cpu: String(user.quota_cpu),
    ram: formatBytes(user.quota_ram_mb * MIB, 1),
    disk: formatBytes(user.quota_disk_mb * MIB, 1),
  });
  const source = user.auth_source === 'oidc' ? t('admin.users.sourceOidc') : t('admin.users.sourceLocal');

  const menu = (
    <RowMenu label={t('admin.users.rowActions', { name: user.username })} sheetTitle={user.username}>
      <MenuItem icon={SquarePen} onSelect={() => onEdit(user)}>{t('common.edit')}</MenuItem>
      <MenuItem icon={Gauge} onSelect={() => onQuota(user)}>{t('admin.users.editQuota')}</MenuItem>
      <MenuSeparator />
      {active ? (
        <MenuItem icon={UserX} danger disabled={busy} onSelect={() => onDeactivate(user)}>{t('admin.users.deactivate')}</MenuItem>
      ) : (
        <>
          <MenuItem icon={RotateCcw} disabled={busy} onSelect={() => onActivate(user)}>{t('admin.users.activate')}</MenuItem>
          <MenuItem icon={PowerOff} danger disabled={busy} onSelect={() => onDeactivate(user)}>{t('admin.users.stopWorkloads')}</MenuItem>
        </>
      )}
    </RowMenu>
  );

  const quick = (
    <>
      <RowQuickAction icon={SquarePen} label={t('common.edit')} onClick={() => onEdit(user)} />
      <RowQuickAction icon={Gauge} label={t('admin.users.editQuota')} onClick={() => onQuota(user)} />
    </>
  );

  return (
    <ListRow template={compact ? userListLayout.compact : userListLayout.wide} onOpen={() => onEdit(user)}>
      <div className="flex min-w-0 items-center gap-3.5">
        <Dot tone={userTone(user.status)} />
        <div className="min-w-0 flex-1">
          <div className={cn('truncate font-semibold', compact ? 'text-[15px]' : 'text-[13.5px]')} title={user.username}>
            {user.username}
          </div>
          {compact ? (
            <div className="mt-0.5 truncate text-[12.5px] text-label-2">
              <span className="text-label">{status}</span>
              {` · ${role} · ${t('admin.users.quotaKind', { kind: quotaKind })}`}
            </div>
          ) : (
            <div className="truncate text-[11.5px] text-label-3" title={`${user.email} · ${source}`}>
              {user.email} · {source}
            </div>
          )}
        </div>
      </div>
      {!compact && (
        <>
          <div className="truncate">{role}</div>
          <div className="truncate">{status}</div>
          <div className="min-w-0 truncate" title={figures}>
            <span className={cn(user.quota_custom ? 'font-medium text-label' : 'text-label-2')}>{quotaKind}</span>
            <span className="text-label-3"> · {figures}</span>
          </div>
        </>
      )}
      <RowEnd quick={quick} menu={menu} />
    </ListRow>
  );
}
