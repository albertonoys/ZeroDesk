# macOS reference for ZeroDesk

Measured from real apps on macOS 26.5.1 (this Mac, 1x points) and screenshots the user supplied on 2026-11-17. HIG citations are added in the "HIG" section. macOS 27 deltas are listed separately.

## Window

| Item | Value |
|---|---|
| Traffic lights | 12 pt circles, centres 20 pt apart, first centre 20 pt from the left edge, vertically centred in the 52 pt top band. Grey when the window is inactive |
| Top band (toolbar area) | 52 pt. No toolbar background: content scrolls under it. Controls sit in glass capsules |
| Window corner | large, continuous; inner panels are concentric (radius = window radius − inset) |
| Title | 15 pt semibold, next to the navigation capsule (Finder) or in the sidebar header (Voice Memos "All Recordings"). Grey when inactive |

## Sidebar (Finder, System Settings, Music)

| Item | Value |
|---|---|
| Shape | floating panel inset 8 pt from the window's top, left and bottom; ~18 pt corner radius; no divider line |
| Fill | light: near-white grey (#f5f5f5–#f7f7f7 on white), dark: translucent glass |
| Traffic lights | live inside the sidebar's top band |
| Width | Finder 138 pt (compact), System Settings 214 pt, Music ~200 pt |
| Section header | 11 pt semibold, tertiary grey, aligned with the row icons |
| Row | 32 pt pitch, 13 pt regular label, 16–18 pt line glyph in secondary grey |
| Selected row | rounded rect (~10 pt radius) in a light grey fill; icon and label take the app's tint (Music: pink/red, Finder: accent). Never a solid accent bar |
| System Settings rows | 20 pt rounded-square colour tiles with a white glyph instead of line glyphs |
| Search | as a sidebar row (Music) or a 28 pt pill field at the top (System Settings) |
| Bottom | account row pinned at the bottom (Music) |

## Toolbar controls

- Grouped in glass capsules, 36 pt tall, fully rounded: back/forward pair; view switcher; share/favourite/delete; edit tools.
- Search: capsule field 36 pt, magnifier glyph + placeholder (Music: centred and wide; Voice Memos: trailing).
- Segmented scope control (Music "Library | Apple Music | iTunes Store") is itself a capsule; the selected segment is a darker inner capsule.

## Lists and tables

| Item | Value |
|---|---|
| Finder list row | 20 pt, alternating very light stripe, disclosure chevrons, 16 pt file icons |
| Column header | 11–12 pt secondary grey, no heavy rule |
| Voice Memos row | 56 pt: title 13 pt semibold, date 11 pt secondary, duration right-aligned; hairline separators inset 16 pt |
| Selection (focused) | grey rounded rect for sidebars/lists of documents; Finder table uses accent only when the table is key |

## Grouped content (System Settings)

- Inset cards: light grey fill on white, ~12 pt radius, rows ~44 pt with inset hairlines.
- Row: optional 32 pt icon, 13 pt label, 11 pt secondary description below, trailing push button.
- Push buttons: 24 pt tall, grey fill, ~6–8 pt radius, 13 pt label.
- Section title outside the card: 13 pt bold.

## Empty states

- Large line glyph (~100 pt) + title 22–26 pt bold in secondary grey, centred ("Searching Your Library", "No Recording Selected"). No body text.

## Music (user screenshot, macOS 26/27)

- Floating sidebar: Search, Home, Radio; Library (Recently Added, Artists, Albums, Songs); Store; Playlists; account row at the bottom.
- Selected sidebar row: light grey rounded rect, icon + label in Music pink.
- Top: wide centred search capsule; scope segmented capsule at the trailing edge.
- **Player is a floating glass capsule at the bottom of the content area**, ~52 pt tall, inset ~24 pt from the sides: shuffle, previous, play, next, repeat on the left; now-playing LCD in the centre (Apple logo when idle); lyrics, queue, AirPlay, volume on the right.

## Voice Memos (real app + user screenshots)

- Left column 320 pt, white, hairline on its right. Header: traffic lights, sidebar toggle in a 36 pt glass circle, "All Recordings" 15 pt semibold.
- Record button: solid red 40 pt circle floating at the bottom centre of the list over a white fade. No ring.
- Detail toolbar: capsule (share | favourite | delete) on the left, capsule (enhance | transcript | waveform) on the right, search capsule "Titles, Transcripts" trailing.
- Detail: title 13 pt semibold + "time · duration" 11 pt centred at the top; a full-width grey band holding a scrolling waveform with a vertical playhead (dots at both ends) in the middle; a time ruler under the band; an overview strip (whole recording, thin waveform, thick handle) below; big timer `00:11.90` ~28 pt bold; bottom-centre capsule (−15, play, +15).
- Colours: recording = red waveform/playhead; playback = black waveform, accent playhead.
- While recording the list disappears: the window shows only the recording view; bottom-left capsule with a red pause glyph (paused: red "RESUME" text, uppercase semibold; it only looks grey when the window is inactive), bottom-right "Done" pill; top-right capsule (Undo | enhance | transcript | trim).

## App icons (decision 2026-11-17: original artwork in macOS 27 grammar)

Observed from this Mac's system icons and Apple's macOS 27 notes:
- Full-bleed rounded-square tile, bold saturated background (Finder blue, Music red→pink, Terminal near-black, Settings graphite, Apps white, Voice Memos black).
- One clear foreground subject, frontal and flat, mostly white or high-contrast; subtle top highlight and a soft inner edge; no thin lines.
- macOS 27: sharper, more defined, bolder colours, extra Liquid Glass layers inside the artwork.
- We do not copy Apple's glyphs (Finder face, Music note shape, gear) — same grammar, our own subjects.
- Cardputer icon: the hardware drawing from commit c7868fa6 (user's choice).

## macOS 27 deltas vs 26

- Icons: sharper, bolder colours, more glass layers in the artwork (MacStories, Basic Apple Guy).
- Finder sidebar extends to the window edge; window corner radii unified (AppleInsider).
- Liquid Glass transparency slider in Settings (AppleInsider).

## HIG

Read 2026-11-17. Apple publishes no point values for window/control radii, toolbar height, sidebar width or row heights; those above are measured.

- Menu bar height 24 pt (HIG The menu bar).
- macOS text styles (HIG Typography): Large Title 26/32, Title 1 22/26, Title 2 17/22, Title 3 15/20, Headline 13 bold, Body 13/16, Callout 12/15, Subheadline 11/14, Footnote 10/13, Caption 10/13. Default 13 pt, minimum 10 pt.
- Hit targets: default control 28×28 pt, minimum 20×20 (HIG Accessibility).
- App icons: 1024 canvas, system mask, one background + foreground layers, no text, no thin lines, no replicas of Apple hardware; light / dark / clear / tinted appearances (HIG App icons; WWDC25 "Say hello to the new look of app icons").
- Liquid Glass belongs to controls and navigation, never to content; no glass on glass; Clear variant only over media with a 35% dimming layer (HIG Materials; WWDC25 "Meet Liquid Glass").
- Toolbar: leading (back, sidebar toggle, title ≤ 15 chars), centre (common controls), trailing (search, inspectors). Buttons share one piece of glass; segmented controls, pop-ups and search fields get their own (WWDC25 "Build an AppKit app with the new design").
- Sidebar: floating glass in 26; content flows under it; icons use the accent colour; at most two levels (HIG Sidebars).
- macOS 27 (WWDC26 "Modernize your AppKit app"): sidebar reaches the window edges, selected sidebar item is semibold, glass toolbar items above the sidebar, interactive glass bounces on click; all windows share one corner radius.
