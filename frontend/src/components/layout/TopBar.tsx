import { Moon, Sun, LogOut, User } from 'lucide-react';
import { useThemeStore } from '@/store/themeStore';
import { useAuthStore } from '@/store/authStore';
import { logoutFn } from '@/features/auth/api';
import { useToastStore } from '@/store/toastStore';
import { useNavigate } from 'react-router-dom';

export function TopBar() {
  const { theme, toggleTheme } = useThemeStore();
  const { logout } = useAuthStore();
  const addToast = useToastStore((state) => state.addToast);
  const navigate = useNavigate();

  const handleLogout = async () => {
    try {
      await logoutFn();
      logout();
      navigate('/login');
    } catch {
      addToast('Ошибка при выходе из системы', 'error');
    }
  };

  return (
    <header className="h-16 shrink-0 border-b border-white/20 dark:border-slate-700/50 bg-white/30 dark:bg-slate-900/30 backdrop-blur-xl flex items-center justify-between px-6 z-10 transition-colors">
      <div className="flex items-center gap-4">
      </div>

      <div className="flex items-center gap-3">
        <button
          onClick={toggleTheme}
          className="p-2.5 rounded-xl bg-white/50 dark:bg-slate-800/50 hover:bg-white/80 dark:hover:bg-slate-700/50 border border-white/50 dark:border-slate-600/50 transition-colors text-slate-700 dark:text-slate-300"
        >
          {theme === 'dark' ? <Sun className="w-4 h-4" /> : <Moon className="w-4 h-4" />}
        </button>

        <div className="h-8 w-px bg-slate-300/50 dark:bg-slate-700/50 mx-1" />

        <div className="flex items-center gap-2 px-3 py-1.5 rounded-xl bg-white/40 dark:bg-slate-800/40 border border-white/50 dark:border-slate-700/50">
          <div className="w-7 h-7 rounded-full bg-indigo-100 dark:bg-indigo-900/50 flex items-center justify-center text-indigo-600 dark:text-indigo-400">
            <User className="w-4 h-4" />
          </div>
          <span className="text-sm font-medium text-slate-700 dark:text-slate-300 hidden sm:block">
            Пользователь
          </span>
        </div>

        <button
          onClick={handleLogout}
          className="p-2.5 rounded-xl bg-red-500/10 hover:bg-red-500/20 text-red-600 dark:text-red-400 border border-red-500/20 transition-colors"
          title="Выйти"
        >
          <LogOut className="w-4 h-4" />
        </button>
      </div>
    </header>
  );
}