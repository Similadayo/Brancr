# Admin UI Frontend Notes

This document describes the lightweight Admin UI enhancements implemented and the near-term plan for componentization.

## What was added

- `adminapp/dist/assets/ui.js` — lightweight, framework-free AdminUI API:
  - `AdminUI.viewEscalation(id)` — prompts for reauth (POST `/admin/reauth`) if needed and fetches the full escalation content, displays it in a viewer modal.
  - `AdminUI.toast(message, kind)` — shows transient toasts.
  - `AdminUI.attachTooltips()` — attach tooltips to elements with `data-tooltip`.

- Styles added to `adminapp/dist/assets/app.css` for buttons, toast, tooltip, and viewer.
- Escalations page: content preview elements now include `data-tooltip` attributes explaining the preview is masked.

## Next steps

1. Add unit tests (Vitest + jsdom) for `ui.js` and tooltips/toasts.
2. Add Playwright E2E test for `View` → Reauth → Viewer flow. Run Playwright in CI against the `postgres-integration` job or a separate backend instance.
3. Migrate primitives into an `adminapp/src/ui` component library (React+Vite) when ready; continue progressive migration.

## How to test locally

- Serve the app (e.g., run the backend and open `/admin/escalations`) and click `View` on a row to see the reauth modal and viewer.
- The tooltips will automatically attach when `ui.js` loads; call `AdminUI.attachTooltips()` if injecting content dynamically.

