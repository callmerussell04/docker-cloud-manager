import { Box, Languages, Moon, Sun } from 'lucide-react';

import { useLanguageStore, type Locale } from '@/store/languageStore';
import { useThemeStore } from '@/store/themeStore';
import { useT } from '@/lib/i18n';
import { cn } from '@/lib/utils';

const languageOptions: Locale[] = ['ru', 'en'];

export function AuthHeader() {
  const { theme, toggleTheme } = useThemeStore();
  const { language, setLanguage } = useLanguageStore();
  const t = useT();

  return (
    <header className="h-16 shrink-0 border-b border-white/20 bg-white/30 px-4 backdrop-blur-xl transition-colors dark:border-slate-700/50 dark:bg-slate-900/30 sm:px-6">
      <div className="mx-auto flex h-full w-full max-w-6xl items-center justify-between gap-4">
        <div className="flex min-w-0 items-center gap-3 text-indigo-600 dark:text-indigo-400">
          <Box className="h-8 w-8 shrink-0 stroke-[2]" />
          <span className="truncate text-lg font-bold tracking-tight text-slate-900 dark:text-slate-100 sm:text-xl">
            {t('app.name')}
          </span>
        </div>

        <div className="flex shrink-0 items-center gap-2 sm:gap-3">
          <button
            type="button"
            onClick={toggleTheme}
            className="rounded-xl border border-white/50 bg-white/50 p-2.5 text-slate-700 transition-colors hover:bg-white/80 dark:border-slate-600/50 dark:bg-slate-800/50 dark:text-slate-300 dark:hover:bg-slate-700/50"
            aria-label={t('theme.switch')}
            title={t('theme.switch')}
          >
            {theme === 'dark' ? <Sun className="h-4 w-4" /> : <Moon className="h-4 w-4" />}
          </button>

          <div
            className="flex items-center gap-1 rounded-xl border border-white/50 bg-white/50 p-1 text-slate-700 dark:border-slate-600/50 dark:bg-slate-800/50 dark:text-slate-300"
            aria-label={t('language.switch')}
          >
            <Languages className="ml-1 hidden h-4 w-4 text-slate-500 dark:text-slate-400 sm:block" />
            {languageOptions.map((option) => (
              <button
                key={option}
                type="button"
                onClick={() => setLanguage(option)}
                className={cn(
                  'h-7 rounded-lg px-2 text-xs font-semibold transition-colors',
                  language === option
                    ? 'bg-indigo-600 text-white shadow-sm'
                    : 'text-slate-600 hover:bg-white/70 dark:text-slate-300 dark:hover:bg-slate-700/70'
                )}
                title={t('language.switch')}
              >
                {t(option === 'ru' ? 'language.ru' : 'language.en')}
              </button>
            ))}
          </div>
        </div>
      </div>
    </header>
  );
}
