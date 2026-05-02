import { useState } from 'react';
import { useQuery } from '@tanstack/react-query';
import { Plus, RefreshCcw, Search, Box } from 'lucide-react';

import { Button } from '@/components/ui/Button';
import { Input } from '@/components/ui/Input';
import { ContainerRow } from '@/features/containers/components/ContainerRow';
import { CreateContainerModal } from '@/features/containers/components/CreateContainerModal';
import { ExposeContainerModal } from '@/features/containers/components/ExposeContainerModal';
import { ContainerLogsModal } from '@/features/containers/components/ContainerLogsModal';
import { ContainerTerminalModal } from '@/features/containers/components/ContainerTerminalModal';
import { getContainersFn } from '@/features/containers/api';
import { type ContainerData } from '@/features/containers/types';

export function ContainersPage() {
  const [isCreateModalOpen, setIsCreateModalOpen] = useState(false);
  const [exposeContainer, setExposeContainer] = useState<ContainerData | null>(null);
  const[logsContainer, setLogsContainer] = useState<ContainerData | null>(null);
  const [terminalContainer, setTerminalContainer] = useState<ContainerData | null>(null);
  const [search, setSearch] = useState('');

  const { data: containers = [], isLoading, refetch, isFetching } = useQuery({
    queryKey:['containers'],
    queryFn: getContainersFn,
  });

  const filteredContainers = containers.filter(c => 
    c.name.toLowerCase().includes(search.toLowerCase()) || 
    c.image_tag.toLowerCase().includes(search.toLowerCase())
  );

  return (
    <div className="space-y-6 flex flex-col h-full">
      <div className="flex flex-col sm:flex-row justify-between items-start sm:items-center gap-4 shrink-0">
        <div>
          <h1 className="text-3xl font-bold tracking-tight">Контейнеры</h1>
          <p className="text-slate-500 dark:text-slate-400 mt-1">Управление вычислительными ресурсами</p>
        </div>
        
        <div className="flex items-center gap-3 w-full sm:w-auto">
          <div className="relative w-full sm:w-64">
            <Search className="absolute left-3 top-1/2 -translate-y-1/2 w-4 h-4 text-slate-400" />
            <Input 
              placeholder="Поиск..." 
              value={search}
              onChange={(e) => setSearch(e.target.value)}
              className="pl-9"
            />
          </div>
          <Button variant="secondary" onClick={() => refetch()} isLoading={isFetching} className="px-3">
            <RefreshCcw className="w-4 h-4" />
          </Button>
          <Button onClick={() => setIsCreateModalOpen(true)}>
            <Plus className="w-4 h-4 mr-2" />
            Создать
          </Button>
        </div>
      </div>

      <div className="bg-white/40 dark:bg-slate-900/40 backdrop-blur-xl border border-white/50 dark:border-slate-700/50 rounded-2xl overflow-hidden flex-1 flex flex-col">
        <div className="overflow-x-auto flex-1">
          <div className="min-w-[900px]">
            <div className="grid grid-cols-[2fr_1fr_1.5fr_auto] gap-4 p-4 border-b border-white/20 dark:border-slate-700/50 bg-slate-50/50 dark:bg-slate-800/50 text-sm font-medium text-slate-500 dark:text-slate-400">
              <div className="flex items-center gap-4"><div className="w-10 shrink-0" /><div>Контейнер</div></div>
              <div>Статус</div>
              <div>Маршрутизация</div>
              <div className="text-right pr-2">Действия</div>
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
              ) : filteredContainers.length > 0 ? (
                filteredContainers.map((container) => (
                  <ContainerRow 
                    key={container.id} 
                    container={container} 
                    onExpose={setExposeContainer}
                    onViewLogs={setLogsContainer}
                    onOpenTerminal={setTerminalContainer}
                  />
                ))
              ) : (
                <div className="p-12 flex flex-col items-center justify-center text-center">
                  <div className="w-16 h-16 bg-indigo-100 dark:bg-indigo-900/30 text-indigo-600 dark:text-indigo-400 rounded-2xl flex items-center justify-center mb-4">
                    <Box className="w-8 h-8" />
                  </div>
                  <h3 className="text-xl font-semibold mb-2">Нет контейнеров</h3>
                  <p className="text-slate-500 dark:text-slate-400 max-w-sm mb-6">
                    У вас пока нет запущенных контейнеров. Создайте свой первый контейнер, чтобы начать работу.
                  </p>
                  <Button onClick={() => setIsCreateModalOpen(true)}>Создать контейнер</Button>
                </div>
              )}
            </div>
          </div>
        </div>
      </div>

      <CreateContainerModal 
        isOpen={isCreateModalOpen} 
        onClose={() => setIsCreateModalOpen(false)} 
      />

      <ExposeContainerModal 
        container={exposeContainer} 
        onClose={() => setExposeContainer(null)} 
      />

      <ContainerLogsModal 
        container={logsContainer}
        onClose={() => setLogsContainer(null)}
      />

      <ContainerTerminalModal
        container={terminalContainer}
        onClose={() => setTerminalContainer(null)}
      />
    </div>
  );
}