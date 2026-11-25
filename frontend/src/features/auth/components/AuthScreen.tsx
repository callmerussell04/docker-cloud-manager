import type { ReactNode } from 'react';
import { Languages, Moon, Sun } from 'lucide-react';

import { useLanguageStore } from '@/shared/lib/languageStore';
import { useThemeStore } from '@/shared/lib/themeStore';
import { useT } from '@/shared/i18n';
import { cn } from '@/shared/lib/utils';
import { Glass } from '@/shared/ui/Glass';

// AuthScreen is every sign-in screen: one dense glass card in the middle of
// the window with the DCM name above it, over the wallpaper blurred to soft
// shapes, and language and appearance in a small glass control in the corner.
export function AuthScreen({ children }: { children: ReactNode }) {
  const t = useT();
  return (
    <div className="relative isolate flex min-h-screen flex-col">
      <div aria-hidden className="frost-veil fixed inset-0 -z-10" />
      <main className="flex flex-1 flex-col items-center justify-center gap-[18px] px-4 pb-10 pt-20">
        <div className="text-xl font-bold tracking-[-0.02em] text-label">{t('app.name')}</div>
        <Glass material="dense" className="w-full max-w-[420px] rounded-[28px] px-[30px] pb-[26px] pt-[30px] max-[420px]:px-5">
          {children}
        </Glass>
      </main>
      <AuthCorner />
    </div>
  );
}

function AuthCorner() {
  const { theme, toggleTheme } = useThemeStore();
  const { language, setLanguage } = useLanguageStore();
  const t = useT();
  const cornerButtonClass = 'inline-flex h-8 items-center gap-1.5 rounded-full px-3 font-medium text-label-2 transition-colors hover:bg-fill hover:text-label';
  return (
    <Glass role="group" aria-label={t('auth.corner')} className="absolute right-4 top-4 inline-flex gap-0.5 rounded-full p-1">
      <button
        type="button"
        onClick={() => setLanguage(language === 'ru' ? 'en' : 'ru')}
        className={cornerButtonClass}
        aria-label={`${t('language.switch')}: ${t(language === 'ru' ? 'language.ruName' : 'language.enName')}`}
        title={t('language.switch')}
      >
        <Languages className="h-[15px] w-[15px]" />
        <span aria-hidden>{t(language === 'ru' ? 'language.ru' : 'language.en')}</span>
      </button>
      <button
        type="button"
        onClick={toggleTheme}
        className={cn(cornerButtonClass, 'w-8 justify-center px-0')}
        aria-label={t('theme.switch')}
        title={t('theme.switch')}
      >
        {theme === 'dark' ? <Sun className="h-[15px] w-[15px]" /> : <Moon className="h-[15px] w-[15px]" />}
      </button>
    </Glass>
  );
}

const titleClass = 'text-2xl font-bold tracking-[-0.025em] text-label';

// AuthHeading is a screen's title and its one line of explanation.
export function AuthHeading({ title, subtitle }: { title: string; subtitle?: string }) {
  return (
    <div className="mb-1.5">
      <h1 className={titleClass}>{title}</h1>
      {subtitle && <p className="mt-1 leading-normal text-label-2">{subtitle}</p>}
    </div>
  );
}

// AuthState is a screen with no form, centred: an optional mark, the title,
// one line of explanation, then its actions.
export function AuthState({ role, mark, title, description, children }: {
  role: 'status' | 'alert';
  mark?: ReactNode;
  title: string;
  description: string;
  children?: ReactNode;
}) {
  return (
    <div role={role} className="grid justify-items-center gap-3 text-center">
      {mark}
      <h1 className={titleClass}>{title}</h1>
      <p className="leading-normal text-label-2">{description}</p>
      {children}
    </div>
  );
}

// AuthNotice is a short note in the card: bad for a failure the user can fix
// by trying again, plain for something unavailable.
export function AuthNotice({ tone = 'plain', children }: { tone?: 'plain' | 'bad'; children: ReactNode }) {
  return (
    <div
      role={tone === 'bad' ? 'alert' : 'note'}
      className={cn(
        'rounded-xl px-3.5 py-2.5 leading-[1.45]',
        tone === 'bad' ? 'bg-red/10 text-red-text' : 'bg-fill text-label-2',
      )}
    >
      {children}
    </div>
  );
}

// AuthDivider is a hairline with a short word in the middle.
export function AuthDivider({ children }: { children: ReactNode }) {
  return (
    <div className="flex items-center gap-3 text-xs text-label-3 before:h-px before:flex-1 before:bg-separator after:h-px after:flex-1 after:bg-separator">
      {children}
    </div>
  );
}

// authFieldClass and authButtonClass give the card's fields and buttons their
// larger sign-in size.
export const authFieldClass = 'h-11 text-[15px]';
export const authButtonClass = 'h-11 w-full text-[15px]';
