import animate from 'tailwindcss-animate';

// Design tokens from src/app/index.css, exposed as theme colours so classes
// name them (text-label-2, bg-accent-fill, border-separator) instead of
// carrying literals.
/** @type {import('tailwindcss').Config} */
export default {
  content: [
    "./index.html",
    "./src/**/*.{js,ts,jsx,tsx}",
  ],
  darkMode: 'class',
  theme: {
    extend: {
      colors: {
        label: { DEFAULT: 'var(--label)', 2: 'var(--label-2)', 3: 'var(--label-3)' },
        separator: 'var(--separator)',
        fill: { DEFAULT: 'var(--fill)', hover: 'var(--fill-hover)' },
        accent: { DEFAULT: 'var(--accent)', fill: 'var(--accent-fill)', soft: 'var(--accent-soft)' },
        'on-accent': 'var(--on-accent)',
        green: 'var(--green)',
        orange: 'var(--orange)',
        red: { DEFAULT: 'var(--red)', text: 'var(--red-text)', soft: 'var(--red-soft)' },
        glass: { DEFAULT: 'var(--glass)', dense: 'var(--glass-dense)', sidebar: 'var(--glass-sidebar)' },
        frost: 'var(--frost)',
        scrim: 'var(--scrim)',
        lift: 'var(--lift)',
        tile: 'var(--tile)',
        console: {
          DEFAULT: 'var(--console)',
          label: { DEFAULT: 'var(--console-label)', 2: 'var(--console-label-2)', 3: 'var(--console-label-3)' },
          fill: { DEFAULT: 'var(--console-fill)', hover: 'var(--console-fill-hover)' },
        },
        wallpaper: {
          0: 'var(--wallpaper-0)',
          1: 'var(--wallpaper-1)',
          2: 'var(--wallpaper-2)',
          3: 'var(--wallpaper-3)',
          highlight: 'var(--wallpaper-highlight)',
        },
        series: {
          1: 'var(--series-1)',
          2: 'var(--series-2)',
          3: 'var(--series-3)',
          4: 'var(--series-4)',
          5: 'var(--series-5)',
        },
      },
      fontFamily: {
        sans: ['var(--font-sans)'],
        mono: ['var(--font-mono)'],
      },
    },
  },
  plugins: [animate],
}
