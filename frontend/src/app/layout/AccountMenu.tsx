import { ChevronsUpDown, Languages, LogOut, Moon, Sun } from 'lucide-react';
import { useNavigate } from 'react-router-dom';
import { leaveForProviderSignOut, logoutFn } from '@/features/auth/api';
import { failureReason } from '@/shared/api/apiError';
import { useAuthStore } from '@/shared/api/sessionStore';
import { useT } from '@/shared/i18n';
import { useLanguageStore } from '@/shared/lib/languageStore';
import { useThemeStore } from '@/shared/lib/themeStore';
import { cn } from '@/shared/lib/utils';
import { Menu, MenuGroup, MenuItem, MenuSeparator } from '@/shared/ui/Menu';
import { useNotificationStore } from '@/shared/ui/notificationStore';
import { useAccountName } from './useAccountName';

// AccountMenuItems are the account's choices: the appearance, the language
// and logging out. The sidebar's AccountMenu and the phone tab bar's More
// sheet both hold them.
export function AccountMenuItems() {
  const t = useT();
  const navigate = useNavigate();
  const { theme, setTheme } = useThemeStore();
  const { language, setLanguage } = useLanguageStore();
  const logout = useAuthStore((state) => state.logout);
  const notify = useNotificationStore((state) => state.notify);

  const handleLogout = async () => {
    try {
      const destination = await logoutFn();
      logout();
      if (!leaveForProviderSignOut(destination)) navigate('/login');
    } catch (error) {
      notify(t('shell.logoutFailed'), 'error', failureReason(error, t));
    }
  };

  return (
    <>
      <MenuGroup label={t('shell.appearance')}>
        <MenuItem icon={Sun} checked={theme === 'light'} onSelect={() => setTheme('light')}>{t('shell.appearanceLight')}</MenuItem>
        <MenuItem icon={Moon} checked={theme === 'dark'} onSelect={() => setTheme('dark')}>{t('shell.appearanceDark')}</MenuItem>
      </MenuGroup>
      <MenuSeparator />
      <MenuGroup label={t('shell.language')}>
        <MenuItem icon={Languages} checked={language === 'ru'} onSelect={() => setLanguage('ru')}>{t('language.ruName')}</MenuItem>
        <MenuItem icon={Languages} checked={language === 'en'} onSelect={() => setLanguage('en')}>{t('language.enName')}</MenuItem>
      </MenuGroup>
      <MenuSeparator />
      <MenuItem icon={LogOut} onSelect={handleLogout}>{t('shell.logout')}</MenuItem>
    </>
  );
}

// AccountMenu sits at the foot of the sidebar: the signed-in user, and in its
// menu the appearance, the language and logging out.
export function AccountMenu({ compact }: { compact: boolean }) {
  const t = useT();
  const name = useAccountName();

  return (
    <Menu
      label={t('shell.account', { name })}
      iconOnly={compact}
      side="above-start"
      triggerClassName={cn('nav-item h-11', compact && 'justify-center px-0')}
      trigger={
        <>
          <span aria-hidden className="grid h-[26px] w-[26px] shrink-0 place-items-center rounded-full bg-fill-hover text-[11.5px] font-semibold text-label">
            {name.slice(0, 1).toUpperCase()}
          </span>
          {!compact && <span className="flex-1 truncate text-left font-[550]">{name}</span>}
          {!compact && <ChevronsUpDown className="h-3.5 w-3.5" />}
        </>
      }
    >
      <AccountMenuItems />
    </Menu>
  );
}
