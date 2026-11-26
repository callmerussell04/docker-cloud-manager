import { useCallback, useState } from 'react';
import type { Scope } from '@/shared/api/scope';
import type { ContainerData } from '../model';
import { ExposeContainerDialog } from './ExposeContainerDialog';

// useExposeDialog owns which container the routing settings dialog shows,
// and renders it; place `exposeDialog` once in the page. Routing is changed
// only on the user's own containers.
export function useExposeDialog(scope: Scope) {
  const [exposeContainer, setExposeContainer] = useState<ContainerData | null>(null);
  const closeExpose = useCallback(() => setExposeContainer(null), []);

  const exposeDialog = scope === 'own' ? <ExposeContainerDialog container={exposeContainer} onClose={closeExpose} /> : null;

  return { openExpose: setExposeContainer, exposeDialog };
}
