# Header and palette correction — 2026-09-22

User reference: the authenticated [home page](home-reference.png). This supersedes the custom header and palette in the earlier [terminal concept](../rework/concept.png); its chart/replay/trading composition remains. Focused correction within the site's existing design system, so no new image concept was generated.

Latest deployed [desktop render](replay-1586.png) and [mobile render](replay-390.png). Browser/IAB unavailable; Playwright with Chromium-based Edge captured and checked 1586×992 (the earlier concept's native size), 1024×992 and 390×992. `view_image` inspected the home reference and both latest renders together.

| Comparison | Evidence and correction |
| --- | --- |
| Header container | Removed all replay-specific Header overrides. Same shared Header, 1152px maximum inner width, 71px height, 16px padding. |
| Typography | Exact computed font/line-height matches home: 18px brand, 16px nav, 14px account button. |
| Navigation and account | Original spacing, link treatment, rounded outlined account button; removed turquoise active underline. |
| Surfaces | Toolbar/dock/dialog use MiniScanners #2a3b2b; chart uses home table #18231c; site's layout gradient restored. |
| Accents / borders | Emerald translucent actions, light emerald selected text, white/10 separators; buy/sell retain green/red semantics with softer fills. |
| Responsive layout | Standard mobile header, usable wrapped replay controls and trade fields, no horizontal document overflow. |
| Copy and composition | No visible copy added or renamed. Existing terminal layout, tools, chart and trading retained. |

Header geometry, fonts, padding, backgrounds and account styling were compared programmatically with the home page in the same session at all three widths: identical. Hidden closed menu contents were excluded from geometry comparison. [Results](checks.json), [computed styles](computed-styles.json).

Production build and frontend lint passed. Server rebuilt frontend successfully. Mobile Play/Pause, buy and close completed without runtime errors. No backend or trading calculation changes.

Intentional differences from the home page: the chart workspace retains its previously accepted terminal layout and static header positioning. No header styling differences remain. Faithfully verified against the user's latest home-page reference; no material visual mismatch remains within this correction's scope.
