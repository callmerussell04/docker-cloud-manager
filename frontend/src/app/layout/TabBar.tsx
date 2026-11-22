import { matchPath, NavLink, useLocation, useNavigate } from 'react-router-dom';
import { MoreHorizontal } from 'lucide-react';
import { useAuthStore } from '@/shared/api/sessionStore';
import { useT } from '@/shared/i18n';
import { cn } from '@/shared/lib/utils';
import { Glass } from '@/shared/ui/Glass';
import { Menu, MenuGroup, MenuItem, MenuLabel, MenuSeparator } from '@/shared/ui/Menu';
import { AccountMenuItems } from './AccountMenu';
import { adminNavigation, navigation, tabCount, type NavEntry } from './navigation';
import { useAccountName } from './useAccountName';

// TabBar replaces the sidebar on phones: a glass capsule at the bottom with
// the most used pages as icons only, then More, whose sheet holds the other
// pages, Administration for admins, and the account. The current page is
// marked by its highlight alone; More takes the highlight when the current
// page is in its sheet.
export function TabBar() {
  const t = useT();
  const navigate = useNavigate();
  const { pathname } = useLocation();
  const isAdmin = useAuthStore((state) => state.role) === 'admin';
  const name = useAccountName();

  const tabs = navigation.slice(0, tabCount);
  const rest = navigation.slice(tabCount);
  const isCurrent = (entry: NavEntry) => matchPath({ path: entry.href, end: false }, pathname) !== null;
  const moreCurrent = [...rest, ...(isAdmin ? adminNavigation : [])].some(isCurrent);

  const moreItem = (entry: NavEntry) => (
    <MenuItem key={entry.href} icon={entry.icon} current={isCurrent(entry)} onSelect={() => navigate(entry.href)}>
      {t(entry.nameKey)}
    </MenuItem>
  );

  return (
    <Glass className="absolute inset-x-3 bottom-3 z-20 h-16 rounded-full p-[5px]">
      <nav aria-label={t('shell.navigation')} className="flex h-full">
        {tabs.map((entry) => {
          const label = t(entry.nameKey);
          return (
            <NavLink key={entry.href} to={entry.href} end={entry.href === '/'} className="tab" aria-label={label} title={label}>
              <entry.icon className="h-5 w-5" strokeWidth={1.8} />
            </NavLink>
          );
        })}
        <Menu
          label={t('shell.more')}
          iconOnly
          triggerClassName={cn('tab', moreCurrent && 'tab-current')}
          trigger={<MoreHorizontal className="h-5 w-5" strokeWidth={1.8} />}
        >
          {rest.map(moreItem)}
          {isAdmin && (
            <>
              <MenuSeparator />
              <MenuGroup label={t('common.admin')}>{adminNavigation.map(moreItem)}</MenuGroup>
            </>
          )}
          <MenuSeparator />
          <MenuLabel>{name}</MenuLabel>
          <AccountMenuItems />
        </Menu>
      </nav>
    </Glass>
  );
}
