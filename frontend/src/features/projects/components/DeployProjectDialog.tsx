import { useEffect, useRef, useState, type ReactNode } from 'react';
import { useForm, useWatch } from 'react-hook-form';
import { FolderArchive, GitBranch } from 'lucide-react';

import { useT } from '@/shared/i18n';
import { ChoiceCards } from '@/shared/ui/ChoiceCards';
import { Field } from '@/shared/ui/Field';
import { Input } from '@/shared/ui/Input';
import { StepDialog } from '@/shared/ui/StepDialog';
import { FileDropzone } from '@/features/images/components/FileDropzone';
import { useBuildAvailability } from '@/features/images/queries';
import {
  deployStepIssues,
  deploySteps,
  isRawComposeFile,
  projectNameFrom,
  type DeployField,
  type DeployProjectForm,
  type DeploySource,
  type DeployStepIssue,
} from '../createFlow';
import { useCreateProject } from '../queries';

interface DeployProjectDialogProps {
  isOpen: boolean;
  onClose: () => void;
}

// DeployProjectDialog deploys a Compose project in three steps: Source (an
// archive or YAML file, or a Git repository, chosen on cards), Project (its
// name, which follows the source until the user types their own, and the
// Compose file's path) and Review. Its state starts over each time it opens.
export function DeployProjectDialog({ isOpen, onClose }: DeployProjectDialogProps) {
  if (!isOpen) return null;
  return <DeployProjectFlow onClose={onClose} />;
}

const emptyForm: DeployProjectForm = { source: 'upload', project_name: '', repo_url: '', ref: '', compose_file: '' };

// Messages for what is not a form field: the file and the source choice.
type OuterErrors = Partial<Record<Extract<DeployField, 'file' | 'source'>, string>>;

const formFields = new Set<DeployField>(['project_name', 'repo_url', 'ref', 'compose_file']);

function DeployProjectFlow({ onClose }: { onClose: () => void }) {
  const t = useT();
  const [step, setStep] = useState(0);
  const [reached, setReached] = useState(0);
  const [file, setFile] = useState<File | null>(null);
  const [outerErrors, setOuterErrors] = useState<OuterErrors>({});
  // The name follows the source until the user types their own.
  const [nameEdited, setNameEdited] = useState(false);
  const fileZoneRef = useRef<{ focus: () => void }>(null);
  const { data: availability } = useBuildAvailability();
  const gitEnabled = availability?.git_sources_enabled !== false;
  const buildsEnabled = availability?.enabled !== false;

  const { register, control, getValues, setValue, setError, clearErrors, setFocus, subscribe, formState: { errors } } = useForm<DeployProjectForm>({
    defaultValues: emptyForm,
  });
  const values = useWatch({ control }) as DeployProjectForm;
  const mutation = useCreateProject();
  const context = { file, gitEnabled };

  // A field's message goes once the field changes; Continue checks again.
  useEffect(() => subscribe({
    formState: { values: true },
    callback: ({ name }) => {
      if (name) clearErrors(name as keyof DeployProjectForm);
    },
  }), [subscribe, clearErrors]);

  const suggestName = (input: Parameters<typeof projectNameFrom>[0]) => {
    if (nameEdited) return;
    setValue('project_name', projectNameFrom(input));
  };

  const showIssues = (issues: DeployStepIssue[]) => {
    clearErrors();
    const outer: OuterErrors = {};
    for (const issue of issues) {
      if (formFields.has(issue.field)) setError(issue.field as keyof DeployProjectForm, { message: issue.message });
      else outer[issue.field as keyof OuterErrors] = issue.message;
    }
    setOuterErrors(outer);
    const first = issues[0];
    if (first?.field === 'file') fileZoneRef.current?.focus();
    else if (first && formFields.has(first.field)) setFocus(first.field as keyof DeployProjectForm);
    return issues.length === 0;
  };

  const submit = () => {
    const current = deploySteps[step];
    if (current !== 'review') {
      if (showIssues(deployStepIssues(getValues(), current, t, context))) {
        setStep(step + 1);
        setReached((furthest) => Math.max(furthest, step + 1));
      }
      return;
    }
    // A step that changed since it was checked is opened again with its
    // messages.
    const failing = deploySteps.findIndex((target) => deployStepIssues(getValues(), target, t, context).length > 0);
    if (failing >= 0) {
      setStep(failing);
      showIssues(deployStepIssues(getValues(), deploySteps[failing], t, context));
      return;
    }
    const form = getValues();
    const composeFile = form.compose_file.trim();
    if (form.source === 'git') {
      mutation.mutate(
        { project_name: form.project_name.trim(), repo_url: form.repo_url.trim(), ref: form.ref.trim() || undefined, compose_file: composeFile || undefined },
        { onSuccess: onClose },
      );
      return;
    }
    const data = new FormData();
    data.append('project_name', form.project_name.trim());
    if (file && !isRawComposeFile(file.name) && composeFile) data.append('compose_file', composeFile);
    data.append('archive', file as File);
    mutation.mutate(data, { onSuccess: onClose });
  };

  const chooseSource = (source: DeploySource) => {
    setValue('source', source);
    clearErrors();
    setOuterErrors({});
    suggestName(source === 'git' ? { source, repoUrl: getValues('repo_url') } : { source, fileName: file?.name });
  };

  const repoField = register('repo_url', {
    onChange: (event) => suggestName({ source: 'git', repoUrl: event.target.value }),
  });

  return (
    <StepDialog
      isOpen
      onClose={onClose}
      title={t('projects.deployTitle')}
      steps={deploySteps.map((id) => t(`projects.new.step.${id}`))}
      current={step}
      reached={reached}
      onStep={(index) => {
        clearErrors();
        setOuterErrors({});
        setStep(index);
      }}
      onSubmit={submit}
      finishLabel={t('projects.deploy')}
      isSubmitting={mutation.isPending}
    >
      {deploySteps[step] === 'source' && (
        <div className="grid gap-5">
          <ChoiceCards
            label={t('projects.new.step.source')}
            value={values.source}
            onChange={chooseSource}
            items={[
              { id: 'upload', icon: FolderArchive, title: t('projects.new.upload'), description: t('projects.new.uploadDescription') },
              {
                id: 'git',
                icon: GitBranch,
                title: t('projects.new.git'),
                description: gitEnabled ? t('projects.new.gitDescription') : t('common.gitUnavailable'),
              },
            ]}
          />
          {outerErrors.source && <p className="-mt-3 text-xs leading-snug text-red-text">{outerErrors.source}</p>}
          {values.source === 'upload' ? (
            <FileDropzone
              ref={fileZoneRef}
              id="deploy-file"
              file={file}
              accept=".zip,.tar,.tar.gz,.tgz,.yml,.yaml"
              title={t('projects.uploadFile')}
              hint={t('projects.archiveHint')}
              extraHint={t('projects.ymlHint')}
              removeLabel={t('projects.chooseOtherFile')}
              error={outerErrors.file}
              onFileChange={(selected) => {
                setFile(selected);
                setOuterErrors({});
                suggestName({ source: 'upload', fileName: selected.name });
              }}
              onRemove={() => setFile(null)}
            />
          ) : (
            <div className="grid items-start gap-4 sm:grid-cols-[1fr_9rem]">
              <Field label={t('form.gitRepositoryUrl')} htmlFor="deploy-repo" error={errors.repo_url?.message}>
                <Input id="deploy-repo" placeholder="https://github.com/org/repo.git" autoComplete="off" spellCheck={false} error={!!errors.repo_url} {...repoField} />
              </Field>
              <Field label={t('form.ref')} htmlFor="deploy-ref" hint={t('projects.new.refHint')}>
                <Input id="deploy-ref" placeholder="main" autoComplete="off" spellCheck={false} {...register('ref')} />
              </Field>
            </div>
          )}
          {!buildsEnabled && <p className="text-xs leading-snug text-orange">{t('projects.composeBuildUnavailable')}</p>}
        </div>
      )}
      {deploySteps[step] === 'project' && (
        <div className="grid gap-5">
          <Field label={t('projects.projectName')} htmlFor="deploy-name" hint={t('projects.new.nameHint')} error={errors.project_name?.message}>
            <Input
              id="deploy-name"
              placeholder="my-compose-app"
              autoComplete="off"
              spellCheck={false}
              error={!!errors.project_name}
              {...register('project_name', { onChange: (event) => setNameEdited(event.target.value !== '') })}
            />
          </Field>
          {!(values.source === 'upload' && file && isRawComposeFile(file.name)) && (
            <Field
              label={t('projects.composeFile')}
              htmlFor="deploy-compose-file"
              hint={t(values.source === 'git' ? 'projects.composeFileGitHint' : 'projects.composeFileArchiveHint')}
            >
              <Input id="deploy-compose-file" placeholder="docker-compose.yml" autoComplete="off" spellCheck={false} {...register('compose_file')} />
            </Field>
          )}
          <ComposeHelp />
        </div>
      )}
      {deploySteps[step] === 'review' && <ReviewStep values={values} file={file} />}
    </StepDialog>
  );
}

const supportedDirectives = [
  'services',
  'volumes (named)',
  'image',
  'build.context',
  'build.dockerfile',
  'build.args',
  'environment',
  'command',
  'entrypoint',
  'restart (no/on-failure)',
  'depends_on',
  'healthcheck',
  'labels: dcm.domain_prefix',
  'labels: dcm.internal_port',
];

// ComposeHelp is what DCM's Compose support takes and refuses, folded away
// until asked for.
function ComposeHelp() {
  const t = useT();
  return (
    <details className="group text-[13px]">
      <summary className="cursor-pointer select-none font-medium text-accent">{t('projects.composeHelp')}</summary>
      <div className="mt-3 grid gap-4 text-label-2">
        <p className="text-xs leading-snug">{t('projects.composeDirectiveLimitHint')} {t('projects.serverResourceHint')}</p>
        <div className="grid gap-2">
          <h3 className="text-xs font-semibold text-label">{t('projects.allowedDirectives')}</h3>
          <div className="flex flex-wrap gap-1.5">
            {supportedDirectives.map((tag) => (
              <span key={tag} className="rounded-md bg-fill px-2 py-0.5 font-mono text-[11px] text-label">{tag}</span>
            ))}
          </div>
          <p className="text-xs leading-snug">{t('projects.buildDirectiveHelp')}</p>
        </div>
        <div className="grid gap-2">
          <h3 className="text-xs font-semibold text-label">{t('projects.routingHelp')}</h3>
          <p className="text-xs leading-snug">{t('projects.routingLabelHelp')}</p>
          <pre className="overflow-x-auto rounded-xl bg-console p-3 font-mono text-[11px] text-console-label">
{`services:
  web:
    image: nginx
    labels:
      - "dcm.domain_prefix=my-app"
      - "dcm.internal_port=80"`}
          </pre>
        </div>
        <div className="grid gap-2">
          <h3 className="text-xs font-semibold text-label">{t('projects.forbidden')}</h3>
          <ul className="list-disc space-y-1 pl-4 text-xs leading-snug">
            <li>{t('projects.forbiddenBindMounts')}</li>
            <li>{t('projects.forbiddenPrivileged')}</li>
            <li>{t('projects.forbiddenHostNetwork')}</li>
            <li>{t('projects.forbiddenCustomNetworks')}</li>
          </ul>
        </div>
      </div>
    </details>
  );
}

function ReviewStep({ values, file }: { values: DeployProjectForm; file: File | null }) {
  const t = useT();
  const raw = values.source === 'upload' && file ? isRawComposeFile(file.name) : false;
  const composeFile = values.compose_file.trim();
  const facts: [string, ReactNode][] = [
    [t('projects.projectName'), <span className="break-all">{values.project_name.trim()}</span>],
    values.source === 'git'
      ? [
        t('projects.source'),
        <span className="break-all">
          {values.repo_url.trim()}
          {values.ref.trim() && <span className="text-label-2"> · {values.ref.trim()}</span>}
        </span>,
      ]
      : [t('projects.source'), <span className="break-all">{file?.name}</span>],
  ];
  if (!raw) facts.push([t('projects.composeFile'), composeFile ? <span className="break-all font-mono text-[12px]">{composeFile}</span> : t('projects.new.composeFileDefault')]);
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
      <p className="text-xs leading-snug text-label-2">{t('projects.new.reviewNote')}</p>
    </div>
  );
}
