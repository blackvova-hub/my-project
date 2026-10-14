# Live News terminal

The terminal extends the existing Go API and `news_items` store. Register
`newsAPI.TerminalRoutes(r)` inside the existing `/api` router and run
`go newsAPI.TerminalRun(shutdownContext)` for scheduled refresh. `New(nil)` also
works for development: it reads the same real providers and retains a bounded
in-memory cache. There is no fixture/demo data path.

Apply migration `072_news_terminal.sql` before starting the integrated API.
Terminal collection sets `terminal_collected=true`, `publication_eligible=false`.
It cannot itself approve a Telegram publication. The existing collector may
claim a cached row only after its existing filtering and publication checks
approve the independently fetched item. Collector publication rules are preserved.

## HTTP contract

`GET /api/news/terminal` accepts:

- `category=all|crypto|macro|world|markets`
- `source=<source ID>` and `asset=<ticker>`
- `q=<up to 200 Unicode characters>`
- `importance=all|high|medium|low`
- `from`, `to`: RFC3339 timestamps or UTC `YYYY-MM-DD` dates. Timestamp `to` is
  exclusive; date-only `to` includes that entire day.
- `sort=latest|important`, `limit=1..100` (default 60), `cursor=<opaque cursor>`.

The response is `{items,sources,asOf,nextCursor,total,storage,syncing,truncated,
historyWindowDays}`. `asOf` is null until an actual provider synchronization
succeeds. `nextCursor` is null for the final page. `storage` is `postgres` or
`memory`. The history window is 30 days, matching the existing collector's
retention policy. SQL filters run before the bounded 10,001-row scan. If the
scan limit is reached, `truncated=true`; `total` describes the loaded, normalized
result rather than claiming an unbounded archive count.

Items include `id,title,url,source,sourceId,category,publishedAt,timestampBasis,
summary?,assets,tags,importance,importanceScore,importanceReason,breaking,
language,attribution`. The canonical URL produces a stable item ID. GDELT
`seendate` is indexing time, explicitly exposed as `timestampBasis=indexed`;
RSS/Atom publication dates use `published`. Missing or invalid publication dates
are skipped; ingestion time is never substituted for publication time.

`GET /api/news/terminal/sources` returns `{sources,asOf,syncing}`. Source health
includes `id,name,url,termsUrl,status,enabled,lastAttempt,lastSuccess,lastError,
itemCount,attribution,reason?,intervalSeconds`. Status is
`pending|ok|stale|error|disabled`. `itemCount` is the number of accepted recent
records in the last full provider response; it is retained on HTTP 304.

Cursors are snapshot-based, bound to all filtering/sorting fields and signed
with HMAC-SHA256. Set the server-only `NEWS_TERMINAL_CURSOR_SECRET` to a random
secret of at least 32 characters when using multiple API replicas. Without it,
an ephemeral per-process key is generated; a restart invalidates old cursors.
The frontend should clear an invalid/expired cursor and reload the first page.

## Sources and permissions checked on 2026-10-14

Only feed-provided headlines and excerpts are ingested. Excerpts are sanitized
and limited to 420 Unicode characters. Original article pages and images are
never scraped. Display the item's attribution and a direct external source
link in article details. An HTTP 200 is a point-in-time availability check,
not a service-level guarantee.

| Source | Default | Live check | Basis and limitations |
| --- | --- | --- | --- |
| [Ethereum Foundation](https://blog.ethereum.org/feed.xml) | Enabled | 200; 3 recent posts | [Foundation terms](https://ethereum.org/terms-of-use/) cover the blog and license non-code content under [CC BY 4.0](https://creativecommons.org/licenses/by/4.0/), unless separately marked. Attribution names the Foundation/available author, links the license, and states that the excerpt is shortened. |
| [Bitcoin Core](https://bitcoincore.org/en/rss.xml) | Enabled | 200; no posts in the recent 30-day window | Official website source/documentation uses the [MIT license](https://github.com/bitcoin-core/bitcoincore.org/blob/master/LICENSE). Attribution links that license. Do not imply endorsement or redistribute unrelated third-party materials. |
| [Federal Reserve](https://www.federalreserve.gov/feeds/press_all.xml) | Enabled | 200 | The [RSS guide](https://www.federalreserve.gov/feeds/feeds.htm) describes aggregation of links, headlines, and brief summaries; [disclaimer](https://www.federalreserve.gov/disclaimer.htm) distinguishes Board/public material from third-party copyright. |
| [ECB](https://www.ecb.europa.eu/rss/press.html) | Enabled | 200 | [Copyright policy](https://www.ecb.europa.eu/services/using-our-site/disclaimer/html/index.en.html) allows accurate reuse with source credit. State that the information is freely available from ECB. Named author papers are excluded from this news feed ingestion. Original pages open as external links. |
| [BLS](https://www.bls.gov/feed/bls_latest.rss) | Enabled | Go collector 200; initial PowerShell probe 403 | [Official RSS feeds](https://www.bls.gov/feed/) and [reuse guidance](https://www.bls.gov/bls/linksite.htm). Government press-release excerpts only; bot/geographic restrictions may produce HTTP 403 and are shown as source errors. |
| [SEC](https://www.sec.gov/news/pressreleases.rss) | Enabled | 200 | [SEC RSS guidance](https://www.sec.gov/about/rss-feeds) describes subscription and aggregation. Only Commission press releases are included, not privately copyrighted issuer filings. |
| [GDELT](https://www.gdeltproject.org/) | Enabled | Endpoint returned 429 during live checks | [Open database statement](https://www.gdeltproject.org/data.html) and [official DOC API documentation](https://blog.gdeltproject.org/gdelt-doc-2-0-api-debuts/). One broad financial/crypto/world query every 15 minutes; title/link/index timestamp metadata only, no article text or image. Reviewed restrictive publishers are excluded even from this metadata route. Rate-limit failures remain visible; no alternate fabricated data is substituted. |
| [CoinDesk](https://www.coindesk.com/terms) | Disabled | Not polled by terminal | Terms reserve public/commercial republication without prior written consent. |
| [Cointelegraph](https://cointelegraph.com/terms-and-privacy) | Disabled | Not polled by terminal | Terms prohibit unauthorized redistribution and independent automated content pipelines. |
| [Decrypt](https://decrypt.co/terms-of-service) | Disabled | Not polled by terminal | Terms restrict service use to noncommercial contexts; no public commercial syndication assumed. |
| [CryptoSlate](https://cryptoslate.com/terms-and-conditions/) | Disabled | Not polled by terminal | Terms prohibit unauthorized automated extraction, caching, and republication. |
| [BBC](https://downloads.bbc.co.uk/usingthebbc/bbc_terms_of_use_19September2022english.pdf) | Disabled | Not polled by terminal | Business RSS use requires publisher permission, potentially a licence fee. |
| [Bybit](https://www.bybit.com/common-static/compliance/legal/BYBIT/df1923006718fbba8ba70d7d762b9866.pdf) | Not added to terminal | Official announcement API 200 | API terms prohibit repackaging/resale and commercial exploitation. Existing collector adapters are unchanged; accessibility alone is not a redistribution licence. |

If the operator already has written publisher authorization,
`NEWS_TERMINAL_LICENSED_SOURCES=coindesk,cointelegraph,decrypt,cryptoslate,bbc`
enables only the named feed(s). This variable does not itself grant a licence.
The new terminal never silently enables publishers configured in the legacy
collector. Existing stored rows are restricted to enabled audited source IDs.

## Normalization, caching, and honest ranking

All provider times are normalized to UTC; common US feed timezone abbreviations
are resolved explicitly. The API honors ETag, Last-Modified, and Retry-After.
Provider requests have bounded timeouts/bodies; failures preserve cached items.
Health and conditional-request state persist in PostgreSQL. A shared advisory
lock coordinates insertion with the existing collector. URL tracking parameters
and case/punctuation in headlines are normalized; equal headlines across
sources within 24 hours collapse into one record. Memory/SQL history is bounded.

Ranking is rule-based, never a model's fabricated confidence: base 15 points,
15 for an official source, plus the strongest matched topic (up to 35 points).
High starts at 60; medium at 35. `importanceReason` exposes matched rules.
Full FOMC names, policy/discount-rate decisions, CPI/PCE, payrolls/NFP, GDP,
and their Russian equivalents are matched explicitly. Short abbreviations
use token boundaries (GDP is not GDPR). Dedicated official ECB policy-account
and Fed monetary-policy URLs can identify otherwise generic meeting titles;
the source name alone never makes a publication high importance. Mainnet
upgrade and explicit protocol security-alert themes add 20 points; testnet
announcement titles do not receive a mainnet-upgrade boost. Current ranking
rules are reapplied when reading persistent rows and the hot cache.
Themes and tickers use a small multilingual keyword dictionary and may require
human interpretation. `breaking=true` requires an explicit breaking marker in
the beginning of the original headline, a known publication timestamp, and age
at most ten minutes. GDELT indexing times cannot establish a breaking publication.
A recently fetched old item is never called breaking. No percentage market
sentiment is inferred from wording.

Run package tests with `GOWORK=off go test ./internal/httpapi/news` from
`backend/api`. Optional real-provider verification:
`NEWS_TERMINAL_LIVE_TEST=1 GOWORK=off go test ./internal/httpapi/news -run TestTerminalLiveSources -v -count=1`.
