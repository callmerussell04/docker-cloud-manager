import { SearchX } from 'lucide-react';
import { EmptyState } from '@/shared/ui/EmptyState';
import { useT } from '@/shared/i18n';

// NoMatchesState is shown when a list page has rows but the search hides all
// of them, so it is not mistaken for an empty list.
export function NoMatchesState() {
  const t = useT();
  return (
    <EmptyState
      icon={<SearchX className="h-8 w-8" />}
      title={t('common.noMatches')}
      description={t('common.noMatchesDescription')}
    />
  );
}
