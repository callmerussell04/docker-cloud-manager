import { useEffect, type ReactNode } from 'react';
import { useLanguageStore } from '@/shared/lib/languageStore';

interface LanguageProviderProps {
  children: ReactNode;
}

export function LanguageProvider({ children }: LanguageProviderProps) {
  const language = useLanguageStore((state) => state.language);

  useEffect(() => {
    document.documentElement.lang = language;
  }, [language]);

  return children;
}
