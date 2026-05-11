import { useEffect, useState } from 'react';
import { useForm } from 'react-hook-form';
import { zodResolver } from '@hookform/resolvers/zod';
import { Info, ChevronDown } from 'lucide-react';

import { Modal } from '@/components/ui/Modal';
import { Input } from '@/components/ui/Input';
import { Label } from '@/components/ui/Label';
import { Button } from '@/components/ui/Button';
import { WarningBanner } from '@/components/ui/WarningBanner';
import { useToastStore } from '@/store/toastStore';
import { type CreateProjectForm, createProjectSchema } from '../types';
import type { BuildAvailability } from '@/features/images/types';
import { cn } from '@/lib/utils';
import { useT } from '@/lib/i18n';
import { FileDropzone } from '@/components/common/FileDropzone';
import { SourceModeSwitch, type SourceMode } from '@/components/common/SourceModeSwitch';
import { useCreateProject } from '../hooks';

interface CreateProjectModalProps {
  isOpen: boolean;
  onClose: () => void;
  buildAvailability?: BuildAvailability;
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

export function CreateProjectModal({ isOpen, onClose, buildAvailability }: CreateProjectModalProps) {
  const addToast = useToastStore((state) => state.addToast);
  const t = useT();
  
  const [file, setFile] = useState<File | null>(null);
  const [sourceMode, setSourceMode] = useState<SourceMode>('archive');
  const [showInfo, setShowInfo] = useState(false);
  const [isHintHidden, setIsHintHidden] = useState(false);
  const isGitDisabled = buildAvailability?.git_sources_enabled === false;

  const { register, handleSubmit, reset, formState: { errors } } = useForm<CreateProjectForm>({
    resolver: zodResolver(createProjectSchema(t)),
  });

  const mutation = useCreateProject();

  useEffect(() => {
    if (isGitDisabled && sourceMode === 'git') {
      setSourceMode('archive');
    }
  }, [isGitDisabled, sourceMode]);

  const onSubmit = (data: CreateProjectForm) => {
    if (sourceMode === 'archive' && !file) {
      addToast(t('validation.fileRequired'), 'error');
      return;
    }

    if (sourceMode === 'git') {
      if (isGitDisabled) {
        addToast(t('common.gitUnavailable'), 'error');
        return;
      }
      if (!data.repo_url?.trim()) {
        addToast(t('validation.gitUrlRequired'), 'error');
        return;
      }
      mutation.mutate({
        project_name: data.project_name,
        repo_url: data.repo_url.trim(),
        ref: data.ref?.trim() || undefined,
        compose_file: data.compose_file?.trim() || undefined,
      }, {
        onSuccess: handleClose,
      });
      return;
    }

    const formData = new FormData();
    formData.append('project_name', data.project_name);
    formData.append('archive', file as File);

    mutation.mutate(formData, {
      onSuccess: handleClose,
    });
  };

  const handleClose = () => {
    reset();
    setFile(null);
    setSourceMode('archive');
    setShowInfo(false);
    onClose();
  };

  const handleFileChange = (selected: File) => {
    const validTypes = ['application/zip', 'application/gzip', 'application/x-tar', 'application/x-yaml', 'text/yaml'];
    const validExtensions = ['.zip', '.tar.gz', '.tgz', '.tar', '.yml', '.yaml'];
    const isValidExt = validExtensions.some(ext => selected.name.toLowerCase().endsWith(ext));

    if (!validTypes.includes(selected.type) && !isValidExt) {
      addToast(t('validation.projectFileType'), 'error');
      return;
    }

    setFile(selected);
  };

  return (
    <Modal isOpen={isOpen} onClose={handleClose} title={t('projects.deployTitle')} className="max-w-2xl">
      <div>
        <form onSubmit={handleSubmit(onSubmit)} className="space-y-6 mt-4 pb-2">
          {isGitDisabled && (
            <WarningBanner>{t('common.gitUnavailable')}</WarningBanner>
          )}

          {!isHintHidden && (
            <WarningBanner onDismiss={() => setIsHintHidden(true)} dismissLabel={t('common.close')}>
              <div className="space-y-2">
                <p>{t('projects.composeDirectiveLimitHint')}</p>
                <p>{t('projects.serverResourceHint')}</p>
              </div>
            </WarningBanner>
          )}
          
          <div className="space-y-2">
            <Label htmlFor="project_name">{t('projects.projectName')}</Label>
            <Input id="project_name" placeholder="my-compose-app" error={!!errors.project_name} {...register('project_name')} />
            {errors.project_name && <p className="text-sm text-red-500">{errors.project_name?.message as string}</p>}
          </div>

          <SourceModeSwitch
            value={sourceMode}
            onChange={setSourceMode}
            archiveLabel={t('projects.archive')}
            gitLabel={t('common.git')}
            gitDisabled={isGitDisabled}
          />

          {sourceMode === 'archive' && (
            <FileDropzone
              file={file}
              accept=".zip,.tar,.tar.gz,.tgz,.yml,.yaml"
              title={t('projects.uploadFile')}
              hint={t('projects.archiveHint')}
              extraHint={t('projects.ymlHint')}
              removeLabel={t('projects.chooseOtherFile')}
              onFileChange={handleFileChange}
              onRemove={() => setFile(null)}
            />
          )}

          {sourceMode === 'git' && (
            <div className="grid grid-cols-1 md:grid-cols-3 gap-4">
              <div className="space-y-2 md:col-span-2">
                <Label htmlFor="repo_url">{t('form.gitRepositoryUrl')}</Label>
                <Input id="repo_url" placeholder="https://github.com/org/repo.git" {...register('repo_url')} />
              </div>
              <div className="space-y-2">
                <Label htmlFor="ref">{t('form.ref')}</Label>
                <Input id="ref" placeholder="main" {...register('ref')} />
              </div>
              <div className="space-y-2 md:col-span-3">
                <Label htmlFor="compose_file">{t('projects.composeFile')}</Label>
                <Input id="compose_file" placeholder="docker-compose.yml" {...register('compose_file')} />
              </div>
            </div>
          )}

          <div className="border border-slate-200 dark:border-slate-700/50 rounded-xl overflow-hidden bg-slate-50/50 dark:bg-slate-800/30 transition-all">
            <button
              type="button"
              onClick={() => setShowInfo(!showInfo)}
              className="w-full px-4 py-3 flex items-center justify-between text-sm font-medium hover:bg-slate-100/50 dark:hover:bg-slate-700/30 transition-colors"
            >
              <div className="flex items-center gap-2 text-indigo-600 dark:text-indigo-400">
                <Info className="w-4 h-4" />
                {t('projects.composeHelp')}
              </div>
              <ChevronDown className={cn("w-4 h-4 transition-transform text-slate-400", showInfo && "rotate-180")} />
            </button>
            
            {showInfo && (
              <div className="px-4 pb-4 pt-1 text-sm text-slate-600 dark:text-slate-300 space-y-4 border-t border-slate-200 dark:border-slate-700/50 mt-2 pt-4">
                <div>
                  <h4 className="font-semibold text-slate-900 dark:text-slate-100 mb-2">{t('projects.allowedDirectives')}</h4>
                  <div className="flex flex-wrap gap-1.5 mb-2">
                    {supportedDirectives.map(tag => (
                      <span key={tag} className="px-2 py-0.5 bg-slate-200/50 dark:bg-slate-700/50 rounded-md font-mono text-xs">{tag}</span>
                    ))}
                  </div>
                  <p className="text-xs leading-relaxed">{t('projects.buildDirectiveHelp')}</p>
                </div>

                <div>
                  <h4 className="font-semibold text-slate-900 dark:text-slate-100 mb-2">{t('projects.routingHelp')}</h4>
                  <p className="text-xs leading-relaxed mb-2">
                    {t('projects.routingLabelHelp')}
                  </p>
                  <pre className="text-[11px] font-mono bg-slate-900 text-slate-300 p-3 rounded-lg overflow-x-auto">
{`services:
  web:
    image: nginx
    labels:
      - "dcm.domain_prefix=my-app"
      - "dcm.internal_port=80"`}
                  </pre>
                </div>

                <div>
                  <h4 className="font-semibold text-red-600 dark:text-red-400 mb-2">{t('projects.forbidden')}</h4>
                  <ul className="list-disc pl-4 space-y-1 text-xs text-red-800/80 dark:text-red-300/80">
                    <li>{t('projects.forbiddenBindMounts')}</li>
                    <li>{t('projects.forbiddenPrivileged')}</li>
                    <li>{t('projects.forbiddenHostNetwork')}</li>
                    <li>{t('projects.forbiddenCustomNetworks')}</li>
                  </ul>
                </div>
              </div>
            )}
          </div>

          <div className="flex justify-end gap-3 pt-4 border-t border-slate-200 dark:border-slate-700/50">
            <Button type="button" variant="ghost" onClick={handleClose}>{t('common.cancel')}</Button>
            <Button type="submit" isLoading={mutation.isPending}>{t('projects.deploy')}</Button>
          </div>
        </form>
      </div>
    </Modal>
  );
}
