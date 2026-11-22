import { useEffect, useState } from 'react';
import { useForm, type FieldErrors } from 'react-hook-form';
import { zodResolver } from '@hookform/resolvers/zod';
import { Save, Undo2 } from 'lucide-react';

import { Card, CardContent } from '@/shared/ui/Card';
import { Button } from '@/shared/ui/Button';
import { Page } from '@/shared/ui/Page';
import { Toolbar } from '@/shared/ui/Toolbar';
import { SearchInput } from '@/shared/ui/SearchInput';
import { failureReason } from '@/shared/api/apiError';
import { ErrorState } from '@/shared/ui/ErrorState';
import { LoadingRows } from '@/shared/ui/LoadingRows';
import { useT, type TFunction } from '@/shared/i18n';
import { useAuthConfig } from '@/features/auth/queries';
import { sectionOf, sections, type SectionId, type SettingDef } from '../catalogue';
import type { SettingName, SystemConfig } from '../model';
import { systemConfigSchema } from '../schemas';
import { useSystemConfig, useUpdateSystemConfig } from '../queries';
import { AuthSettingsStatus } from '../components/AuthSettingsStatus';
import { SectionNav } from '../components/SectionNav';
import { SettingRow } from '../components/SettingRow';
import { CPUSummaryNote, MemorySummaryNote, QuotaSummaryNote } from '../components/SettingsSummary';

// The sign-in status is the last section: read-only, set in the environment.
type PageSection = SectionId | 'auth';

function countBySection(names: string[]) {
  const counts: Partial<Record<SectionId, number>> = {};
  for (const name of names) {
    const section = sectionOf[name as SettingName];
    if (section) counts[section] = (counts[section] ?? 0) + 1;
  }
  return counts;
}

function matches(setting: SettingDef, query: string, t: TFunction) {
  return [
    setting.name,
    t(`admin.settings.field.${setting.name}.label`),
    t(`admin.settings.field.${setting.name}.hint`),
  ].some((text) => text.toLowerCase().includes(query));
}

export function SystemSettingsPage() {
  const t = useT();
  const { data: config, isLoading, isError, error, isFetching, refetch } = useSystemConfig();
  const authConfigQuery = useAuthConfig();
  const mutation = useUpdateSystemConfig();
  const [active, setActive] = useState<PageSection>('users');
  const [search, setSearch] = useState('');

  const { control, handleSubmit, reset, formState } = useForm<SystemConfig>({
    resolver: zodResolver(systemConfigSchema),
    mode: 'onChange',
  });
  const { dirtyFields, errors, isDirty } = formState;

  // The form follows the server until the administrator changes something;
  // after that a background refetch must not wipe what they typed.
  useEffect(() => {
    if (config && !isDirty) reset(config);
  }, [config, isDirty, reset]);

  const onValid = (data: SystemConfig) => {
    mutation.mutate(data, { onSuccess: () => reset(data) });
  };
  const onInvalid = (invalid: FieldErrors<SystemConfig>) => {
    const first = sections.find((section) => section.settings.some((setting) => invalid[setting.name]));
    if (first) {
      setSearch('');
      setActive(first.id);
    }
  };

  const titleOnly = <Toolbar title={t('admin.settings.title')} />;

  if (isLoading) {
    return (
      <Page toolbar={titleOnly}>
        <LoadingRows rows={8} dot={false} columns={[1, 1]} />
      </Page>
    );
  }

  if (isError) {
    return (
      <Page toolbar={titleOnly}>
        <ErrorState
          title={t('admin.settings.loadFailed')}
          message={failureReason(error, t)}
          onRetry={() => refetch()}
          isRetrying={isFetching}
        />
      </Page>
    );
  }

  const changed = countBySection(Object.keys(dirtyFields));
  const invalid = countBySection(Object.keys(errors));
  const changedTotal = Object.keys(dirtyFields).length;
  const invalidTotal = Object.keys(errors).length;
  const query = search.trim().toLowerCase();
  const found = query
    ? sections
      .map((section) => ({ ...section, settings: section.settings.filter((setting) => matches(setting, query, t)) }))
      .filter((section) => section.settings.length > 0)
    : [];
  const shown = query ? found : sections.filter((section) => section.id === active);
  const navItems = [
    ...sections.map((section) => ({
      id: section.id as PageSection,
      title: t(`admin.settings.section.${section.id}.title`),
      changed: changed[section.id] ?? 0,
      invalid: invalid[section.id] ?? 0,
    })),
    { id: 'auth' as PageSection, title: t('admin.settings.section.auth.title'), changed: 0, invalid: 0 },
  ];

  return (
    <Page
      toolbar={
        <Toolbar
          title={t('admin.settings.title')}
          actions={<SearchInput value={search} onChange={setSearch} placeholder={t('admin.settings.searchPlaceholder')} />}
        />
      }
    >

      <form onSubmit={handleSubmit(onValid, onInvalid)} noValidate className="grid gap-6 lg:grid-cols-[14rem_minmax(0,1fr)]">
        <div className="lg:sticky lg:top-6 lg:self-start">
          <SectionNav
            items={navItems}
            active={query ? null : active}
            onSelect={(id) => {
              setSearch('');
              setActive(id);
            }}
            t={t}
          />
        </div>

        <div className="min-w-0 space-y-6">
          {shown.map((section) => (
            <Card key={section.id}>
              <CardContent className="p-6">
                <SectionHeading id={section.id} t={t} />
                {!query && section.id === 'users' && <QuotaSummaryNote control={control} t={t} />}
                {!query && section.id === 'memory' && <MemorySummaryNote control={control} t={t} />}
                {!query && section.id === 'cpu' && <CPUSummaryNote control={control} t={t} />}
                <div className="divide-y divide-slate-200/70 dark:divide-slate-700/50">
                  {section.settings.map((setting) => (
                    <SettingRow key={setting.name} setting={setting} control={control} t={t} />
                  ))}
                </div>
              </CardContent>
            </Card>
          ))}

          {query && found.length === 0 && (
            <Card>
              <CardContent className="p-6 text-sm text-slate-500 dark:text-slate-400">
                {t('admin.settings.searchNoMatches')}
              </CardContent>
            </Card>
          )}

          {!query && active === 'auth' && (
            <Card>
              <CardContent className="p-6">
                <SectionHeading id="auth" t={t} />
                <AuthSettingsStatus
                  authConfig={authConfigQuery.data}
                  isLoading={authConfigQuery.isLoading}
                  isError={authConfigQuery.isError}
                  t={t}
                />
              </CardContent>
            </Card>
          )}

          <div className="sticky bottom-4 z-10 flex flex-col gap-3 rounded-2xl border border-white/50 bg-white/80 p-3 shadow-xl backdrop-blur-xl dark:border-slate-700/50 dark:bg-slate-900/80 sm:flex-row sm:items-center sm:justify-between">
            <p
              aria-live="polite"
              className={invalidTotal
                ? 'px-2 text-sm font-medium text-red-600 dark:text-red-400'
                : 'px-2 text-sm font-medium text-slate-600 dark:text-slate-300'}
            >
              {invalidTotal
                ? t('admin.settings.unsaved.invalid', { count: invalidTotal })
                : changedTotal
                  ? t('admin.settings.unsaved.count', { count: changedTotal })
                  : t('admin.settings.unsaved.none')}
            </p>
            <div className="flex shrink-0 gap-2 whitespace-nowrap">
              <Button
                type="button"
                variant="secondary"
                disabled={!isDirty || mutation.isPending}
                onClick={() => reset(config)}
              >
                <Undo2 className="mr-2 h-4 w-4" />
                {t('admin.settings.discard')}
              </Button>
              <Button type="submit" disabled={!isDirty} isLoading={mutation.isPending}>
                <Save className="mr-2 h-4 w-4" />
                {t('common.saveChanges')}
              </Button>
            </div>
          </div>
        </div>
      </form>
    </Page>
  );
}

function SectionHeading({ id, t }: { id: PageSection; t: TFunction }) {
  return (
    <div className="mb-4 space-y-1">
      <h2 className="text-xl font-semibold tracking-tight">{t(`admin.settings.section.${id}.title`)}</h2>
      <p className="text-sm text-slate-500 dark:text-slate-400">{t(`admin.settings.section.${id}.description`)}</p>
    </div>
  );
}
