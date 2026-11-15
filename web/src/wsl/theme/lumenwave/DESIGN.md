---
version: alpha
name: Lumenwave Finance
description: A confident, light-mode fintech design language built on a warm off-white canvas, ink-black UI weight, and a single electric lime signal — extended by floating pill geometry, hairline cards, and editorial serif headlines.
theme: light
colors:
  primary: "#0B0B0E"
  on-primary: "#FFFFFF"
  secondary: "#D8FF36"
  on-secondary: "#0B0B0E"
  tertiary: "#7A5BFF"
  on-tertiary: "#FFFFFF"
  accent-warm: "#FF8A3D"
  neutral: "#5C5F66"
  surface: "#FFFFFF"
  surface-sunken: "#EEEFF1"
  surface-raised: "#FFFFFF"
  on-surface: "#0B0B0E"
  on-surface-muted: "#5C5F66"
  border: "#E3E5E9"
  border-strong: "#0B0B0E"
  focus: "#D8FF36"
  success: "#2BB673"
  error: "#E5484D"
typography:
  display-xl:
    fontFamily: "'Instrument Serif', 'Times New Roman', serif"
    fontWeight: 400
    fontSize: "72px"
    lineHeight: 1.02
    letterSpacing: "-0.02em"
  display-lg:
    fontFamily: "'Instrument Serif', 'Times New Roman', serif"
    fontWeight: 400
    fontSize: "56px"
    lineHeight: 1.04
    letterSpacing: "-0.02em"
  display-italic:
    fontFamily: "'Instrument Serif', 'Times New Roman', serif"
    fontWeight: 400
    fontStyle: italic
    fontSize: "56px"
    lineHeight: 1.04
    letterSpacing: "-0.015em"
  headline-md:
    fontFamily: "'Inter', system-ui, sans-serif"
    fontWeight: 600
    fontSize: "24px"
    lineHeight: 1.2
    letterSpacing: "-0.01em"
  headline-sm:
    fontFamily: "'Inter', system-ui, sans-serif"
    fontWeight: 600
    fontSize: "18px"
    lineHeight: 1.25
    letterSpacing: "-0.005em"
  body-md:
    fontFamily: "'Inter', system-ui, sans-serif"
    fontWeight: 400
    fontSize: "16px"
    lineHeight: 1.55
    letterSpacing: "0em"
  body-sm:
    fontFamily: "'Inter', system-ui, sans-serif"
    fontWeight: 400
    fontSize: "14px"
    lineHeight: 1.5
    letterSpacing: "0em"
  label-md:
    fontFamily: "'Inter', system-ui, sans-serif"
    fontWeight: 500
    fontSize: "14px"
    lineHeight: 1.2
    letterSpacing: "0.005em"
  label-sm:
    fontFamily: "'Inter', system-ui, sans-serif"
    fontWeight: 500
    fontSize: "12px"
    lineHeight: 1.2
    letterSpacing: "0.04em"
    textTransform: uppercase
  numeric-xl:
    fontFamily: "'JetBrains Mono', 'SFMono-Regular', monospace"
    fontWeight: 700
    fontSize: "40px"
    lineHeight: 1.05
    letterSpacing: "-0.02em"
  numeric-md:
    fontFamily: "'JetBrains Mono', 'SFMono-Regular', monospace"
    fontWeight: 500
    fontSize: "16px"
    lineHeight: 1.2
    letterSpacing: "0em"
rounded:
  none: "0px"
  xs: "6px"
  sm: "8px"
  md: "12px"
  lg: "20px"
  xl: "28px"
  full: "999px"
spacing:
  xs: "4px"
  sm: "8px"
  md: "12px"
  lg: "16px"
  xl: "24px"
  2xl: "32px"
  3xl: "48px"
  4xl: "72px"
  5xl: "112px"
  container: "1200px"
  gutter: "24px"
elevation:
  flat: "none"
  contact: "0 1px 2px rgba(11, 11, 14, 0.05)"
  raised: "0 1px 2px rgba(11, 11, 14, 0.04), 0 12px 28px -16px rgba(11, 11, 14, 0.18)"
  halo: "0 1px 2px rgba(11, 11, 14, 0.04), 0 24px 60px -24px rgba(11, 11, 14, 0.22)"
  drift: "0 2px 4px rgba(11, 11, 14, 0.06), 0 32px 80px -28px rgba(11, 11, 14, 0.28)"
components:
  button-primary:
    backgroundColor: "{colors.primary}"
    textColor: "{colors.on-primary}"
    typography: "{typography.label-md}"
    rounded: "{rounded.full}"
    padding: "14px 24px"
    height: "48px"
  button-primary-hover:
    backgroundColor: "#1A1A20"
    textColor: "{colors.on-primary}"
  button-accent:
    backgroundColor: "{colors.secondary}"
    textColor: "{colors.on-secondary}"
    typography: "{typography.label-md}"
    rounded: "{rounded.full}"
    padding: "14px 24px"
    height: "48px"
  button-accent-hover:
    backgroundColor: "#C7EE2A"
    textColor: "{colors.on-secondary}"
  button-ghost:
    backgroundColor: "transparent"
    textColor: "{colors.on-surface}"
    typography: "{typography.label-md}"
    rounded: "{rounded.full}"
    padding: "14px 24px"
    height: "48px"
  input-field:
    backgroundColor: "{colors.surface}"
    textColor: "{colors.on-surface}"
    typography: "{typography.body-md}"
    rounded: "{rounded.md}"
    padding: "14px 16px"
    height: "48px"
  input-field-focus:
    backgroundColor: "{colors.surface}"
    textColor: "{colors.on-surface}"
  card:
    backgroundColor: "{colors.surface}"
    textColor: "{colors.on-surface}"
    rounded: "{rounded.lg}"
    padding: "24px"
  card-elevated:
    backgroundColor: "{colors.surface}"
    rounded: "{rounded.xl}"
    padding: "28px"
  checkbox:
    backgroundColor: "{colors.surface}"
    rounded: "{rounded.xs}"
    size: "20px"
  checkbox-checked:
    backgroundColor: "{colors.secondary}"
    textColor: "{colors.on-secondary}"
    rounded: "{rounded.xs}"
    size: "20px"
  tabs-track:
    backgroundColor: "{colors.surface-sunken}"
    rounded: "{rounded.full}"
    padding: "4px"
    height: "48px"
  tabs-active:
    backgroundColor: "{colors.primary}"
    textColor: "{colors.on-primary}"
    typography: "{typography.label-md}"
    rounded: "{rounded.full}"
  tabs-inactive:
    backgroundColor: "transparent"
    textColor: "{colors.on-surface-muted}"
    typography: "{typography.label-md}"
  tile-cluster-card:
    backgroundColor: "{colors.surface}"
    rounded: "{rounded.lg}"
    padding: "20px"
  pill-chip:
    backgroundColor: "{colors.primary}"
    textColor: "{colors.on-primary}"
    typography: "{typography.label-sm}"
    rounded: "{rounded.full}"
    padding: "6px 12px"
  pill-chip-accent:
    backgroundColor: "{colors.secondary}"
    textColor: "{colors.on-secondary}"
    typography: "{typography.label-sm}"
    rounded: "{rounded.full}"
    padding: "6px 12px"
---

## Overview

Lumenwave Finance is a light-mode fintech design language for products that move money — banking dashboards, payments, investment, and treasury tooling. The vibe is editorial calm meeting operational precision: the page should read like a confident magazine cover that happens to run real ledger UI. A warm off-white canvas does the heavy lifting, ink-black UI weight grounds every action, and a single signal lime accent earns attention without ever being decorative.

What it should feel like in use: bright, quiet, premium, and unmistakably digital. Tiles feel like paper resting on the canvas. Pill geometry reads as motion-ready. Headlines feel hand-set. Numbers feel measured. The system should never feel busy, neon, or candy-colored. It should never feel like a generic SaaS recolor or a chat-app pastel kit.

Essential traits to preserve:

- One lime accent — never two competing accent colors fighting for the same role.
- Pill (`999px`) geometry on all primary actions and chips, contrasted against 20-28px tiles and 12px inputs.
- Serif display ↔ grotesque UI ↔ mono numeric — three voices, each with a clear job.
- Floating dashboard tiles arranged with deliberate overlap and tilt, lifted by soft halo shadows, not hard drop shadows.
- Generous canvas breathing room around the hero; tighter rhythm inside data zones.

## Colors

The palette is intentionally narrow: four neutrals do the structural work, lime carries every primary action, violet stays inside data visualization, and tangerine appears only in badges or small punches of warmth.

- `surface-sunken` `#EEEFF1` — the warm-cool off-white canvas. The single primary background of the entire system. Use it behind every page, behind the sunken tab track, and as the rest state for the application chrome.
- `surface` `#FFFFFF` — paper. Reserved for cards, dashboard tiles, inputs, dropdowns, and elevated panes. Always rests above the canvas with a visible hairline border and a soft halo shadow.
- `primary` `#0B0B0E` — near-black ink. Carries body headlines, primary buttons, the active tab thumb, and the brand pill on chrome elements. Use it sparingly as a fill so it always reads as authoritative weight.
- `neutral` `#5C5F66` — graphite. The only secondary text color. Use for meta, captions, labels under numbers, and inactive tab labels.
- `secondary` `#D8FF36` — signal lime. The product's single accent. Use it for the primary CTA fill, key chips, active-state dots, focus rings, currency pings, and "this matters" highlights. Never use lime for body text and never tint surfaces with lime; it loses signal value the moment it covers area.
- `tertiary` `#7A5BFF` — violet pulse. Reserved for charts, sparklines, progress arcs, and data tracks. Do not use it for buttons or copy.
- `accent-warm` `#FF8A3D` — tangerine tag. Used only for rare alert pills, currency badges, and seasonal highlights. Never paired with lime in the same component.
- `border` `#E3E5E9` — hairline. The 1px stroke around every paper tile, input, and divider. Always 1px; never thicker.

Contrast: ink on canvas hits 16:1; ink on paper hits 18:1; lime is used only with ink on top, never with white on top.

## Typography

Three families, three roles:

- **Instrument Serif** — display only. Used for the hero headline and editorial pull quotes. Optional italic variant adds personality on the second clause of the headline ("…for *a better tomorrow*"). Never set body or UI in serif.
- **Inter** — every UI surface. Body, navigation, buttons, labels, chip text, table headers, table cells when they aren't numeric. Weights `400` for body, `500` for labels and buttons, `600` for headlines, `700` reserved for emphasis.
- **JetBrains Mono** — numbers. Balances, transaction amounts, KPI deltas, percentages, tabular columns. Use `500` for small numerics, `700` for hero balance figures. Tabular numerals only — never mix with proportional digits.

Type rhythm: hero serif (`display-xl` 72px) sits 32-48px above a 16px Inter sub-line in graphite. Card headlines are 18-24px Inter semibold; supporting copy is 14px Inter regular in graphite. Money figures jump to 40px JetBrains Mono in ink, with a 12px uppercase Inter label above in graphite.

Hierarchy rule: a section can only have one display-class element. Everything else descends to headline, body, label, or numeric.

## Layout

The layout language is **calm-canvas plus floating-tile cluster**.

Containers and rhythm:

- Page container: max width `1200px`, centered, with `gutter` `24px` side padding that opens to `48-72px` at 1024px+.
- Vertical rhythm: hero sections breathe with `112px` top padding and `72-96px` bottom padding. Data dashboards collapse to `48-64px` section padding. Card internal padding is `24-28px`; tile cluster cards drop to `20px` to feel compact.
- Section grid: 12-column grid for marketing surfaces; data dashboards use a 4-column tile grid with `24px` gutter and explicit overlap on the signature cluster.

Header / navigation pattern:

- Top navigation is a single pill-shaped pod resting on the canvas: brand pill on the left (ink fill, lime brand dot), inline links in graphite, and two CTAs grouped on the right — a ghost "Sign In" pill and a lime "Create Account" pill. Keep the entire nav block under 80px tall.
- The header pod never spans full width; it lives inside the page container so the canvas frames it.

Hero pattern:

- Editorial center-aligned serif headline (two lines, with italic emphasis on a key phrase), a single 14px graphite sub-paragraph beneath, then a horizontal pair of CTAs: ink primary + ghost secondary.
- Below the CTAs, the **floating balance tile cluster** appears centered with deliberate horizontal overlap — left-edge balance card, central dashboard tile, right-edge KPI tile — each tilted between -3° and 3°, lifted by halo shadow. The small tiles stack above the central tile and may overlap it only as far as its padding, never far enough to cover a number or a chart.

Dashboard pattern:

- Sidebar on the left (pill nav, brand pill at top), main canvas with stacked tiles: a "welcome" row, a 3-up KPI row (Balance / Volume / Fees), a chart card spanning two columns, and a transactions list.
- Each tile is a paper card with hairline border and halo shadow. KPI tiles always pair: 12px label (graphite uppercase) → 40px mono numeric (ink) → 14px sparkline or delta chip.

Form pattern:

- Stack labels above inputs in `label-sm` graphite uppercase. Inputs are 48px tall, 12px radius, paper fill, hairline border, lime focus ring. Submit button is full-width pill ink-primary unless the form sits inside a tile cluster — then use the lime variant.

Responsive behavior:

- At `<960px`, the cluster un-overlaps into a vertical stack with reduced tilt (0°), and the hero sub-line drops below the headline at 18px.
- At `<640px`, the header pill collapses to brand pill + hamburger pill, the hero headline drops to `display-lg` 56px, KPIs reflow to one-up, and tabs scroll horizontally.

Alignment rules:

- Marketing surfaces (hero, footers, feature blocks): center-aligned with editorial calm.
- Operational surfaces (dashboard, settings, transactions): left-aligned with strict grid.
- Never center-align numeric columns. Tabular numbers always right-align inside cells.

## Elevation & Depth

Depth is achieved through a **two-layer soft shadow** system. No hard drop shadows, no neon glows, no gradients on chrome.

- `flat` — used for the canvas itself and for inline chips. No shadow.
- `contact` — `0 1px 2px rgba(11,11,14,0.05)`. Used on hover-able pill chips and small ghost surfaces.
- `raised` — combined contact + medium halo. Standard for dashboard tiles and form cards.
- `halo` — combined contact + broad halo with -24px offset. Used on hero tile cluster cards and any tile that should feel "lifted from the page."
- `drift` — strongest. Used only on the central tile of the signature cluster to anchor it.

Material rules:

- Paper tiles always sit on the canvas — never on a colored tint.
- Lime CTAs gain a subtle inner gloss (1px inset top highlight at 30% white) so they feel pressed-glossy, not flat.
- Ink CTAs use no inner highlight; their weight comes from the contrast against the canvas.
- Cluster tiles overlap with `z-index` order, but each tile keeps its full hairline border — never strip the border at the overlap.

## Shapes

Lumenwave uses three coexisting radius languages that signal function:

- **Pill (`full` 999px)** — buttons, primary chips, brand pods, tab tracks, focus rings. Signals "interactive, motion-ready."
- **Large radius (`lg` 20px / `xl` 28px)** — cards and dashboard tiles. Signals "object resting on canvas."
- **Medium radius (`md` 12px)** — inputs, dropdowns, small tiles, KPI inner badges. Signals "container of data."
- **Small radius (`xs` 6px / `sm` 8px)** — checkboxes, tag chips inside tiles, status dots. Signals "nested, granular."

Border weight is always 1px. The only exception is the focus ring, which uses a 3px lime offset.

Decorative motifs:

- A `12px` lime dot used as a "live" indicator on cards (balance is updating, account is active).
- A short hairline `1px` divider used inside cards to separate label rows; never between tiles — tiles separate by canvas gap.
- Avatar stack chips (small overlapping circle pills) used to show participants on shared finance features.

## Components

### Button

- **Primary (`btn-primary`)** — pill, ink fill, white label, `48px` tall, `24px` horizontal padding. Hover lifts to `#1A1A20`. Use for any single most-important page action.
- **Accent (`btn-accent`)** — pill, lime fill, ink label, same dimensions. Use only when the primary action sits **inside** a dashboard tile or on a marketing CTA where energy matters. Never two accent buttons in the same view.
- **Ghost (`btn-ghost`)** — transparent fill, ink label, hairline border. Companion to primary or accent.
- **Icon-leading** — any variant may host a 16px Lucide icon left of the label, with 8px gap. Use `currentColor` on the icon.

### Input

- **Text field (`input-field`)** — `48px` tall, paper fill, 12px radius, hairline border, 16px horizontal padding, `body-md` text. Label sits above in `label-sm` uppercase graphite. Focus gains a 3px lime offset ring and the border darkens to ink.
- **Currency field** — same shell, but the value is set in `numeric-md` JetBrains Mono and prefixed with a small ink "$" glyph in Inter. Cursor caret stays ink.
- **Search field** — pill variant (`full` radius) with a 16px Lucide search icon left of the cursor.

### Card

- **Paper card (`card`)** — paper fill, 20px radius, hairline border, `raised` shadow, 24px padding. Default container for any block of related information.
- **Tile card (`card-tile`)** — 20px radius, `halo` shadow, 20px padding, used inside the signature cluster. Always carries a corner element (currency dot, avatar, chip).
- **Feature card** — paper card with a 12px lime dot at top-left, headline + 2-line description + ghost CTA.

### Checkbox

- **Unchecked (`checkbox`)** — `20px` square, paper fill, 6px radius, ink hairline border.
- **Checked (`checkbox-checked`)** — lime fill, ink check glyph, same 6px radius. Animation hint: fill scales from 0.7 → 1.0 over 120ms; the check draws in 80ms.
- Pair with `label-md` Inter, left-aligned, with 12px gap.

### Tabs

- **Track (`tabs-track`)** — pill (`full` radius), `surface-sunken` fill, 4px inner padding, 48px tall.
- **Active thumb (`tabs-active`)** — pill, ink fill, white label, slides between positions with a 200ms ease.
- **Inactive (`tabs-inactive`)** — graphite label, no fill. Hover darkens to ink.
- Use tabs to switch between data views (Day / Week / Month, or Income / Expenses / Transfers).

### Signature: Floating Balance Tile Cluster

The system's fifth, signature element is the **floating balance tile cluster** — an overlapping arrangement of three to five small finance tiles that drift over the canvas. It establishes Lumenwave's identity as much as the lime accent does.

Anatomy:

- A **central dashboard tile**: paper, 20px radius, `drift` shadow, contains a "Welcome back, {name}" line, three KPI columns (Balance / Volume / Fees), and a 3-row recent transactions list.
- A **left balance card**: paper, 16-20px radius, `halo` shadow, tilted -2° to -3°, shows a logo pill, "Money transfer" label, and a `+$XXX,XXX` amount in mono.
- A **right KPI tile**: paper, 16-20px radius, `halo` shadow, tilted +2° to +3°, shows "Average spent in half a year" with a small violet bar chart.
- **Accessory chips**: at most two floating pills, and each one must carry real data (an incoming amount, a named counterparty). Never add a chip as decoration to fill space.

Composition rules:

- Tiles overlap by `24-48px` horizontally — never more than half a tile's width.
- Each tile keeps its hairline border intact at the overlap; never cut the border.
- Use `transform: translate() rotate()` to set drift; keep rotation under 3.5° absolute.
- The cluster sits below the hero CTAs with `48-64px` clearance, on canvas, never inside another container.

### Data Visualization

When charts appear inside tiles:

- Lines and bars: violet pulse (`#7A5BFF`), 2px stroke.
- Sparkline area fills: violet at 12% opacity.
- Axis labels: `label-sm` graphite.
- Currency markers on the line: 4px ink dots.
- A single emphasized data point uses a 6px lime dot with a 2px ink ring.

## Do's and Don'ts

**Do**

- Do center-align the marketing hero serif headline and keep the canvas wide around it.
- Do let the floating balance tile cluster overlap with 1-3° tilt — it is the system's signature.
- Do use the pill geometry for all primary actions, chips, and the brand pod.
- Do reserve lime for one role per view: either the primary CTA, or the focus ring, or the live dot — not all three at once.
- Do use Instrument Serif italics on a single emphasized clause inside the hero headline.
- Do always render money in JetBrains Mono with tabular numerals.
- Do keep dashboard tiles on the canvas, never inside a tinted container.

**Don't**

- Don't introduce a second accent color alongside lime; violet stays inside charts and tangerine stays inside badges.
- Don't use lime as a background fill for a section or a large card — it loses signal value.
- Don't apply hard drop shadows or neon glows; use only the two-layer halo system.
- Don't set body or UI text in Instrument Serif.
- Don't right-align or center-align money columns inside tables anywhere except the totals row.
- Don't stack more than three radius languages in one component (pill + 20px + 12px is the maximum).
- Don't split a section heading and its supporting line across the same row. Stack them, heading first, one supporting line only.
- Don't set a label, tag, or all-caps line above a heading. The headline opens the section.
- Don't scatter ornamental metadata (version tags, specimen numbers, coordinates, reading times, invented counts). A label ships only when a user needs it.
- Don't lay glow or gradient washes over the canvas. The off-white is the background, flat and undisturbed.

## Iconography

The system uses **Lucide** (https://lucide.dev/, ISC license) as the sole icon library. Lucide's 1.5px outline weight and rounded line caps match the system's calm + precise tone: friendly enough for marketing, technical enough for the dashboard. Use icons at 16px in buttons and labels, 20px inside tile chrome, and 24px in sidebar navigation. Always set icon color via `currentColor` so they inherit ink, graphite, or on-accent context. Pair icons with a 14-16px Inter label whenever possible; avoid icon-only buttons except for utility chrome.
