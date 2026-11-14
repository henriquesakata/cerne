---
name: cerne
description: A warm-paper ledger for project state — ink, rules, and colour only where it carries meaning.
colors:
  paper: "#faf7f2"
  ink: "#1f2421"
  ink-muted: "#5c635e"
  ink-faint: "#8a8f8b"
  ink-dim: "#c4bfb4"
  leaf: "#ffffff"
  surface-sunk: "#f3efe7"
  surface-sunk-deep: "#ece9e2"
  rule: "#d8d2c6"
  rule-soft: "#e6e1d8"
  rule-card: "#e0dacf"
  rule-strong: "#b9b2a4"
  danger: "#aa3333"
  link: "#1d4f82"
  accent: "#3a5bbf"
  state-indev-bg: "#dbe9f7"
  approved-green: "#2c5e2e"
  approved-green-bg: "#dff0d8"
  approved-green-border: "#bcd4b4"
  implemented-violet: "#5b3d8a"
  implemented-violet-bg: "#ece4f5"
  warning-amber: "#b45309"
  warning-amber-bg: "#fdf0d5"
  notice-gold: "#c9a23a"
  notice-gold-bg: "#fbf7ec"
  state-terminal-bg: "#efece6"
  graph-teal: "#0d9488"
  graph-gray: "#9ca3af"
typography:
  hero:
    fontFamily: "system-ui, -apple-system, 'Segoe UI', sans-serif"
    fontSize: "2.2rem"
    fontWeight: 700
    lineHeight: 1.1
  display:
    fontFamily: "system-ui, -apple-system, 'Segoe UI', sans-serif"
    fontSize: "1.5rem"
    fontWeight: 700
    lineHeight: 1.2
  prose-heading:
    fontFamily: "system-ui, -apple-system, 'Segoe UI', sans-serif"
    fontSize: "1.25rem"
    fontWeight: 600
    lineHeight: 1.3
  prose-subheading:
    fontFamily: "system-ui, -apple-system, 'Segoe UI', sans-serif"
    fontSize: "1.1rem"
    fontWeight: 600
    lineHeight: 1.35
  title:
    fontFamily: "system-ui, -apple-system, 'Segoe UI', sans-serif"
    fontSize: "1rem"
    fontWeight: 600
    lineHeight: 1.4
  body:
    fontFamily: "system-ui, -apple-system, 'Segoe UI', sans-serif"
    fontSize: "0.95rem"
    fontWeight: 400
    lineHeight: 1.55
  prose-lead:
    fontFamily: "system-ui, -apple-system, 'Segoe UI', sans-serif"
    fontSize: "0.92rem"
    fontWeight: 400
    lineHeight: 1.55
  control:
    fontFamily: "system-ui, -apple-system, 'Segoe UI', sans-serif"
    fontSize: "0.9rem"
    fontWeight: 400
    lineHeight: 1.4
  label:
    fontFamily: "system-ui, -apple-system, 'Segoe UI', sans-serif"
    fontSize: "0.85rem"
    fontWeight: 400
    lineHeight: 1.4
    letterSpacing: "0.04em"
  mono:
    fontFamily: "ui-monospace, monospace"
    fontSize: "0.78rem"
    fontWeight: 400
    lineHeight: 1.5
  micro:
    fontFamily: "system-ui, -apple-system, 'Segoe UI', sans-serif"
    fontSize: "0.72rem"
    fontWeight: 600
    lineHeight: 1.5
rounded:
  md: "6px"
  lg: "8px"
  pill: "999px"
spacing:
  2xs: "0.25rem"
  xs: "0.4rem"
  sm: "0.5rem"
  md: "0.7rem"
  lg: "1rem"
  xl: "1.25rem"
  2xl: "1.5rem"
components:
  button-primary:
    backgroundColor: "{colors.ink}"
    textColor: "{colors.paper}"
    rounded: "{rounded.md}"
    padding: "0.45rem 1.1rem"
  card:
    backgroundColor: "{colors.leaf}"
    textColor: "{colors.ink}"
    rounded: "{rounded.md}"
    padding: "0.6rem 0.7rem"
  input:
    backgroundColor: "{colors.leaf}"
    textColor: "{colors.ink}"
    rounded: "{rounded.md}"
    padding: "0.45rem 0.6rem"
  nav-link:
    textColor: "{colors.ink-muted}"
    rounded: "{rounded.md}"
    padding: "0.3rem 0.7rem"
  nav-link-hover:
    backgroundColor: "{colors.surface-sunk-deep}"
    textColor: "{colors.ink-muted}"
    rounded: "{rounded.md}"
    padding: "0.3rem 0.7rem"
  nav-link-active:
    backgroundColor: "{colors.ink}"
    textColor: "{colors.paper}"
    rounded: "{rounded.md}"
    padding: "0.3rem 0.7rem"
  filter:
    backgroundColor: "{colors.leaf}"
    textColor: "{colors.ink-muted}"
    rounded: "{rounded.pill}"
    padding: "0.3rem 0.7rem"
  filter-active:
    backgroundColor: "{colors.ink}"
    textColor: "{colors.paper}"
    rounded: "{rounded.pill}"
    padding: "0.3rem 0.7rem"
  tab:
    textColor: "{colors.ink-muted}"
    padding: "0.45rem 0.8rem"
  tab-active:
    textColor: "{colors.ink}"
    padding: "0.45rem 0.8rem"
  badge:
    backgroundColor: "{colors.surface-sunk}"
    textColor: "{colors.ink-muted}"
    rounded: "{rounded.pill}"
    padding: "0.05rem 0.4rem"
  chip-candidate:
    backgroundColor: "{colors.surface-sunk-deep}"
    textColor: "{colors.ink-muted}"
    rounded: "{rounded.pill}"
    padding: "0.1rem 0.55rem"
  chip-approved:
    backgroundColor: "{colors.approved-green-bg}"
    textColor: "{colors.approved-green}"
    rounded: "{rounded.pill}"
    padding: "0.1rem 0.55rem"
  chip-in-development:
    backgroundColor: "{colors.state-indev-bg}"
    textColor: "{colors.link}"
    rounded: "{rounded.pill}"
    padding: "0.1rem 0.55rem"
  chip-implemented:
    backgroundColor: "{colors.implemented-violet-bg}"
    textColor: "{colors.implemented-violet}"
    rounded: "{rounded.pill}"
    padding: "0.1rem 0.55rem"
  chip-terminal:
    backgroundColor: "{colors.state-terminal-bg}"
    textColor: "{colors.ink-faint}"
    rounded: "{rounded.pill}"
    padding: "0.1rem 0.55rem"
---

# Design System: cerne

Scope: the cerne web UI (`ui/`, served by `cerne ui` at `localhost:7717`).
Product truth lives in [PRODUCT.md](PRODUCT.md); this file is strictly visual.

Frontmatter values are the **light theme**, which is normative. Every colour token
has a dark counterpart defined in `[data-theme='dark']`; the pairs are carried in
`.impeccable/design.json` under `extensions.colorMeta.*.darkValue`.

## Overview

**Creative North Star: "The Heartwood Ledger"**

cerne is a record of what a project actually is, kept the way a ledger is kept:
warm paper, one ink, ruled lines, and colour used only where the entry itself
demands it. The product's whole thesis is that project state is *data*, derived
and never authored — so the interface behaves like a page that was printed from
that data, not a canvas someone decorated afterward. Nothing here is present for
atmosphere. Every mark on the page is either a value, a boundary, or a state.

The base is deliberately warm — unbleached paper (#faf7f2), not the cool gray of
every dashboard template — and the ink is a near-black with a faint green cast
(#1f2421) rather than pure #000. That pairing is the whole identity: it reads as
something set down and kept, and it makes the semantic colours land as *marks on
a page* instead of as brand chrome. There is no brand accent, no gradient, no
logo colour. When a control is active, it does not tint — it fills with full ink
and inverts, the way a stamp presses.

Density is high and unapologetic: the primary user opens this daily to scan a
roster and clear gates, and a second, non-technical reader must still be able to
follow the rollups. The system serves both through typographic hierarchy and
tonal layering, never through decorative separation. Components are printed and
set: consistent corners, full ink when active, nothing halfway.

Confirmed anti-references — this system must never read as: a **generic SaaS
dashboard** (cool grays, purple-to-blue brand gradient, cards nested in cards,
rounded icon tiles above headings); a **terminal / hacker aesthetic** (monospace
everywhere, green-on-black, ASCII ornament — the mono here is semantic, not a
costume); or an **editorial / marketing document** (display serifs, hero
viewport, generous air — it fights the daily reading density).

**Key Characteristics:**

- Warm neutral base, never cool gray.
- One ink; no brand accent colour exists.
- Colour is reserved for lifecycle state and entity kind.
- Monospace marks machine truth; sans carries human prose.
- Flat by default; shadow means "detached from the page".
- Motionless: state changes are instant.
- System fonts only — the bundle ships inside a distributed binary.

## Colors

Warm neutrals carry the entire interface; the chromatic tokens are a state
vocabulary, not a palette. Neutrals are named for their material (paper, ink,
rule); state colours keep purely functional names, because to the reader they
*are* the data.

### Primary

- **Oak-Gall Ink** (`ink`): the single load-bearing colour. Body text, headings,
  every active or primary surface. Not pure black — the green cast keeps it
  seated on warm paper instead of punching a hole in it. This is also the
  "primary button" colour, the active nav background, and the active filter fill.
- **Unbleached Paper** (`paper`): the page. Also the *text* colour on any
  ink-filled control, which is what makes inversion the system's signature move.

### Secondary

- **Ledger Blue** (`link`): links, and the text of an in-development state chip.
  The only chromatic colour permitted in running prose.
- **Marginal Blue** (`accent`): epic relationships and facet affordances in the
  graph and roster. Structural, never decorative.

### Tertiary

The lifecycle state colours. Each one names a state in the data and may not be
reused for anything else:

- **Approved Green** (`approved-green` on `approved-green-bg`): approved, and the
  stronger human-accepted "done" state (F-0131), which additionally carries a
  1px `approved-green-border`.
- **Implemented Violet** (`implemented-violet` on `implemented-violet-bg`):
  implemented.
- **Warning Amber** (`warning-amber` on `warning-amber-bg`): lint findings and
  epic-behind-children warnings.
- **Notice Gold** (`notice-gold` on `notice-gold-bg`): advisory notices (the
  `ask` preamble).
- **Graph Teal** / **Graph Gray** (`graph-teal`, `graph-gray`): node kinds in the
  entity graph only.
- **Danger** (`danger`): destructive confirmation and form error text.

### Neutral

- **Fresh Leaf** (`leaf`): the raised surface — cards, inputs, table rows.
- **Foxed Surface** (`surface-sunk`) and **Foxed Deep** (`surface-sunk-deep`):
  recessed layers — the nav bar, badges, hover fills, candidate chips. These two
  plus `leaf` are the entire depth system in the document flow.
- **Ledger Rule** (`rule`), **Faint Rule** (`rule-soft`), **Card Rule**
  (`rule-card`), **Strong Rule** (`rule-strong`): the border ladder. `rule-card`
  is a card at rest; `rule-strong` is that same card on hover.
- **Faded Ink** (`ink-muted`), **Ghost Ink** (`ink-faint`), **Dim Ink**
  (`ink-dim`): the demotion ladder for metadata, counts, and empty-state text.

### Named Rules

**The Signal Rule.** Colour never decorates chrome. Every chromatic token in this
system names a lifecycle state, an entity kind, or a severity. If you cannot say
which datum a colour means, it does not belong on the screen. Audit test: point
at any coloured pixel and name the backend field it reflects.

**The Warm Base Rule.** Neutrals are warm. `paper` (#faf7f2) and its dark
counterpart (#191817) are unbleached, and the ink is green-cast, never #000.
Introducing a cool gray anywhere is a defect, not a variation.

**The Two-Block Rule.** No theme colour may exist outside `:root` and
`[data-theme='dark']` in `src/app.css`. A literal hex anywhere else in the
stylesheet or in a component is a bug — it will break dark mode silently.

**The No Brand Accent Rule.** There is no brand colour and none may be invented.
A control that wants emphasis takes full ink, not a hue.

**Full ink is for controls, not for prose.** Inversion works because a control is
small: a button, an active nav link, a selected filter. Applied to a block of
running text it stops reading as emphasis and becomes glare — in dark mode `ink`
is #e8e4da, so an inverted paragraph is a near-white panel. A block of prose that
needs to be set apart uses a recessed surface (`surface-sunk-deep`) and keeps its
text in ink; position and tone carry the distinction. Recorded 2026-08-08
(F-0241), after the chat's own message bubble was inverted and had to be undone.

A practical consequence: a control living inside a container whose buttons are
styled full ink must be **excluded at the source of that rule**, not patched
afterwards with higher specificity. The chat composer's own controls hit exactly
this — `.backlog-chat-form button` outranked their classes and repainted them
full ink, reintroducing the glare that had just been removed from the bubble.

## Typography

**Body Font:** system-ui (with -apple-system, 'Segoe UI', sans-serif)
**Label/Mono Font:** ui-monospace (with monospace)

**Character:** the native system face, used without apology. No display face, no
webfont, no font pairing in the usual sense — the only typographic contrast in
the system is sans versus mono, and that contrast is *semantic*. The result reads
like the operating system's own record of the project rather than a product with
a typographic personality, which is precisely the intent.

### Hierarchy

Eleven steps carry meaning. Eight are chrome; three (`prose-*`) are the internal
heading scale of rendered markdown, which is a separate context and legitimately
keeps its own hierarchy — a document's own `h2` must not be forced to equal the
page `h1`.

- **Hero** (700, 2.2rem): a single headline number on a dashboard card, at most
  one per screen. Not a heading — a measurement.
- **Display** (700, 1.5rem): the page title, one per screen (`h1`). The largest
  *heading* in the system, and still modest.
- **Prose-heading** (600, 1.25rem) / **Prose-subheading** (600, 1.1rem): headings
  *inside* rendered markdown (`.doc`), never chrome.
- **Title** (600, 1rem): panel and card headings, diagram toolbar labels.
- **Body** (400, 0.95rem, line-height 1.55): spec and result prose, list content,
  page-level form fields. The most-read size in the product.
- **Prose-lead** (400, 0.92rem): running text inside `.doc`, and entity titles.
- **Control** (400, 0.9rem): interactive chrome — nav links, tabs, ghost buttons.
- **Label** (400, 0.85rem, uppercase, letter-spacing 0.04em, `ink-muted`): section
  headings (`h2`) and stat labels. Deliberately *smaller* than body.
- **Mono** (400, 0.78rem): entity ids (F-0123, N-0007), the nav wordmark, event
  types, code, and diagram names. Also the state-chip size, at weight 600.
- **Micro** (600, 0.72rem): tags and badges — the smallest type that ships, and
  only for things that classify.

### Known debt: nine sizes to collapse

Recorded 2026-08-07 (F-0234). These values exist in the implementation but are
**not** part of the scale. They are drift, not steps: six of them live within
0.12rem of each other, a distinction no reader can perceive. Do not add new uses;
collapse the existing ones when you next touch the surface they live on.

| Value | Uses | Where | Collapse to |
|---|---|---|---|
| `0.65rem` | 2 | `.diagram-tag`, `.heatmap-months` | `micro` (0.72) |
| `0.68rem` | 4 | `.mnode-toggle`, `.notif-badge`, `.notif-chip-action` | `micro` (0.72) |
| `0.7rem` | 1 | `.clarify-count` | `micro` (0.72) |
| `0.75rem` | 7 | `.backlog-chat-options`, `.card-superseded-by` | `mono` (0.78) |
| `0.8rem` | 13 | `.backlinks-type`, `.conversation-list-new`, `.notif-toggle` | `label` (0.85) |
| `0.82rem` | 11 | `.action-form`, `.dash-foot`, `.mermaid-error` | `label` (0.85) |
| `0.88rem` | 8 | `.action-form`, `.actions`, `.artifacts` | `control` (0.9) |
| `1.05rem` | 1 | `.backlog-chat-toolbar h2` | `title` (1) |
| `3rem` | 1 | `.stats-pct` | `hero` (2.2) |

The design hook will keep reporting these, and that is correct — they are debt,
not false positives. Suppressing them would erase the only record that the
consolidation is still owed.

### Named Rules

**The Two Voices Rule.** Sans is the human voice; mono is the machine's. Entity
ids, event types, and code are always mono. Prose is never mono. The wordmark is
mono because the product's name is also an identifier. Getting this backwards
turns a semantic distinction into decoration.

**The Quiet Heading Rule.** `h2` is smaller than body text, uppercase, and muted.
Section headings label a region; they do not compete with its contents. Never
scale a heading up to create hierarchy — demote what surrounds it instead.

**The No New Step Rule.** The eleven steps above are a closed set. A size that is
not one of them is drift, however small the gap looks in isolation: 0.82rem next
to 0.85rem is not a decision, it is an accident that outlived its author. Adding
a step means editing this document first, with the reason.

## Layout

A single centred column: `.page` is `max-width: 70rem`, `margin: 0 auto`, with
`1.5rem` padding — the same padding used by the kanban board and the nav bar, so
the horizontal rhythm is continuous across every route. Narrow utility screens
(status, error) use `640px`; popovers and menus use `24rem`–`30rem`.

`#root` is a full-viewport flex column (`min-height: 100dvh`); ordinary pages flow
as blocks, while the graph route stretches (`flex: 1`) to claim the remaining
height. The nav is `position: sticky; top: 0` at `z-index: 10` and is the only
sticky element.

Spacing is a small, tight scale: `0.25rem` for chip and tab gaps, `0.4rem` for
inline metadata, `0.5rem`–`0.7rem` inside cards, `1rem` for section gaps,
`1.25rem`–`1.5rem` between major regions. Density is intentional; do not
introduce a larger scale step to "give things room".

Responsive behaviour is deliberately sparse — two breakpoints in the entire
system: `60rem` and `720px`. The pattern at both is the same: multi-column grids
(`minmax(0, 1fr) 18rem` for the feature page, `minmax(0, 320px) minmax(0, 1fr)`
for split views) collapse to `1fr`. There is no mobile-specific navigation and
none is needed — this is a desktop tool run on the operator's own machine.

**The Collapse-Don't-Reflow Rule.** Responsive adaptation means a grid becomes one
column. It does not mean new components, hidden content, or a different
information hierarchy at small widths.

## Elevation & Depth

The system is flat in the document flow. Depth comes from **tonal layering plus a
1px rule**: `leaf` raised on `paper`, `surface-sunk` and `surface-sunk-deep`
recessed beneath it, each boundary drawn by the appropriate rule token. A card at
rest has no shadow — it has a `rule-card` border, and on hover that border
darkens to `rule-strong`. Subtle hover elevation is permitted where it earns its
place, but the border shift is the cheaper and quieter default and should be
tried first.

Shadows exist for exactly one purpose: to say *this element has left the page*.
Four shadows exist in the entire stylesheet, all on floating overlays, plus one
token for graph nodes.

### Shadow Vocabulary

- **Overflow menu** (`box-shadow: 0 6px 24px rgba(0, 0, 0, 0.18)`): the nav "Mais"
  menu.
- **Dropdown** (`box-shadow: 0 4px 16px rgba(0, 0, 0, 0.18)`): the notification
  panel.
- **Popover** (`box-shadow: 0 8px 24px rgba(0, 0, 0, 0.25)`): the clarify popover,
  anchored to a marker in the document.
- **Command palette** (`box-shadow: 0 10px 30px rgba(31, 36, 33, 0.18)`): the
  modal palette; the only shadow tinted with ink rather than neutral black.
- **Graph node** (`--gnode-shadow`, `0 1px 3px rgba(31, 36, 33, 0.1)` light /
  `0 1px 4px rgba(0, 0, 0, 0.4)` dark): the sole tokenised shadow, because nodes
  sit on a canvas rather than on the page.

### Named Rules

**The Detached Shadow Rule.** A shadow means the element is out of the document
flow and dismisses on outside click. Never use a shadow for emphasis, for a card
at rest, or to separate two things a rule could separate.

**The Still Frame Rule.** Motion is permitted only where it represents work that
is *actually in progress*. State changes are instant, because the underlying data
changed instantly — an animated state chip implies a process that is not
happening. This composes with hover elevation: permitted hover states appear
*immediately*, without an eased transition.

The whole stylesheet contains exactly **one** motion declaration, and it earns
the rule rather than breaking it: `.backlog-chat-spinner`
(`animation: backlog-chat-spin 0.8s linear infinite`, F-0230), which spins while
an agent request is genuinely in flight. Audit test for any new motion: name the
process it depicts, and show that the process is running. If you cannot, the
motion is decoration.

Beyond that, motion belongs only inside the visualization surfaces that own their
own runtime (React Flow, Mermaid, Excalidraw).

*Corrigido em 2026-08-08:* esta regra afirmava que o sistema não tinha nenhuma
declaração de movimento. Era verdade quando medida, e deixou de ser quando o
F-0230 entrou por merge. O documento estava errado, não o spinner.

## Shapes

One corner language, applied consistently: **6px** is the default radius and
covers cards, inputs, buttons, nav links, and panels (38 of 69 radius
declarations). **8px** is the larger step, used for panels and grouped
containers. **999px** is reserved — and this is the meaningful part — for
elements that *classify or count*: state chips, tags, filter pills, badges. A
pill in this system always means "this is a category or a state", never "this is
a button I made round".

Container borders are `1px solid`. The token is a rule token by default; a
container whose *whole* border carries a semantic colour is allowed, because the
colour then names what the container is rather than decorating one edge of it.

- `.errata-section` → `1px solid` danger. `.backlinks-section` → `1px solid`
  teal. `.ask-notice` → `1px solid` gold.
- The one exception on width is the active tab underline (`2px solid` ink) — a
  marker, not a container edge.
- `.doc blockquote` keeps a `3px` left rule in the **neutral** `--border` token.
  It is a typographic device on running text, not a card, and it carries no
  colour; it is not the side-tab pattern.

**Resolved 2026-08-07 (F-0234).** These three previously carried a `3px` coloured
*left* border — the accent side-tab, which broke both the 1px doctrine and the
ban on decorative colour. Collapsing the whole border to 1px in the semantic
colour keeps the meaning and drops the tell.

No clipping, no non-rectangular silhouettes, no ornamental dividers.

**The Pill Means Category Rule.** Full-round (999px) is a semantic signal, not a
style choice. If the element does not classify data, it takes 6px.

## Components

### Buttons

- **Shape:** 6px radius (`rounded.md`), `1px solid` border matching the fill.
- **Primary:** full ink — `background: ink`, `color: paper`, `border: 1px solid
  ink`, padding `0.45rem 1.1rem`. This is the *only* filled button style; there
  is no coloured variant.
- **Disabled:** `opacity: 0.5` with `cursor: default`. Never a separate grey fill.
- **Hover / Focus:** no transition. Where a hover treatment exists it is a
  discrete tonal or border change applied instantly. Focus-visible is a `2px
  solid` ink outline with `1px` offset.
- **Ghost:** `border: none; background: transparent; font: inherit; cursor:
  pointer` — used for icon and overflow triggers. `font: inherit` is mandatory on
  every `<button>`; the system never lets a control fall back to the UA font.

### Chips

State chips are the product's most characteristic component: `999px` radius,
`0.78rem`, weight 600, padding `0.1rem 0.55rem`, each carrying a fixed
background/foreground pair from the state palette.

- **candidate:** `surface-sunk-deep` / `ink-muted` — colourless on purpose; a
  candidate has no commitment yet.
- **approved:** `approved-green-bg` / `approved-green`.
- **in_development:** `state-indev-bg` / `link`.
- **implemented:** `implemented-violet-bg` / `implemented-violet`.
- **done:** the approved-green pair plus a `1px solid approved-green-border` — the
  only chip with a border, because human acceptance is the strongest signal in
  the cycle.
- **rejected / superseded / obsolete:** `state-terminal-bg` / `ink-faint`, with
  `text-decoration: line-through`.

**The Struck Record Rule.** Terminal states are struck through, never hidden and
never greyed into invisibility. The product's thesis is that history stays
readable; the strikethrough is that thesis rendered.

### Cards / Containers

- **Corner Style:** 6px.
- **Background:** `leaf` on the page's `paper`.
- **Border:** `1px solid rule-card`, darkening to `rule-strong` on hover.
- **Shadow Strategy:** none — see Elevation & Depth.
- **Internal Padding:** `0.6rem 0.7rem`. Cards are links (`display: block;
  text-decoration: none; color: inherit`), so the whole card is the hit target.

### Inputs / Fields

- **Style:** `1px solid rule`, 6px radius, `leaf` background, `font: inherit` with
  an explicit size (`0.88rem` in inline action forms, `0.95rem` in page-level
  forms). Textareas add `resize: vertical`.
- **Field edge (`--border-field`, F-0413):** `rule` measures 1.41:1 (light) and 1.77:1
  (dark) against the page, below the 3:1 a control's boundary needs (WCAG 1.4.11).
  `--border-field` aliases `ink-faint` (3.08:1 light, 4.46:1 dark) and is the border
  of a field whose edge is the only cue that it is editable. Today only the Settings
  screen's fields use it; adopting it app-wide is a follow-up.
- **Focus:** `2px solid` ink outline, `1px` offset. No glow, no colour shift.
- **Error:** message text in `danger`; the field itself is not recoloured.

### Navigation

Sticky top bar on `surface-sunk` with a `1px solid rule-soft` bottom edge,
`0.6rem 1.5rem` padding, `z-index: 10`. The wordmark is mono, weight 700.

- **Default link:** `ink-muted`, 0.9rem, 6px radius, `0.3rem 0.7rem` padding.
- **Hover:** `surface-sunk-deep` fill.
- **Active:** full inversion — `background: ink; color: paper`. The active route
  is the darkest element on the screen.
- **Overflow:** links that do not fit collapse into a "Mais" menu — the one place
  in the nav where a shadow appears.

### Filter Pills

`999px` radius, `leaf` background, `1px solid rule`, `ink-muted` text,
`0.3rem 0.7rem` padding. Hover fills with `surface-sunk`; active inverts to full
ink with a matching ink border, and its embedded count demotes to `dim-inverse`
so the number stays legible against the inverted field.

### Tabs

Borderless, transparent, `0.45rem 0.8rem`, `ink-muted`, with a `2px solid
transparent` bottom edge. Active promotes the text to ink and colours that bottom
edge ink. The tab strip sits on a `1px solid rule-soft` baseline.

## Do's and Don'ts

### Do:

- **Do** define every theme colour in `:root` and `[data-theme='dark']` in
  `src/app.css`, and reference it as `var(--token)` everywhere else.
- **Do** invert to full ink (`background: ink; color: paper`) for the active or
  primary state of any control.
- **Do** use `ui-monospace` for entity ids, event types, code, and the wordmark —
  and only for those.
- **Do** use a `999px` radius only for elements that classify or count; use 6px
  for everything else.
- **Do** express depth with the `leaf` / `surface-sunk` / `surface-sunk-deep`
  ladder plus a 1px rule token.
- **Do** strike through terminal lifecycle states rather than hiding them.
- **Do** set `font: inherit` on every `<button>`, `<input>`, `<select>`, and
  `<textarea>`.
- **Do** let pt-BR labels run longer than their English equivalents — wrap and
  `flex-wrap` rather than fixing widths to fit a shorter string.
- **Do** collapse multi-column grids to `1fr` at `60rem` / `720px` and change
  nothing else.

### Don't:

- **Don't** introduce a brand accent colour, a gradient, or a coloured primary
  button. Full ink is the emphasis mechanism.
- **Don't** write a literal colour value outside the two theme blocks.
- **Don't** add `transition`, `animation`, or `@keyframes` to chrome.
- **Don't** put a shadow on anything that stays in the document flow.
- **Don't** load a webfont or add an icon font. The bundle ships inside the
  distributed Go binary (`go:embed ui/dist`); system faces and inline SVG only.
- **Don't** use a cool gray anywhere — the neutral ladder is warm by decision.
- **Don't** reuse a lifecycle state colour for a non-state purpose, or invent a
  new hue without a backend field behind it.
- **Don't** scale headings up to build hierarchy; `h2` is smaller than body and
  stays that way.
- **Don't** add avatars, presence indicators, or collaboration chrome — the
  product is local and single-operator.
- **Don't** nest a card inside a card, or place a rounded icon tile above a
  heading. Both are the generic-dashboard tell this system rejects.
