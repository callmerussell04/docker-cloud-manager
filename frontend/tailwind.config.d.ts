import type { Config } from 'tailwindcss';

// Lets src/app/tokens.test.ts read the theme with its types.
declare const config: Config;
export default config;
