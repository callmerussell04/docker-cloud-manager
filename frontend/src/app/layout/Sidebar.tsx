import { useEffect, useRef, type Ref } from 'react';
import { NavLink } from 'react-router-dom';
import {
  Activity,
  BarChart3,
  Box,
  Disc,
  HardDrive,
  Layers,
  LayoutDashboard,
  PanelLeftClose,
  PanelLeftOpen,
  Settings,
  ShieldCheck,
  Users,
  type LucideIcon,
} from 'lucide-react';
import { useAuthStore } from '@/shared/api/sessionStore';
import { useT, type TranslationKey } from '@/shared/i18n';
import { BASE_DOMAIN } from '@/shared/lib/config';
import { cn } from '@/shared/lib/utils';
import { Glass } from '@/shared/ui/Glass';
import { AccountMenu } from './AccountMenu';
import { sidebarInset, sidebarWidth } from './shell';

interface NavEntry {
  nameKey: TranslationKey;
  href: string;
  icon: LucideIcon;
}

const navigation: NavEntry[] = [
  { nameKey: 'nav.dashboard', href: '/', icon: LayoutDashboard },
  { nameKey: 'nav.containers', href: '/containers', icon: Box },
  { nameKey: 'nav.images', href: '/images', icon: Disc },
  { nameKey: 'nav.volumes', href: '/volumes', icon: HardDrive },
  { nameKey: 'nav.projects', href: '/projects', icon: Layers },
];

const adminNavigation: NavEntry[] = [
  { nameKey: 'nav.systemMonitoring', href: '/admin/monitoring', icon: Activity },
  { nameKey: 'nav.adminReports', href: '/admin/reports', icon: BarChart3 },
  { nameKey: 'nav.adminResources', href: '/admin/resources', icon: ShieldCheck },
  { nameKey: 'nav.adminUsers', href: '/admin/users', icon: Users },
  { nameKey: 'nav.systemSettings', href: '/admin/settings', icon: Settings },
];

function NavGroup({ items, compact }: { items: NavEntry[]; compact: boolean }) {
  const t = useT();
  return (
    <ul className="grid gap-px">
      {items.map((item) => {
        const name = t(item.nameKey);
        return (
          <li key={item.href}>
            <NavLink
              to={item.href}
              end={item.href === '/'}
              className={cn('nav-item', compact && 'h-10 justify-center px-0')}
              aria-label={compact ? name : undefined}
              title={compact ? name : undefined}
            >
              <item.icon className={compact ? 'h-[18px] w-[18px]' : 'h-4 w-4'} strokeWidth={1.8} />
              {!compact && name}
            </NavLink>
          </li>
        );
      })}
    </ul>
  );
}

interface SidebarProps {
  expanded: boolean;
  onToggle: () => void;
  ref?: Ref<HTMLDivElement>;
}

// Sidebar floats as clearer glass through a cut-out in the window's frost:
// the pages, then Administration for admins, and the account at its foot.
// Labelled, it has a collapse button that turns it into an icon rail, whose
// expand button turns it back.
export function Sidebar({ expanded, onToggle, ref }: SidebarProps) {
  const t = useT();
  const isAdmin = useAuthStore((state) => state.role) === 'admin';
  const compact = !expanded;
  const toggleLabel = expanded ? t('shell.collapseSidebar') : t('shell.expandSidebar');

  // The toggle button moves between the brand row and the rail when it is
  // used, so focus follows it to its new place.
  const toggleRef = useRef<HTMLButtonElement>(null);
  const toggled = useRef(false);
  useEffect(() => {
    if (!toggled.current) return;
    toggled.current = false;
    toggleRef.current?.focus();
  }, [expanded]);

  const toggle = (
    <button
      ref={toggleRef}
      type="button"
      className={cn('grid shrink-0 place-items-center rounded-lg text-label-2 transition-colors hover:bg-fill-hover hover:text-label', compact ? 'h-9 w-full' : 'h-7 w-7')}
      aria-label={toggleLabel}
      title={toggleLabel}
      aria-expanded={expanded}
      onClick={() => {
        toggled.current = true;
        onToggle();
      }}
    >
      {expanded ? <PanelLeftClose className="h-4 w-4" strokeWidth={1.8} /> : <PanelLeftOpen className="h-[18px] w-[18px]" strokeWidth={1.8} />}
    </button>
  );

  return (
    <Glass
      ref={ref}
      material="sidebar"
      className="absolute z-20 rounded-[24px]"
      style={{ top: sidebarInset, bottom: sidebarInset, left: sidebarInset, width: expanded ? sidebarWidth.expanded : sidebarWidth.rail }}
    >
      <nav aria-label={t('shell.navigation')} className={cn('flex h-full flex-col overflow-y-auto', compact ? 'px-2 py-3.5' : 'p-3.5')}>
        <div className={cn('flex items-center gap-2', compact ? 'justify-center pb-2.5 pt-1' : 'px-2 pb-3 pt-1')}>
          <span className={cn('font-bold tracking-[-0.02em] text-label', compact ? 'text-[13px]' : 'text-[15px]')}>DCM</span>
          {expanded && <span className="min-w-0 flex-1 truncate text-[11.5px] text-label-2">{BASE_DOMAIN}</span>}
          {expanded && toggle}
        </div>
        {compact && <div className="mb-2.5">{toggle}</div>}

        <NavGroup items={navigation} compact={compact} />

        {isAdmin && (
          <>
            {compact ? (
              <div role="separator" aria-label={t('common.admin')} className="mx-2.5 my-3 h-px bg-separator" />
            ) : (
              <div className="px-[9px] pb-1.5 pt-[18px] text-[11px] font-semibold text-label-2">{t('common.admin')}</div>
            )}
            <NavGroup items={adminNavigation} compact={compact} />
          </>
        )}

        <div className="min-h-4 flex-1" />
        <AccountMenu compact={compact} />
      </nav>
    </Glass>
  );
}
