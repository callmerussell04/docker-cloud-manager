import { Settings } from 'lucide-react';

import { Card, CardContent, CardHeader, CardTitle } from '@/shared/ui/Card';
import { Label } from '@/shared/ui/Label';
import type { TFunction } from '@/shared/i18n';
import { cn } from '@/shared/lib/utils';
import type { AuthConfig } from '@/features/auth/model';
import { InfoHint } from './InfoHint';

/** Shows the sign-in settings, which come from bootstrap configuration and cannot be changed here. */
export function AuthSettingsStatus({
  authConfig,
  isLoading,
  isError,
  t,
}: {
  authConfig?: AuthConfig;
  isLoading: boolean;
  isError: boolean;
  t: TFunction;
}) {
  const oidcProviders = authConfig?.oidc_providers.map((provider) => provider.name).filter(Boolean) ?? [];
  const externalSSOEnabled = oidcProviders.length > 0;

  return (
    <Card>
      <CardHeader>
        <CardTitle className="flex items-center gap-2 text-lg">
          <Settings className="h-5 w-5 text-indigo-600 dark:text-indigo-400" />
          {t('admin.settings.section.auth')}
        </CardTitle>
      </CardHeader>
      <CardContent>
        {isLoading ? (
          <div className="h-24 rounded-xl bg-white/40 animate-pulse dark:bg-slate-900/40" />
        ) : isError ? (
          <p className="text-sm text-red-600 dark:text-red-400">{t('admin.settings.authStatusUnavailable')}</p>
        ) : (
          <div className="grid grid-cols-1 gap-5 md:grid-cols-2 xl:grid-cols-3">
            <ReadOnlyStatusField
              label={t('admin.settings.field.local_login_enabled.label')}
              hint={t('admin.settings.field.local_login_enabled.hint')}
              enabled={authConfig?.local_login_enabled ?? false}
              t={t}
            />
            <ReadOnlyStatusField
              label={t('admin.settings.field.local_register_enabled.label')}
              hint={t('admin.settings.field.local_register_enabled.hint')}
              enabled={authConfig?.local_register_enabled ?? false}
              t={t}
            />
            <ReadOnlyStatusField
              label={t('admin.settings.field.external_sso_enabled.label')}
              hint={t('admin.settings.field.external_sso_enabled.hint', {
                providers: externalSSOEnabled ? oidcProviders.join(', ') : t('admin.settings.none'),
              })}
              enabled={externalSSOEnabled}
              t={t}
            />
          </div>
        )}
      </CardContent>
    </Card>
  );
}

function ReadOnlyStatusField({
  label,
  hint,
  enabled,
  t,
}: {
  label: string;
  hint: string;
  enabled: boolean;
  t: TFunction;
}) {
  return (
    <div className="space-y-2">
      <div className="flex items-center gap-2">
        <Label>{label}</Label>
        <InfoHint text={hint} label={label} t={t} />
      </div>
      <div
        className={cn(
          'flex w-full items-center justify-between rounded-xl border px-4 py-3 text-left text-sm',
          enabled
            ? 'border-indigo-300 bg-indigo-50 text-indigo-900 dark:border-indigo-800 dark:bg-indigo-950/40 dark:text-indigo-100'
            : 'border-white/40 bg-white/40 text-slate-700 dark:border-slate-700/50 dark:bg-slate-900/40 dark:text-slate-300'
        )}
      >
        <span className="font-medium">{enabled ? t('admin.settings.enabled') : t('admin.settings.disabled')}</span>
        <span className={cn('relative h-6 w-11 rounded-full', enabled ? 'bg-indigo-600' : 'bg-slate-300 dark:bg-slate-700')}>
          <span className={cn('absolute top-1 h-4 w-4 rounded-full bg-white shadow', enabled ? 'translate-x-6' : 'translate-x-1')} />
        </span>
      </div>
    </div>
  );
}
