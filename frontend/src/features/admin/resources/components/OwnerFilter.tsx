import { ChevronDown, UserRound } from 'lucide-react';
import { useT } from '@/shared/i18n';
import { cn } from '@/shared/lib/utils';
import { Glass } from '@/shared/ui/Glass';
import { Menu, MenuGroup, MenuItem } from '@/shared/ui/Menu';
import type { OwnerOption } from '../model';

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
  const chosen = owner ? (options.find((option) => option.id === owner)?.name ?? owner) : t('admin.resources.everyone');
  const label = t('admin.resources.ownerFilter');
  const trigger = round ? (
    <UserRound aria-hidden className={cn('h-[17px] w-[17px]', owner ? 'text-accent' : 'text-label')} strokeWidth={owner ? 2.3 : 1.9} />
  ) : (
    <>
      <UserRound aria-hidden className="h-[15px] w-[15px] shrink-0 text-label-2" strokeWidth={1.9} />
      <span className="sr-only">{label}: </span>
      <span className="max-w-[140px] truncate">{chosen}</span>
      <ChevronDown aria-hidden className="h-[14px] w-[14px] shrink-0 text-label-2" strokeWidth={2.2} />
    </>
  );
  return (
    <Glass className="shrink-0 rounded-full">
      <Menu
        label={round ? `${label}: ${chosen}` : label}
        iconOnly={round}
        sheetTitle={label}
        side="below-end"
        trigger={trigger}
        triggerClassName={cn(
          'flex h-10 items-center rounded-full font-medium text-label transition-colors hover:bg-lift aria-expanded:bg-lift',
          round ? 'w-10 justify-center' : 'gap-1.5 pl-3 pr-2.5',
        )}
      >
        <MenuGroup label={label}>
          <MenuItem checked={owner === ''} onSelect={() => onChange('')}>{t('admin.resources.everyone')}</MenuItem>
          {options.map((option) => (
            <MenuItem key={option.id} checked={owner === option.id} onSelect={() => onChange(option.id)}>{option.name}</MenuItem>
          ))}
        </MenuGroup>
      </Menu>
    </Glass>
  );
}
