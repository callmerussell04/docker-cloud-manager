import {
  Activity,
  BarChart3,
  Box,
  Disc,
  HardDrive,
  Layers,
  LayoutDashboard,
  Settings,
  ShieldCheck,
  Users,
  type LucideIcon,
} from 'lucide-react';
import type { TranslationKey } from '@/shared/i18n';

// The signed-in pages, as the sidebar lists them and the phone tab bar and
// its More sheet split them.

export interface NavEntry {
  nameKey: TranslationKey;
  href: string;
  icon: LucideIcon;
}

export const navigation: NavEntry[] = [
  { nameKey: 'nav.dashboard', href: '/', icon: LayoutDashboard },
  { nameKey: 'nav.containers', href: '/containers', icon: Box },
  { nameKey: 'nav.images', href: '/images', icon: Disc },
  { nameKey: 'nav.volumes', href: '/volumes', icon: HardDrive },
  { nameKey: 'nav.projects', href: '/projects', icon: Layers },
];

export const adminNavigation: NavEntry[] = [
  { nameKey: 'nav.systemMonitoring', href: '/admin/monitoring', icon: Activity },
  { nameKey: 'nav.adminReports', href: '/admin/reports', icon: BarChart3 },
  { nameKey: 'nav.adminResources', href: '/admin/resources', icon: ShieldCheck },
  { nameKey: 'nav.adminUsers', href: '/admin/users', icon: Users },
  { nameKey: 'nav.systemSettings', href: '/admin/settings', icon: Settings },
];

// The phone tab bar shows this many of the user's pages as tabs; the rest go
// in its More sheet.
export const tabCount = 4;
