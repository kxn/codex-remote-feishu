---
name: build-page-mock
description: "Use when creating or revising a page mock, browser-runnable prototype, or interactive demo for this repository's web, setup, onboarding, status, or admin pages, and also when implementing a real product page from an approved mock. Ensures the artifact is final-user-facing, hides design-intent text, uses fake data only as interactive coverage, and keeps the final product aligned with the approved mock except for real business data."
---

# build-page-mock

Use this skill when the task is to create, revise, review, or implement from a page mock / prototype / interactive demo that users should experience in a browser.

Typical triggers:

- user asks for `页面 Mock` / `页面原型` / `高保真原型` / `可交互 demo`
- user asks for a browser-runnable preview page before the real backend is ready
- user asks to `按 mock 落产品` / `从 mock 生成页面` / `按原型实现最终页面`
- touched files include `docs/draft/*mock*.html`, `web/src/**` preview routes, or `internal/app/daemon/adminui/**` mock pages

## Read first

Read these docs before editing:

- `docs/general/page-mock-guidelines.md`
- `docs/general/web-design-guidelines.md`

## Rules

### 1. Treat the mock as a final-user artifact

- The rendered page must show only final-user-facing content.
- Do not render `mock`, `demo`, `prototype`, `wireframe`, `设计说明`, `TODO`, or other internal wording in the page.
- This includes browser title, page header, helper text, placeholders, empty states, notices, and debug panels.

### 2. Make it runnable, not illustrative

- The mock must run in a browser.
- Static screenshots or dead HTML do not count.
- Standalone HTML is acceptable only if it is genuinely interactive and browser-runnable.
- If the repository already has an app shell or route model that fits the task, prefer integrating there instead of building a disconnected static artifact.

### 3. Fake data is allowed only as real interaction coverage

- Use fake data freely when the backend is absent.
- Cover every user-editable, user-selectable, filterable, searchable, or navigable data surface that the page exposes.
- If the user can reach empty / populated / validation / success / failure states, make those states reachable in the mock.

### 4. Every visible interaction must be real

- Buttons, tabs, dialogs, forms, expanders, navigation, search, filters, sort, pagination, and multi-step flows must all change real page state.
- Do not leave dead controls.
- If a backend action does not exist yet, simulate it with local state, fake services, or deterministic in-browser behavior instead of a no-op.

### 5. Responsive behavior is part of the deliverable

- Verify desktop and mobile.
- Verify width changes and portrait/landscape rotation.
- If layout or navigation should differ by breakpoint or orientation, implement those behaviors in the mock.
- Do not rely on a single static viewport.

### 6. The approved mock is the user-visible contract for the real product

- When implementing the real product from an approved mock, keep user-visible structure, copy, states, interaction paths, and responsive behavior aligned with that mock.
- The main allowed difference is replacing fake business data and local fake services with real data and real integrations.
- Do not add extra user-visible copy during implementation just because the backend, validation, or edge cases are more complicated than the mock.
- If the user-visible contract must change, update the mock or the canonical guideline first, then update the product.

## Delivery checklist

Before finishing, verify:

1. No user-visible design-intent wording remains.
2. The artifact runs in a browser.
3. All visible controls and flows work.
4. Fake data covers the page's full interactive surface.
5. Desktop, mobile, and orientation changes remain usable.
6. If implementing from an approved mock, the real product still matches that mock in all user-visible aspects except business data.

If one of these fails, the mock is not done.
