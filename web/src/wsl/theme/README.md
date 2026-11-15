# Product Theme

The product uses an operational adaptation of **Lumenwave Finance**, Copyright 2026 Uiverse, matching the owner's referenced multi-tenant workspace. Original guidance, attribution and license are retained in `lumenwave/DESIGN.md` and `lumenwave/LICENSE.md`. Historical Fairweather and Voltline resources remain archived and are not imported.

`lumenwave/system.css` defines the single active palette and local font stack. `product.css` adapts it to Vue, Element Plus and existing product layouts. Both are imported once by the product entry point. Existing `--fw-*` names are compatibility aliases to this palette, not a second theme.

Use a light gray canvas, white working surfaces, ink actions and navigation, and a small lime signal. Violet is reserved for charts; success, warning and danger retain their meaning. Product controls use 6px radii, dialogs use 8px, and page sections use separators rather than floating cards. Local fonts and existing icons avoid external asset requests.

The 224px sidebar supports desktop collapse and collapsible navigation groups; mobile retains the existing drawer and workspace switching. Keep the independent assistant entry, branded mark, scoped editors, saved drafts and permission checks. Styling never submits a form or changes runtime activation.

The adaptation uses local Chinese and Latin fonts, existing Element Plus icons and platform logos. It makes no remote font or icon requests. Controls use compact 4-8 px corners, zero letter spacing and accessible accent text. The canvas is cool gray, work surfaces white, navigation neutral and active selections blue. Red, amber and green retain semantic business meanings and never imply readiness. Marketing heroes, sky bands, dot grids and large template cards are omitted from operational screens. Body text is 14px and controls 36-40px.

Login, navigation, tables, forms, dialogs, account dashboards, native capabilities, the AI gateway and assistant share the product tokens. Message threads keep their familiar two-pane layout. Global acceptance state lives in a compact header popover; unmeasured capacity targets remain labelled and collapsed. Neither presentation changes permissions, model activation nor acceptance gates.

The framework demo's global theme, routes and theme persistence are not loaded by the product entry point. Existing standalone chart components remain usable.
