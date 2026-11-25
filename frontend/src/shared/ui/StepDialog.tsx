import { useEffect, useId, useRef, type ReactNode } from 'react';
import { Check } from 'lucide-react';
import { useT } from '@/shared/i18n';
import { cn } from '@/shared/lib/utils';
import { Button } from './Button';
import { Dialog, DialogFooter, DialogHeader } from './Dialog';

interface StepDialogProps {
  isOpen: boolean;
  onClose: () => void;
  title: string;
  // steps are the step labels, in order.
  steps: readonly string[];
  // current is the index of the step shown.
  current: number;
  // reached is the furthest step Continue has opened; every step up to it
  // stays open to a click after going back.
  reached: number;
  // onStep opens a step reached before; steps not reached yet cannot be opened.
  onStep: (index: number) => void;
  // onSubmit runs on the primary button or Enter in a field: Continue checks
  // the current step and moves on, and on the last step it finishes.
  onSubmit: () => void;
  // finishLabel names the last step's action, such as Create.
  finishLabel: string;
  isSubmitting?: boolean;
  children: ReactNode;
}

const focusableSelector = 'input:not([disabled]), select:not([disabled]), textarea:not([disabled]), button:not([disabled]):not([tabindex="-1"]), [tabindex="0"]';

// StepDialog is a create flow as a centred glass dialog in steps: the step
// list under the title (done steps stay clickable), the current step's
// fields in a body of steady minimum height, then Back, Cancel and Continue
// (the finish label on the last step). It is one form, so Enter in a field
// continues. Focus moves to the first control of each step it opens, or to
// the primary button when the step has none.
export function StepDialog({ isOpen, onClose, title, steps, current, reached, onStep, onSubmit, finishLabel, isSubmitting, children }: StepDialogProps) {
  const t = useT();
  const titleId = useId();
  const bodyRef = useRef<HTMLDivElement>(null);
  const submitRef = useRef<HTMLButtonElement>(null);
  const shownStep = useRef(current);
  const last = current === steps.length - 1;

  useEffect(() => {
    if (shownStep.current === current) return;
    shownStep.current = current;
    bodyRef.current?.scrollTo({ top: 0 });
    // A step with nothing to fill in, such as a review, focuses its action.
    (bodyRef.current?.querySelector<HTMLElement>(focusableSelector) ?? submitRef.current)?.focus();
  }, [current]);

  return (
    <Dialog isOpen={isOpen} onClose={onClose} label={title} className="max-w-[600px] rounded-3xl">
      <DialogHeader title={title} titleId={titleId} onClose={onClose} />
      <Steps labels={steps} current={current} reached={Math.max(reached, current)} onStep={onStep} />
      <form
        noValidate
        aria-labelledby={titleId}
        className="flex min-h-0 flex-1 flex-col"
        onSubmit={(event) => {
          event.preventDefault();
          onSubmit();
        }}
      >
        <div ref={bodyRef} className="min-h-[340px] flex-1 overflow-y-auto px-6 pb-6 pt-4">
          {children}
        </div>
        <DialogFooter>
          {current > 0 && (
            <Button type="button" variant="ghost" className="rounded-full" onClick={() => onStep(current - 1)}>{t('common.back')}</Button>
          )}
          <div className="flex-1" />
          <Button type="button" variant="secondary" className="rounded-full" onClick={onClose}>{t('common.cancel')}</Button>
          <Button ref={submitRef} type="submit" className="rounded-full" isLoading={isSubmitting}>{last ? finishLabel : t('common.continue')}</Button>
        </DialogFooter>
      </form>
    </Dialog>
  );
}

interface StepsProps {
  labels: readonly string[];
  current: number;
  reached: number;
  onStep: (index: number) => void;
}

// Steps is the list of a flow's steps: those reached before marked with a
// check and clickable, the current one marked as the current step, the rest
// not yet reachable. On narrow windows only the current step keeps its label.
function Steps({ labels, current, reached, onStep }: StepsProps) {
  const t = useT();
  return (
    <ol aria-label={t('common.steps')} className="flex shrink-0 items-center gap-1 overflow-x-auto px-5 pb-3">
      {labels.map((label, index) => {
        const isCurrent = index === current;
        const done = !isCurrent && index <= reached;
        return (
          <li key={label} className="flex min-w-0 items-center gap-1">
            {index > 0 && <span aria-hidden className="h-px w-3 shrink-0 bg-separator sm:w-5" />}
            <button
              type="button"
              disabled={index > reached}
              aria-current={isCurrent ? 'step' : undefined}
              aria-label={done ? t('common.stepDone', { step: label }) : undefined}
              onClick={() => onStep(index)}
              className={cn(
                'inline-flex h-8 items-center gap-2 whitespace-nowrap rounded-full px-2 transition-colors disabled:cursor-default',
                isCurrent ? 'font-semibold text-label' : done ? 'text-label-2 hover:bg-fill hover:text-label' : 'text-label-3',
              )}
            >
              <span
                aria-hidden
                className={cn(
                  'inline-flex h-5 w-5 shrink-0 items-center justify-center rounded-full text-[11px] font-semibold tabular-nums',
                  isCurrent ? 'bg-accent-fill text-on-accent' : done ? 'bg-fill-hover text-label' : 'bg-fill text-label-3',
                )}
              >
                {done ? <Check className="h-3 w-3" strokeWidth={3} /> : index + 1}
              </span>
              <span className={cn(!isCurrent && 'hidden sm:inline')}>{label}</span>
            </button>
          </li>
        );
      })}
    </ol>
  );
}
