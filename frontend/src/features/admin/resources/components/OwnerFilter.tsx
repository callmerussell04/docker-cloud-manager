import { ChevronDown, UserRound } from 'lucide-react';
import { useT } from '@/shared/i18n';
import { Glass } from '@/shared/ui/Glass';
import { Menu, MenuGroup, MenuItem } from '@/shared/ui/Menu';
import type { OwnerOption } from '../model';

interface OwnerFilterProps {
  // owner is the chosen owner's user ID; empty for everyone.
  owner: string;
  options: readonly OwnerOption[];
  onChange: (owner: string) => void;
}

// OwnerFilter is the toolbar's glass capsule that shows one user's resources
// or everyone's: a menu of choices, the chosen one marked.
export function OwnerFilter({ owner, options, onChange }: OwnerFilterProps) {
  const t = useT();
  const chosen = owner ? (options.find((option) => option.id === owner)?.name ?? owner) : t('admin.resources.everyone');
  return (
    <Glass className="shrink-0 rounded-full">
      <Menu
        label={t('admin.resources.ownerFilter')}
        side="below-end"
        trigger={(
          <>
            <UserRound aria-hidden className="h-[15px] w-[15px] shrink-0 text-label-2" strokeWidth={1.9} />
            <span className="sr-only">{t('admin.resources.ownerFilter')}: </span>
            <span className="max-w-[140px] truncate">{chosen}</span>
            <ChevronDown aria-hidden className="h-[14px] w-[14px] shrink-0 text-label-2" strokeWidth={2.2} />
          </>
        )}
        triggerClassName="flex h-10 items-center gap-1.5 rounded-full pl-3 pr-2.5 font-medium text-label transition-colors hover:bg-lift aria-expanded:bg-lift"
      >
        <MenuGroup label={t('admin.resources.ownerFilter')}>
          <MenuItem checked={owner === ''} onSelect={() => onChange('')}>{t('admin.resources.everyone')}</MenuItem>
          {options.map((option) => (
            <MenuItem key={option.id} checked={owner === option.id} onSelect={() => onChange(option.id)}>{option.name}</MenuItem>
          ))}
        </MenuGroup>
      </Menu>
    </Glass>
  );
}
