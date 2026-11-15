---
version: alpha
name: Fairweather
description: A calm, friendly product system for messaging and audience tools. Soft white cards rise out of dithered pixel-art skies on a quiet dot-grid ground, and one saturated green marks the only live thing on screen.
colors:
  primary: "#171717"
  on-primary: "#FFFFFF"
  secondary: "#3F6FD1"
  secondary-deep: "#2F58AE"
  tertiary: "#1DB954"
  tertiary-text: "#12994A"
  tertiary-tint: "#E7F8EE"
  neutral: "#F2F2F1"
  surface: "#FFFFFF"
  on-surface: "#171717"
  on-surface-muted: "#6E6E6B"
  well: "#ECECEA"
  well-deep: "#D9D9D6"
  dot: "#DCDCD9"
  cloudlight: "#F4E7C8"
  border: "#EBEBEB"
  border-strong: "#D9D9D9"
  focus: "#3F6FD1"
  error: "#D14343"
  error-tint: "#FBEAEA"
  chrome-close: "#FF5F57"
  chrome-min: "#FEBC2E"
  chrome-max: "#28C840"
typography:
  display:
    fontFamily: Albert Sans
    fontSize: 64px
    fontWeight: 600
    lineHeight: 1.02
    letterSpacing: -0.03em
  headline-lg:
    fontFamily: Albert Sans
    fontSize: 48px
    fontWeight: 600
    lineHeight: 1.08
    letterSpacing: -0.02em
  headline-md:
    fontFamily: Albert Sans
    fontSize: 36px
    fontWeight: 500
    lineHeight: 1.12
    letterSpacing: -0.02em
  headline-sm:
    fontFamily: Albert Sans
    fontSize: 26px
    fontWeight: 500
    lineHeight: 1.22
    letterSpacing: -0.02em
  title:
    fontFamily: Albert Sans
    fontSize: 20px
    fontWeight: 500
    lineHeight: 1.3
    letterSpacing: -0.02em
  body-lg:
    fontFamily: Albert Sans
    fontSize: 18px
    fontWeight: 400
    lineHeight: 1.5
  body-md:
    fontFamily: Albert Sans
    fontSize: 17px
    fontWeight: 400
    lineHeight: 1.5
  body-sm:
    fontFamily: Albert Sans
    fontSize: 15px
    fontWeight: 400
    lineHeight: 1.45
  label-md:
    fontFamily: Albert Sans
    fontSize: 14px
    fontWeight: 500
    lineHeight: 1.3
  mono-channel:
    fontFamily: Fragment Mono
    fontSize: 14px
    fontWeight: 400
    lineHeight: 1.2
    letterSpacing: 0.04em
  mono-sm:
    fontFamily: Fragment Mono
    fontSize: 13px
    fontWeight: 400
    lineHeight: 1.3
rounded:
  none: 0px
  sm: 8px
  md: 14px
  bubble: 18px
  window: 24px
  card: 32px
  full: 999px
spacing:
  xxs: 4px
  xs: 8px
  sm: 12px
  md: 16px
  lg: 24px
  xl: 32px
  xxl: 40px
  section-sm: 80px
  section: 120px
  card-padding: 40px
  gutter: 24px
  dot-grid: 8px
  container: 1200px
  container-narrow: 720px
components:
  button-primary:
    backgroundColor: "{colors.primary}"
    textColor: "{colors.on-primary}"
    typography: "{typography.body-md}"
    rounded: "{rounded.md}"
    height: 48px
    padding: 0 24px
  button-primary-hover:
    backgroundColor: "#2A2A2A"
    textColor: "{colors.on-primary}"
  button-key:
    backgroundColor: "{colors.surface}"
    textColor: "{colors.on-surface}"
    typography: "{typography.body-md}"
    rounded: "{rounded.md}"
    height: 48px
    padding: 0 24px
  button-key-pressed:
    backgroundColor: "{colors.surface}"
    textColor: "{colors.on-surface}"
  button-quiet:
    backgroundColor: "#00000000"
    textColor: "{colors.on-surface}"
    rounded: "{rounded.md}"
  button-quiet-hover:
    backgroundColor: "{colors.well}"
  input-field:
    backgroundColor: "{colors.well}"
    textColor: "{colors.on-surface}"
    typography: "{typography.body-md}"
    rounded: "{rounded.md}"
    height: 48px
    padding: 0 16px
  input-field-focus:
    backgroundColor: "{colors.surface}"
    textColor: "{colors.on-surface}"
  checkbox:
    backgroundColor: "{colors.surface}"
    rounded: 6px
    size: 20px
  checkbox-checked:
    backgroundColor: "{colors.primary}"
    textColor: "{colors.on-primary}"
  toggle:
    backgroundColor: "{colors.well-deep}"
    rounded: "{rounded.full}"
    width: 40px
    height: 24px
  toggle-on:
    backgroundColor: "{colors.tertiary}"
  tabs:
    backgroundColor: "{colors.well}"
    rounded: "{rounded.md}"
    padding: 4px
  tabs-active:
    backgroundColor: "{colors.surface}"
    textColor: "{colors.on-surface}"
    typography: "{typography.label-md}"
    rounded: 10px
  tabs-inactive:
    textColor: "{colors.on-surface-muted}"
  card:
    backgroundColor: "{colors.surface}"
    textColor: "{colors.on-surface}"
    rounded: "{rounded.card}"
    padding: 40px
  channel-label:
    textColor: "{colors.tertiary-text}"
    typography: "{typography.mono-channel}"
  live-dot:
    backgroundColor: "{colors.tertiary}"
    size: 8px
  weather-band:
    backgroundColor: "{colors.secondary}"
    height: 300px
  app-window:
    backgroundColor: "{colors.surface}"
    rounded: "{rounded.window}"
  skeleton:
    backgroundColor: "{colors.well}"
    rounded: "{rounded.md}"
  bubble-in:
    backgroundColor: "{colors.surface}"
    textColor: "{colors.on-surface}"
    typography: "{typography.body-sm}"
    rounded: "{rounded.bubble}"
  bubble-out:
    backgroundColor: "{colors.tertiary-tint}"
    textColor: "{colors.on-surface}"
    typography: "{typography.body-sm}"
    rounded: "{rounded.bubble}"
  link:
    textColor: "{colors.secondary}"
  link-hover:
    textColor: "{colors.secondary-deep}"
---

# Fairweather

## Overview

Fairweather is a clear window on a good day. The product itself is plain: white cards, big soft corners, one hairline, grey supporting copy. All of the atmosphere lives in exactly one place, a dithered pixel-art sky band clipped to the top of a card, with a white app window rising out of its bottom edge. The product literally emerges from the weather.

The target feeling is **calm competence**. It is a messaging and audience tool (campaigns, contacts, replies, connected channels) that feels like opening a window rather than opening a dashboard. It should read as friendly and precise at once: soft radii and warm skies for friendliness, a tight neo-grotesk and a mono voice for precision.

What it must never feel like:

- A dark-mode developer console. Fairweather is light, always.
- A gradient SaaS landing page. The sky is pixel-stippled imagery, never a CSS gradient.
- A retro pixel-art game. The pixels live only inside the sky. Type, buttons and icons stay crisp and modern.
- A dashboard full of chips, badges and colored metrics. Ninety percent of the surface is neutral.

The three traits that must survive every screen:

1. **The weather window.** Dithered sky band on top, white window with traffic-light chrome and skeleton content rising out of it, cut off by the band.
2. **One live green.** Signal Green appears only where something is actually connected, sending or succeeding.
3. **The dot-grid ground.** A pale Overcast field with 1px dots on an 8px grid. Cards float on it; nothing else decorates it.

## Colors

Near-monochrome neutrals carry the whole interface. Color is a signal, not a decoration.

| Token | Hex | Role |
|---|---|---|
| Overcast (`neutral`) | `#F2F2F1` | Page ground. Always carries the dot grid (`#DCDCD9` dots). |
| Paper (`surface`) | `#FFFFFF` | Cards, app windows, key buttons, bubbles. Every readable surface. |
| Ink (`primary`) | `#171717` | Headings, primary copy, primary button fill, checked checkbox. |
| Drizzle (`on-surface-muted`) | `#6E6E6B` | Card descriptions, meta, placeholders, inactive tabs. |
| Mist (`well`) | `#ECECEA` | Input wells, tab tracks, skeleton blocks, icon tiles. |
| Mist deep (`well-deep`) | `#D9D9D6` | Key-cap bottom edge, toggle off track. |
| Signal Green (`tertiary`) | `#1DB954` | Live dots, toggle on, channel icon. The only saturated UI color. |
| Signal text (`tertiary-text`) | `#12994A` | Green text on white (channel labels, live status). |
| Signal tint (`tertiary-tint`) | `#E7F8EE` | Outgoing message bubbles, live-dot halo. |
| Zenith Blue (`secondary`) | `#3F6FD1` | Links, focus rings, the fallback fill of a sky band before its image loads. |
| Cloudlight | `#F4E7C8` | Lives inside sky imagery. Never a UI fill. |

Rules:

- **Green means live.** Use Signal Green only for connected channels, delivered/sent states, on-toggles and success. Never for a CTA, a heading, a logo or decoration. If nothing on the screen is live, there is no green on the screen.
- **Small green text is always `#12994A`**, never `#1DB954`. Pair it with the mono face and an icon so meaning never depends on color alone.
- **Blue is for navigation and focus**, nothing else. No blue buttons, no blue cards.
- **Borders are `rgba(23,23,23,0.08)`** (flattened as `#EBEBEB` in tokens). Strong borders (`0.16`) are only for key-cap buttons and checkboxes.
- **No gradients in UI chrome.** The sky image is the only gradient-like area in the entire system.
- Traffic-light red, amber and green (`#FF5F57`, `#FEBC2E`, `#28C840`) appear only as 8px window chrome dots. Never reuse them as status colors.

## Typography

Two families, strictly separated.

**Albert Sans** (400, 500, 600) sets everything readable. Headings use 500 or 600 with `-0.02em` tracking (display tightens to `-0.03em`). Never set anything at 700 or heavier. Sentence case always; no title case, no all-caps sans.

**Fragment Mono** is the machine voice. It is allowed for exactly four things: the channel label, live status, numerals in tables/counts, and code. Everything else is Albert Sans.

| Level | Size / line | Weight | Use |
|---|---|---|---|
| display | 64 / 1.02 | 600 | One hero statement per page. |
| headline-lg | 48 / 1.08 | 600 | Page `h1`. |
| headline-md | 36 / 1.12 | 500 | Section `h2`. |
| headline-sm | 26 / 1.22 | 500 | Card titles. |
| title | 20 / 1.3 | 500 | Sub-sections, dialog titles. |
| body-lg | 18 / 1.5 | 400 | Card descriptions and ledes, in Drizzle. |
| body-md | 17 / 1.5 | 400 | Default body, buttons. |
| body-sm | 15 / 1.45 | 400 | Tables, bubbles, meta rows. |
| label-md | 14 / 1.3 | 500 | Form labels, tabs. |
| mono-channel | 14, +0.04em, uppercase | 400 | Channel label, in `#12994A`. |
| mono-sm | 13 | 400 | Status text, timestamps inside real message bubbles, counts. |

Rules:

- Headings are Ink. Supporting copy directly under a heading is Drizzle at body-lg, one sentence, max 56 characters per line.
- **One subtitle per heading block.** Heading, one supporting sentence, then an action or content.
- The mono channel label may sit directly above a **card title**, because it names a real channel ("WhatsApp integration", "SMS channel", "Instagram inbox"). It never sits above a page-level `h1` or section `h2`, and never names a category, section or marketing idea.
- Numerals in tables and counts use Fragment Mono with tabular figures so columns align.

## Layout

The ground is the dot grid, and the unit is the card. Layout is generous and centered on a 1200px container with 24px gutters, but cards themselves are internally left-aligned.

**Page rhythm**

- Container `1200px`, narrow container `720px` for forms and long reading.
- Sections are separated by `120px` of plain ground (`80px` for tight sections). Never use horizontal rules between sections; the dot grid carries the separation.
- Card inner padding is `40px` (32px under 960px, 24px under 640px).
- Spacing steps on a 4px base: 4, 8, 12, 16, 24, 32, 40, 56, 80, 120. Don't invent in-between values.

**Grid behavior**

- Feature and integration cards sit in 2 or 3 columns. Three columns collapse to two under 960px and to one under 640px.
- Prefer **fewer, larger cards**. A row of three integration cards is the maximum density for weather-window cards; use channel rows (lists) when there are more than six items.
- Do not wrap everything in cards. Section headings, ledes, tables and forms sit directly on the ground or inside one card per section, never card-in-card.

**Section patterns**

- **Hero.** A single wide card, `display` or `headline-lg` heading and one lede sentence left-aligned at the top of the card body, primary and key buttons below. The card's top is a tall weather band (`fw-weather--hero`, 560px) with a wide app window (`fw-window--wide`) rising from its bottom edge. The heading lives in the white body below the sky, never over the sky image.
- **Integration grid.** `h2` plus one supporting sentence stacked vertically (never side by side), then a 3-column grid of weather-window cards: sky band, window mockup, channel label, title, one-sentence description, one key-cap button.
- **Channel list / settings.** One card with channel rows: icon tile, name + meta, live status in mono, toggle. Dividers are hairlines between rows only.
- **Audience table.** One flat card containing a table. Headers in Drizzle label-md, numerals in mono, row hover in `#FAFAF9`.
- **Conversation.** A card holding a thread of bubbles. Incoming white with hairline, outgoing Signal tint. Used to show actual product behavior instead of describing it.
- **Forms.** Narrow container, single column, labels above Mist-well inputs, primary action bottom-left.
- **Empty state.** A small weather window (240px band) with an empty window mockup, a title, one sentence, one primary button. No icon enlarged into an illustration.
- **Call to action.** Plain ground, centered `h2`, one sentence, primary + key buttons. This is the only centered text pattern in the system.

**Small screens**

Stack everything to one column. Weather bands shrink (240px cards, 340px hero) but the window still rises from the bottom edge and is still cut off. Nav links collapse; the logo lockup stays.

## Elevation & Depth

Depth is low and physical, like paper on a desk.

- **Cards:** 1px hairline plus the two-layer whisper shadow `0 1px 2px rgba(23,23,23,.04), 0 12px 32px -12px rgba(23,23,23,.10)`. Linked cards lift 2px on hover with a slightly deeper shadow.
- **App windows on the sky:** `0 -8px 24px rgba(0,0,0,.08)`, cast upward into the sky so the window reads as standing in front of the weather.
- **Key-cap button:** white fill, 1px strong border, 2px solid bottom shadow in `#D9D9D6`. On press it drops 1px and the shadow shrinks to 1px. This is the system's only tactile flourish.
- **Tabs:** the active tab is a white chip in a Mist well with a 1px bottom edge, a smaller echo of the key cap.
- **Never** use glassmorphism, backdrop blur, glows, colored shadows or inner shadows. Never stack more than two elevation levels on one screen (ground, card, window).

## Shapes

Everything is soft; nothing is sharp.

| Token | Value | Use |
|---|---|---|
| `card` | 32px | Cards (24px on mobile). |
| `window` | 24px | App-window mockups, **top corners only**. Windows never show a bottom edge. |
| `bubble` | 18px | Message bubbles, with the tail corner at 6px. |
| `md` | 14px | Buttons, inputs, tab wells, skeleton blocks. |
| `sm` | 8px | Focus outline radius, small tiles. |
| `full` | 999px | Skeleton bars, toggles, live dots only. |

- Buttons are **rounded rectangles at 14px**, never pills.
- The sky band has no radius of its own; the card's `overflow: hidden` clips it to the 32px top corners.
- Borders are always 1px. There are no 2px borders except the key-cap bottom edge and the window tab divider.

## Components

All classes live in `css/system.css` and are prefixed `fw-`. Icons are **Phosphor Icons, regular weight only**, loaded from `https://cdn.jsdelivr.net/npm/@phosphor-icons/web@2.1.2/src/regular/style.css`, used as `<i class="ph ph-chat-circle-text fw-icon"></i>` at 16–24px in `currentColor`. Useful glyphs: `ph-whatsapp-logo`, `ph-instagram-logo`, `ph-chat-circle-text`, `ph-paper-plane-tilt`, `ph-users-three`, `ph-megaphone`, `ph-magnifying-glass`, `ph-arrow-right`, `ph-check`.

**Ground** (`.fw-ground`): Overcast with the 8px dot grid. Apply to `body` or the page wrapper.

**Logo lockup** (`.fw-logo`): the four-circle mark inline (`fill="currentColor"`) at 26px plus "Fairweather" in Albert Sans 600. Ink in nav and footer, `.fw-logo--on-sky` for Paper on imagery, `.fw-logo--in-app` for the 20px Drizzle brand row inside window mockups. Never green.

**Buttons** (`.fw-btn`):
- `.fw-btn--primary`: Ink fill, Paper text. One per view, for the main commitment ("Start a campaign").
- `.fw-btn--key`: the white key cap. The default action on cards ("See how it works", "Connect").
- `.fw-btn--quiet`: text-only, Mist on hover, for row-level actions.
- Sizes: `--sm` (36px), default (48px), `--lg` (56px), `--icon` (40px square). Focus is the Zenith ring.

**Inputs** (`.fw-field`, `.fw-label`, `.fw-input`, `.fw-select`, `.fw-textarea`, `.fw-hint`): Mist well, no border at rest. On focus the well turns Paper with a 1px Ink border and a 3px Zenith ring. Errors use `aria-invalid="true"` for a red tint and border plus a `.fw-hint--error` message. `.fw-input-group` places a leading 16–20px icon.

**Checkbox** (`.fw-check`): 20px white key with 6px radius; checked fills Ink with a white tick. Never green, because checking a box is not a live state.

**Toggle** (`.fw-toggle`): 40×24 track. Off is Mist deep; on is Signal Green. Use toggles only for turning a channel or automation on, which is exactly why they are green.

**Tabs** (`.fw-tabs`, `.fw-tab[aria-selected]`): segmented control in a Mist well; the active tab is a white raised chip. Use `role="tablist"` and `role="tab"`.

**Card** (`.fw-card`, `__body`, `__title`, `__text`, `__actions`): 32px radius, hairline, whisper shadow. `--link` adds hover lift, `--flat` removes shadow for cards containing tables, `--compact` tightens padding to 24px.

**Weather window** (signature). Anatomy, top to bottom:

```html
<article class="fw-card">
  <div class="fw-sky fw-weather" style="--sky-image: url('../images/sky.png')">
    <div class="fw-window">
      <div class="fw-window__tab">
        <span class="fw-window__dot"></span><span class="fw-window__dot"></span><span class="fw-window__dot"></span>
      </div>
      <div class="fw-window__panel">
        <span class="fw-logo fw-logo--in-app"><!-- mark --> Fairweather</span>
        <div class="fw-skeleton-grid">
          <span class="fw-skeleton-bar"></span><span class="fw-skeleton-bar"></span>
          <span class="fw-skeleton-block"></span><span class="fw-skeleton-block"></span>
          <span class="fw-skeleton-block fw-skeleton-block--cut"></span><span class="fw-skeleton-block fw-skeleton-block--cut"></span>
        </div>
      </div>
    </div>
  </div>
  <div class="fw-card__body">
    <span class="fw-channel"><i class="ph ph-whatsapp-logo fw-icon"></i>WhatsApp integration</span>
    <h3 class="fw-card__title">Reach your WhatsApp audience in one place</h3>
    <p class="fw-card__text">Send personal replies and targeted campaigns to every contact.</p>
    <div class="fw-card__actions"><button class="fw-btn fw-btn--key">See how it works</button></div>
  </div>
</article>
```

- The sky image is set per instance through `--sky-image`; the stylesheet only defines the treatment (`cover`, `image-rendering: pixelated`, Zenith fallback).
- The window is two tiers: a narrower back tab (84% width) carrying the three chrome dots, and a wider front panel overlapping it. Both have 24px top corners and **no bottom edge**; the band crops them.
- The last skeleton row must be cut (`--cut`) so content visibly continues below the fold of the sky.
- The window may hold real UI instead of skeletons (a thread of bubbles, a channel row) in hero use. Skeletons are the default for grid cards.
- Band heights: 300px default, `--tall` 420px, `--hero` 560px.

**Channel label** (`.fw-channel`): Phosphor channel icon in Signal Green plus uppercase Fragment Mono text in `#12994A`. Only above card titles, only naming a real channel.

**Live status** (`.fw-live-dot`, `.fw-status--live`): 8px green dot with a tint halo plus mono text such as "Live · 1,284 contacts". `.fw-live-dot--off` and plain `.fw-status` for disconnected.

**Channel row** (`.fw-channel-list`, `.fw-channel-row`, `__icon`, `__name`, `__meta`): four-column grid of icon tile, name and meta, status, and toggle.

**Message bubbles** (`.fw-thread`, `.fw-bubble`, `.fw-bubble--out`, `.fw-bubble__meta`): incoming Paper with hairline and a 6px bottom-left tail corner; outgoing Signal tint with bottom-right tail.

**Table** (`.fw-table`, `.fw-num`): hairline rows, Drizzle headers, mono right-aligned numerals.

**Stat** (`.fw-stat`, `__value`, `__label`): a real number in headline-md weight 500 with a Drizzle label below. Only for real product figures.

**Skeleton** (`.fw-skeleton-bar`, `.fw-skeleton-block`, `.fw-skeleton--loading`): Mist bars and blocks; `--loading` adds a slow opacity breathe that respects reduced motion.

**Accessibility.** Ink on Paper is 17.9:1; Drizzle on Paper is 5.1:1. Signal text `#12994A` is used only for the 14px uppercase mono channel label and status, always paired with an icon or dot, never as the sole carrier of meaning. Every interactive element shows the Zenith focus ring. Sky bands are decorative: give their images empty `alt` or use the CSS custom property.

**Framework adaptation.** Map each `fw-` class to one component (`<Button variant="key">`, `<WeatherWindow sky={...}>`, `<ChannelLabel icon="whatsapp-logo">`). Keep the tokens as CSS custom properties; do not re-derive colors in component code.

## Do's and Don'ts

**Do**

- Do put a weather window at the top of the hero and every integration or feature card. It is the system's signature; reuse it rather than inventing other decorations.
- Do let the app window rise out of the bottom edge of the sky and get cut off by it. A window that floats fully inside the sky is wrong.
- Do render sky imagery with `image-rendering: pixelated` so the dither stays crisp.
- Do keep 90% of every screen in Overcast, Paper, Ink, Drizzle and Mist.
- Do show product behavior with real UI (bubbles, channel rows, a contact table) instead of icon-and-paragraph feature blocks.
- Do write copy like a real messaging product: campaigns, contacts, replies, channels, audiences. Plain sentences, sentence case.
- Do stack a section heading and its single supporting sentence vertically, left-aligned.
- Do use the key-cap button as the default card action and reserve the Ink primary button for the one main commitment per view.

**Don't**

- Don't use Signal Green for anything that isn't live: no green CTAs, headings, links, borders or logos.
- Don't place text over the sky image. Headings and copy live on Paper below the band.
- Don't build the sky with CSS gradients, and don't use photographs. Skies are dithered pixel art only.
- Don't pixelate anything outside the sky: no pixel fonts, no pixel borders, no stepped shadows.
- Don't use pill buttons, bold 700 weights, all-caps sans, or title case.
- Don't put the mono channel label above page or section headings, and don't invent channel names for decoration.
- Don't add decorative badges, numbered cards, invented timestamps, version tags or other metadata filler.
- Don't use glassmorphism, glows, colored shadows, or more than two elevation levels on one screen.
- Don't nest cards inside cards or wrap every paragraph in a container; the dot grid is enough separation.
- Don't enlarge an icon into an illustration. Icons stay at 16–24px; graphics are always weather windows.
- Don't use em dashes in product copy.
