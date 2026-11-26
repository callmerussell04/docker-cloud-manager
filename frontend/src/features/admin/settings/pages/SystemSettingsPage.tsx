import { useCallback, useEffect, useRef } from 'react';
import { useSearchParams } from 'react-router-dom';
import { useForm, type FieldErrors } from 'react-hook-form';
import { zodResolver } from '@hookform/resolvers/zod';

import { Page } from '@/shared/ui/Page';
import { Toolbar, ToolbarSearch } from '@/shared/ui/Toolbar';
import { SaveBar } from '@/shared/ui/SaveBar';
import { NoMatchesState } from '@/shared/ui/NoMatchesState';
import { ErrorState } from '@/shared/ui/ErrorState';
import { LoadingRows } from '@/shared/ui/LoadingRows';
import { useShellLayout } from '@/shared/ui/shellLayout';
import { cn } from '@/shared/lib/utils';
import { useElementWidth } from '@/shared/lib/hooks';
import { rowsCompact } from '@/shared/lib/sizeClass';
import { useT, type TFunction } from '@/shared/i18n';
import { useAuthConfig } from '@/features/auth/queries';
import { sections, type SectionId, type SettingDef } from '../catalogue';
import type { SystemConfig } from '../model';
import { systemConfigSchema } from '../schemas';
import { countBySection, searchSettings } from '../settingsSearch';
import { useSystemConfig, useUpdateSystemConfig } from '../queries';
import { AuthSettingsStatus } from '../components/AuthSettingsStatus';
import { SectionNav, type SectionNavItem } from '../components/SectionNav';
import { SettingRow } from '../components/SettingRow';
import { CPUSummaryNote, MemorySummaryNote, QuotaSummaryNote } from '../components/SettingsSummary';

// The sign-in status is the last section: read-only, set in the environment.
type PageSection = SectionId | 'auth';

const formId = 'system-settings-form';

const sectionCountLabels = {
  found: 'admin.settings.sectionFound',
  invalid: 'admin.settings.sectionInvalid',
  changed: 'admin.settings.sectionChanged',
} as const;

const pageSections: PageSection[] = [...sections.map((section) => section.id), 'auth'];
const firstSection = pageSections[0];

function isPageSection(value: string | null): value is PageSection {
  return pageSections.includes(value as PageSection);
}

// useSettingsView keeps the shown section and the search in the URL
// (?section=memory, ?q=boost; the first section and no search are left out),
// replacing the history entry, so a refresh or a shared link shows the same.
function useSettingsView() {
  const [params, setParams] = useSearchParams();
  const sectionParam = params.get('section');
  const active: PageSection = isPageSection(sectionParam) ? sectionParam : firstSection;
  const search = params.get('q') ?? '';
  const update = useCallback(
    (next: { section?: PageSection; search?: string }) =>
      setParams(
        (current) => {
          const updated = new URLSearchParams(current);
          if (next.section !== undefined) {
            if (next.section === firstSection) updated.delete('section');
            else updated.set('section', next.section);
          }
          if (next.search !== undefined) {
            if (next.search) updated.set('q', next.search);
            else updated.delete('q');
          }
          return updated;
        },
        { replace: true },
      ),
    [setParams],
  );
  return { active, search, update };
}

const settingTexts = (t: TFunction) => (setting: SettingDef) => [
  t(`admin.settings.field.${setting.name}.label`),
  t(`admin.settings.field.${setting.name}.hint`),
];

export function SystemSettingsPage() {
  const t = useT();
  const { rowsCompact: compact } = useShellLayout();
  const { data: config, isLoading, isError, error, isFetching, refetch } = useSystemConfig();
  const authConfigQuery = useAuthConfig();
  const mutation = useUpdateSystemConfig();
  const { active, search, update } = useSettingsView();
  const bodyRef = useRef<HTMLDivElement | null>(null);
  // Rows stack their field under their label by the width of the column
  // they are in, which the section list beside it narrows.
  const [measureBody, bodyWidth] = useElementWidth<HTMLDivElement>(window.innerWidth);
  const setBody = useCallback((element: HTMLDivElement | null) => {
    bodyRef.current = element;
    measureBody(element);
  }, [measureBody]);
  const stackedRows = compact || rowsCompact(bodyWidth);

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

  // A newly chosen section starts at its top.
  useEffect(() => {
    // On compact content the page itself scrolls, not the content column.
    const body = bodyRef.current;
    (compact ? body?.closest('.page-scroll') : body)?.scrollTo({ top: 0 });
  }, [active, compact]);

  const onValid = (data: SystemConfig) => {
    mutation.mutate(data, { onSuccess: () => reset(data) });
  };
  const onInvalid = (invalid: FieldErrors<SystemConfig>) => {
    const first = sections.find((section) => section.settings.some((setting) => invalid[setting.name]));
    if (first) update({ section: first.id, search: '' });
  };

  const title = t('admin.settings.title');

  if (isLoading) {
    return (
      <Page toolbar={<Toolbar title={title} />}>
        <LoadingRows rows={8} dot={false} columns={[1, 1]} />
      </Page>
    );
  }

  if (isError) {
    return (
      <Page toolbar={<Toolbar title={title} />}>
        <ErrorState
          title={t('admin.settings.loadFailed')}
          error={error}
          onRetry={() => refetch()}
          isRetrying={isFetching}
        />
      </Page>
    );
  }

  const changedNames = Object.keys(dirtyFields);
  const invalidNames = Object.keys(errors);
  const changed = countBySection(changedNames);
  const invalid = countBySection(invalidNames);
  const found = searchSettings(search, settingTexts(t));
  const foundCounts = found ? countBySection(found.flatMap((section) => section.settings.map((setting) => setting.name))) : null;

  // Each section counts its matches while searching, else its refused
  // settings, else its changed ones.
  const navItems: Array<SectionNavItem<PageSection>> = pageSections
    .filter((id) => !foundCounts || (id !== 'auth' && foundCounts[id]))
    .map((id) => {
      const item: SectionNavItem<PageSection> = { id, title: t(`admin.settings.section.${id}.title`) };
      if (id === 'auth') return item;
      const [kind, count] = foundCounts
        ? ['found', foundCounts[id] ?? 0] as const
        : invalid[id] ? ['invalid', invalid[id]] as const : ['changed', changed[id] ?? 0] as const;
      return count ? { ...item, mark: { kind, count, label: t(sectionCountLabels[kind], { count }) } } : item;
    });

  const onSelectSection = (id: PageSection) => {
    // While searching, a section in the list leads to its matches.
    if (found) {
      bodyRef.current?.querySelector(`#settings-section-${id}`)?.scrollIntoView({ block: 'start' });
      return;
    }
    update({ section: id });
  };

  const shown = found ?? sections.filter((section) => section.id === active);

  return (
    <Page
      // On compact content the page scrolls as a whole, so the rows pass
      // under the phone tab bar; beside the section list each column
      // scrolls on its own.
      fill={!compact}
      // The save bar floats over the page outside its scroll area, so its
      // glass refracts the rows beneath it; Save submits the form by its id.
      overlay={isDirty && (
        <SaveBar
          form={formId}
          label={t('admin.settings.unsavedLabel')}
          message={invalidNames.length
            ? t('admin.settings.unsaved.invalid', { count: invalidNames.length })
            : t('admin.settings.unsaved.count', { count: changedNames.length })}
          invalid={invalidNames.length > 0}
          discardLabel={t('admin.settings.discard')}
          saveLabel={t('common.save')}
          onDiscard={() => reset(config)}
          saving={mutation.isPending}
        />
      )}
      toolbar={
        <Toolbar
          title={title}
          actions={<ToolbarSearch value={search} onChange={(value) => update({ search: value })} label={t('admin.settings.search')} />}
        />
      }
    >
      <form
        id={formId}
        onSubmit={handleSubmit(onValid, onInvalid)}
        noValidate
        className={cn(
          compact ? 'grid gap-2' : 'grid h-full grid-cols-[minmax(10rem,13rem)_minmax(0,1fr)] gap-5',
        )}
      >
        <div
          className={cn(
            compact
              ? '-mx-3 overflow-x-auto px-3 py-1 [scrollbar-width:none]'
              : 'min-h-0 overflow-y-auto pb-14 [scrollbar-width:thin]',
          )}
        >
          <SectionNav
            items={navItems}
            active={found ? null : active}
            onSelect={onSelectSection}
            label={t('admin.settings.sectionsLabel')}
            compact={compact}
          />
        </div>

        <div
          ref={setBody}
          className={cn(
            'pb-28',
            !compact && 'min-h-0 overflow-y-auto border-l border-separator pl-5 [scrollbar-width:thin]',
          )}
        >
          <div className="grid max-w-[820px] gap-10">
            {shown.map((section) => (
              <section key={section.id} id={`settings-section-${section.id}`} aria-labelledby={`settings-heading-${section.id}`} className="scroll-mt-2">
                <SectionHeading id={section.id} t={t} />
                {!found && section.id === 'users' && <QuotaSummaryNote control={control} t={t} />}
                {!found && section.id === 'memory' && <MemorySummaryNote control={control} t={t} />}
                {!found && section.id === 'cpu' && <CPUSummaryNote control={control} t={t} />}
                <div>
                  {section.settings.map((setting) => (
                    <SettingRow key={setting.name} setting={setting} control={control} compact={stackedRows} t={t} />
                  ))}
                </div>
              </section>
            ))}

            {found?.length === 0 && <NoMatchesState
                query={search}
                description={t('admin.settings.searchNoMatches')}
                onClear={() => update({ search: '' })}
              />}

            {!found && active === 'auth' && (
              <section aria-labelledby="settings-heading-auth">
                <SectionHeading id="auth" t={t} />
                <AuthSettingsStatus
                  authConfig={authConfigQuery.data}
                  isLoading={authConfigQuery.isLoading}
                  isError={authConfigQuery.isError}
                  onRetry={() => authConfigQuery.refetch()}
                  isRetrying={authConfigQuery.isFetching}
                  t={t}
                />
              </section>
            )}
          </div>
        </div>

      </form>
    </Page>
  );
}

function SectionHeading({ id, t }: { id: PageSection; t: TFunction }) {
  return (
    <div className="mb-3.5 px-1">
      <h2 id={`settings-heading-${id}`} className="text-[20px] font-bold tracking-[-0.02em] text-label">
        {t(`admin.settings.section.${id}.title`)}
      </h2>
      <p className="mt-1 max-w-2xl leading-normal text-label-2">{t(`admin.settings.section.${id}.description`)}</p>
    </div>
  );
}
