import { useEffect, useRef, useState, type ReactNode } from 'react';
import { useForm, useWatch, type Path } from 'react-hook-form';
import { FolderArchive, GitBranch } from 'lucide-react';

import { useT } from '@/shared/i18n';
import { formatBytes } from '@/shared/lib/utils';
import { ChoiceCards } from '@/shared/ui/ChoiceCards';
import { Field } from '@/shared/ui/Field';
import { Input } from '@/shared/ui/Input';
import { KeyValueFieldArray } from '@/shared/ui/KeyValueFieldArray';
import { StepDialog } from '@/shared/ui/StepDialog';
import { buildStepIssues, buildSteps, type BuildField, type BuildImageForm, type BuildSource, type BuildStepIssue } from '../buildFlow';
import type { BuildAgain } from '../model';
import { useBuildAvailability, useCreateBuild } from '../queries';
import { FileDropzone } from './FileDropzone';

interface BuildImageDialogProps {
  isOpen: boolean;
  onClose: () => void;
  // initial fills the dialog for Build again: the image's tag and, for a Git
  // build, its repository and ref.
  initial?: BuildAgain;
  // onBuilt receives the queued build's ID.
  onBuilt: (buildId: string) => void;
}

// BuildImageDialog builds an image in three steps: Source (an archive, or a
// Git repository with its ref, chosen on cards), Image (the tag, build
// context, Dockerfile and build arguments) and Review. Its state starts over
// each time it opens.
export function BuildImageDialog({ isOpen, onClose, initial, onBuilt }: BuildImageDialogProps) {
  if (!isOpen) return null;
  return <BuildImageFlow onClose={onClose} initial={initial} onBuilt={onBuilt} />;
}

const emptyForm: BuildImageForm = { source: 'archive', repo_url: '', ref: '', tag: '', context: '.', dockerfile: 'Dockerfile', build_args: [] };

function initialForm(initial?: BuildAgain): BuildImageForm {
  if (!initial) return emptyForm;
  if (initial.source === 'git') return { ...emptyForm, tag: initial.tag, source: 'git', repo_url: initial.repoUrl, ref: initial.ref };
  return { ...emptyForm, tag: initial.tag };
}

// Messages for what is not a form field: the file and the source choice.
type OuterErrors = Partial<Record<'file' | 'source', string>>;

function isOuter(field: BuildField): field is 'file' | 'source' {
  return field === 'file' || field === 'source';
}

function BuildImageFlow({ onClose, initial, onBuilt }: Omit<BuildImageDialogProps, 'isOpen'>) {
  const t = useT();
  const [step, setStep] = useState(0);
  const [reached, setReached] = useState(0);
  const [file, setFile] = useState<File | null>(null);
  const [outerErrors, setOuterErrors] = useState<OuterErrors>({});
  const fileZoneRef = useRef<{ focus: () => void }>(null);
  const { data: availability } = useBuildAvailability();
  const gitEnabled = availability?.git_sources_enabled !== false;

  const { register, control, getValues, setValue, setError, clearErrors, setFocus, subscribe, formState: { errors } } = useForm<BuildImageForm>({
    defaultValues: initialForm(initial),
  });
  const values = useWatch({ control }) as BuildImageForm;
  const mutation = useCreateBuild();
  const context = { file, gitEnabled };

  // A field's message goes once the field changes; Continue checks again.
  useEffect(() => subscribe({
    formState: { values: true },
    callback: ({ name }) => {
      if (name) clearErrors(name as Path<BuildImageForm>);
    },
  }), [subscribe, clearErrors]);

  const showIssues = (issues: BuildStepIssue[]) => {
    clearErrors();
    const outer: OuterErrors = {};
    for (const issue of issues) {
      if (isOuter(issue.field)) outer[issue.field] = issue.message;
      else setError(issue.field as Path<BuildImageForm>, { message: issue.message });
    }
    setOuterErrors(outer);
    const first = issues[0];
    if (first?.field === 'file') fileZoneRef.current?.focus();
    else if (first && !isOuter(first.field)) setFocus(first.field as Path<BuildImageForm>);
    return issues.length === 0;
  };

  const submit = () => {
    const current = buildSteps[step];
    if (current !== 'review') {
      if (showIssues(buildStepIssues(getValues(), current, t, context))) {
        setStep(step + 1);
        setReached((furthest) => Math.max(furthest, step + 1));
      }
      return;
    }
    // A step that changed since it was checked is opened again with its
    // messages.
    const failing = buildSteps.findIndex((target) => buildStepIssues(getValues(), target, t, context).length > 0);
    if (failing >= 0) {
      setStep(failing);
      showIssues(buildStepIssues(getValues(), buildSteps[failing], t, context));
      return;
    }
    const form = getValues();
    const args = form.build_args.length > 0
      ? Object.fromEntries(form.build_args.map(({ key, value }) => [key.trim(), value]))
      : undefined;
    const tag = form.tag.trim();
    const contextDir = form.context.trim() || undefined;
    const dockerfile = form.dockerfile.trim() || undefined;
    const onSuccess = ({ build_id }: { build_id: string }) => {
      onClose();
      onBuilt(build_id);
    };
    if (form.source === 'git') {
      mutation.mutate(
        { repo_url: form.repo_url.trim(), ref: form.ref.trim() || undefined, tag, context: contextDir, dockerfile, build_args: args },
        { onSuccess },
      );
      return;
    }
    // Core reads the fields before the archive, so the archive goes last.
    const data = new FormData();
    data.append('tag', tag);
    if (contextDir) data.append('context', contextDir);
    if (dockerfile) data.append('dockerfile', dockerfile);
    if (args) data.append('build_args', JSON.stringify(args));
    data.append('archive', file as File);
    mutation.mutate(data, { onSuccess });
  };

  const chooseSource = (source: BuildSource) => {
    setValue('source', source);
    clearErrors();
    setOuterErrors({});
  };

  const argErrors = errors.build_args?.map?.((row) => row?.key?.message);

  return (
    <StepDialog
      isOpen
      onClose={onClose}
      title={t('images.createBuildTitle')}
      steps={buildSteps.map((id) => t(`images.new.step.${id}`))}
      current={step}
      reached={reached}
      onStep={(index) => {
        clearErrors();
        setOuterErrors({});
        setStep(index);
      }}
      onSubmit={submit}
      finishLabel={t('images.build')}
      isSubmitting={mutation.isPending}
    >
      {buildSteps[step] === 'source' && (
        <div className="grid gap-5">
          <ChoiceCards
            label={t('images.new.step.source')}
            value={values.source}
            onChange={chooseSource}
            items={[
              { id: 'archive', icon: FolderArchive, title: t('images.new.archive'), description: t('images.new.archiveDescription') },
              {
                id: 'git',
                icon: GitBranch,
                title: t('images.new.git'),
                description: gitEnabled ? t('images.new.gitDescription') : t('common.gitUnavailable'),
              },
            ]}
          />
          {outerErrors.source && <p className="-mt-3 text-xs leading-snug text-red-text">{outerErrors.source}</p>}
          {values.source === 'archive' ? (
            <FileDropzone
              ref={fileZoneRef}
              id="build-file"
              file={file}
              accept=".zip,.tar,.tar.gz,.tgz"
              title={t('images.uploadArchive')}
              hint={t('images.archiveHint')}
              removeLabel={t('images.removeFile')}
              error={outerErrors.file}
              onFileChange={(selected) => {
                setFile(selected);
                setOuterErrors({});
              }}
              onRemove={() => setFile(null)}
            />
          ) : (
            <div className="grid items-start gap-4 sm:grid-cols-[1fr_9rem]">
              <Field label={t('form.gitRepositoryUrl')} htmlFor="build-repo" error={errors.repo_url?.message}>
                <Input id="build-repo" placeholder="https://github.com/org/repo.git" autoComplete="off" spellCheck={false} error={!!errors.repo_url} {...register('repo_url')} />
              </Field>
              <Field label={t('form.ref')} htmlFor="build-ref" hint={t('images.new.refHint')}>
                <Input id="build-ref" placeholder="main" autoComplete="off" spellCheck={false} {...register('ref')} />
              </Field>
            </div>
          )}
        </div>
      )}
      {buildSteps[step] === 'image' && (
        <div className="grid gap-5">
          <Field label={t('images.imageTag')} htmlFor="build-tag" hint={t('images.new.tagHint')} error={errors.tag?.message}>
            <Input id="build-tag" placeholder="my-app:v1" autoComplete="off" spellCheck={false} error={!!errors.tag} {...register('tag')} />
          </Field>
          <div className="grid items-start gap-4 sm:grid-cols-2">
            <Field label={t('images.buildContext')} htmlFor="build-context" hint={t('images.buildContextHint')}>
              <Input id="build-context" placeholder="." autoComplete="off" spellCheck={false} className="font-mono" {...register('context')} />
            </Field>
            <Field label={t('images.dockerfilePath')} htmlFor="build-dockerfile" hint={t('images.dockerfileHint')}>
              <Input id="build-dockerfile" placeholder="Dockerfile" autoComplete="off" spellCheck={false} className="font-mono" {...register('dockerfile')} />
            </Field>
          </div>
          <div className="grid gap-2">
            <h3 className="text-[13px] font-semibold text-label">{t('images.buildArgs')}</h3>
            <KeyValueFieldArray
              control={control}
              register={register}
              name="build_args"
              keyPlaceholder={t('images.key')}
              valuePlaceholder={t('images.value')}
              addLabel={t('images.addArg')}
              errors={argErrors}
            />
          </div>
          <p className="text-xs leading-snug text-label-2">{t('images.serverResourceHint')}</p>
        </div>
      )}
      {buildSteps[step] === 'review' && <ReviewStep values={values} file={file} />}
    </StepDialog>
  );
}

function ReviewStep({ values, file }: { values: BuildImageForm; file: File | null }) {
  const t = useT();
  const facts: [string, ReactNode][] = [
    [t('images.imageTag'), <span className="break-all">{values.tag.trim()}</span>],
    values.source === 'git'
      ? [
        t('images.new.source'),
        <span className="break-all">
          {values.repo_url.trim()}
          {values.ref.trim() && <span className="text-label-2"> · {values.ref.trim()}</span>}
        </span>,
      ]
      : [t('images.new.source'), <span className="break-all">{file?.name} <span className="text-label-2">· {file ? formatBytes(file.size, 1) : ''}</span></span>],
    [t('images.buildContext'), <span className="break-all font-mono text-[12px]">{values.context.trim() || '.'}</span>],
    [t('images.dockerfilePath'), <span className="break-all font-mono text-[12px]">{values.dockerfile.trim() || 'Dockerfile'}</span>],
  ];
  if (values.build_args.length > 0) {
    // Values stay out of the review: a build argument may hold a secret.
    facts.push([t('images.buildArgs'), <span className="break-all font-mono text-[12px]">{values.build_args.map((arg) => arg.key.trim()).join(', ')}</span>]);
  }
  return (
    <div className="grid gap-5">
      <dl className="grid gap-4">
        {facts.map(([label, value]) => (
          <div key={label} className="grid gap-0.5">
            <dt className="text-xs font-medium text-label-3">{label}</dt>
            <dd className="min-w-0">{value}</dd>
          </div>
        ))}
      </dl>
      <p className="text-xs leading-snug text-label-2">{t('images.new.reviewNote')}</p>
    </div>
  );
}
