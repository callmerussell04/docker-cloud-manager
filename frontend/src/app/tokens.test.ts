/// <reference types="node" />
import { readFileSync } from 'node:fs';
import { describe, expect, it } from 'vitest';
import tailwindConfig from '../../tailwind.config.js';

const css = readFileSync(new URL('./index.css', import.meta.url), 'utf8');

// The token table of the redesign spec (#171): every colour the interface
// uses, for light and dark. The values here are copied from the spec, not
// from the stylesheet, so a drift in either direction fails.
const spec: Record<string, [light: string, dark: string]> = {
  label: ['#1d1d1f', '#f5f5f7'],
  'label-2': ['#5f5f66', '#b0b0b8'],
  'label-3': ['#8e8e96', '#7c7c85'],
  separator: ['rgba(0,0,0,.09)', 'rgba(255,255,255,.10)'],
  fill: ['rgba(90,90,100,.13)', 'rgba(160,160,172,.20)'],
  'fill-hover': ['rgba(90,90,100,.22)', 'rgba(160,160,172,.32)'],
  accent: ['#2f5ea8', '#9fb6e0'],
  'accent-fill': ['#3a6cc0', '#4a6db0'],
  green: ['#28a745', '#30d158'],
  orange: ['#f08c00', '#ff9f0a'],
  red: ['#ff3b30', '#ff453a'],
  'red-text': ['#c9241b', '#ff7a70'],
  glass: ['rgba(255,255,255,.40)', 'rgba(90,90,98,.34)'],
  'glass-dense': ['rgba(250,250,252,.78)', 'rgba(40,40,44,.76)'],
  frost: ['rgba(255,255,255,.56)', 'rgba(26,26,29,.40)'],
  'glass-sidebar': ['rgba(255,255,255,.36)', 'rgba(40,40,44,.30)'],
  console: ['#17191d', '#0d0e10'],
  'wallpaper-0': ['#eef3f9', '#08090b'],
  'wallpaper-3': ['#d8e3f1', '#12161d'],
  'wallpaper-1': ['#a9bfdc', '#1f2733'],
  'wallpaper-2': ['#5f82b8', '#33425a'],
  'wallpaper-highlight': ['#ffffff', '#8ea2c4'],
  'series-1': ['#2a78d6', '#3987e5'],
  'series-2': ['#eb6834', '#d95926'],
  'series-3': ['#1baf7a', '#199e70'],
  'series-4': ['#eda100', '#c98500'],
  'series-5': ['#e87ba4', '#d55181'],
};

// declarations returns the custom properties declared in the first rule whose
// selector is exactly the given one.
function declarations(selector: string): Record<string, string> {
  const escaped = selector.replace(/[.*+?^${}()|[\]\\]/g, '\\$&');
  const match = new RegExp(`(?:^|[}\\s])${escaped}\\s*\\{([^}]*)\\}`, 'm').exec(css);
  if (!match) throw new Error(`no ${selector} rule in index.css`);
  const result: Record<string, string> = {};
  for (const [, name, value] of match[1].matchAll(/--([\w-]+)\s*:\s*([^;]+);/g)) result[name] = value.trim();
  return result;
}

// normalize compares colours by value: case, spaces and leading or trailing
// zeros in the alpha do not matter.
function normalize(value: string) {
  return value
    .toLowerCase()
    .replace(/\s+/g, '')
    .replace(/(^|[,(])0+\./g, '$1.')
    .replace(/(\.\d*?)0+\)/g, '$1)')
    .replace(/\.\)/g, ')');
}

describe('design tokens', () => {
  const light = declarations(':root');
  const dark = declarations('.dark');

  it.each(Object.entries(spec))('defines %s for both themes as the spec does', (name, [lightValue, darkValue]) => {
    expect(normalize(light[name] ?? 'missing')).toBe(normalize(lightValue));
    expect(normalize(dark[name] ?? 'missing')).toBe(normalize(darkValue));
  });

  it('exposes every token as a Tailwind colour, and only colours both themes define', () => {
    const exposed = new Set<string>();
    const walk = (value: unknown) => {
      if (typeof value === 'string') {
        for (const [, name] of value.matchAll(/var\(--([\w-]+)\)/g)) exposed.add(name);
      } else if (value && typeof value === 'object') {
        Object.values(value).forEach(walk);
      }
    };
    walk(tailwindConfig.theme?.extend?.colors);
    for (const name of Object.keys(spec)) expect(exposed, name).toContain(name);
    for (const name of exposed) {
      expect(light[name], `${name} in light`).toBeDefined();
      expect(dark[name], `${name} in dark`).toBeDefined();
    }
  });

  it('keeps the frost blur and saturation per theme', () => {
    expect(light['frost-blur']).toBe('28px');
    expect(light['frost-saturate']).toBe('125%');
    expect(dark['frost-blur']).toBe('28px');
    expect(dark['frost-saturate']).toBe('100%');
  });
});
