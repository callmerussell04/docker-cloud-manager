/* eslint-disable @typescript-eslint/no-explicit-any */
import { useState, useRef } from 'react';
import { useForm, useFieldArray } from 'react-hook-form';
import { zodResolver } from '@hookform/resolvers/zod';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { Plus, Trash2, UploadCloud, X } from 'lucide-react';

import { Modal } from '@/components/ui/Modal';
import { Input } from '@/components/ui/Input';
import { Label } from '@/components/ui/Label';
import { Button } from '@/components/ui/Button';
import { useToastStore } from '@/store/toastStore';
import { createBuildFn, getBuildAvailabilityFn } from '../api';
import { type CreateBuildForm, createBuildSchema } from '../types';
import { formatBytes } from '@/lib/utils';

interface CreateBuildModalProps {
  isOpen: boolean;
  onClose: () => void;
  onSuccessSwitchTab: () => void;
}

export function CreateBuildModal({ isOpen, onClose, onSuccessSwitchTab }: CreateBuildModalProps) {
  const queryClient = useQueryClient();
  const addToast = useToastStore((state) => state.addToast);
  
  const [file, setFile] = useState<File | null>(null);
  const fileInputRef = useRef<HTMLInputElement>(null);
  const { data: availability } = useQuery({
    queryKey: ['imageBuildAvailability'],
    queryFn: getBuildAvailabilityFn,
    enabled: isOpen,
  });

  const { register, control, handleSubmit, reset, formState: { errors } } = useForm<CreateBuildForm>({
    resolver: zodResolver(createBuildSchema),
    defaultValues: {
      context: '.',
      dockerfile: 'Dockerfile',
      build_args: [],
    }
  });

  const { fields: argFields, append: appendArg, remove: removeArg } = useFieldArray({
    control,
    name: "build_args"
  });

  const mutation = useMutation({
    mutationFn: createBuildFn,
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['builds'] });
      addToast('Сборка успешно инициирована', 'success');
      handleClose();
      onSuccessSwitchTab();
    },
    onError: (error: any) => {
      addToast(error.response?.data?.error || 'Ошибка при инициализации сборки', 'error');
    },
  });

  const onSubmit = (data: any) => {
    if (!file) {
      addToast('Пожалуйста, выберите архив с кодом', 'error');
      return;
    }

    const formData = new FormData();
    formData.append('tag', data.tag);
    if (data.context) formData.append('context', data.context);
    if (data.dockerfile) formData.append('dockerfile', data.dockerfile);

    if (data.build_args && data.build_args.length > 0) {
      const argsMap = data.build_args.reduce((acc: Record<string, string>, curr: any) => {
        if (curr.key) acc[curr.key] = curr.value || '';
        return acc;
      }, {});
      formData.append('build_args', JSON.stringify(argsMap));
    }
    formData.append('archive', file);

    mutation.mutate(formData);
  };

  const handleClose = () => {
    reset();
    setFile(null);
    onClose();
  };

  const handleFileChange = (e: React.ChangeEvent<HTMLInputElement>) => {
    if (e.target.files && e.target.files[0]) {
      const selected = e.target.files[0];
      const validTypes = ['application/zip', 'application/gzip', 'application/x-tar'];
      const validExtensions = ['.zip', '.tar.gz', '.tgz', '.tar'];
      
      const isValidExt = validExtensions.some(ext => selected.name.toLowerCase().endsWith(ext));
      
      if (!validTypes.includes(selected.type) && !isValidExt) {
        addToast('Допустимы только архивы .zip, .tar, .tar.gz', 'error');
        return;
      }
      
      setFile(selected);
    }
  };

  return (
    <Modal isOpen={isOpen} onClose={handleClose} title="Собрать образ" className="max-w-2xl max-h-[90vh] overflow-y-auto">
      <form onSubmit={handleSubmit(onSubmit)} className="space-y-6">
        {availability && !availability.enabled && (
          <div className="rounded-xl border border-yellow-200 bg-yellow-50 p-3 text-sm text-yellow-800 dark:border-yellow-900/50 dark:bg-yellow-900/20 dark:text-yellow-300">
            {availability.message || 'Сборка образов сейчас недоступна.'}
          </div>
        )}
        
        <div className="p-6 border-2 border-dashed border-slate-300 dark:border-slate-700 rounded-2xl flex flex-col items-center justify-center bg-slate-50/50 dark:bg-slate-900/50 transition-colors hover:bg-slate-100/50 dark:hover:bg-slate-800/50">
          <input 
            type="file" 
            ref={fileInputRef} 
            onChange={handleFileChange} 
            className="hidden" 
            accept=".zip,.tar,.tar.gz,.tgz"
          />
          
          {file ? (
            <div className="flex flex-col items-center text-center">
              <div className="w-12 h-12 bg-indigo-100 dark:bg-indigo-900/50 text-indigo-600 dark:text-indigo-400 rounded-full flex items-center justify-center mb-3">
                <CheckCircle2 className="w-6 h-6" />
              </div>
              <p className="font-medium">{file.name}</p>
              <p className="text-xs text-slate-500 mb-4">{(file.size / 1024 / 1024).toFixed(2)} MB</p>
              <Button type="button" variant="ghost" onClick={() => setFile(null)} className="text-red-500 hover:bg-red-50 dark:hover:bg-red-950">
                <X className="w-4 h-4 mr-2" /> Удалить файл
              </Button>
            </div>
          ) : (
            <div className="flex flex-col items-center text-center" onClick={() => fileInputRef.current?.click()}>
              <div className="w-12 h-12 bg-slate-100 dark:bg-slate-800 text-slate-500 dark:text-slate-400 rounded-full flex items-center justify-center mb-3 cursor-pointer">
                <UploadCloud className="w-6 h-6" />
              </div>
              <p className="font-medium cursor-pointer">Нажмите, чтобы загрузить архив</p>
              <p className="text-xs text-slate-500 mt-1">Только .zip, .tar, .tar.gz. Лимит проверяется backend.</p>
            </div>
          )}
        </div>
        {file && (
          <p className="text-xs text-slate-500 -mt-4">Размер файла: {formatBytes(file.size)}</p>
        )}

        <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
          <div className="space-y-2">
            <Label htmlFor="tag">Тег образа</Label>
            <Input id="tag" placeholder="my-app:v1" error={!!errors.tag} {...register('tag')} />
            {errors.tag && <p className="text-sm text-red-500">{errors.tag?.message as string}</p>}
          </div>

          <div className="space-y-2">
            <Label htmlFor="context">Контекст сборки</Label>
            <Input id="context" placeholder="." error={!!errors.context} {...register('context')} />
            <p className="text-xs text-slate-500">Путь к папке внутри архива</p>
          </div>

          <div className="space-y-2 md:col-span-2">
            <Label htmlFor="dockerfile">Путь к Dockerfile</Label>
            <Input id="dockerfile" placeholder="Dockerfile" error={!!errors.dockerfile} {...register('dockerfile')} />
            <p className="text-xs text-slate-500">Относительно контекста сборки</p>
          </div>
        </div>

        <div className="p-4 rounded-xl bg-slate-50 dark:bg-slate-800/30 border border-slate-200 dark:border-slate-700/50 space-y-4">
          <h4 className="font-medium">Build Args (опционально)</h4>
          {argFields.map((field, index) => (
            <div key={field.id} className="flex gap-2 items-start">
              <div className="flex-1">
                <Input placeholder="Ключ" {...register(`build_args.${index}.key`)} />
                {errors.build_args?.[index]?.key && <p className="text-xs text-red-500 mt-1">{errors.build_args[index]?.key?.message}</p>}
              </div>
              <div className="flex-1">
                <Input placeholder="Значение" {...register(`build_args.${index}.value`)} />
              </div>
              <Button type="button" variant="danger" onClick={() => removeArg(index)} className="px-3">
                <Trash2 className="w-4 h-4" />
              </Button>
            </div>
          ))}
          <Button type="button" variant="secondary" onClick={() => appendArg({ key: '', value: '' })} className="w-full text-sm">
            <Plus className="w-4 h-4 mr-2" /> Добавить аргумент
          </Button>
        </div>

        <div className="flex justify-end gap-3 pt-4 border-t border-slate-200 dark:border-slate-700/50">
          <Button type="button" variant="ghost" onClick={handleClose}>Отмена</Button>
          <Button type="submit" isLoading={mutation.isPending} disabled={availability?.enabled === false}>Собрать</Button>
        </div>
      </form>
    </Modal>
  );
}

import { CheckCircle2 } from 'lucide-react';
