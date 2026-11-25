import { useState } from 'react';
import { Plus, RefreshCcw } from 'lucide-react';

import { failureReason } from '@/shared/api/apiError';
import { useT } from '@/shared/i18n';
import { useListState } from '@/shared/lib/hooks';
import { toggleSort, type ListStateSpec } from '@/shared/lib/listState';
import { ConfirmDialog } from '@/shared/ui/ConfirmDialog';
import { EmptyState } from '@/shared/ui/EmptyState';
import { ErrorState } from '@/shared/ui/ErrorState';
import { List, ListHead, type ListColumn } from '@/shared/ui/List';
import { LoadingRows } from '@/shared/ui/LoadingRows';
import { NoMatchesState } from '@/shared/ui/NoMatchesState';
import { Page } from '@/shared/ui/Page';
import { Pagination } from '@/shared/ui/Pagination';
import { useShellLayout } from '@/shared/ui/shellLayout';
import { Toolbar, ToolbarButton, ToolbarPrimary, ToolbarSearch } from '@/shared/ui/Toolbar';
import { userListLayout } from '../components/listLayout';
import { UserFormDialog } from '../components/UserFormDialog';
import { UserQuotaDialog } from '../components/UserQuotaDialog';
import { UserRow } from '../components/UserRow';
import { defaultUserSort, matchesUserSearch, sortUsers, userSortKeys, type AdminUser, type UserSortKey } from '../model';
import { useAdminUsers, useDeactivateAdminUser, useReactivateAdminUser } from '../queries';

// The users list has no filter; its one filter value keeps the URL state
// shape every list shares.
const listSpec: ListStateSpec<'all', UserSortKey> = {
  filters: ['all'],
  sortKeys: userSortKeys,
  defaultSort: defaultUserSort,
};

// The server's largest page; the sort and the search apply to the page shown.
const pageSize = 100;

// The user dialog shows a new account, or the one being edited.
type FormTarget = { user: AdminUser | null } | null;

export function AdminUsersPage() {
  const t = useT();
  const compact = useShellLayout().rowsCompact;
  const [view, changeView] = useListState(listSpec);
  const [form, setForm] = useState<FormTarget>(null);
  const [quotaUser, setQuotaUser] = useState<AdminUser | null>(null);
  const [deactivateUser, setDeactivateUser] = useState<AdminUser | null>(null);

  const { data, isLoading, isFetching, isError, error, refetch } = useAdminUsers(view.page, pageSize);
  const deactivateMutation = useDeactivateAdminUser();
  const reactivateMutation = useReactivateAdminUser();
  // Deactivating an already deactivated user stops the workloads that remain.
  const stopWorkloadsOnly = deactivateUser?.status === 'deactivated';

  const users = data?.items ?? [];
  const shown = sortUsers(users.filter((user) => matchesUserSearch(user, view.search)), view.sort);

  const columns: ListColumn<UserSortKey>[] = [
    { key: 'name', label: t('admin.users.column.user') },
    { key: 'role', label: t('admin.users.role') },
    { key: 'status', label: t('common.status') },
    { key: 'quota', label: t('admin.users.column.quota') },
  ];

  const content = () => {
    if (isLoading) return <LoadingRows columns={compact ? [1] : [1.6, 0.6, 0.7, 1.5, 0.4]} />;
    if (isError) {
      return <ErrorState title={t('admin.users.loadFailed')} message={failureReason(error, t)} onRetry={() => refetch()} isRetrying={isFetching} />;
    }
    if (users.length === 0) {
      return (
        <EmptyState
          title={t('admin.users.notFound')}
          description={t('admin.users.emptyDescription')}
          action={{ label: t('admin.users.create'), icon: Plus, onClick: () => setForm({ user: null }) }}
        />
      );
    }
    if (shown.length === 0) return <NoMatchesState query={view.search} onClear={() => changeView({ search: '' })} />;
    return (
      <List label={t('admin.users.title')}>
        {shown.map((user) => (
          <UserRow
            key={user.user_id}
            user={user}
            busy={deactivateMutation.isPending || reactivateMutation.isPending}
            onEdit={(target) => setForm({ user: target })}
            onQuota={setQuotaUser}
            onDeactivate={setDeactivateUser}
            onActivate={(target) => reactivateMutation.mutate(target.user_id)}
          />
        ))}
      </List>
    );
  };

  return (
    <Page
      toolbar={
        <Toolbar
          title={t('admin.users.title')}
          count={data?.total_count}
          actions={
            <>
              <ToolbarSearch label={t('admin.users.searchLabel')} value={view.search} onChange={(search) => changeView({ search })} />
              <ToolbarButton icon={RefreshCcw} label={t('common.refresh')} busy={isFetching} onClick={() => refetch()} />
            </>
          }
          primary={<ToolbarPrimary icon={Plus} onClick={() => setForm({ user: null })} label={t('admin.users.create')} />}
        />
      }
      head={(isLoading || shown.length > 0) && (
        <ListHead columns={columns} template={userListLayout.wide} sort={view.sort} onSort={(key) => changeView({ sort: toggleSort(view.sort, key) })} />
      )}
      className="space-y-2"
    >
      {content()}
      {data && <Pagination currentPage={view.page} pageSize={pageSize} totalItems={data.total_count} onPageChange={(page) => changeView({ page })} />}

      <UserFormDialog isOpen={form !== null} user={form?.user ?? null} onClose={() => setForm(null)} />
      <UserQuotaDialog user={quotaUser} onClose={() => setQuotaUser(null)} />
      <ConfirmDialog
        isOpen={!!deactivateUser}
        title={
          stopWorkloadsOnly
            ? t('admin.users.stopWorkloadsTitle', { name: deactivateUser?.username || '' })
            : t('admin.users.deactivateTitle', { name: deactivateUser?.username || '' })
        }
        message={stopWorkloadsOnly ? t('admin.users.stopWorkloadsConfirm') : t('admin.users.deactivateConfirm')}
        confirmLabel={stopWorkloadsOnly ? t('admin.users.stopWorkloads') : t('admin.users.deactivate')}
        isLoading={deactivateMutation.isPending}
        onCancel={() => setDeactivateUser(null)}
        onConfirm={() => {
          if (deactivateUser) {
            deactivateMutation.mutate(deactivateUser.user_id, { onSuccess: () => setDeactivateUser(null) });
          }
        }}
      />
    </Page>
  );
}
