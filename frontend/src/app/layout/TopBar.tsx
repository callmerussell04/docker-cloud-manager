import { Languages, Menu, Moon, Sun, LogOut, User } from 'lucide-react';
import { useThemeStore } from '@/shared/lib/themeStore';
import { useAuthStore } from '@/shared/api/sessionStore';
import { useLanguageStore, type Locale } from '@/shared/lib/languageStore';
import { leaveForProviderSignOut, logoutFn } from '@/features/auth/api';
import { useToastStore } from '@/shared/ui/toastStore';
import { useNavigate } from 'react-router-dom';
import { getApiErrorMessage } from '@/shared/api/apiError';
import { useT } from '@/shared/i18n';
import { cn } from '@/shared/lib/utils';
import { Glass } from '@/shared/ui/Glass';

interface TopBarProps {
  onMenuClick?: () => void;
}

export function TopBar({ onMenuClick }: TopBarProps) {
  const { theme, toggleTheme } = useThemeStore();
  const { language, setLanguage } = useLanguageStore();
  const { logout, username } = useAuthStore();
  const addToast = useToastStore((state) => state.addToast);
  const navigate = useNavigate();
  const t = useT();

  const handleLogout = async () => {
    try {
      const destination = await logoutFn();
      logout();
      if (!leaveForProviderSignOut(destination)) navigate('/login');
    } catch (error) {
      const { message } = getApiErrorMessage(error, t('topbar.logoutFailed'), t);
      addToast(message, 'error');
    }
  };

  const languageOptions: Locale[] = ['ru', 'en'];

  return (
    <header className="h-16 shrink-0 flex items-center justify-between px-6 z-10">
      <div className="flex items-center gap-4">
        <button
          onClick={onMenuClick}
          className="glass rounded-full p-2.5 text-label-2 transition-colors hover:text-label md:hidden"
          aria-label={t('topbar.openNavigation')}
        >
          <Menu className="h-4 w-4" />
        </button>
      </div>

      <div className="flex items-center gap-3">
        <button title={t('theme.switch')} aria-label={t('theme.switch')}
          onClick={toggleTheme}
          className="glass rounded-full p-2.5 text-label-2 transition-colors hover:text-label"
        >
          {theme === 'dark' ? <Sun className="w-4 h-4" /> : <Moon className="w-4 h-4" />}
        </button>

        <Glass
          className="flex items-center gap-1 rounded-full p-1 text-label-2"
          aria-label={t('language.switch')}
        >
          <Languages className="ml-1 h-4 w-4 text-label-3" />
          {languageOptions.map((option) => (
            <button
              key={option}
              type="button"
              onClick={() => setLanguage(option)}
              className={cn(
                "h-7 rounded-full px-2.5 text-xs font-semibold transition-colors",
                language === option
                  ? "bg-accent-fill text-on-accent"
                  : "text-label-2 hover:bg-fill"
              )}
              title={t('language.switch')}
            >
              {t(option === 'ru' ? 'language.ru' : 'language.en')}
            </button>
          ))}
        </Glass>

        <div className="h-8 w-px bg-separator mx-1" />

        <Glass className="flex items-center gap-2 rounded-full py-1 pl-1 pr-3">
          <div className="w-7 h-7 rounded-full bg-fill flex items-center justify-center text-label-2">
            <User className="w-4 h-4" />
          </div>
          <span className="text-sm font-medium text-label hidden sm:block truncate max-w-[150px]">
            {username || t('common.user')}
          </span>
        </Glass>

        <button
          onClick={handleLogout}
          className="glass rounded-full p-2.5 text-label-2 transition-colors hover:text-red-text"
          title={t('topbar.logout')}
          aria-label={t('topbar.logout')}
        >
          <LogOut className="w-4 h-4" />
        </button>
      </div>
    </header>
  );
}
