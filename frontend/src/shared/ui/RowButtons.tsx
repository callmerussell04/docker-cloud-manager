import { Plus, Trash2 } from 'lucide-react';

// RemoveRowButton is the quiet round button that removes one row of a list
// being edited; its label is its accessible name and tooltip.
export function RemoveRowButton({ label, onClick }: { label: string; onClick: () => void }) {
  return (
    <button
      type="button"
      onClick={onClick}
      aria-label={label}
      title={label}
      className="inline-flex h-10 w-10 shrink-0 items-center justify-center rounded-full text-label-2 transition-colors hover:bg-fill hover:text-red-text"
    >
      <Trash2 aria-hidden className="h-4 w-4" />
    </button>
  );
}

// AddRowButton is the link that adds a row below a list being edited.
export function AddRowButton({ label, onClick }: { label: string; onClick: () => void }) {
  return (
    <button
      type="button"
      onClick={onClick}
      className="inline-flex items-center gap-1.5 self-start justify-self-start rounded-full py-1 font-medium text-accent hover:underline"
    >
      <Plus aria-hidden className="h-4 w-4" />
      {label}
    </button>
  );
}
