import { useNavigate } from 'react-router-dom';
import { Box, Disc, HardDrive, Layers, Plus } from 'lucide-react';
import { useT } from '@/shared/i18n';
import { createDialogFlag } from '@/shared/lib/hooks';
import { useSizeClass } from '@/shared/lib/useSizeClass';
import { cn } from '@/shared/lib/utils';
import { Menu, MenuItem } from '@/shared/ui/Menu';

// NewMenu is the dashboard's primary action: New, opening the four create
// actions, each on its own list page with its create dialog open. On a phone
// it is a round plus button, as every primary action is.
export function NewMenu() {
  const t = useT();
  const navigate = useNavigate();
  const round = useSizeClass() === 'phone';
  const open = (path: string) => navigate(`${path}?${createDialogFlag}`);
  return (
    <Menu
      label={t('dashboard.new')}
      iconOnly={round}
      trigger={
        round ? (
          <Plus aria-hidden className="h-[18px] w-[18px]" strokeWidth={2.4} />
        ) : (
          <>
            <Plus aria-hidden className="h-4 w-4" strokeWidth={2.4} />
            {t('dashboard.new')}
          </>
        )
      }
      triggerClassName={cn(
        'inline-flex h-10 shrink-0 items-center whitespace-nowrap rounded-full bg-accent-fill font-semibold text-on-accent',
        'shadow-[var(--primary-shadow)] hover:brightness-110',
        round ? 'w-10 justify-center' : 'gap-1.5 pl-3 pr-4',
      )}
    >
      <MenuItem icon={Box} onSelect={() => open('/containers')}>{t('search.newContainer')}</MenuItem>
      <MenuItem icon={Layers} onSelect={() => open('/projects')}>{t('search.deployProject')}</MenuItem>
      <MenuItem icon={Disc} onSelect={() => open('/images')}>{t('dashboard.new.buildImage')}</MenuItem>
      <MenuItem icon={HardDrive} onSelect={() => open('/volumes')}>{t('dashboard.new.createVolume')}</MenuItem>
    </Menu>
  );
}
