/* eslint-disable @typescript-eslint/no-explicit-any */
import { useState, useRef } from 'react';
import { useForm } from 'react-hook-form';
import { zodResolver } from '@hookform/resolvers/zod';
import { useMutation, useQueryClient } from '@tanstack/react-query';
import { UploadCloud, X, CheckCircle2, Info, ChevronDown } from 'lucide-react';

import { Modal } from '@/components/ui/Modal';
import { Input } from '@/components/ui/Input';
import { Label } from '@/components/ui/Label';
import { Button } from '@/components/ui/Button';
import { useToastStore } from '@/store/toastStore';
import { createProjectFn } from '../api';
import { type CreateProjectForm, createProjectSchema } from '../types';
import { cn } from '@/lib/utils';

interface CreateProjectModalProps {
  isOpen: boolean;
  onClose: () => void;
}

export function CreateProjectModal({ isOpen, onClose }: CreateProjectModalProps) {
  const queryClient = useQueryClient();
  const addToast = useToastStore((state) => state.addToast);
  
  const [file, setFile] = useState<File | null>(null);
  const [showInfo, setShowInfo] = useState(false);
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
      addToast('Пожалуйста, выберите файл', 'error');
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
    setShowInfo(false);
    onClose();
  };

  const handleFileChange = (e: React.ChangeEvent<HTMLInputElement>) => {
    if (e.target.files && e.target.files[0]) {
      const selected = e.target.files[0];
      const validTypes = ['application/zip', 'application/gzip', 'application/x-tar', 'application/x-yaml', 'text/yaml'];
      const validExtensions = ['.zip', '.tar.gz', '.tgz', '.tar', '.yml', '.yaml'];
      
      const isValidExt = validExtensions.some(ext => selected.name.toLowerCase().endsWith(ext));
      
      if (!validTypes.includes(selected.type) && !isValidExt) {
        addToast('Допустимы только архивы (.zip, .tar.gz) или файлы .yml', 'error');
        return;
      }
      
      setFile(selected);
    }
  };

  return (
    <Modal isOpen={isOpen} onClose={handleClose} title="Развернуть Compose проект" className="max-w-2xl">
      <div className="max-h-[75vh] overflow-y-auto pr-2 -mr-2">
        <form onSubmit={handleSubmit(onSubmit)} className="space-y-6 mt-4 pb-2">
          
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
              accept=".zip,.tar,.tar.gz,.tgz,.yml,.yaml"
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
                <p className="font-medium cursor-pointer">Нажмите для загрузки файла</p>
                <p className="text-xs text-slate-500 mt-1">Архив (.zip, .tar.gz), если требуется сборка образов (содержит docker-compose.yml + Dockerfile)</p>
                <p className="text-xs text-slate-500">Или файл .yml, если сборка не требуется</p>
              </div>
            )}
          </div>

          <div className="border border-slate-200 dark:border-slate-700/50 rounded-xl overflow-hidden bg-slate-50/50 dark:bg-slate-800/30 transition-all">
            <button
              type="button"
              onClick={() => setShowInfo(!showInfo)}
              className="w-full px-4 py-3 flex items-center justify-between text-sm font-medium hover:bg-slate-100/50 dark:hover:bg-slate-700/30 transition-colors"
            >
              <div className="flex items-center gap-2 text-indigo-600 dark:text-indigo-400">
                <Info className="w-4 h-4" />
                Справка по поддерживаемым инструкциям
              </div>
              <ChevronDown className={cn("w-4 h-4 transition-transform text-slate-400", showInfo && "rotate-180")} />
            </button>
            
            {showInfo && (
              <div className="px-4 pb-4 pt-1 text-sm text-slate-600 dark:text-slate-300 space-y-4 border-t border-slate-200 dark:border-slate-700/50 mt-2 pt-4">
                <div>
                  <h4 className="font-semibold text-slate-900 dark:text-slate-100 mb-2">✅ Разрешенные директивы</h4>
                  <div className="flex flex-wrap gap-1.5 mb-2">
                    {['image', 'build', 'environment', 'command', 'entrypoint', 'restart', 'depends_on', 'healthcheck', 'volumes (именованные)'].map(tag => (
                      <span key={tag} className="px-2 py-0.5 bg-slate-200/50 dark:bg-slate-700/50 rounded-md font-mono text-xs">{tag}</span>
                    ))}
                  </div>
                  <p className="text-xs leading-relaxed">
                    Для <span className="font-mono bg-slate-200/50 dark:bg-slate-700/50 px-1 rounded">build</span> поддерживаются <span className="font-mono">context</span>, <span className="font-mono">dockerfile</span> и <span className="font-mono">args</span>.
                    Для <span className="font-mono bg-slate-200/50 dark:bg-slate-700/50 px-1 rounded">depends_on</span> доступны условия: <span className="font-mono">service_started</span>, <span className="font-mono">service_healthy</span>, <span className="font-mono">service_completed_successfully</span>.
                  </p>
                </div>

                <div>
                  <h4 className="font-semibold text-slate-900 dark:text-slate-100 mb-2">🌐 Публикация в интернет (Routing)</h4>
                  <p className="text-xs leading-relaxed mb-2">
                    Чтобы ваш сервис был доступен извне, добавьте специальные лейблы в секцию <span className="font-mono bg-slate-200/50 dark:bg-slate-700/50 px-1 rounded">labels</span>:
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
                  <h4 className="font-semibold text-red-600 dark:text-red-400 mb-2">❌ Запрещено (в целях безопасности)</h4>
                  <ul className="list-disc pl-4 space-y-1 text-xs text-red-800/80 dark:text-red-300/80">
                    <li>Абсолютные пути в <span className="font-mono">volumes</span> (Bind mounts)</li>
                    <li>Директива <span className="font-mono">privileged: true</span></li>
                    <li>Сетевые режимы <span className="font-mono">network_mode: host</span> и <span className="font-mono">pid: host</span></li>
                    <li>Создание кастомных <span className="font-mono">networks</span> (игнорируется, все сервисы в одной сети)</li>
                  </ul>
                </div>
              </div>
            )}
          </div>

          <div className="flex justify-end gap-3 pt-4 border-t border-slate-200 dark:border-slate-700/50">
            <Button type="button" variant="ghost" onClick={handleClose}>Отмена</Button>
            <Button type="submit" isLoading={mutation.isPending}>Развернуть</Button>
          </div>
        </form>
      </div>
    </Modal>
  );
}