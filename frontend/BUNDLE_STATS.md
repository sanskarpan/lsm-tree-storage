# Frontend bundle stats

`bundle-stats.html` and `bundle-stats.json` are produced by
[`vite-bundle-visualizer`](https://github.com/btdi/vite-bundle-visualizer).
Open the HTML file in a browser for an interactive treemap.

## Regenerate

```bash
bun run bundle:stats
```

This runs `vite build` then the visualizer, writing:
- `bundle-stats.html` — interactive treemap (open in a browser).
- `bundle-stats.json` — machine-readable, lists every chunk with
  rendered / gzip / brotli sizes.

## Current shape

`app.js` is the initial bundle (327 KB / 103 KB gzip). Each panel is a
lazy-loaded chunk created by `React.lazy()` in `App.tsx`. The biggest
shared chunk is `data-table.js` (76 KB / 21 KB gzip) which holds
TanStack Table + Virtual and is shared by 5 panels.

| chunk                   | rendered | gzip   |
| ----------------------- | -------- | ------ |
| `app.js`                | 327.6 KB | 103 KB |
| `data-table.js`         | 75.9 KB  | 21 KB  |
| `ScenarioLab.js`        | 6.8 KB   | 2.1 KB |
| `WriteWorkbench.js`     | 4.6 KB   | 1.7 KB |
| `LevelMatrix.js`        | 3.9 KB   | 1.5 KB |
| `ReadInspector.js`      | 3.7 KB   | 1.2 KB |
| `AmplificationDeck.js`  | 3.3 KB   | 1.2 KB |
| `CompactionStudio.js`   | 3.1 KB   | 1.0 KB |
| `BloomTelemetry.js`     | 3.0 KB   | 1.0 KB |
| small primitives        | 0.2-2 KB | <0.7 KB |

`app.js` payload breakdown (top 5 by rendered size):
- `@floating-ui/*` (Radix internals): ~54 KB rendered
- `@tanstack/react-virtual`: 25 KB
- `react` + `react-dom`: ~140 KB (vendored)
- `zustand`: 3 KB
- `lucide-react`: ~12 KB

## Budgets

The CI workflow doesn't enforce a budget yet. Reasonable targets:
- `app.js` initial: <= 350 KB rendered.
- `data-table.js` shared: <= 90 KB rendered.
- Any single panel: <= 8 KB rendered.

Add a `bun run bundle:stats:check` script that fails CI on regression.
