import { useCallback, useState } from 'react';
import type { Scope } from '@/shared/api/scope';
import type { ContainerData } from '../model';
import { ExposeContainerModal } from './ExposeContainerModal';

// useContainerModals owns which container the routing settings dialog shows,
// and renders it; place `modals` once in the page. Routing is changed only on
// the user's own containers. Logs and the terminal live on the container
// page, not in dialogs.
export function useContainerModals(scope: Scope) {
  const [exposeContainer, setExposeContainer] = useState<ContainerData | null>(null);
  const closeExpose = useCallback(() => setExposeContainer(null), []);

  const modals = scope === 'own' ? <ExposeContainerModal container={exposeContainer} onClose={closeExpose} /> : null;

  return { openExpose: setExposeContainer, modals };
}
