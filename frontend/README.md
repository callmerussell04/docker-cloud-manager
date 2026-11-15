# Docker Cloud Manager frontend

The web interface of Docker Cloud Manager (DCM), a self-hosted PaaS for one server. Users run containers, build images, manage volumes and deploy Compose projects; administrators manage users, quotas, runtime settings, monitoring, reports and every user's resources.

Built with React, TypeScript and Vite; TanStack Query for server state, Zustand for client state, react-hook-form with Zod for forms, Tailwind CSS for styling, xterm.js for the browser terminal and Recharts for charts.

## Commands

Run from this directory:

| Command | Purpose |
| --- | --- |
| `npm run dev` | Development server |
| `npm run build` | Type-check and production build |
| `npm run lint` | ESLint, including the import-boundary rules |
| `npm test` | Vitest |

The build reads two variables at build time, both required: `VITE_API_URL` (the Gateway API base URL, for example `http://localhost/api/v1`) and `VITE_BASE_DOMAIN` (the domain user services are published under). Set them in `.env` for local development; the Docker image takes them as build arguments.

## Structure

```
src/
  app/        composition root: entry point, providers, router, route guards, page layout
  features/   one folder per domain: auth, dashboard, containers, images, volumes, projects, admin
  shared/     code with no domain knowledge: api, ui, i18n, lib
```

Imports go app → features → shared. Features import one another only in the directions listed in `eslint.config.js` (admin → auth and the resource features; projects → containers and images; containers → images and volumes), and nothing imports admin. `npm run lint` fails on any other import.

A feature keeps its API functions in `api.ts`, its queries, mutations and query keys in `queries.ts`, its form schemas in `schemas.ts`, its wire types and domain rules in `model.ts`, and its `components/` and `pages/` beside them. Resource features take a scope, `own` or `all`, which selects the user or the admin endpoints. Admin is split into `users`, `settings`, `monitoring`, `reports` and `resources`, each laid out the same way.

Tests sit beside the code they test as `*.test.ts`.
