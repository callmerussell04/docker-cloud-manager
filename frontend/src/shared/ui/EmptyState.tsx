import { GhostRows, type GhostShape } from './GhostRows';
import { ListMessage } from './ListMessage';
import type { StateAction } from './StateMessage';

interface EmptyStateProps extends GhostShape {
  title: string;
  description?: string;
  action?: StateAction;
}

// EmptyState is a list with nothing in it yet: the list's shape as faint
// rows fading out, and over them what will appear there, one line, and the
// one way to add the first item as the primary button. Without a shape it is
// the message alone, centred.
export function EmptyState({ title, description, action, ...shape }: EmptyStateProps) {
  const message = <ListMessage title={title} description={description} action={action} primary />;
  if (!shape.template && !shape.columns) return <div className="flex justify-center px-4 pt-10">{message}</div>;
  return <GhostRows tone="empty" {...shape}>{message}</GhostRows>;
}
