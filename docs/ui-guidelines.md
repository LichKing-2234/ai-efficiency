# Frontend UI Guidelines

This document is the current implementation contract for generic frontend UI.
Business behavior remains defined by the relevant feature specs and current code.

## Library Ownership

| Concern | Owner |
| --- | --- |
| Generic interactive components | Element Plus 2.x |
| Generic action icons | `@element-plus/icons-vue` |
| Responsive layout and page composition | Tailwind CSS |
| Charts and chart data presentation | Chart.js through the existing chart components |
| Application state and API access | Pinia stores and the existing API modules |

Element Plus uses its default theme and default control size on tablet and
desktop. On viewports below the shared `md` breakpoint, the mobile stylesheet
sets public Element Plus component roots to a 44px minimum so controls remain
reliable touch targets across the public and authenticated shells. Do not
target private internal DOM classes such as `__wrapper`. Use `small` only for
genuinely dense, non-primary controls such as compact table actions. Do not add
a broader custom theme until a product requirement justifies one.

## Component Rules

- Components and their CSS are imported automatically on demand through the
  Vite resolvers. Never register the complete Element Plus library or import its
  complete stylesheet.
- Supply Element Plus locale configuration through the shared
  `ElementPlusLocaleProvider` at the public and authenticated route-shell
  boundaries. Keeping the provider route-local preserves the initial-shell
  budget while giving every rendered route the same reactive locale behavior.
- Use `El*` components directly. Do not create mirror wrappers such as
  `AppButton`, `AppInput`, or `AppDialog` that only rename or forward Element
  Plus APIs.
- Use the library component that matches the interaction: buttons for commands,
  selects or menus for option sets, segmented or radio controls for modes,
  switches and checkboxes for binary settings, and dialogs for focused decisions.
- Append page-level dialogs and drawers to `body` so their scrims always cover
  the complete viewport. Do not disable teleport on full-viewport overlays;
  local poppers inside a dialog may remain non-teleported when needed.
- Vertically and horizontally center every dialog with Element Plus
  `align-center`. Let the overlay scroll when dialog content is taller than the
  viewport; do not add page-specific top margins or vertical offsets. Drawers
  remain edge-aligned.
- Use official Element Plus icons for generic actions. Do not add handwritten
  SVG markup for actions already covered by the icon package.
- Keep feature-specific components when they own real behavior or domain
  presentation. This rule removes generic UI duplication, not domain boundaries.

## Responsive Behavior

- Critical user and administrator workflows must remain fully operable on
  mobile. Tailwind owns grids, flex layout, breakpoints, width constraints, and
  responsive ordering around Element Plus controls.
- When a table and stacked cards are mutually exclusive render trees, use the
  shared media-query composables aligned with the verified viewport matrix.
  Use the `xl` boundary by default when the desktop sidebar leaves too little
  content width at 768 pixels. The nine-column Admin Users table remains on
  cards through 1280 pixels and switches at 1440 pixels so its complete Actions
  column stays inside the work surface.
- Use scan-friendly tables on wide screens. When a table cannot remain usable on
  mobile, provide a card or stacked presentation rather than horizontal page
  overflow.
- Keep same-row selectable cards and same-purpose summary cards equal in width
  and height. Let the grid stretch its children and let each card fill its grid
  cell; do not hide content differences behind fixed pixel heights. Badges such
  as `Recommended` must not change the outer dimensions of the option card.
- Verify every route at viewport widths of 390, 768, 1024, 1280, and 1440 pixels. Controls,
  labels, status text, dialogs, and pagination must not clip or overlap.
- Keep page headings proportional to operational surfaces. Reserve large display
  type for true hero experiences, not dashboards or settings panels.

## Feedback and Destructive Actions

- Use Element Plus alerts for persistent section state and messages or
  notifications for transient feedback.
- Use Element Plus dialogs and message boxes for confirmations. Destructive and
  bulk actions must retain the feature contract's explicit confirmation and
  in-flight protection.
- Loading, empty, partial, stale, error, success, and disabled states remain
  owned by the feature. A component migration must not merge their lifecycles or
  change API request timing.

## Build Contract

The pre-migration baseline at commit `2e1c2884` is:

| Aggregate | Baseline gzip | Node 20 migration measurement | Enforced maximum |
| --- | ---: | ---: | ---: |
| Initial shell | 67,521 bytes | 72,603 bytes | 73,000 bytes |
| Default English `/usage` | 96,562 bytes | 157,902 bytes | 163,000 bytes |
| Complex `/admin/users` route | 100,309 bytes | 245,974 bytes | 253,909 bytes |

`frontend/scripts/measure-build.mjs` enforces these exact ceilings. The initial
shell ceiling includes the authenticated admin navigation and its route table;
the current 73,000-byte ceiling accounts for the Relay Planning admin entry
while keeping route views lazy-loaded. The `/usage` ceiling accounts for the
Relay Planning bilingual messages that are part of the existing language
dictionary closure. A hosted-runtime measurement that exceeds a ceiling
remains an application bundle regression to remove, not a reason to expand the
contract.
Measured production builds must also prove that Element Plus remains on demand,
locale dictionaries remain route-safe, and Chart.js stays outside the initial
dashboard closure until chart data is ready.

## Verification

- Add or update tests at the rendered Vue page boundary, mocking only external
  API boundaries.
- Run focused tests for each migrated slice and the frontend build regularly.
- Before release, run the complete frontend unit suite, measured production
  build, role E2E suite, and all-route viewport verification.
