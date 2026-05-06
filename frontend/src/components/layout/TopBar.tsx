import { Languages, Menu, Moon, Sun, LogOut, User } from 'lucide-react';
import { useThemeStore } from '@/store/themeStore';
import { useAuthStore } from '@/store/authStore';
import { useLanguageStore, type Locale } from '@/store/languageStore';
import { logoutFn } from '@/features/auth/api';
import { useToastStore } from '@/store/toastStore';
import { useNavigate } from 'react-router-dom';
import { getApiErrorMessage } from '@/lib/apiError';
import { useT } from '@/lib/i18n';
import { cn } from '@/lib/utils';

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
      await logoutFn();
      logout();
      navigate('/login');
    } catch (error) {
      const { message, requestId } = getApiErrorMessage(error, t('topbar.logoutFailed'), t);
      addToast(message, 'error', { requestId });
    }
  };

  const languageOptions: Locale[] = ['ru', 'en'];

  return (
    <header className="h-16 shrink-0 border-b border-white/20 dark:border-slate-700/50 bg-white/30 dark:bg-slate-900/30 backdrop-blur-xl flex items-center justify-between px-6 z-10 transition-colors">
      <div className="flex items-center gap-4">
        <button
          onClick={onMenuClick}
          className="rounded-xl border border-white/50 bg-white/50 p-2.5 text-slate-700 transition-colors hover:bg-white/80 dark:border-slate-600/50 dark:bg-slate-800/50 dark:text-slate-300 dark:hover:bg-slate-700/50 md:hidden"
          aria-label={t('topbar.openNavigation')}
        >
          <Menu className="h-4 w-4" />
        </button>
      </div>

      <div className="flex items-center gap-3">
        <button
          onClick={toggleTheme}
          className="p-2.5 rounded-xl bg-white/50 dark:bg-slate-800/50 hover:bg-white/80 dark:hover:bg-slate-700/50 border border-white/50 dark:border-slate-600/50 transition-colors text-slate-700 dark:text-slate-300"
        >
          {theme === 'dark' ? <Sun className="w-4 h-4" /> : <Moon className="w-4 h-4" />}
        </button>

        <div
          className="flex items-center gap-1 rounded-xl border border-white/50 bg-white/50 p-1 text-slate-700 dark:border-slate-600/50 dark:bg-slate-800/50 dark:text-slate-300"
          aria-label={t('language.switch')}
        >
          <Languages className="ml-1 h-4 w-4 text-slate-500 dark:text-slate-400" />
          {languageOptions.map((option) => (
            <button
              key={option}
              type="button"
              onClick={() => setLanguage(option)}
              className={cn(
                "h-7 rounded-lg px-2 text-xs font-semibold transition-colors",
                language === option
                  ? "bg-indigo-600 text-white shadow-sm"
                  : "text-slate-600 hover:bg-white/70 dark:text-slate-300 dark:hover:bg-slate-700/70"
              )}
              title={t('language.switch')}
            >
              {t(option === 'ru' ? 'language.ru' : 'language.en')}
            </button>
          ))}
        </div>

        <div className="h-8 w-px bg-slate-300/50 dark:bg-slate-700/50 mx-1" />

        <div className="flex items-center gap-2 px-3 py-1.5 rounded-xl bg-white/40 dark:bg-slate-800/40 border border-white/50 dark:border-slate-700/50">
          <div className="w-7 h-7 rounded-full bg-indigo-100 dark:bg-indigo-900/50 flex items-center justify-center text-indigo-600 dark:text-indigo-400">
            <User className="w-4 h-4" />
          </div>
          <span className="text-sm font-medium text-slate-700 dark:text-slate-300 hidden sm:block truncate max-w-[150px]">
            {username || t('common.user')}
          </span>
        </div>

        <button
          onClick={handleLogout}
          className="p-2.5 rounded-xl bg-red-500/10 hover:bg-red-500/20 text-red-600 dark:text-red-400 border border-red-500/20 transition-colors"
          title={t('topbar.logout')}
        >
          <LogOut className="w-4 h-4" />
        </button>
      </div>
    </header>
  );
}
