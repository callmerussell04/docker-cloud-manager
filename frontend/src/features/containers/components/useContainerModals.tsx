import { useCallback, useState } from 'react';
import type { Scope } from '@/shared/api/scope';
import type { ContainerData } from '../model';
import { ContainerLogsModal } from '../streams/ContainerLogsModal';
import { ContainerTerminalModal } from '../streams/ContainerTerminalModal';
import { ExposeContainerModal } from './ExposeContainerModal';

// useContainerModals owns which container the logs, terminal and routing
// modals show, and renders them; place `modals` once in the page. Routing is
// changed only on the user's own containers.
export function useContainerModals(scope: Scope) {
  const [logsContainer, setLogsContainer] = useState<ContainerData | null>(null);
  const [terminalContainer, setTerminalContainer] = useState<ContainerData | null>(null);
  const [exposeContainer, setExposeContainer] = useState<ContainerData | null>(null);
  // Stable callbacks, so an open stream does not reconnect when the page re-renders.
  const closeLogs = useCallback(() => setLogsContainer(null), []);
  const closeTerminal = useCallback(() => setTerminalContainer(null), []);
  const closeExpose = useCallback(() => setExposeContainer(null), []);

  const modals = (
    <>
      <ContainerLogsModal container={logsContainer} scope={scope} onClose={closeLogs} />
      <ContainerTerminalModal container={terminalContainer} scope={scope} onClose={closeTerminal} />
      {scope === 'own' && (
        <ExposeContainerModal container={exposeContainer} onClose={closeExpose} />
      )}
    </>
  );

  return {
    openLogs: setLogsContainer,
    openTerminal: setTerminalContainer,
    openExpose: setExposeContainer,
    modals,
  };
}
