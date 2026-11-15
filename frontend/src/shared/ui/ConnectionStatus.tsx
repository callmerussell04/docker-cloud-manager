interface ConnectionStatusProps {
  connected: boolean;
  connectedLabel: string;
  disconnectedLabel: string;
}

export function ConnectionStatus({ connected, connectedLabel, disconnectedLabel }: ConnectionStatusProps) {
  return (
    <div className="flex items-center gap-2">
      <div className={`h-2 w-2 rounded-full ${connected ? 'bg-green-500' : 'bg-red-500'}`} />
      <span className="text-xs text-slate-500">{connected ? connectedLabel : disconnectedLabel}</span>
    </div>
  );
}
