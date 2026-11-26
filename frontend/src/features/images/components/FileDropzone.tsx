import { forwardRef, useImperativeHandle, useRef } from 'react';
import { FileCheck2, UploadCloud, X } from 'lucide-react';
import { cn } from '@/shared/lib/utils';

interface FileDropzoneProps {
  file: File | null;
  accept: string;
  title: string;
  hint: string;
  removeLabel: string;
  onFileChange: (file: File) => void;
  onRemove: () => void;
  extraHint?: string;
  // error says what is wrong with the choice; the zone is outlined in red
  // and describes itself by it.
  error?: string;
  id?: string;
}

// FileDropzone chooses one file: a dashed zone that opens the file picker,
// then the chosen file's name and size with a way to remove it. Its handle
// focuses the zone, so a step can lead the user to it.
export const FileDropzone = forwardRef<{ focus: () => void }, FileDropzoneProps>(function FileDropzone(
  { file, accept, title, hint, removeLabel, onFileChange, onRemove, extraHint, error, id },
  ref,
) {
  const fileInputRef = useRef<HTMLInputElement>(null);
  const zoneRef = useRef<HTMLButtonElement>(null);
  const removeRef = useRef<HTMLButtonElement>(null);
  const errorId = id ? `${id}-error` : undefined;
  useImperativeHandle(ref, () => ({ focus: () => (zoneRef.current ?? removeRef.current)?.focus() }), []);

  return (
    <div className="grid gap-1.5">
      <div
        className={cn(
          'flex flex-col items-center justify-center rounded-2xl border border-dashed p-5 text-center transition-colors',
          error ? 'border-red' : 'border-separator',
          !file && 'hover:bg-fill',
        )}
      >
        <input
          type="file"
          ref={fileInputRef}
          tabIndex={-1}
          onChange={(event) => {
            const selected = event.target.files?.[0];
            if (selected) onFileChange(selected);
            // Choosing the same file again after removing it fires a change.
            event.target.value = '';
          }}
          className="hidden"
          accept={accept}
        />

        {file ? (
          <div className="flex min-w-0 max-w-full flex-col items-center">
            <FileCheck2 aria-hidden className="mb-2 h-6 w-6 text-label-2" />
            <p className="max-w-full truncate font-medium" title={file.name}>{file.name}</p>
            <p className="mb-3 text-xs text-label-3">{(file.size / 1024 / 1024).toFixed(2)} MB</p>
            <button
              ref={removeRef}
              type="button"
              onClick={onRemove}
              className="inline-flex h-8 items-center gap-1.5 rounded-full px-3 text-[13px] font-medium text-red-text transition-colors hover:bg-fill"
            >
              <X aria-hidden className="h-4 w-4" />
              {removeLabel}
            </button>
          </div>
        ) : (
          <button
            ref={zoneRef}
            id={id}
            type="button"
            aria-invalid={error ? true : undefined}
            aria-describedby={errorId && error ? errorId : undefined}
            className="flex flex-col items-center rounded-xl px-2 py-1"
            onClick={() => fileInputRef.current?.click()}
          >
            <UploadCloud aria-hidden className="mb-2 h-6 w-6 text-label-2" />
            <span className="font-medium text-accent">{title}</span>
            <span className="mt-1 text-xs text-label-2">{hint}</span>
            {extraHint && <span className="text-xs text-label-2">{extraHint}</span>}
          </button>
        )}
      </div>
      {error && <p id={errorId} className="text-xs leading-snug text-red-text">{error}</p>}
    </div>
  );
});
