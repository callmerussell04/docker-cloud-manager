import { useEffect, useState } from 'react';
import { Check, Copy } from 'lucide-react';
import { useT } from '@/shared/i18n';

// CopyButton copies a value to the clipboard and shows a check for a moment
// once it has.
export function CopyButton({ value }: { value: string }) {
  const t = useT();
  const [copied, setCopied] = useState(false);

  useEffect(() => {
    if (!copied) return;
    const timer = window.setTimeout(() => setCopied(false), 1400);
    return () => window.clearTimeout(timer);
  }, [copied]);

  const label = copied ? t('common.copied') : t('common.copyValue', { value });
  return (
    <button
      type="button"
      title={copied ? t('common.copied') : t('common.copy')}
      aria-label={label}
      onClick={() => {
        // Without clipboard access (an insecure page, a denied permission)
        // nothing is copied, and the button shows no check.
        navigator.clipboard?.writeText(value).then(() => setCopied(true), () => setCopied(false));
      }}
      className="-mt-0.5 inline-flex h-[22px] w-6 shrink-0 items-center justify-center rounded-[7px] text-label-2 transition-colors hover:bg-fill hover:text-label"
    >
      {copied ? <Check aria-hidden className="h-[13px] w-[13px]" strokeWidth={2.4} /> : <Copy aria-hidden className="h-[13px] w-[13px]" strokeWidth={2} />}
    </button>
  );
}
