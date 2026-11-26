import type { TFunction } from '@/shared/i18n';
import { StatusDot } from '@/shared/ui/StatusDot';
import type { StatusTone } from '@/shared/ui/statusTone';
import { LoadingRows } from '@/shared/ui/LoadingRows';
import { ErrorState } from '@/shared/ui/ErrorState';
import { type AuthConfig, unreachableProviders } from '@/features/auth/model';

/** Shows the sign-in settings, which come from bootstrap configuration and cannot be changed here. */
export function AuthSettingsStatus({ authConfig, isLoading, isError, onRetry, isRetrying, t }: {
  authConfig?: AuthConfig;
  isLoading: boolean;
  isError: boolean;
  onRetry: () => void;
  isRetrying: boolean;
  t: TFunction;
}) {
  if (isLoading) {
    return <LoadingRows rows={2} dot={false} columns={[1]} />;
  }
  if (isError) {
    return <ErrorState compact title={t('admin.settings.authStatusUnavailable')} onRetry={onRetry} isRetrying={isRetrying} />;
  }

  const providers = authConfig?.oidc_providers.map((provider) => provider.name).filter(Boolean) ?? [];
  const unreachable = unreachableProviders(authConfig);

  return (
    <div>
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
          tone={unreachable.length === 0 ? 'ok' : 'bad'}
          value={unreachable.length === 0
            ? t('admin.settings.reachable')
            : t('admin.settings.unreachable', { providers: unreachable.join(', ') })}
          t={t}
        />
      )}
    </div>
  );
}

/** One read-only sign-in setting: its label and description beside its state. */
function StatusRow({ label, description, enabled, tone, value, t }: {
  label: string;
  description: string;
  enabled: boolean;
  /** Replaces the tone Enabled and Disabled take. */
  tone?: StatusTone;
  /** Replaces the Enabled/Disabled text. */
  value?: string;
  t: TFunction;
}) {
  return (
    <div className="flex flex-wrap items-center justify-between gap-x-6 gap-y-2 border-t border-separator px-1 py-3.5">
      <div className="min-w-0 max-w-xl flex-1 basis-64">
        <p className="font-semibold text-label">{label}</p>
        <p className="mt-0.5 text-[12.5px] leading-normal text-label-2">{description}</p>
      </div>
      <StatusDot tone={tone ?? (enabled ? 'ok' : 'idle')} className="font-medium text-label-2">
        {value ?? (enabled ? t('admin.settings.enabled') : t('admin.settings.disabled'))}
      </StatusDot>
    </div>
  );
}
