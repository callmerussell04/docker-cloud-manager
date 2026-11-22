import { StateMessage, type StateAction } from './StateMessage';

interface EmptyStateProps {
  title: string;
  description?: string;
  action?: StateAction;
}

// EmptyState is a list with nothing in it yet, and the one way to add
// something.
export function EmptyState({ title, description, action }: EmptyStateProps) {
  return <StateMessage title={title} description={description} action={action} />;
}
