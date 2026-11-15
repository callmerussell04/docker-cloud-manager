import { useState } from 'react';
import { useForm } from 'react-hook-form';
import { zodResolver } from '@hookform/resolvers/zod';
import { z } from 'zod';

import { Modal } from '@/shared/ui/Modal';
import { Input } from '@/shared/ui/Input';
import { Label } from '@/shared/ui/Label';
import { Button } from '@/shared/ui/Button';
import { WarningBanner } from '@/shared/ui/WarningBanner';
import { useToastStore } from '@/shared/ui/toastStore';
import { type CreateBuildForm, createBuildSchema } from '../schemas';
import { formatBytes } from '@/shared/lib/utils';
import { useT } from '@/shared/i18n';
import { FileDropzone } from '@/features/images/components/FileDropzone';
import { KeyValueFieldArray } from '@/shared/ui/KeyValueFieldArray';
import { SourceModeSwitch, type SourceMode } from '@/features/images/components/SourceModeSwitch';
import { useBuildAvailability, useCreateBuild } from '../queries';

interface CreateBuildModalProps {
  isOpen: boolean;
  onClose: () => void;
  onSuccessSwitchTab: () => void;
}

type CreateBuildValues = z.output<ReturnType<typeof createBuildSchema>>;

export function CreateBuildModal({ isOpen, onClose, onSuccessSwitchTab }: CreateBuildModalProps) {
  const addToast = useToastStore((state) => state.addToast);
  const t = useT();
  
  const [file, setFile] = useState<File | null>(null);
  const [selectedSourceMode, setSelectedSourceMode] = useState<SourceMode>('archive');
  const [isHintHidden, setIsHintHidden] = useState(false);
  const { data: availability } = useBuildAvailability(isOpen);

  const { register, control, handleSubmit, reset, formState: { errors } } = useForm<CreateBuildForm, unknown, CreateBuildValues>({
    resolver: zodResolver(createBuildSchema(t)),
    defaultValues: {
      context: '.',
      dockerfile: 'Dockerfile',
      build_args: [],
    }
  });

  const isGitDisabled = availability?.git_sources_enabled === false;
  // Git sources can be switched off while the modal is open; archive is then the only source.
  const sourceMode: SourceMode = isGitDisabled && selectedSourceMode === 'git' ? 'archive' : selectedSourceMode;

  const mutation = useCreateBuild();

  const onSubmit = (data: CreateBuildValues) => {
    if (sourceMode === 'archive' && !file) {
      addToast(t('validation.archiveRequired'), 'error');
      return;
    }

    const argsMap = data.build_args && data.build_args.length > 0
      ? data.build_args.reduce((acc: Record<string, string>, curr) => {
        if (curr.key) acc[curr.key] = curr.value || '';
        return acc;
      }, {})
      : undefined;

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
        repo_url: data.repo_url.trim(),
        ref: data.ref?.trim() || undefined,
        tag: data.tag,
        context: data.context,
        dockerfile: data.dockerfile,
        build_args: argsMap,
      }, {
        onSuccess: () => {
          handleClose();
          onSuccessSwitchTab();
        },
      });
      return;
    }

    const formData = new FormData();
    formData.append('tag', data.tag);
    if (data.context) formData.append('context', data.context);
    if (data.dockerfile) formData.append('dockerfile', data.dockerfile);
    if (argsMap) {
      formData.append('build_args', JSON.stringify(argsMap));
    }
    formData.append('archive', file as File);

    mutation.mutate(formData, {
      onSuccess: () => {
        handleClose();
        onSuccessSwitchTab();
      },
    });
  };

  const handleClose = () => {
    reset();
    setFile(null);
    setSelectedSourceMode('archive');
    onClose();
  };

  const handleFileChange = (selected: File) => {
    const validTypes = ['application/zip', 'application/gzip', 'application/x-tar'];
    const validExtensions = ['.zip', '.tar.gz', '.tgz', '.tar'];
    const isValidExt = validExtensions.some(ext => selected.name.toLowerCase().endsWith(ext));

    if (!validTypes.includes(selected.type) && !isValidExt) {
      addToast(t('validation.buildFileType'), 'error');
      return;
    }

    setFile(selected);
  };

  return (
    <Modal isOpen={isOpen} onClose={handleClose} title={t('images.createBuildTitle')} className="max-w-2xl">
      <form onSubmit={handleSubmit(onSubmit)} className="space-y-6">
        {isGitDisabled && (
          <WarningBanner>{t('common.gitUnavailable')}</WarningBanner>
        )}

        {!isHintHidden && (
          <WarningBanner onDismiss={() => setIsHintHidden(true)} dismissLabel={t('common.close')}>
            {t('images.serverResourceHint')}
          </WarningBanner>
        )}

        <SourceModeSwitch
          value={sourceMode}
          onChange={setSelectedSourceMode}
          archiveLabel={t('images.archive')}
          gitLabel={t('common.git')}
          gitDisabled={isGitDisabled}
        />
        
        {sourceMode === 'archive' && (
          <FileDropzone
            file={file}
            accept=".zip,.tar,.tar.gz,.tgz"
            title={t('images.uploadArchive')}
            hint={t('images.archiveHint')}
            removeLabel={t('images.removeFile')}
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
          </div>
        )}
        {sourceMode === 'archive' && file && (
          <p className="text-xs text-slate-500 -mt-4">{t('images.fileSize', { size: formatBytes(file.size) })}</p>
        )}

        <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
          <div className="space-y-2">
            <Label htmlFor="tag">{t('images.imageTag')}</Label>
            <Input id="tag" placeholder="my-app:v1" error={!!errors.tag} {...register('tag')} />
            {errors.tag && <p className="text-sm text-red-500">{errors.tag?.message as string}</p>}
          </div>

          <div className="space-y-2">
            <Label htmlFor="context">{t('images.buildContext')}</Label>
            <Input id="context" placeholder="." error={!!errors.context} {...register('context')} />
            <p className="text-xs text-slate-500">{t('images.buildContextHint')}</p>
          </div>

          <div className="space-y-2 md:col-span-2">
            <Label htmlFor="dockerfile">{t('images.dockerfilePath')}</Label>
            <Input id="dockerfile" placeholder="Dockerfile" error={!!errors.dockerfile} {...register('dockerfile')} />
            <p className="text-xs text-slate-500">{t('images.dockerfileHint')}</p>
          </div>
        </div>

        <div className="p-4 rounded-xl bg-slate-50 dark:bg-slate-800/30 border border-slate-200 dark:border-slate-700/50 space-y-4">
          <h4 className="font-medium">{t('images.buildArgs')}</h4>
          <KeyValueFieldArray
            control={control}
            register={register}
            name="build_args"
            keyPlaceholder={t('images.key')}
            valuePlaceholder={t('images.value')}
            addLabel={t('images.addArg')}
          />
        </div>

        <div className="flex justify-end gap-3 pt-4 border-t border-slate-200 dark:border-slate-700/50">
          <Button type="button" variant="ghost" onClick={handleClose}>{t('common.cancel')}</Button>
          <Button type="submit" isLoading={mutation.isPending} disabled={availability?.enabled === false}>{t('images.build')}</Button>
        </div>
      </form>
    </Modal>
  );
}
