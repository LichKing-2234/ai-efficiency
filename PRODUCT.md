# Product

## Register

product

## Users

AI Efficiency Platform is used by engineering, platform, and operations users who need to inspect AI-assisted development usage, repository attribution, PR evidence, provider setup, OAuth/device access, and administrative account state. They usually arrive inside an authenticated work session and need dense, scan-friendly task surfaces rather than marketing content.

## Product Purpose

The product measures and improves AI-assisted development efficiency. The frontend must make backend truth easy to inspect, keep authentication and proxy boundaries clear, and expose operational workflows for usage records, repositories, PR attribution, user setup, settings, and admin users.

## Brand Personality

Precise, calm, work-focused.

## Anti-references

Avoid decorative SaaS landing-page patterns, oversized hero treatments, one-off route styling, fake or hardcoded product state, direct browser calls to real backend origins, and UI that invents new controls where standard shadcn/Radix primitives already fit the task.

## Design Principles

- Backend data is the source of truth; the UI should not fabricate product state.
- Same-origin browser requests and cookie-backed auth are part of the product contract.
- Dense information should stay readable through consistent tables, cards, filters, labels, empty states, and status surfaces.
- Route-specific styling should move into shared primitives when a pattern appears more than once or defines a product-wide convention.
- MiSans, shadcn/Radix primitives, semantic tokens, and i18n resources are the default UI vocabulary.

## Accessibility & Inclusion

Target accessible product UI defaults: keyboard-reachable controls, semantic form labels, visible focus states, adequate contrast, reduced-motion-safe transitions, and English/Chinese copy served through i18n resources rather than hardcoded route text.
