import { CheckCircle2, UploadCloud, X } from 'lucide-react';
import { useRef } from 'react';
import { Button } from '@/components/ui/Button';

interface FileDropzoneProps {
  file: File | null;
  accept: string;
  title: string;
  hint: string;
  removeLabel: string;
  onFileChange: (file: File) => void;
  onRemove: () => void;
  extraHint?: string;
}

export function FileDropzone({ file, accept, title, hint, removeLabel, onFileChange, onRemove, extraHint }: FileDropzoneProps) {
  const fileInputRef = useRef<HTMLInputElement>(null);

  return (
    <div className="flex flex-col items-center justify-center rounded-2xl border-2 border-dashed border-slate-300 bg-slate-50/50 p-6 transition-colors hover:bg-slate-100/50 dark:border-slate-700 dark:bg-slate-900/50 dark:hover:bg-slate-800/50">
      <input
        type="file"
        ref={fileInputRef}
        onChange={(event) => {
          const selected = event.target.files?.[0];
          if (selected) onFileChange(selected);
        }}
        className="hidden"
        accept={accept}
      />

      {file ? (
        <div className="flex flex-col items-center text-center">
          <div className="mb-3 flex h-12 w-12 items-center justify-center rounded-full bg-indigo-100 text-indigo-600 dark:bg-indigo-900/50 dark:text-indigo-400">
            <CheckCircle2 className="h-6 w-6" />
          </div>
          <p className="font-medium">{file.name}</p>
          <p className="mb-4 text-xs text-slate-500">{(file.size / 1024 / 1024).toFixed(2)} MB</p>
          <Button type="button" variant="ghost" onClick={onRemove} className="text-red-500 hover:bg-red-50 dark:hover:bg-red-950">
            <X className="mr-2 h-4 w-4" />
            {removeLabel}
          </Button>
        </div>
      ) : (
        <button type="button" className="flex flex-col items-center text-center" onClick={() => fileInputRef.current?.click()}>
          <div className="mb-3 flex h-12 w-12 items-center justify-center rounded-full bg-slate-100 text-slate-500 dark:bg-slate-800 dark:text-slate-400">
            <UploadCloud className="h-6 w-6" />
          </div>
          <span className="font-medium">{title}</span>
          <span className="mt-1 text-xs text-slate-500">{hint}</span>
          {extraHint && <span className="text-xs text-slate-500">{extraHint}</span>}
        </button>
      )}
    </div>
  );
}
