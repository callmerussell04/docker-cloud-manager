import { Link, useLocation } from 'react-router-dom';
import { LayoutDashboard, Box, Disc, HardDrive, Layers, Settings, ShieldAlert } from 'lucide-react';
import { cn } from '@/lib/utils';
import { useAuthStore } from '@/store/authStore';

const navigation = [
  { name: 'Дашборд', href: '/', icon: LayoutDashboard },
  { name: 'Контейнеры', href: '/containers', icon: Box },
  { name: 'Образы', href: '/images', icon: Disc },
  { name: 'Тома', href: '/volumes', icon: HardDrive },
  { name: 'Docker Compose', href: '/projects', icon: Layers },
];

const adminNavigation = [
  { name: 'Все ресурсы', href: '/admin/resources', icon: ShieldAlert },
  { name: 'Настройки системы', href: '/admin/settings', icon: Settings },
];

export function Sidebar() {
  const location = useLocation();
  const role = useAuthStore((state) => state.role);

  return (
    <aside className="w-64 hidden md:flex flex-col border-r border-white/20 dark:border-slate-700/50 bg-white/30 dark:bg-slate-900/30 backdrop-blur-xl shrink-0 transition-colors">
      <div className="h-16 flex items-center px-6 border-b border-white/20 dark:border-slate-700/50">
        <div className="flex items-center gap-2 text-indigo-600 dark:text-indigo-400">
          <Box className="w-6 h-6 stroke-[2.5]" />
          <span className="font-bold text-lg tracking-tight text-slate-900 dark:text-slate-100">
            CloudManager
          </span>
        </div>
      </div>

      <nav className="flex-1 overflow-y-auto py-4 px-3 space-y-6">
        <div className="space-y-1">
          {navigation.map((item) => {
            const isActive = location.pathname === item.href || 
              (item.href !== '/' && location.pathname.startsWith(item.href) && !location.pathname.startsWith('/admin'));
            
            return (
              <Link
                key={item.name}
                to={item.href}
                className={cn(
                  "flex items-center gap-3 px-3 py-2.5 rounded-xl text-sm font-medium transition-all duration-200",
                  isActive
                    ? "bg-indigo-600/10 text-indigo-700 dark:text-indigo-300 shadow-sm border border-indigo-500/20"
                    : "text-slate-600 dark:text-slate-400 hover:bg-white/50 dark:hover:bg-slate-800/50 hover:text-slate-900 dark:hover:text-slate-100"
                )}
              >
                <item.icon className={cn("w-5 h-5", isActive ? "text-indigo-600 dark:text-indigo-400" : "")} />
                {item.name}
              </Link>
            );
          })}
        </div>

        {role === 'admin' && (
          <div>
            <div className="px-3 mb-2 text-xs font-semibold text-slate-500 uppercase tracking-wider">
              Администрирование
            </div>
            <div className="space-y-1">
              {adminNavigation.map((item) => {
                const isActive = location.pathname.startsWith(item.href);
                
                return (
                  <Link
                    key={item.name}
                    to={item.href}
                    className={cn(
                      "flex items-center gap-3 px-3 py-2.5 rounded-xl text-sm font-medium transition-all duration-200",
                      isActive
                        ? "bg-red-500/10 text-red-700 dark:text-red-400 shadow-sm border border-red-500/20"
                        : "text-slate-600 dark:text-slate-400 hover:bg-white/50 dark:hover:bg-slate-800/50 hover:text-slate-900 dark:hover:text-slate-100"
                    )}
                  >
                    <item.icon className={cn("w-5 h-5", isActive ? "text-red-600 dark:text-red-400" : "")} />
                    {item.name}
                  </Link>
                );
              })}
            </div>
          </div>
        )}
      </nav>
    </aside>
  );
}