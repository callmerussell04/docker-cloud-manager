import { UserRound } from 'lucide-react';
import { useT } from '@/shared/i18n';
import type { OwnerOption } from '../model';
import { FilterMenu } from './FilterMenu';

interface OwnerFilterProps {
  // owner is the chosen owner's user ID; empty for everyone.
  owner: string;
  options: readonly OwnerOption[];
  onChange: (owner: string) => void;
  // round shows the icon alone, as a round button for a narrow toolbar's
  // top row; the chosen owner is then its accessible name and tooltip, and
  // a chosen one (not everyone) marks the icon in the accent.
  round?: boolean;
}

// OwnerFilter is the toolbar's glass capsule that shows one user's resources
// or everyone's: a menu of choices, the chosen one marked.
export function OwnerFilter({ owner, options, onChange, round = false }: OwnerFilterProps) {
  const t = useT();
  const everyone = t('admin.resources.everyone');
  return (
    <FilterMenu
      label={t('admin.resources.ownerFilter')}
      icon={UserRound}
      value={owner}
      chosen={owner ? (options.find((option) => option.id === owner)?.name ?? owner) : everyone}
      options={[{ id: '', label: everyone }, ...options.map((option) => ({ id: option.id, label: option.name }))]}
      onChange={onChange}
      narrowed={owner !== ''}
      round={round}
    />
  );
}
