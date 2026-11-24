import { useCallback, type HTMLAttributes, type Ref } from 'react';
import { cn } from '@/shared/lib/utils';
import { useLens, type LensOptions } from './lens';

type GlassMaterial = 'clear' | 'dense' | 'sidebar';

interface GlassProps extends HTMLAttributes<HTMLDivElement> {
  // material picks the fill: clear for controls, dense for menus, dialogs,
  // the search palette, the save bar and the sign-in card, sidebar for the
  // sidebar.
  material?: GlassMaterial;
  ref?: Ref<HTMLDivElement>;
}

// Each material's class, and the blur and saturation its lens keeps in
// Chromium, where the lens replaces the class's backdrop filter: the same
// values as the class, so a dense surface stays dense when it refracts. Clear
// controls keep a light blur so the bent backdrop stays visible.
const materials: Record<GlassMaterial, { className?: string; lens: LensOptions }> = {
  clear: { lens: { blur: 3, saturate: 170 } },
  dense: { className: 'glass-dense', lens: { blur: 24, saturate: 180 } },
  sidebar: { className: 'glass-sidebar', lens: { blur: 10, saturate: 170 } },
};

// Glass is the refracting surface for controls that float over content: the
// sidebar, toolbar pills, menus, the search palette, dialogs, notifications
// and the sign-in card (a dialog with no page behind it). Content never sits on
// its own Glass, and Glass inside Glass would refract
// nothing. In Chromium the backdrop bends at the rim; elsewhere it is blurred.
export function Glass({ material = 'clear', className, ref, ...props }: GlassProps) {
  const lensRef = useLens<HTMLDivElement>(materials[material].lens);
  // The lens and a caller's ref both see the element.
  const setRef = useCallback((element: HTMLDivElement | null) => {
    lensRef.current = element;
    if (typeof ref === 'function') ref(element);
    else if (ref) ref.current = element;
  }, [lensRef, ref]);
  return <div ref={setRef} className={cn('glass', materials[material].className, className)} {...props} />;
}
