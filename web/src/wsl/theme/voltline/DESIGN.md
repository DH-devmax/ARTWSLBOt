---
version: alpha
name: Voltline Analytics
description: A light, finance-grade dashboard language built around a cool platinum canvas, bright paper cards, deep ink typography, and a single electric chartreuse accent.
theme: light
colors:
  primary: "#D6FF3D"
  primary-deep: "#A8CC22"
  primary-soft: "#EAFFA8"
  on-primary: "#0B1015"
  secondary: "#0B1015"
  secondary-soft: "#1A2330"
  tertiary: "#6B7785"
  neutral-50: "#FFFFFF"
  neutral-100: "#F2F5F8"
  neutral-150: "#E8EDF2"
  neutral-200: "#E6EAEE"
  neutral-300: "#D8DEE5"
  surface: "#FFFFFF"
  surface-inset: "#F2F5F8"
  surface-canvas: "#E6EAEE"
  on-surface: "#0B1015"
  on-surface-muted: "#6B7785"
  border: "#D6DCE3"
  border-soft: "#E4E8ED"
  focus: "#A8CC22"
  positive: "#2BB673"
  error: "#E25555"
typography:
  font-display: "Instrument Sans, Inter, system-ui, sans-serif"
  font-body: "Inter, system-ui, sans-serif"
  font-mono: "JetBrains Mono, SF Mono, Menlo, monospace"
  display-xl:
    family: "{typography.font-display}"
    size: "64px"
    weight: 700
    lineHeight: 1.08
    tracking: "-0.02em"
  display-lg:
    family: "{typography.font-display}"
    size: "56px"
    weight: 700
    lineHeight: 1.08
    tracking: "-0.02em"
  headline-lg:
    family: "{typography.font-display}"
    size: "40px"
    weight: 700
    lineHeight: 1.1
    tracking: "-0.015em"
  headline-md:
    family: "{typography.font-display}"
    size: "30px"
    weight: 600
    lineHeight: 1.2
    tracking: "-0.015em"
  headline-sm:
    family: "{typography.font-display}"
    size: "22px"
    weight: 600
    lineHeight: 1.25
  title-md:
    family: "{typography.font-display}"
    size: "18px"
    weight: 600
    lineHeight: 1.3
  body-lg:
    family: "{typography.font-body}"
    size: "17px"
    weight: 400
    lineHeight: 1.55
  body-md:
    family: "{typography.font-body}"
    size: "15px"
    weight: 400
    lineHeight: 1.55
  body-sm:
    family: "{typography.font-body}"
    size: "13px"
    weight: 500
    lineHeight: 1.5
  label-sm:
    family: "{typography.font-body}"
    size: "12px"
    weight: 600
    lineHeight: 1.4
    tracking: "0.02em"
  mono-md:
    family: "{typography.font-mono}"
    size: "15px"
    weight: 500
    lineHeight: 1.4
  mono-lg:
    family: "{typography.font-mono}"
    size: "32px"
    weight: 500
    lineHeight: 1.1
rounded:
  none: "0px"
  xs: "6px"
  sm: "10px"
  md: "12px"
  lg: "18px"
  xl: "24px"
  2xl: "28px"
  full: "999px"
spacing:
  0: "0px"
  1: "4px"
  2: "8px"
  3: "12px"
  4: "16px"
  5: "20px"
  6: "24px"
  7: "32px"
  8: "40px"
  9: "56px"
  10: "72px"
  11: "96px"
  gutter: "24px"
  container: "1200px"
elevation:
  none: "none"
  sm: "0 1px 0 rgba(11,16,21,0.04), 0 1px 2px rgba(11,16,21,0.04)"
  md: "0 6px 16px rgba(11,16,21,0.05), 0 1px 2px rgba(11,16,21,0.04)"
  lg: "0 18px 38px rgba(11,16,21,0.06), 0 2px 6px rgba(11,16,21,0.04)"
  xl: "0 28px 60px rgba(11,16,21,0.08), 0 8px 16px rgba(11,16,21,0.05)"
  accent: "0 0 0 1px rgba(168,204,34,0.4), 0 8px 22px rgba(214,255,61,0.35)"
  focus-ring: "0 0 0 3px rgba(168,204,34,0.35)"
components:
  button-primary:
    backgroundColor: "{colors.primary}"
    textColor: "{colors.on-primary}"
    typography: "{typography.body-md}"
    rounded: "{rounded.full}"
    height: "44px"
    padding: "0 24px"
    elevation: "{elevation.accent}"
  button-primary-hover:
    backgroundColor: "{colors.primary-deep}"
    textColor: "{colors.on-primary}"
  button-secondary:
    backgroundColor: "{colors.surface}"
    textColor: "{colors.on-surface}"
    typography: "{typography.body-md}"
    rounded: "{rounded.full}"
    height: "44px"
    padding: "0 24px"
    borderColor: "{colors.border}"
  button-secondary-hover:
    backgroundColor: "{colors.surface-inset}"
    borderColor: "{colors.tertiary}"
  button-ink:
    backgroundColor: "{colors.secondary}"
    textColor: "{colors.neutral-50}"
    typography: "{typography.body-md}"
    rounded: "{rounded.full}"
    height: "44px"
    padding: "0 24px"
  input-field:
    backgroundColor: "{colors.surface-inset}"
    textColor: "{colors.on-surface}"
    typography: "{typography.body-md}"
    rounded: "{rounded.md}"
    height: "44px"
    padding: "12px 14px"
    borderColor: "{colors.border}"
  input-field-focus:
    backgroundColor: "{colors.surface}"
    borderColor: "{colors.primary-deep}"
    elevation: "{elevation.focus-ring}"
  card:
    backgroundColor: "{colors.surface}"
    textColor: "{colors.on-surface}"
    rounded: "{rounded.lg}"
    padding: "24px"
    borderColor: "{colors.border}"
    elevation: "{elevation.sm}"
  card-accent:
    backgroundColor: "{colors.primary}"
    textColor: "{colors.on-primary}"
    rounded: "{rounded.lg}"
    padding: "24px"
    elevation: "{elevation.accent}"
  card-ink:
    backgroundColor: "{colors.secondary}"
    textColor: "{colors.neutral-50}"
    rounded: "{rounded.lg}"
    padding: "24px"
  checkbox:
    backgroundColor: "{colors.surface}"
    borderColor: "{colors.border}"
    rounded: "{rounded.xs}"
    size: "20px"
  checkbox-checked:
    backgroundColor: "{colors.primary}"
    borderColor: "{colors.primary-deep}"
    textColor: "{colors.on-primary}"
  tabs-rail:
    backgroundColor: "{colors.surface-inset}"
    borderColor: "{colors.border}"
    rounded: "{rounded.full}"
    padding: "4px"
  tabs-active:
    backgroundColor: "{colors.surface}"
    textColor: "{colors.on-surface}"
    typography: "{typography.body-sm}"
    rounded: "{rounded.full}"
    elevation: "{elevation.sm}"
  tabs-inactive:
    backgroundColor: "transparent"
    textColor: "{colors.on-surface-muted}"
    typography: "{typography.body-sm}"
  tabs-underline-active:
    backgroundColor: "transparent"
    textColor: "{colors.on-surface}"
    borderColor: "{colors.primary-deep}"
  kpi-tile:
    backgroundColor: "{colors.surface}"
    textColor: "{colors.on-surface}"
    rounded: "{rounded.lg}"
    padding: "20px 24px 24px"
    borderColor: "{colors.border}"
  kpi-tile-ink:
    backgroundColor: "{colors.secondary}"
    textColor: "{colors.neutral-50}"
    rounded: "{rounded.lg}"
    padding: "20px 24px 24px"
  chip-lime:
    backgroundColor: "{colors.primary}"
    textColor: "{colors.on-primary}"
    rounded: "{rounded.full}"
    height: "28px"
    padding: "0 12px"
    typography: "{typography.label-sm}"
  chip-neutral:
    backgroundColor: "{colors.surface-inset}"
    textColor: "{colors.on-surface}"
    rounded: "{rounded.full}"
    height: "28px"
    padding: "0 12px"
    typography: "{typography.label-sm}"
    borderColor: "{colors.border}"
---

## Overview

Voltline Analytics is a daylight design language for analytical and trading product surfaces. A cool platinum canvas frames bright paper cards and deep ink typography; a single electric chartreuse accent (Volt Lime) carries every primary action and live data emphasis. The system is editorial and confident at the display scale, calm and utilitarian at the UI scale, with mono numerics for tickers and percentages.

Most trading interfaces go dark and then compensate with glow. Voltline takes the opposite position: it stays bright and earns its contrast from tonal layering, hairline borders, and one assertive accent against an airy platinum field. The result reads as premium fintech without dimming the screen.

The system is also deliberately quiet in its labelling. Analytical products invite clutter, so Voltline holds a hard line against it: no kickers, no uppercase mono ornament, no version stamps or invented metadata dressed up as detail. Density comes from real numbers and hairlines, never from filler.

## Colors

The palette is intentionally narrow. One canvas, one paper surface, one inset mist, one ink, one muted slate, one signature lime with its pressed deep, plus a hairline neutral. Color discipline matters: lime should never occupy more than roughly ten percent of any surface. It earns its weight by being rare.

| Token | Hex | Role |
| --- | --- | --- |
| `surface-canvas` | `#E6EAEE` | Primary page background; the cool platinum field |
| `surface` | `#FFFFFF` | Paper card and elevated container surface |
| `surface-inset` | `#F2F5F8` | Insets: input fields, tab rails, KPI tracks |
| `neutral-150` | `#E8EDF2` | Subtle secondary inset, low-attention bars |
| `neutral-300` | `#D8DEE5` | Deeper canvas variant for grouping |
| `secondary` (ink) | `#0B1015` | Primary text, ink panels, deep inversions |
| `secondary-soft` | `#1A2330` | Pressed/hover state for ink surfaces |
| `tertiary` (slate) | `#6B7785` | Secondary text, captions, inactive controls |
| `primary` (Volt Lime) | `#D6FF3D` | Signature accent, primary CTA, live data |
| `primary-deep` | `#A8CC22` | Pressed/hover lime, focus ring tone, lime borders |
| `primary-soft` | `#EAFFA8` | Soft tint for hover halos and selected states |
| `border` | `#D6DCE3` | Card, input, divider hairlines, chart gridlines |
| `positive` | `#2BB673` | Positive deltas, success states |
| `error` | `#E25555` | Negative deltas, destructive surfaces |

Contrast targets: ink on canvas and on paper meets WCAG AA at all body sizes. Slate on paper is reserved for non-critical secondary text. Ink on Volt Lime meets WCAG AA. Never invert a lime CTA to white labels.

## Typography

Three typefaces work in concert:

- **Instrument Sans** carries the editorial display moments: hero phrases, section headings, card titles, KPI values. It is narrow, sharply cut, and quiet at large sizes, which is what lets a 96px headline sit on a light canvas without shouting. Set it tight (-0.02em) and heavy (700) for display, 600 for titles.
- **Inter** handles all paragraph copy and UI labels. Its wider proportions separate it clearly from the display face at small sizes and keep dense interface text legible.
- **JetBrains Mono** is reserved for prices, tickers, percentages, and any tabular numbers. Tabular figures (`font-feature-settings: "tnum"`) are always on for numeric columns.

Hierarchy: a single display moment per surface, supported by `headline-md` for section titles, `headline-sm` for card headers, `body-md` for content, and `label-sm` for chips and controls. That is the whole ladder. Nothing sits above a heading.

**No eyebrows, no micro-label decoration.** The system has no kicker style, and it never sets uppercase tracked mono as ornament. Uppercase tracking survives in exactly one place: the ticker line on a KPI tile, where it labels a real instrument. Every other label must be sentence case, in the body face, and must carry information the user cannot already see. If removing a label loses nothing, it should not exist.

The highlight treatment uses a subtle bottom-half lime wash behind a phrase rather than coloring the whole word lime, so the accent still feels like ink first.

## Layout

Container width is 1200px max with 24px gutter; sections breathe with 56–96px vertical rhythm. The grid is content-led: two- to four-column splits, with the lead card occupying twice the width of supporting tiles. Density steps from generous on the hero to comfortable on dashboard surfaces.

Composition rules:

- A dark ink panel may anchor the hero. It hosts the display headline, intro copy, primary CTA, and at most one supporting card. Depth comes from a faint lime radial wash inside the panel, never from floating shapes.
- Paper cards sit on the platinum canvas with hairline borders and the lightest shadow. They never need both a border *and* a heavy shadow, since the hairline already separates them.
- Reserve the lime card variant for the single most important callout per screen (e.g. the active KPI, a featured action, a single highlight tile).
- Inline navigation is a pill rail; section nav is an underline rail with a lime indicator.

## Elevation & Depth

Voltline uses three soft layers rather than dramatic shadows:

1. **Canvas:** flat platinum, no shadow.
2. **Paper:** bright card with hairline border and a 1–2px contact shadow. Hover lifts to `elevation.md` (a low, soft drop) so cards feel grippable but not aggressive.
3. **Lime:** accent surfaces carry a tight lime-tinted halo (`elevation.accent`). This is the only surface that "glows," reserved for the one accent action per region.

Ink panels live on the canvas at the same z-plane as paper cards but visually recede via tonal contrast rather than shadow. Focus rings are a 3px translucent lime-deep glow around the control; never use system blue.

## Shapes

The shape language is rounded but architectural: pills and 18px paper cards, never blob-soft.

- **Pills (999px):** all buttons, chips, tab rails, badge dots, nav links.
- **Card radius (18px):** all paper and ink cards, KPI tiles, sidebars.
- **Input radius (12px):** text fields, selects, textareas, small surfaces.
- **Hero radius (28px):** the anchor ink panel and major hero containers.
- **Check radius (6px):** the only true small-corner shape; keeps controls precise without feeling sharp.

Pill geometry is reserved for controls that do something: buttons, chips, tabs, nav links, status dots. Never float a pill, ellipse, or blob into a layout as ornament. If a surface needs depth, use a faint lime radial wash inside the panel instead of a decorative shape.

## Components

### Button

Two primary variants, plus ghost and ink supplemental:

- **Primary:** Volt Lime fill, ink label, pill shape, lime-tinted halo, 44px default height. The CTA of the system.
- **Secondary:** paper fill, hairline border, ink label. Pairs alongside primary; never carries the lime halo.
- **Ghost:** transparent, ink label, mist hover. For tertiary and toolbar actions.
- **Ink:** deep ink fill, paper label. Used inside lime-heavy regions to invert hierarchy.

All buttons accept a leading or trailing icon (`vl-btn__icon`). Sizes: `vl-btn--sm` (36px), default (44px), `vl-btn--lg` (52px).

### Input

Rounded 12px fields on mist surface with hairline borders. The label sits above, the help text below, and an optional leading icon enters the field at 14px from the left edge via `vl-input-group`. Focus replaces the mist with paper, swaps the border to `primary-deep`, and adds a translucent lime focus ring. Inputs support a mono modifier for numeric entry.

### Card

The base unit is the paper card: 18px radius, hairline border, 24px padding, soft elevation. Two variants extend it:

- `vl-card--accent` swaps the surface to Volt Lime, keeps ink type, and adopts the accent halo. Use once per screen.
- `vl-card--ink` flips to a deep ink surface for hero or contrast panels.

A card header is a row of title + meta separated by `space-between`. Cards may contain KPI tiles, lists, or freeform content.

### Checkbox

A 20×20px tile with a 6px radius. Default state is a paper square with a hairline border; checked state fills with Volt Lime, swaps the border to lime-deep, and reveals an ink checkmark drawn from CSS borders rather than an SVG path. Focus draws the same lime ring as inputs and buttons.

### Tabs

Two patterns, one shared language:

- **Pill rail:** a mist track with paper-on-mist active tabs. Best for view switchers and toolbars.
- **Underline rail:** borderless tabs with a lime-deep underline on the active item. Best for top-level page navigation inside cards.

Both expose an `aria-selected="true"` selector for active state, and `.is-active` as a convenience hook.

### Signature: KPI Sparkline Tile

The system's signature element is `vl-kpi`: a rounded paper (or ink) card that shows a mono ticker, a large display value, an optional lime delta chip, and a compact bar-burst sparkline. The bars mix Volt Lime, lime-deep, and ink so a distribution reads as structure rather than decoration. This tile is reusable as a dashboard primitive and as a marketing surface; it carries the most identity per square pixel in the system.

### Navigation

`vl-nav` is a deep ink pill bar with a lime mark in the brand block, ghost-style links, and an inline space for sign-in or primary CTA. Use one navigation per page; never duplicate the lime brand mark.

### Icons

The system uses **Lucide** (https://lucide.dev/, ISC) exclusively. Icons render at `currentColor`, default stroke width 1.75, default size 16–18px in controls and 20–24px in surface accents. Initialize with `lucide.createIcons()` after dropping the official browser script tag. Do not introduce custom SVG paths or mix icon libraries.

## Do's and Don'ts

**Do**

- Lead with ink on platinum or ink on paper; let lime carry the single most important moment in any region.
- Pair Instrument Sans display with Inter UI and JetBrains Mono numbers. One trio, no substitutions.
- Use pill geometry for actions and 18px rounding for cards consistently across a surface.
- Add the lime halo only to the one primary action or one accent tile per region.
- Use tabular figures for all numeric columns and KPIs.
- Lead a section with its heading. Stack any supporting sentence directly beneath it, never beside it.
- Give every number a plain-language label, or drop the number.

**Don't**

- Don't introduce a second accent color. Volt Lime is the only signature hue; greens, blues, and oranges as decoration will dilute it.
- Don't set an eyebrow, kicker, or category label above a heading. The system has no such style.
- Don't line surfaces with uppercase mono micro-labels, version stamps, or invented metadata. The KPI ticker is the only uppercase tracked element in the system.
- Don't use pills or badges as ornament. A chip must report live state, a delta, or a count.
- Don't use heavy drop shadows or coloured shadows other than the lime halo on accent surfaces.
- Don't set white type on Volt Lime. Labels must stay ink for contrast.
- Don't mix icon libraries or invent custom glyph paths; stay inside Lucide.
- Don't apply the `vl-highlight` lime wash to more than one phrase per hero, or to body copy.
- Don't shrink padding below the defined spacing scale; density comes from hairlines and tone, not cramped layout.
