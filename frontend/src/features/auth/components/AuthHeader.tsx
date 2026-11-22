import { Box, Languages, Moon, Sun } from 'lucide-react';

import { useLanguageStore, type Locale } from '@/shared/lib/languageStore';
import { useThemeStore } from '@/shared/lib/themeStore';
import { useT } from '@/shared/i18n';
import { cn } from '@/shared/lib/utils';
import { Glass } from '@/shared/ui/Glass';

const languageOptions: Locale[] = ['ru', 'en'];

export function AuthHeader() {
  const { theme, toggleTheme } = useThemeStore();
  const { language, setLanguage } = useLanguageStore();
  const t = useT();

  return (
    <header className="h-16 shrink-0 px-4 sm:px-6">
      <div className="mx-auto flex h-full w-full max-w-6xl items-center justify-between gap-4">
        <div className="flex min-w-0 items-center gap-3 text-label">
          <Box className="h-8 w-8 shrink-0 stroke-[2]" />
          <span className="truncate text-lg font-bold tracking-tight sm:text-xl">
            {t('app.name')}
          </span>
        </div>

        <div className="flex shrink-0 items-center gap-2 sm:gap-3">
          <button
            type="button"
            onClick={toggleTheme}
            className="glass rounded-full p-2.5 text-label-2 transition-colors hover:text-label"
            aria-label={t('theme.switch')}
            title={t('theme.switch')}
          >
            {theme === 'dark' ? <Sun className="h-4 w-4" /> : <Moon className="h-4 w-4" />}
          </button>

          <Glass
            className="flex items-center gap-1 rounded-full p-1 text-label-2"
            aria-label={t('language.switch')}
          >
            <Languages className="ml-1 hidden h-4 w-4 text-label-3 sm:block" />
            {languageOptions.map((option) => (
              <button
                key={option}
                type="button"
                onClick={() => setLanguage(option)}
                className={cn(
                  'h-7 rounded-full px-2.5 text-xs font-semibold transition-colors',
                  language === option
                    ? 'bg-accent-fill text-on-accent'
                    : 'text-label-2 hover:bg-fill'
                )}
                title={t('language.switch')}
              >
                {t(option === 'ru' ? 'language.ru' : 'language.en')}
              </button>
            ))}
          </Glass>
        </div>
      </div>
    </header>
  );
}
