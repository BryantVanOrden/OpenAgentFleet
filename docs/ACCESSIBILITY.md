# Accessibility — theme contrast audit (WCAG 2.1 AA)

Scope: the ten themes defined in `admin/src/index.css` (`data-mode` light/dark ×
`data-accent` amber/red/blue/purple/green).

Ratios below are computed, not estimated: sRGB → relative luminance per WCAG 2.1
(`L = 0.2126R + 0.7152G + 0.0722B` over the linearised channels), then
`(L_lighter + 0.05) / (L_darker + 0.05)`. The audit script resolves the CSS
cascade (specificity + document order) directly from `index.css`, so the numbers
reflect what the browser actually paints rather than what the palette comments
claim.

Thresholds applied: **4.5:1** for text (1.4.3), **3:1** for non-text UI
boundaries (1.4.11).

## Result

84 pairs checked across all ten themes. **6 failures before, 0 after.**

Worst offender: `--line` in light mode at **1.28:1** against the page surface —
about a quarter of the required 3:1.

## Failures found (before)

| Mode | Pair | Colours | Ratio | Need |
|---|---|---|---|---|
| light | `--line` on `--surface-0` | `#d5dce5` on `#f4f6f9` | **1.28** | 3.0 |
| light | `--line` on `--surface-1` | `#d5dce5` on `#ffffff` | **1.38** | 3.0 |
| dark | `--line` on `--surface-1` | `#2b3240` on `#0d0f14` | **1.49** | 3.0 |
| dark | `--line` on `--surface-0` | `#2b3240` on `#08090c` | **1.55** | 3.0 |
| light | `--text-dim` on `--surface-1` | `#97a2b2` on `#ffffff` | **2.58** | 4.5 |
| dark | `--text-dim` on `--surface-1` | `#5b687c` on `#0d0f14` | **3.39** | 4.5 |

Two notes on why these are real failures rather than pedantry:

- **`--line` is not decorative.** It is the `ring-1 ring-inset ring-ink-600` on
  every text input (`admin/src/components/ui.tsx:90`) and on the `subtle` button
  variant (`ui.tsx:51`). Those are control boundaries, which 1.4.11 covers. A
  1.3:1 input border in light mode is, in practice, an invisible input.
- **`--text-dim` is real text**, used 88 times across `admin/src` for hints,
  timestamps, and field labels — frequently at 11px (`text-[11px]`), which is
  the worst case, not the best. Light mode at 2.58:1 was the more serious of the
  two.

Everything else passed first time, including the pair most likely to fail: the
primary button (`bg-live-500 text-ink-950`, i.e. `--surface-0` on `--accent`)
clears 4.5:1 in **all ten** themes, the tightest being light/amber at 4.64:1.
The light-mode accents are deep enough to carry the near-white `--surface-0` as
a fill, and the dark-mode accents are bright enough to carry near-black.

## Changes made

Lightness-only moves. Hue and saturation are preserved on every token, dark
accents stay bright, light accents stay deep.

| Mode | Variable | Before | After | Why |
|---|---|---|---|---|
| dark | `--text-dim` | `#5b687c` | `#77859b` | 3.39 → 5.12 on `--surface-1` |
| dark | `--line` | `#2b3240` | `#55627e` | 1.49 → 3.14 on `--surface-1` |
| dark | `--line-strong` | `#3d4757` | `#677894` | keep it above the new `--line` |
| light | `--text-dim` | `#97a2b2` | `#606e82` | 2.58 → 5.19 on `--surface-1` |
| light | `--text-muted` | `#667487` | `#515c6b` | preserve the dim/muted step |
| light | `--line` | `#d5dce5` | `#768ca9` | 1.28 → 3.18 on `--surface-0` |
| light | `--line-strong` | `#b3becd` | `#617693` | keep it below the new `--line` |

Two of these were not themselves failing, and are consequences of the ones that
were:

- **`--line-strong` in both modes.** Raising `--line` past the old
  `--line-strong` would have inverted the scale — `ink-500` (34 usages) would
  have rendered *less* prominent than `ink-600`. Both were moved to keep the
  ordering the names promise.
- **`--text-muted` in light mode.** It passed at 4.76:1, but fixing `--text-dim`
  to 5.19:1 would have made the two indistinguishable. Muted moved to 6.79:1 so
  the dim → muted → soft → text ramp still reads as four distinct steps.

Text-dim and text-muted were solved against `--surface-3`, the lightest/darkest
surface they actually appear on (`bg-ink-850`, `bg-ink-800`), not just against
`--surface-1` — so they hold on every card and hover state, not only on the
common case.

## After

All 84 pairs pass. Tightest margins per category:

| Category | Tightest pair | Ratio | Need |
|---|---|---|---|
| Body text | `--text` on `--surface-0` (light) | 16.91 | 4.5 |
| Secondary text | `--text-muted` on `--surface-3` (light) | 6.00 | 4.5 |
| Hint text | `--text-dim` on `--surface-3` (light) | 4.58 | 4.5 |
| Status text | `--good` on `--surface-1` (light) | 5.02 | 4.5 |
| Accent as text | `--accent` on `--surface-0` (light/amber) | 4.64 | 4.5 |
| Primary button | `--surface-0` on `--accent` (light/amber) | 4.64 | 4.5 |
| Borders | `--line` on `--surface-0` (light) | 3.18 | 3.0 |

Light/amber is the tightest theme in the set at 4.64:1 — passing, but with the
least headroom. If the amber accent is ever nudged lighter, it fails first.

Build verified after the change: `vite build`, 59 modules, no errors.

## Reviewed, not changed

### `admin/src/components/ThemePicker.tsx`

Keyboard reachability is fine and `aria-expanded` is correct — it is a real
`<button>` with `aria-expanded={open}` bound to actual state
(`ThemePicker.tsx:41-46`), and Escape closes the popover (`:28`). Swatches are
labelled: each carries both `aria-label={a.label}` and `title` (`:91-92`), so
they are not unlabelled colour chips. Gaps, none severe enough to fix under this
audit's mandate:

- **Selected state is colour-only.** Neither the mode buttons (`:66-80`) nor the
  accent swatches (`:87-101`) expose selection to assistive tech — the current
  choice is conveyed purely by a ring and a background tint. `aria-pressed={mode
  === m.id}` on the mode buttons and `aria-pressed={accent === a.id}` on the
  swatches would fix it in two lines. This is the one worth doing.
- **Focus is not returned to the trigger** when the popover closes, and focus is
  not trapped inside it while open. Tabbing past the last swatch walks into the
  page behind the popover, which stays open.
- **The dismiss handler is `mousedown` only** (`:29`), so a keyboard user who
  tabs away leaves the popover open.
- `aria-haspopup` and `aria-controls` are absent; minor, given `aria-expanded`
  is present and correct.

### Focus ring

`:focus-visible { outline: 2px solid var(--accent); outline-offset: 2px; }`
(`index.css:222-225`) is visible against every surface in all ten themes — the
accent clears 3:1 on `--surface-0` everywhere, minimum 4.64:1 (light/amber).

One real weakness: **on an accent-filled control the ring is invisible.** A
focused primary button (`bg-live-500`) gets an accent-coloured outline sitting
2px outside an accent-coloured fill — roughly 1:1 against the thing it is meant
to delimit. It still contrasts with the surface beyond it, so it is not wholly
lost, but it reads as a fatter button rather than a focus indicator. Switching
the outline to `var(--text)` for accent-filled controls, or using a two-tone
ring (`outline` + contrasting `box-shadow`), would resolve it. Left unchanged:
it is a component-level decision, not a palette bug.

### Hard-coded colours

One offender, and it is defensible:

- `admin/src/lib/theme.ts:17-21` — the five accent entries hard-code both the
  dark and light hex for each accent (e.g. `{ id: "amber", dark: "#f5a524",
  light: "#b45309" }`).

These feed the inline `style={{ background: swatch(a) }}` on the picker swatches
(`ThemePicker.tsx:99`), which needs the literal value because a swatch must
preview an accent that is *not* currently applied — a CSS variable would only
ever yield the active one. So the duplication is structural, not sloppiness.
It is still a drift risk: these ten values silently shadow `index.css` and
nothing enforces agreement. No accent values were altered by this audit, so they
remain in sync today. If accents are ever retuned, both files must move
together.

No other hard-coded hex exists anywhere in `admin/src` outside `index.css`.

## Reproducing

The audit parses `index.css` directly rather than hard-coding the palette, so it
stays honest as the theme evolves. Re-derive with any WCAG relative-luminance
implementation over the resolved variable values for each of the ten
`data-mode` × `data-accent` combinations.
