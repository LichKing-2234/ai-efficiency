# Collection Navigation Contract

This contract describes the current indexed, cursor, and branch-incremental
collection semantics and their responsive presentation. Read it before changing
pagination, list filters, cursor controls, tree loading, collection empty/error
states, or desktop/mobile collection layouts.

Loading, cancellation, and stale-response rules also follow the
[platform loading](./platform-loading.md) contract. Business authorization and
snapshot rules remain owned by each domain contract.

## Classification

A collection surface is one user-facing unit containing context, filters,
content, loading/empty/error state, and its collection navigation. Content
presentation (table, compact rows, or tree) is separate from navigation.

The available data contract selects exactly one navigation mode:

1. **Indexed pagination** applies when the result has an exact total and page or
   offset addressing can reach a chosen page.
2. **Cursor pagination** applies when navigation uses an opaque continuation or
   a cursor bound to a snapshot/integrity boundary. It promises sequential
   directions, not random page numbers.
3. **Branch incremental loading** applies when the next batch belongs to one
   parent in a hierarchy. It changes only that branch.

Client-side slicing still counts as indexed pagination when the user has exact
total and addressable pages. A browser-held cursor stack remains cursor
pagination and cannot manufacture random-access page semantics.

## Collection Surface

One collection uses one outer surface. Its heading/context, filters, content,
states, and footer remain in that surface rather than nesting decorative cards.
Desktop uses a table where horizontal comparison matters; narrow layouts render
compact divided rows from the same loaded data. Individual cards are reserved
for records with a real independent status or action boundary.

The footer follows its collection content. Stable control dimensions keep
loading text, disabled states, and range labels from shifting surrounding
layout. Empty collections and non-empty single-page collections omit navigation.
Multi-page requests retain the last successful content and footer dimensions.

Desktop and mobile presentations share one query, result, filters, and
navigation state. Responsive rendering does not issue a second request or mount
duplicate hidden record trees, and the page does not require horizontal scroll
to preserve a wide table.

## Indexed Pagination

### Full-Page Collections

Full-page indexed collections expose the true visible range and exact total,
use direct Element Plus pagination, default to 20 rows, and offer only
`20 / 50 / 100` page sizes. Desktop shows range, total, page size, and numeric
pages. Mobile contracts to fixed Previous, current-page, and Next controls
without horizontal overflow.

The URL owns `page`, `page_size`, search, and filters. Reload, shared links, and
browser navigation restore the same state. Search/filter changes restart at
page 1. Invalid or unsupported URL values normalize once to allowed values
without a request/rewrite loop. Default values may be omitted when restoration
remains deterministic.

### Embedded Collections

Dropdowns, dialogs, settings sections, and per-object collections use compact
indexed pagination with their contract's fixed page size. They retain numeric
page capability without a page-size selector. Their state belongs to the local
component or business object and never pollutes the page URL.

Closing the surface, changing its owner object, or changing search/filter state
restarts at page 1. Narrow containers use the same Previous/current/Next
contraction as full pages. User-visible pages are 1-based; API/store boundaries
normalize offset or 0-based representations before rendering.

Indexed collections use `ElPagination` directly. Shared constants and tested
recipes own common layouts; a forwarding pagination wrapper adds no contract.

## Cursor Pagination

Cursor footers keep Previous and Next in fixed positions and disable an
unavailable direction instead of hiding it. When the server provides a true
rank range and total, the center displays them. Without a total, the UI does not
infer page count or fabricate numeric navigation.

`CursorPager` owns this sequential interaction. The browser may retain a stack
of prior cursors only to support Previous. Query, sort, authorization scope,
subject, or snapshot changes discard inapplicable cursors and restart the
collection from its first batch.

A `snapshot_expired` response restarts only the affected cursor collection. It
does not clear or refetch successful sibling sections.

## Branch Incremental Loading

Each parent independently owns its loaded departments/members, continuation,
loading state, error, and retry. Load More controls appear at the end of the
specific branch and append only that parent's next direct batch.

During a request the control stays in place and becomes loading/disabled while
already loaded nodes remain visible. Exhausting a cursor removes that branch's
control. Failure preserves the branch contents and shows an in-place retry.
Snapshot expiry reloads only that branch from its first batch.

A page-level paginator cannot control multiple branches. Branches load through
explicit action rather than nested automatic infinite scroll.

## Loading and Failure States

- Empty results show one empty state without navigation.
- One-page non-empty results omit navigation.
- Page/cursor changes keep the last successful content, disable navigation, and
  show local loading.
- Failure retains the last successful page and navigation state with an in-place
  error/retry; it does not silently jump to page 1.
- A stale response cannot overwrite a newer page, filter, sort, subject, or
  branch intent.
- Feature-specific partial, stale, disabled, and mutation states retain their
  independent request lifecycles.

## Current Surface Map

| Surface | Mode | State and capacity |
| --- | --- | --- |
| Admin Users, repository inventory, Directory Offboarding | Full-page indexed | URL; 20/50/100 |
| Admin Users root departments | Embedded indexed | Local; fixed 25 |
| Admin Department Picker, Directory run history | Embedded indexed | Local; fixed 20 |
| Relay target-user and Account searches | Embedded indexed | Per target; fixed 20 |
| Relay managed mappings | Embedded indexed | Page-local; fixed 10 |
| Activity repositories, PRs, and team members | Cursor | Query/team-local cursor stack |
| Team Usage member ranking | Snapshot cursor | True rank range and total |
| Team Usage and Activity organization trees | Branch incremental | Per parent |
| Admin Users child departments | Branch incremental | Per parent; fixed 25 |

Quota Reset currently fetches all pages for its queues and performs local
filtering/selection without visible pagination. That behavior remains a
separate loading gap; collection styling does not reclassify it or change its
requests.
