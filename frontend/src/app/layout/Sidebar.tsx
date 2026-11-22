import { Link, useLocation } from 'react-router-dom';
import { Activity, BarChart3, LayoutDashboard, Box, Disc, HardDrive, Layers, Settings, ShieldAlert, Users } from 'lucide-react';
import { cn } from '@/shared/lib/utils';
import { useAuthStore } from '@/shared/api/sessionStore';
import { useT, type TranslationKey } from '@/shared/i18n';

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
        "w-64 flex-col border-r border-separator transition-colors",
        mobile ? "flex h-full glass-dense" : "hidden shrink-0 glass-sidebar md:flex"
      )}
    >
      <div className="h-16 flex items-center px-6 border-b border-separator">
        <div className="flex items-center gap-2 text-label">
          <Box className="w-6 h-6 stroke-[2.5]" />
          <span className="font-bold text-lg tracking-tight text-label">
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
                    ? "bg-fill-hover text-label"
                    : "text-label-2 hover:bg-fill hover:text-label"
                )}
              >
                <item.icon className={cn("w-5 h-5", isActive && "text-accent")} />
                {t(item.nameKey as TranslationKey)}
              </Link>
            );
          })}
        </div>

        {role === 'admin' && (
          <div>
            <div className="px-3 mb-2 text-xs font-semibold text-label-3 uppercase tracking-wider">
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
                    ? "bg-fill-hover text-label"
                    : "text-label-2 hover:bg-fill hover:text-label"
                    )}
                  >
                    <item.icon className={cn("w-5 h-5", isActive && "text-accent")} />
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
