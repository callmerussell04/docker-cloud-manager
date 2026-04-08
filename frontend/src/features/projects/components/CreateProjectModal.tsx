/* eslint-disable @typescript-eslint/no-explicit-any */
import { useState, useRef } from 'react';
import { useForm } from 'react-hook-form';
import { zodResolver } from '@hookform/resolvers/zod';
import { useMutation, useQueryClient } from '@tanstack/react-query';
import { UploadCloud, X, CheckCircle2 } from 'lucide-react';

import { Modal } from '@/components/ui/Modal';
import { Input } from '@/components/ui/Input';
import { Label } from '@/components/ui/Label';
import { Button } from '@/components/ui/Button';
import { useToastStore } from '@/store/toastStore';
import { createProjectFn } from '../api';
import { type CreateProjectForm, createProjectSchema } from '../types';

interface CreateProjectModalProps {
  isOpen: boolean;
  onClose: () => void;
}

export function CreateProjectModal({ isOpen, onClose }: CreateProjectModalProps) {
  const queryClient = useQueryClient();
  const addToast = useToastStore((state) => state.addToast);
  
  const [file, setFile] = useState<File | null>(null);
  const fileInputRef = useRef<HTMLInputElement>(null);

  const { register, handleSubmit, reset, formState: { errors } } = useForm<CreateProjectForm>({
    resolver: zodResolver(createProjectSchema),
  });

  const mutation = useMutation({
    mutationFn: createProjectFn,
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['projects'] });
      addToast('Проект успешно запущен. Сборка и развертывание происходят в фоне.', 'success');
      handleClose();
    },
    onError: (error: any) => {
      addToast(error.response?.data?.error || 'Ошибка при развертывании проекта', 'error');
    },
  });

  const onSubmit = (data: any) => {
    if (!file) {
      addToast('Пожалуйста, выберите архив с docker-compose.yml', 'error');
      return;
    }

    const formData = new FormData();
    formData.append('project_name', data.project_name);
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
    <Modal isOpen={isOpen} onClose={handleClose} title="Развернуть Compose проект" className="max-w-xl">
      <form onSubmit={handleSubmit(onSubmit)} className="space-y-6 mt-4">
        
        <div className="space-y-2">
          <Label htmlFor="project_name">Имя проекта</Label>
          <Input id="project_name" placeholder="my-compose-app" error={!!errors.project_name} {...register('project_name')} />
          {errors.project_name && <p className="text-sm text-red-500">{errors.project_name?.message as string}</p>}
        </div>

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
                <X className="w-4 h-4 mr-2" /> Выбрать другой файл
              </Button>
            </div>
          ) : (
            <div className="flex flex-col items-center text-center" onClick={() => fileInputRef.current?.click()}>
              <div className="w-12 h-12 bg-slate-100 dark:bg-slate-800 text-slate-500 dark:text-slate-400 rounded-full flex items-center justify-center mb-3 cursor-pointer">
                <UploadCloud className="w-6 h-6" />
              </div>
              <p className="font-medium cursor-pointer">Нажмите для загрузки архива</p>
              <p className="text-xs text-slate-500 mt-1">Архив должен содержать docker-compose.yml в корне</p>
            </div>
          )}
        </div>

        <div className="flex justify-end gap-3 pt-4 border-t border-slate-200 dark:border-slate-700/50">
          <Button type="button" variant="ghost" onClick={handleClose}>Отмена</Button>
          <Button type="submit" isLoading={mutation.isPending}>Развернуть</Button>
        </div>
      </form>
    </Modal>
  );
}