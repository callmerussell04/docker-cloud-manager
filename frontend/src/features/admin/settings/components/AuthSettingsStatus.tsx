import type { TFunction } from '@/shared/i18n';
import { cn } from '@/shared/lib/utils';
import { type AuthConfig, unreachableProviders } from '@/features/auth/model';

/** Shows the sign-in settings, which come from bootstrap configuration and cannot be changed here. */
export function AuthSettingsStatus({ authConfig, isLoading, isError, t }: {
  authConfig?: AuthConfig;
  isLoading: boolean;
  isError: boolean;
  t: TFunction;
}) {
  if (isLoading) {
    return <div className="h-24 animate-pulse rounded-xl bg-white/40 dark:bg-slate-900/40" />;
  }
  if (isError) {
    return <p className="py-5 text-sm text-red-600 dark:text-red-400">{t('admin.settings.authStatusUnavailable')}</p>;
  }

  const providers = authConfig?.oidc_providers.map((provider) => provider.name).filter(Boolean) ?? [];
  const unreachable = unreachableProviders(authConfig);

  return (
    <div className="divide-y divide-slate-200/70 dark:divide-slate-700/50">
      <StatusRow
        label={t('admin.settings.field.local_login_enabled.label')}
        description={t('admin.settings.field.local_login_enabled.hint')}
        enabled={authConfig?.local_login_enabled ?? false}
        t={t}
      />
      <StatusRow
        label={t('admin.settings.field.local_register_enabled.label')}
        description={t('admin.settings.field.local_register_enabled.hint')}
        enabled={authConfig?.local_register_enabled ?? false}
        t={t}
      />
      <StatusRow
        label={t('admin.settings.field.external_sso_enabled.label')}
        description={t('admin.settings.field.external_sso_enabled.hint', {
          providers: providers.length ? providers.join(', ') : t('admin.settings.none'),
        })}
        enabled={providers.length > 0}
        t={t}
      />
      {providers.length > 0 && (
        <StatusRow
          label={t('admin.settings.field.external_sso_reachable.label')}
          description={t('admin.settings.field.external_sso_reachable.hint')}
          enabled={unreachable.length === 0}
          value={unreachable.length === 0
            ? t('admin.settings.reachable')
            : t('admin.settings.unreachable', { providers: unreachable.join(', ') })}
          t={t}
        />
      )}
    </div>
  );
}

function StatusRow({ label, description, enabled, value, t }: {
  label: string;
  description: string;
  enabled: boolean;
  /** Replaces the Enabled/Disabled text. */
  value?: string;
  t: TFunction;
}) {
  return (
    <div className="grid gap-3 py-5 md:grid-cols-[minmax(0,1fr)_minmax(0,20rem)] md:gap-8">
      <div className="min-w-0 space-y-1">
        <p className="text-sm font-medium leading-none">{label}</p>
        <p className="text-sm leading-relaxed text-slate-500 dark:text-slate-400">{description}</p>
      </div>
      <p className={cn(
        'self-start rounded-xl border px-4 py-2.5 text-sm font-medium',
        enabled
          ? 'border-separator bg-fill text-accent'
          : 'border-white/40 bg-white/40 text-slate-700 dark:border-slate-700/50 dark:bg-slate-900/40 dark:text-slate-300',
      )}>
        {value ?? (enabled ? t('admin.settings.enabled') : t('admin.settings.disabled'))}
      </p>
    </div>
  );
}
