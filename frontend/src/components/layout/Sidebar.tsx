import { Link, useLocation } from 'react-router-dom';
import { Activity, BarChart3, LayoutDashboard, Box, Disc, HardDrive, Layers, Settings, ShieldAlert, Users } from 'lucide-react';
import { cn } from '@/lib/utils';
import { useAuthStore } from '@/store/authStore';
import { useT, type TranslationKey } from '@/lib/i18n';

const navigation = [
  { nameKey: 'nav.dashboard', href: '/', icon: LayoutDashboard },
  { nameKey: 'nav.containers', href: '/containers', icon: Box },
  { nameKey: 'nav.images', href: '/images', icon: Disc },
  { nameKey: 'nav.volumes', href: '/volumes', icon: HardDrive },
  { nameKey: 'nav.projects', href: '/projects', icon: Layers },
];

const adminNavigation = [
  { nameKey: 'nav.systemMonitoring', href: '/admin/monitoring', icon: Activity },
  { nameKey: 'nav.adminReports', href: '/admin/reports', icon: BarChart3 },
  { nameKey: 'nav.adminResources', href: '/admin/resources', icon: ShieldAlert },
  { nameKey: 'nav.adminUsers', href: '/admin/users', icon: Users },
  { nameKey: 'nav.systemSettings', href: '/admin/settings', icon: Settings },
];

interface SidebarProps {
  mobile?: boolean;
  onNavigate?: () => void;
}

export function Sidebar({ mobile = false, onNavigate }: SidebarProps) {
  const location = useLocation();
  const role = useAuthStore((state) => state.role);
  const t = useT();

  return (
    <aside
      className={cn(
        "w-64 flex-col border-r border-white/20 bg-white/90 backdrop-blur-xl transition-colors dark:border-slate-700/50 dark:bg-slate-900/90",
        mobile ? "flex h-full shadow-2xl" : "hidden shrink-0 md:flex"
      )}
    >
      <div className="h-16 flex items-center px-6 border-b border-white/20 dark:border-slate-700/50">
        <div className="flex items-center gap-2 text-indigo-600 dark:text-indigo-400">
          <Box className="w-6 h-6 stroke-[2.5]" />
          <span className="font-bold text-lg tracking-tight text-slate-900 dark:text-slate-100">
            DCM
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
                key={item.nameKey}
                to={item.href}
                onClick={onNavigate}
                className={cn(
                  "flex items-center gap-3 px-3 py-2.5 rounded-xl text-sm font-medium transition-all duration-200",
                  isActive
                    ? "bg-indigo-600/10 text-indigo-700 dark:text-indigo-300 shadow-sm border border-indigo-500/20"
                    : "text-slate-600 dark:text-slate-400 hover:bg-white/50 dark:hover:bg-slate-800/50 hover:text-slate-900 dark:hover:text-slate-100"
                )}
              >
                <item.icon className={cn("w-5 h-5", isActive ? "text-indigo-600 dark:text-indigo-400" : "")} />
                {t(item.nameKey as TranslationKey)}
              </Link>
            );
          })}
        </div>

        {role === 'admin' && (
          <div>
            <div className="px-3 mb-2 text-xs font-semibold text-slate-500 uppercase tracking-wider">
              {t('common.admin')}
            </div>
            <div className="space-y-1">
              {adminNavigation.map((item) => {
                const isActive = location.pathname.startsWith(item.href);
                
                return (
                  <Link
                    key={item.nameKey}
                    to={item.href}
                    onClick={onNavigate}
                    className={cn(
                      "flex items-center gap-3 px-3 py-2.5 rounded-xl text-sm font-medium transition-all duration-200",
                      isActive
                        ? "bg-red-500/10 text-red-700 dark:text-red-400 shadow-sm border border-red-500/20"
                        : "text-slate-600 dark:text-slate-400 hover:bg-white/50 dark:hover:bg-slate-800/50 hover:text-slate-900 dark:hover:text-slate-100"
                    )}
                  >
                    <item.icon className={cn("w-5 h-5", isActive ? "text-red-600 dark:text-red-400" : "")} />
                    {t(item.nameKey as TranslationKey)}
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
