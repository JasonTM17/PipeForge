# Console design guidelines

The console is intentionally compact and data-forward. It follows the existing
PipeForge visual language: dark graphite surfaces, electric cyan for active
signals, green for verified success, and orange/red for retry or failure.

## Rules

- Render only API responses; never manufacture counts, worker states, or
  artifact rows for an operational view.
- Keep loading, empty, error, unauthorized, and stale states visible.
- Abort superseded requests and prevent stale responses from replacing the
  current job or session state.
- Use keyboard-visible focus rings and controls with text labels.
- Keep bearer tokens in browser session storage only for this learning surface;
  never put MinIO credentials or presigned URLs in the bundle.
- Use dense tables on desktop and single-column cards below 760px.
- Prefer short state labels and UTC/locale-rendered timestamps over decorative
  charts with no authoritative source.

The source of truth for the current layout is the API-backed implementation in
`frontend/src/`; the publish-grade architecture and lifecycle visuals live in
`docs/assets/images/`.
