import { useState } from 'react';
import { useQuery } from '@tanstack/react-query';
import { Plus, RefreshCcw, Search, Layers } from 'lucide-react';

import { Button } from '@/components/ui/Button';
import { Input } from '@/components/ui/Input';
import { Pagination } from '@/components/ui/Pagination';
import { ProjectRow } from '@/features/projects/components/ProjectRow';
import { CreateProjectModal } from '@/features/projects/components/CreateProjectModal';
import { getProjectsFn } from '@/features/projects/api';
import { tableLayouts } from '@/components/ui/tableLayouts';
import { cn } from '@/lib/utils';

export function ProjectsPage() {
  const [isCreateModalOpen, setIsCreateModalOpen] = useState(false);
  const [search, setSearch] = useState('');
  const [page, setPage] = useState(1);
  const limit = 20;

  const { data, isLoading, refetch, isFetching } = useQuery({
    queryKey: ['projects', page],
    queryFn: () => getProjectsFn(page, limit),
    refetchInterval: 5000,
  });
  const projects = data?.items || [];

  const filteredProjects = projects.filter(p => 
    p.name.toLowerCase().includes(search.toLowerCase())
  );

  return (
    <div className="space-y-6 flex flex-col h-full">
      <div className="flex flex-col sm:flex-row justify-between items-start sm:items-center gap-4 shrink-0">
        <div>
          <h1 className="text-3xl font-bold tracking-tight">Docker Compose</h1>
          <p className="text-slate-500 dark:text-slate-400 mt-1">Оркестрация многоконтейнерных приложений</p>
        </div>
        
        <div className="flex items-center gap-3 w-full sm:w-auto">
          <div className="relative w-full sm:w-64">
            <Search className="absolute left-3 top-1/2 -translate-y-1/2 w-4 h-4 text-slate-400" />
            <Input 
              placeholder="Поиск..." 
              value={search}
              onChange={(e) => {
                setSearch(e.target.value);
                setPage(1);
              }}
              className="pl-9"
            />
          </div>
          <Button variant="secondary" onClick={() => refetch()} isLoading={isFetching} className="px-3">
            <RefreshCcw className="w-4 h-4" />
          </Button>
          <Button onClick={() => setIsCreateModalOpen(true)}>
            <Plus className="w-4 h-4 mr-2" />
            Развернуть
          </Button>
        </div>
      </div>

      <div className="bg-white/40 dark:bg-slate-900/40 backdrop-blur-xl border border-white/50 dark:border-slate-700/50 rounded-2xl overflow-hidden flex-1 flex flex-col">
        <div className="overflow-x-auto flex-1">
          <div className={tableLayouts.projects.minWidth}>
            <div className={cn("grid items-center gap-4 p-4 border-b border-white/20 dark:border-slate-700/50 bg-slate-50/50 dark:bg-slate-800/50 text-sm font-medium text-slate-500 dark:text-slate-400", tableLayouts.projects.grid)}>
              <div className="flex items-center gap-3"><div className="w-10 shrink-0" /><div>Имя проекта</div></div>
              <div>Статус</div>
              <div>Ошибки</div>
              <div>Дата создания</div>
              <div className="flex justify-end">Действия</div>
            </div>

            <div className="flex flex-col">
              {isLoading ? (
                [...Array(5)].map((_, i) => (
                  <div key={i} className="flex gap-4 p-4 items-center border-b border-white/20 dark:border-slate-700/50">
                    <div className="w-10 h-10 rounded-xl bg-slate-200/50 dark:bg-slate-700/50 animate-pulse shrink-0" />
                    <div className="flex-1 space-y-2">
                      <div className="h-4 bg-slate-200/50 dark:bg-slate-700/50 rounded w-1/3 animate-pulse" />
                      <div className="h-3 bg-slate-200/50 dark:bg-slate-700/50 rounded w-1/2 animate-pulse" />
                    </div>
                  </div>
                ))
              ) : filteredProjects.length > 0 ? (
                filteredProjects.map((project) => (
                  <ProjectRow key={project.id} project={project} />
                ))
              ) : (
                <div className="p-12 flex flex-col items-center justify-center text-center">
                  <div className="w-16 h-16 bg-indigo-100 dark:bg-indigo-900/30 text-indigo-600 dark:text-indigo-400 rounded-2xl flex items-center justify-center mb-4">
                    <Layers className="w-8 h-8" />
                  </div>
                  <h3 className="text-xl font-semibold mb-2">Нет проектов</h3>
                  <p className="text-slate-500 dark:text-slate-400 max-w-sm mb-6">
                    Здесь вы можете загрузить архив с docker-compose.yml для развертывания нескольких связанных контейнеров.
                  </p>
                  <Button onClick={() => setIsCreateModalOpen(true)}>Развернуть проект</Button>
                </div>
              )}
            </div>
          </div>
        </div>
        {data && (
          <div className="shrink-0 bg-slate-50/50 dark:bg-slate-800/50">
            <Pagination currentPage={page} pageSize={limit} totalItems={data.total_count} onPageChange={setPage} />
          </div>
        )}
      </div>

      <CreateProjectModal 
        isOpen={isCreateModalOpen} 
        onClose={() => setIsCreateModalOpen(false)} 
      />
    </div>
  );
}
