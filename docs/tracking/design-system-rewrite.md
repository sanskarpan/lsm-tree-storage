# Design-System Rewrite — Plan

> Status: **draft, not yet executed**. Each section is a candidate GitHub issue.
> Decisions captured in §1 (ADR). Stack is locked in; per-issue scope can be
> re-cut before filing.

## 0. Problem statement

The current dashboard (`frontend/src/`) was assembled panel-by-panel against
a single 620-line `styles.css`. It is functional but the foundation does not
scale and reads as 2021-2022 SaaS-vapor rather than a serious observability
tool. Specific defects:

- `App.tsx` lays out 7 panels with `:nth-child` `grid-column` selectors. Reorder
  one panel in JSX and the grid breaks. This is a real maintainability bug.
- `styles.css` is a single 620-line file. BEM classnames are scattered across
  panels. There is no clear ownership boundary.
- `rgba(103, 245, 198, 0.x)` is repeated ~30 times. The accent colour is not a
  token. Drift is guaranteed the next time someone hand-edits a value.
- Dark-only. `color-scheme: dark` is hardcoded on `:root`. No light mode.
- `border-radius: 24px` on every surface reads as buttons, not panels.
- `backdrop-filter: blur(14px)` on every panel is GPU-heavy on low-end devices.
- Hero copy *"Storage telemetry without the 2,000-line inline script"* is smug
  and dates itself.
- Tabular data (WAL entries, immutable memtables) is rendered as `<ul>` lists.
  Fine for N<20, falls over at N>100.
- No empty / loading / error skeletons — just `?? <p>empty</p>`.

## 1. Architecture decision record (ADR)

### ADR-001: Tailwind v4 + shadcn/ui as the design-system foundation

**Status:** accepted.

**Context.** The dashboard needs a real component library with accessible
primitives, design tokens, and a styling system that scales. Three candidates
were considered:

| Option | Pros | Cons |
|---|---|---|
| **Tailwind v4 + shadcn/ui** | Industry default. v4 has CSS-first `@theme` tokens. shadcn copies Radix-based primitives into the repo (you own the code). Best ecosystem. | Tailwind learning curve for contributors who have not used it. |
| CSS Modules + Radix UI | Hand-rolled, no utility framework. | Verbose, slow to iterate, no token system without extra infra. |
| vanilla-extract + Radix UI | Type-safe CSS-in-TS, zero runtime. | More setup, less ecosystem, smaller talent pool. |

**Decision.** Adopt Tailwind v4 + shadcn/ui.

**Consequences.**
- Tailwind v4 with the CSS-first `@theme` directive replaces `styles.css` as the
  single source of design tokens (colour, spacing, radius, type, motion).
- shadcn/ui is installed via `bunx shadcn@latest init` then `bunx shadcn@latest
  add <primitive>` per primitive. Each primitive is committed to
  `frontend/src/components/ui/` as readable TSX. The repo owns the code, not a
  package.
- Radix UI is the accessibility backbone under shadcn. Focus management, ARIA,
  keyboard nav, and portal behaviour come for free.

### ADR-002: Incremental migration behind a layout flag

**Status:** accepted.

**Context.** Two paths: big-bang rewrite of all 7 panels, or incremental
migration panel-by-panel. Big-bang is faster wall-clock but harder to bisect
regressions and cannot ship a partial state.

**Decision.** Incremental migration. New components live under
`frontend/src/features/<panel>/` and `frontend/src/components/ui/`. The legacy
`frontend/src/components/*.tsx` and `frontend/src/styles.css` stay until the
last panel is migrated, then deleted in a final cleanup PR. A `?ui=v2` query
param or localStorage flag (`lsm.ui.v2`) switches the new layout on for
dogfooding before the cutover.

**Consequences.**
- A "foundations" PR can land without touching any panel.
- Each panel migration is independently shippable and revertable.
- The legacy CSS file is dead weight for ~6 PRs. Documented in §6.

### ADR-003: Dark + Light + Dense/Comfortable density

**Status:** accepted.

**Context.** Real observability tools run for 8+ hours. Dark-only is fine for
short demos but light mode is required for accessibility and for
paper/printable runbooks. Density toggle (compact vs comfortable) is the
single highest-leverage ergonomic improvement for a tool that lives on a
second monitor.

**Decision.** Ship dark + light themes (system preference default, manual
override persisted in `localStorage`) and a comfortable/dense density toggle.
All three are CSS-class toggles on `<html>`: `data-theme="dark|light"` and
`data-density="comfortable|compact"`. Tokens resolve via `var(--token)` so
adding a third theme is a one-file change.

**Consequences.**
- Every component must resolve colour, spacing, and radius from a token — no
  raw `rgba()` in component code.
- Initial design effort ~1 extra day for the second theme.

### ADR-004: Supporting library picks

| Concern | Pick | Reason |
|---|---|---|
| Icons | `lucide-react` | Tree-shakeable, MIT, matches shadcn defaults. |
| Data tables | `@tanstack/react-table` v8 | Headless, accessible, used by shadcn. |
| Virtualization | `@tanstack/react-virtual` v3 | Headless, pairs with TanStack Table. |
| Forms | `react-hook-form` + `zod` | De-facto standard. |
| Animation | CSS-only first, `framer-motion` for layout transitions | Avoid pulling framer unless needed. |
| State | `zustand` for cross-panel dashboard data; `useState`/`useReducer` for local | Two layers, kept simple. |
| Date/number | `date-fns` + `d3-format` | Smallest viable. |
| Testing | `vitest` + `@testing-library/react` + `@axe-core/react` | Matches Bun-native if Bun is added to the test path. |
| Component docs | Storybook 8 with `@storybook/react-vite` | Visual regression + living docs. |
| Lint | `eslint` + `eslint-plugin-react-hooks` + `@typescript-eslint` | Standard. |
| Format | `prettier` | Standard. |

## 2. Design token system

The full token set lives in `frontend/src/styles/tokens.css` (generated from
`tailwind.config.ts` via Tailwind v4's `@theme`). Initial scale:

### Colour

Semantic tokens (resolved per theme):

```
--color-bg            page background
--color-bg-elevated   panel background
--color-bg-sunken     code/log region
--color-fg            primary text
--color-fg-muted      secondary text
--color-fg-subtle     tertiary text
--color-border        default border
--color-border-strong focused border
--color-accent        primary accent (teal in dark, indigo in light)
--color-accent-fg     text on accent
--color-success       green
--color-warning       amber
--color-danger        red
--color-info          blue
```

Plus a neutral scale `--color-neutral-50` … `--color-neutral-950` (warm-grey,
not pure grey, to avoid the "everything looks blue" trap).

### Spacing

4 px base. `--space-0` … `--space-12` (0, 4, 8, 12, 16, 20, 24, 32, 40, 48, 64,
80, 96).

### Radius

4 steps: `--radius-sm: 4px`, `--radius-md: 6px`, `--radius-lg: 8px`,
`--radius-xl: 12px`. Down from the current 24px.

### Typography

`--font-sans`, `--font-mono`, `--font-size-xs` … `--font-size-3xl`,
`--line-height-tight|normal|relaxed`, `--font-weight-normal|medium|semibold`.

### Motion

`--duration-fast: 120ms`, `--duration-base: 200ms`, `--duration-slow: 320ms`.
`--ease-standard: cubic-bezier(0.2, 0, 0, 1)`.

### Elevation

Three shadow levels. Drop the `backdrop-filter: blur` entirely.

### Z-index

`--z-base: 0`, `--z-sticky: 10`, `--z-overlay: 100`, `--z-modal: 200`,
`--z-toast: 300`, `--z-tooltip: 400`.

## 3. Primitive component library

shadcn/ui primitives to install, in dependency order:

1. `button` — primary/secondary/ghost/destructive variants
2. `input` / `textarea` / `label` / `form` (with react-hook-form + zod)
3. `card` — replaces `.panel`
4. `badge` — replaces `.file-chip`, `.token`
5. `tabs` — for the in-panel sectioning
6. `dialog` / `sheet` — for the scenario launcher
7. `dropdown-menu` — for the theme/density switcher in the top bar
8. `tooltip`
9. `toast` — for the error banner and operation feedback
10. `skeleton` — replaces the `?? <p>empty</p>` patterns
11. `separator`
12. `scroll-area`
13. `select` / `combobox`
14. `switch` — for the density toggle
15. `progress` — replaces the meter fills
16. `code` / `kbd` — for the read-trace key inspection

All primitives land in `frontend/src/components/ui/` with one file per
primitive, owned by the repo, not shadcn's package.

## 4. Domain components

Panels to rebuild (one issue per group):

| # | Panel | Notes |
|---|---|---|
| 1 | **AppShell + TopBar** | Replaces `HeaderBar` + `App.tsx` layout. Owns the theme/density switcher, connection pill, session counters. |
| 2 | **WriteWorkbench** | Split into `<PutForm>`, `<MemtablePressureMeter>`, `<WALActivityFeed>`, `<SessionWriteFeed>`, `<MutableRecordsList>`. |
| 3 | **LevelMatrix** | New `<TopologyStrip>` visualisation per level. `<MemtableOwnershipCard>`. `<CompactionBalanceList>`. |
| 4 | **BloomTelemetry** | `<BloomHistogram>`, `<FalsePositiveRate>`, `<HashLab>`. |
| 5 | **ReadInspector** | `<TraceQueryForm>`, `<TraceTimeline>`, `<TombstoneTrace>`. |
| 6 | **CompactionStudio** | `<StyleSelector>`, `<ForceControls>`, `<ActiveCompactionCard>`, `<CompactionFeed>`. |
| 7 | **AmplificationDeck** | `<WAGauge>`, `<RAGauge>`, `<SAGauge>`, `<HistoryChart>`. |
| 8 | **ScenarioLab** | `<ScenarioCatalog>`, `<RunControls>`, `<OpsFeed>`, `<BenchmarkResultCard>`, `<CloseAttemptDialog>`. |

## 5. State management

Two layers:

- **Local**: `useState`, `useReducer` for per-component state (form inputs,
  toggles, accordion expansion).
- **Cross-panel**: a `useDashboardStore` (zustand) holding the live WebSocket
  bus, the engine config, and the derived metrics. The current
  `useDashboardData` hook becomes a thin selector over the store.

State migration is a single PR (issue #8) once the panels are migrated.

## 6. Legacy CSS teardown

- `frontend/src/styles.css` is deleted in the last migration PR.
- Legacy `frontend/src/components/*.tsx` are deleted as their replacements
  ship (per issue, not all at once).
- `App.tsx`'s `:nth-child` grid is replaced with explicit `gridColumn` props
  on `<AppShell.Panel>`.

## 7. Validation

For every panel migration:

- Storybook story with at least 4 states: empty, loading, populated, error.
- `@axe-core/react` reports 0 critical/serious violations on the panel.
- Lighthouse perf score ≥ 90 (mobile) for the dashboard route.
- Bundle size delta is reported in the PR body (using `bun run build`
  output and `vite-bundle-visualizer`).

For the foundation PR:

- `bun run typecheck` clean.
- `bun run build` clean.
- Storybook boots, all primitives render in both themes and both densities.

## 8. Rollout plan (issue ordering)

| Issue | Title | Est. | Depends on |
|---|---|---|---|
| #1 | Adopt Tailwind v4 + shadcn/ui (ADR + deps) | 0.5 d | — |
| #2 | Design token + theme system (dark/light/density) | 1 d | #1 |
| #3 | Primitive component library (shadcn init + ~15 primitives) | 2 d | #2 |
| #4 | DataTable primitive (TanStack Table + Virtual) | 1 d | #3 |
| #5 | AppShell + TopBar replacement | 1 d | #3 |
| #6 | Panel migration batch 1 (WriteWorkbench, LevelMatrix, BloomTelemetry) | 2 d | #4, #5 |
| #7 | Panel migration batch 2 (ReadInspector, CompactionStudio, AmplificationDeck) | 2 d | #4, #5 |
| #8 | Panel migration batch 3 (ScenarioLab) + zustand store | 1.5 d | #4, #5 |
| #9 | Storybook + visual regression + a11y/perf validation | 1 d | #6, #7, #8 |

Total: ~12 days, 9 issues, shippable per-PR.

## 9. Open questions

- Should we add Bun's test runner or keep vitest? Bun test would be more
  idiomatic per `frontend/CLAUDE.md` but vitest has better React Testing
  Library integration. **Default: vitest**, switch to `bun test` if a blocker
  appears.
- Do we want a separate `frontend/dist/` Storybook build, or a sub-route on
  the BFF? **Default: separate build**, mounted at `/storybook` for dogfood.
- A11y i18n: do we need RTL/locale-aware date/number formats, or is `en-US`
  acceptable for v1? **Default: `en-US`**, defer localisation.

## 10. References

- shadcn/ui — https://ui.shadcn.com/
- Tailwind v4 `@theme` — https://tailwindcss.com/docs/theme
- TanStack Table — https://tanstack.com/table
- TanStack Virtual — https://tanstack.com/virtual
- Radix UI primitives — https://www.radix-ui.com/primitives
- lucide icons — https://lucide.dev/
- WCAG 2.2 AA — https://www.w3.org/TR/WCAG22/
