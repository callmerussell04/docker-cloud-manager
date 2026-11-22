import { useMemo } from 'react';
import { useNavigate } from 'react-router-dom';
import { Box, Layers, Plus, SunMoon } from 'lucide-react';
import { useContainers } from '@/features/containers/queries';
import { useAuthStore } from '@/shared/api/sessionStore';
import { useT } from '@/shared/i18n';
import { createDialogFlag } from '@/shared/lib/hooks';
import { useThemeStore } from '@/shared/lib/themeStore';
import { SearchPalette, type PaletteItem } from '@/shared/ui/SearchPalette';
import type { PaletteGroup } from '@/shared/ui/searchPalette';
import { adminNavigation, navigation } from './navigation';

// The palette lists the user's containers from one page of the list, the
// largest the API serves.
const containerPageSize = 100;
// It shows no more than this many matching containers.
const containerMatches = 6;

interface ShellSearchProps {
  open: boolean;
  onClose: () => void;
}

// ShellSearch is the search palette of the signed-in shell: actions (New
// container, Deploy a Compose project, Switch appearance), the pages the user
// can open, and, once something is typed, the user's containers. Choosing a
// container opens its page. The containers are asked for only while the
// palette is open.
export function ShellSearch({ open, onClose }: ShellSearchProps) {
  const t = useT();
  const navigate = useNavigate();
  const isAdmin = useAuthStore((state) => state.role) === 'admin';
  const toggleTheme = useThemeStore((state) => state.toggleTheme);
  const { data } = useContainers('own', 1, containerPageSize, { enabled: open });
  const containers = data?.items;

  const groups = useMemo<PaletteGroup<PaletteItem>[]>(() => {
    const pages = isAdmin ? [...navigation, ...adminNavigation] : navigation;
    return [
      {
        id: 'actions',
        label: t('search.actions'),
        items: [
          { id: 'new-container', icon: Plus, label: t('search.newContainer'), onSelect: () => navigate(`/containers?${createDialogFlag}`) },
          { id: 'deploy-project', icon: Layers, label: t('search.deployProject'), onSelect: () => navigate(`/projects?${createDialogFlag}`) },
          { id: 'switch-appearance', icon: SunMoon, label: t('search.switchAppearance'), onSelect: toggleTheme },
        ],
      },
      {
        id: 'pages',
        label: t('search.goTo'),
        items: pages.map((entry) => ({ id: entry.href, icon: entry.icon, label: t(entry.nameKey), onSelect: () => navigate(entry.href) })),
      },
      {
        id: 'containers',
        label: t('search.containers'),
        whenSearching: true,
        limit: containerMatches,
        items: (containers ?? []).map((container) => ({
          id: container.id,
          icon: Box,
          label: container.name,
          keywords: container.image_tag,
          meta: container.image_tag,
          onSelect: () => navigate(`/containers/${container.id}`),
        })),
      },
    ];
  }, [t, navigate, isAdmin, toggleTheme, containers]);

  return (
    <SearchPalette
      open={open}
      onClose={onClose}
      groups={groups}
      label={t('search.label')}
      placeholder={t('search.placeholder')}
      emptyText={t('search.nothingFound')}
    />
  );
}
