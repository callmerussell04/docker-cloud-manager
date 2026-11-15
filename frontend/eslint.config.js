import js from '@eslint/js'
import globals from 'globals'
import reactHooks from 'eslint-plugin-react-hooks'
import reactRefresh from 'eslint-plugin-react-refresh'
import tseslint from 'typescript-eslint'
import { defineConfig, globalIgnores } from 'eslint/config'

// Layers: app -> features -> shared. Features may import one another only in
// the directions listed here; a new cross-feature import is added deliberately.
// Nothing imports admin.
const features = ['auth', 'dashboard', 'containers', 'images', 'volumes', 'projects', 'admin']
const allowedFeatureImports = {
  admin: ['auth', 'containers', 'images', 'volumes', 'projects'],
  projects: ['containers', 'images'],
  containers: ['images', 'volumes'],
}

// no-restricted-imports also applies to type-only imports.
function restrictImports(patterns) {
  return { 'no-restricted-imports': ['error', { patterns }] }
}

// Matches an import of the named top-level folders, through the @/ alias or a
// relative path that climbs out of the importing folder.
function folderImport(folders, base) {
  const names = folders.join('|')
  return `^(@/${base}|(\\.\\./)+)(${names})(/|$)`
}

const layerRules = [
  {
    files: ['src/shared/**/*.{ts,tsx}'],
    rules: restrictImports([
      { regex: folderImport(['features', 'app'], ''), message: 'Shared code imports nothing from features or the app layer.' },
    ]),
  },
  ...features.map((feature) => {
    const forbidden = features.filter((other) => other !== feature && !(allowedFeatureImports[feature] ?? []).includes(other))
    return {
      files: [`src/features/${feature}/**/*.{ts,tsx}`],
      rules: restrictImports([
        { regex: '^@/app(/|$)', message: 'Features import nothing from the app layer.' },
        { regex: folderImport(forbidden, 'features/'), message: `The ${feature} feature may not import this feature; see allowedFeatureImports in eslint.config.js.` },
      ]),
    }
  }),
]

export default defineConfig([
  globalIgnores(['dist']),
  {
    files: ['**/*.{ts,tsx}'],
    extends: [
      js.configs.recommended,
      tseslint.configs.recommended,
      reactHooks.configs.flat.recommended,
      reactRefresh.configs.vite,
    ],
    languageOptions: {
      ecmaVersion: 2020,
      globals: globals.browser,
    },
  },
  ...layerRules,
])
