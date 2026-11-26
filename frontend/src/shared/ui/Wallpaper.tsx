import { useId } from 'react';

// The sheets: large tilted, rounded panes laid over each other, in the
// wallpaper tokens from the deepest to the lightest, under one blur.
const panes = [
  { x: 180, y: -220, fill: 'var(--wallpaper-3)', opacity: 0.9 },
  { x: 520, y: 40, fill: 'var(--wallpaper-1)', opacity: 0.75 },
  { x: 860, y: 300, fill: 'var(--wallpaper-2)', opacity: 0.65 },
  { x: -200, y: 420, fill: 'var(--wallpaper-1)', opacity: 0.55 },
];

// Wallpaper fills the window behind everything with blurred sheets, so glass
// has edges to bend wherever it sits. Render it once, at the root.
export function Wallpaper() {
  const filterId = `wallpaper-${useId().replace(/:/g, '')}`;
  return (
    <svg className="wallpaper" viewBox="0 0 1440 900" preserveAspectRatio="xMidYMid slice" aria-hidden="true" focusable="false">
      <filter id={filterId} x="-20%" y="-20%" width="140%" height="140%">
        <feDropShadow dx="0" dy="14" stdDeviation="22" style={{ floodColor: 'var(--wallpaper-shadow)' }} />
        <feGaussianBlur stdDeviation="3" />
      </filter>
      <rect width="1440" height="900" style={{ fill: 'var(--wallpaper-0)' }} />
      <g filter={`url(#${filterId})`} transform="rotate(-16 720 450)">
        {panes.map((pane) => (
          <rect key={pane.x} x={pane.x} y={pane.y} width="760" height="620" rx="90" style={{ fill: pane.fill, opacity: pane.opacity }} />
        ))}
      </g>
      <path d="M-80 760 C 400 620, 900 860, 1520 520" style={{ fill: 'none', stroke: 'var(--wallpaper-highlight)', strokeWidth: 2, opacity: 0.5 }} />
    </svg>
  );
}
