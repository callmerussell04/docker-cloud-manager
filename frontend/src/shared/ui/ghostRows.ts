import type { CSSProperties } from 'react';

// gridTracks splits a CSS grid template into its tracks, keeping each
// function's arguments with it: 'minmax(0,1fr) 40px' is two tracks.
export function gridTracks(template: string): string[] {
  const tracks: string[] = [];
  let depth = 0;
  let current = '';
  for (const char of template.trim()) {
    if (char === '(') depth++;
    if (char === ')') depth--;
    if (/\s/.test(char) && depth === 0) {
      if (current) tracks.push(current);
      current = '';
    } else {
      current += char;
    }
  }
  if (current) tracks.push(current);
  return tracks;
}

// ghostBarWidth is how much of its cell a placeholder bar fills, in percent:
// varied by row and column so the rows read as rows of a list, the same on
// every render.
export function ghostBarWidth(row: number, cell: number): number {
  const first = cell === 0;
  const span = first ? 40 : 45;
  const base = first ? 45 : 35;
  return base + ((row * 37 + cell * 23) % span);
}

// ghostRowsMask fades placeholder rows toward the bottom. Behind a message
// it also clears an oval where the message sits (centred, its middle about
// 110 px down), so no row line or bar runs behind the message's text.
export function ghostRowsMask(behindMessage: boolean): CSSProperties {
  const fade = 'linear-gradient(to bottom, #000 0%, transparent 92%)';
  if (!behindMessage) return { maskImage: fade, WebkitMaskImage: fade };
  const clear = 'radial-gradient(ellipse 320px 125px at 50% 110px, transparent 55%, #000 100%)';
  return {
    maskImage: `${fade}, ${clear}`,
    maskComposite: 'intersect',
    WebkitMaskImage: `${fade}, ${clear}`,
    WebkitMaskComposite: 'source-in',
  };
}
