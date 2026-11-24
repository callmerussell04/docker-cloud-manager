import { useT } from '@/shared/i18n';
import { StateMessage } from './StateMessage';

interface NoMatchesStateProps {
  query: string;
  onClear: () => void;
  // description replaces the line about rows on this page of a list, for a
  // search that is not over a list's page.
  description?: string;
}

// NoMatchesState is shown when a list has rows but the search hides all of
// them, so it is not mistaken for an empty list. It offers Clear the search.
export function NoMatchesState({ query, onClear, description }: NoMatchesStateProps) {
  const t = useT();
  return (
    <StateMessage
      role="status"
      title={query.trim() ? t('common.noMatchesFor', { query: query.trim() }) : t('common.noMatches')}
      description={description ?? t('common.noMatchesDescription')}
      action={{ label: t('common.clearSearch'), onClick: onClear }}
    />
  );
}
