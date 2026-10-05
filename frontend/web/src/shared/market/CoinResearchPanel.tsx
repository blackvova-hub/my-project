import { useId, useState, type ReactNode } from "react";
import { motion, useReducedMotion } from "framer-motion";
import type { CoinInfo } from "./coinInfoQueries";
import { useDexLiquidity } from "./dexScreenerQueries";
import { useCexOrderbookLiquidity, useDerivativesMarketData } from "./derivativesQueries";
import { useNewsItems } from "../news/newsQueries";
import type { PrimaryExchange } from "../exchange/primaryExchange";

const compactNumber = new Intl.NumberFormat("ru-RU", {
  notation: "compact",
  maximumFractionDigits: 2,
});
const regularNumber = new Intl.NumberFormat("ru-RU", { maximumFractionDigits: 4 });
const newsDate = new Intl.DateTimeFormat("ru-RU", {
  day: "2-digit",
  month: "short",
  hour: "2-digit",
  minute: "2-digit",
});

function formatUsd(value?: number) {
  if (value === undefined || !Number.isFinite(value)) return "—";
  if (Math.abs(value) >= 1_000) return `$${compactNumber.format(value)}`;
  if (value !== 0 && Math.abs(value) < 0.01) {
    return `$${value.toLocaleString("en-US", { maximumSignificantDigits: 6 })}`;
  }
  return `$${value.toLocaleString("en-US", { minimumFractionDigits: 2, maximumFractionDigits: 4 })}`;
}

function formatAmount(value?: number) {
  if (value === undefined || !Number.isFinite(value)) return "—";
  return Math.abs(value) >= 1_000 ? compactNumber.format(value) : regularNumber.format(value);
}

function formatPercent(value?: number | null) {
  if (value === undefined || value === null || !Number.isFinite(value)) return "—";
  return `${value > 0 ? "+" : ""}${value.toFixed(2)}%`;
}

function FactRow({ label, children }: { label: string; children: ReactNode }) {
  return (
    <div className="flex items-start justify-between gap-4">
      <span className="shrink-0 text-muted-foreground">{label}</span>
      <div className="min-w-0 text-right font-semibold text-foreground">{children}</div>
    </div>
  );
}

function Metric(props: {
  label: string;
  value: string;
  detail?: string;
  accent?: boolean;
  tone?: "positive" | "negative";
}) {
  const { label, value, detail, accent = false, tone } = props;
  const valueTone = tone === "negative" ? "text-destructive" : tone === "positive" || accent ? "text-primary" : "text-foreground";
  return (
    <div className="rounded-xl border border-border bg-background px-3 py-2.5">
      <div className="text-[10px] uppercase tracking-wide text-muted-foreground">{label}</div>
      <div className={`mt-1 truncate text-sm font-semibold ${valueTone}`} title={value}>
        {value}
      </div>
      {detail ? <div className="mt-1 truncate text-[10px] text-muted-foreground" title={detail}>{detail}</div> : null}
    </div>
  );
}

function changeTone(value?: number | null) {
  if (value === undefined || value === null || !Number.isFinite(value)) return undefined;
  return value >= 0 ? "positive" as const : "negative" as const;
}

function formatFunding(value?: number | null) {
  if (value === undefined || value === null || !Number.isFinite(value)) return "—";
  const percent = value * 100;
  return `${percent > 0 ? "+" : ""}${percent.toFixed(4)}%`;
}

function formatNextFunding(timestamp?: number) {
  if (!timestamp || !Number.isFinite(timestamp)) return "Время недоступно";
  return `Следующая: ${new Date(timestamp).toLocaleString("ru-RU", { day: "2-digit", month: "2-digit", hour: "2-digit", minute: "2-digit" })}`;
}

function InfoMetric(props: {
  label: string;
  value: string;
  ariaLabel: string;
  panelTitle: string;
  children: ReactNode;
  tone?: "positive" | "negative" | "neutral";
  align?: "left" | "right";
  wide?: boolean;
}) {
  const { label, value, ariaLabel, panelTitle, children, tone = "neutral", align = "left", wide = false } = props;
  const valueTone = tone === "positive" ? "text-primary" : tone === "negative" ? "text-destructive" : "text-foreground";
  return (
    <div className="relative rounded-xl border border-border bg-background px-3 py-2.5">
      <div className="flex items-center gap-1.5 text-[10px] uppercase tracking-wide text-muted-foreground">
        <span>{label}</span>
        <div className="group/info-tooltip relative">
          <button
            type="button"
            aria-label={ariaLabel}
            className="inline-flex h-4 w-4 items-center justify-center rounded-full border border-border-strong text-[9px] text-muted-foreground outline-none hover:border-border-strong hover:text-primary focus-visible:ring-2 focus-visible:ring-ring"
          >
            i
          </button>
          <div
            className={`pointer-events-none absolute bottom-5 z-40 hidden rounded-xl border border-border-strong bg-background p-3 normal-case shadow-2xl group-hover/info-tooltip:block group-focus-within/info-tooltip:block ${align === "right" ? "right-0" : "left-0"} ${wide ? "min-w-72" : "min-w-56"}`}
          >
            <div className="mb-2 text-[10px] uppercase tracking-wide text-primary">{panelTitle}</div>
            {children}
          </div>
        </div>
      </div>
      <div className={`mt-1 truncate text-sm font-semibold ${valueTone}`} title={value}>{value}</div>
    </div>
  );
}

function DetailRow(props: { label: string; value: string; tone?: "positive" | "negative" }) {
  const { label, value, tone } = props;
  const valueTone = tone === "positive" ? "text-primary" : tone === "negative" ? "text-destructive" : "text-foreground";
  return (
    <div className="flex items-center justify-between gap-5 text-xs">
      <span className="text-muted-foreground">{label}</span>
      <span className={`font-semibold ${valueTone}`}>{value}</span>
    </div>
  );
}

function LiquidationsMetric(props: { totalUsd: number; venues: Array<{ exchange: string; usd: number }> }) {
  const { totalUsd, venues } = props;
  return (
    <InfoMetric
      label="Ликвидации 24ч"
      value={formatUsd(totalUsd)}
      ariaLabel="Показать ликвидации по биржам"
      panelTitle="По биржам · 24 часа"
      tone="negative"
    >
      {venues.length > 0 ? (
        <div className="space-y-1.5">
          {venues.map((venue) => (
            <DetailRow key={venue.exchange} label={venue.exchange} value={formatUsd(venue.usd)} />
          ))}
        </div>
      ) : (
        <div className="text-xs text-muted-foreground">В кеше нет ликвидаций: $0.00</div>
      )}
    </InfoMetric>
  );
}

function OpenInterestMetric(props: {
  usd?: number;
  change1h?: number | null;
  change4h?: number | null;
  change24h?: number | null;
}) {
  const { usd, change1h, change4h, change24h } = props;
  return (
    <InfoMetric
      label="Открытый интерес"
      value={formatUsd(usd)}
      ariaLabel="Показать изменения открытого интереса"
      panelTitle="Изменение OI"
      tone="positive"
    >
      <div className="space-y-1.5">
        <DetailRow label="За 1 час" value={formatPercent(change1h)} tone={changeTone(change1h)} />
        <DetailRow label="За 4 часа" value={formatPercent(change4h)} tone={changeTone(change4h)} />
        <DetailRow label="За 24 часа" value={formatPercent(change24h)} tone={changeTone(change24h)} />
      </div>
    </InfoMetric>
  );
}

function CexLiquidityMetric(props: {
  exchange: PrimaryExchange;
  liquidity: ReturnType<typeof useCexOrderbookLiquidity>;
}) {
  const { exchange, liquidity } = props;
  return (
    <InfoMetric
      label="CEX ликвидность"
      value={formatUsd(liquidity?.within1.totalUsd)}
      ariaLabel="Показать детализацию CEX ликвидности"
      panelTitle={`Стакан ${exchange === "binance" ? "Binance" : "Bybit"}`}
      align="right"
      wide
    >
      {liquidity ? (
        <div className="space-y-2.5">
          <div>
            <DetailRow label="В диапазоне ±0,5%" value={formatUsd(liquidity.within05.totalUsd)} />
            <div className="mt-1 text-[10px] text-muted-foreground">
              Bids {formatUsd(liquidity.within05.bidsUsd)} · Asks {formatUsd(liquidity.within05.asksUsd)}
            </div>
          </div>
          <div className="border-t border-border pt-2">
            <DetailRow label="В диапазоне ±1%" value={formatUsd(liquidity.within1.totalUsd)} />
            <div className="mt-1 text-[10px] text-muted-foreground">
              Bids {formatUsd(liquidity.within1.bidsUsd)} · Asks {formatUsd(liquidity.within1.asksUsd)}
            </div>
          </div>
          <div className="space-y-1.5 border-t border-border pt-2">
            <DetailRow label="Spread" value={`${liquidity.spreadPct.toFixed(4)}% · ${formatUsd(liquidity.spreadUsd)}`} />
            <DetailRow label="Глубина стакана" value={`${liquidity.depth} уровней`} />
          </div>
        </div>
      ) : (
        <div className="text-xs text-muted-foreground">Стакан загружается…</div>
      )}
    </InfoMetric>
  );
}

function CoinOverview({ coinInfo, infoError }: { coinInfo: CoinInfo | null; infoError: string }) {
  const [expanded, setExpanded] = useState(false);
  const contentId = useId();
  const reduceMotion = useReducedMotion();
  const transition = { duration: reduceMotion ? 0 : 0.32, ease: [0.22, 1, 0.36, 1] as const };
  const socials = [
    { label: "X", url: coinInfo?.twitter },
    { label: "Telegram", url: coinInfo?.telegram },
    { label: "Reddit", url: coinInfo?.subreddit },
    { label: "Discord", url: coinInfo?.discord },
    { label: "Facebook", url: coinInfo?.facebook },
  ].filter((item): item is { label: string; url: string } => Boolean(item.url));
  return (
    <div>
      <div id={contentId} className="grid items-stretch gap-4 lg:grid-cols-2">
        <section className="h-full rounded-2xl border border-border-strong bg-accent p-4">
          <div className="text-xs uppercase tracking-wide text-primary">Описание</div>
          <motion.div
            initial={false}
            animate={{ height: expanded ? "auto" : 96 }}
            transition={transition}
            className="mt-3 overflow-hidden"
          >
          {infoError ? (
            <div className="text-xs text-destructive">{infoError}</div>
          ) : (
            <p className="whitespace-pre-line text-sm leading-6 text-foreground">
              {coinInfo?.description || "Описание на CoinGecko пока недоступно."}
            </p>
          )}
          </motion.div>
        </section>

        <section className="h-full rounded-2xl border border-border-strong bg-accent p-4">
          <div className="text-xs uppercase tracking-wide text-primary">Создатели и факты</div>
          <motion.div initial={false} animate={{ height: expanded ? "auto" : 96 }} transition={transition} className="mt-3 overflow-hidden">
          <div className="grid gap-3 text-xs text-foreground">
            <FactRow label="Официальный сайт">
              {coinInfo?.homepage ? (
                <a href={coinInfo.homepage} target="_blank" rel="noreferrer" className="text-primary hover:text-primary">
                  Перейти
                </a>
              ) : "—"}
            </FactRow>
            {coinInfo?.categories?.length ? (
              <FactRow label="Категории">{coinInfo.categories.slice(0, 4).join(", ")}</FactRow>
            ) : null}
            <motion.div initial={false} animate={{ opacity: expanded ? 1 : 0 }} transition={transition} inert={!expanded} aria-hidden={!expanded} className="grid gap-3">
            {coinInfo?.countryOrigin ? <FactRow label="Страна">{coinInfo.countryOrigin}</FactRow> : null}
            {coinInfo?.hashingAlgorithm ? (
              <FactRow label="Алгоритм">{coinInfo.hashingAlgorithm}</FactRow>
            ) : null}
            {coinInfo?.github ? (
              <FactRow label="GitHub">
                <a href={coinInfo.github} target="_blank" rel="noreferrer" className="text-primary hover:text-primary">
                  Репозиторий
                </a>
              </FactRow>
            ) : null}
            {socials.length > 0 ? (
              <div className="border-t border-border pt-3">
                <div className="mb-2 text-[10px] uppercase tracking-wide text-muted-foreground">Соцсети</div>
                <div className="flex flex-wrap gap-2">
                  {socials.map((social) => (
                    <a
                      key={social.label}
                      href={social.url}
                      target="_blank"
                      rel="noreferrer"
                      className="rounded-full border border-border-strong bg-accent px-3 py-1.5 font-semibold text-primary transition-colors hover:bg-accent"
                    >
                      {social.label}
                    </a>
                  ))}
                </div>
              </div>
            ) : null}
            </motion.div>
          </div>
          </motion.div>
        </section>
      </div>

      <button
        type="button"
        aria-expanded={expanded}
        aria-controls={contentId}
        aria-label={expanded ? "Свернуть описание и факты" : "Развернуть описание и факты"}
        onClick={() => setExpanded((value) => !value)}
        className="mt-2 inline-flex items-center gap-1.5 rounded-lg px-2 py-1.5 text-xs font-semibold text-primary outline-none transition-colors hover:bg-accent hover:text-primary focus-visible:ring-2 focus-visible:ring-ring"
      >
        {expanded ? "Свернуть" : "Развернуть"}
        <motion.svg initial={false} animate={{ rotate: expanded ? 180 : 0 }} transition={transition} aria-hidden="true" viewBox="0 0 20 20" fill="none" className="h-4 w-4">
          <path d="m5 7.5 5 5 5-5" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" strokeLinejoin="round" />
        </motion.svg>
      </button>
    </div>
  );
}

export function CoinResearchPanel(props: {
  base: string;
  symbol?: string;
  exchange?: PrimaryExchange;
  coinInfo: CoinInfo | null;
  infoError?: string;
}) {
  const { base, symbol = `${base}USDT`, exchange = "bybit", coinInfo, infoError = "" } = props;
  const dexQuery = useDexLiquidity({
    symbol: base,
    name: coinInfo?.name ?? "",
    contracts: coinInfo?.contracts ?? [],
    enabled: Boolean(base && coinInfo),
  });
  const newsQuery = useNewsItems({
    category: "crypto",
    limit: 6,
    tzOffset: new Date().getTimezoneOffset(),
    asset: base,
    searchName: coinInfo?.name ?? "",
    enabled: Boolean(base && coinInfo),
    refetchInterval: 60_000,
  });
  const derivativesQuery = useDerivativesMarketData(exchange, symbol, Boolean(symbol));
  const cexLiquidity = useCexOrderbookLiquidity(exchange, symbol, Boolean(symbol));

  const dex = dexQuery.data ?? null;
  const news = newsQuery.data ?? [];
  const market = coinInfo?.market;
  const derivatives = derivativesQuery.data;
  const oi = derivatives?.openInterest;
  const funding = derivatives?.funding;
  const liquidations = derivatives?.liquidations;

  return (
    <div className="mt-4 grid gap-4">
      <CoinOverview key={`${exchange}:${symbol}`} coinInfo={coinInfo} infoError={infoError} />

      <div className="grid items-stretch gap-4 xl:grid-cols-2">
        <section className="rounded-2xl border border-border bg-secondary p-4">
          <div className="flex items-center justify-between gap-3">
            <div className="text-xs uppercase tracking-wide text-muted-foreground">Рынок</div>
            <div className="text-[10px] text-muted-foreground">
              CoinGecko · {derivatives?.source ?? (exchange === "binance" ? "Binance" : "Bybit")} · Redis
            </div>
          </div>
          <div className="mt-3 grid grid-cols-2 gap-2 sm:grid-cols-4">
            <Metric label="Цена" value={formatUsd(market?.priceUsd)} />
            <Metric label="За 24 часа" value={formatPercent(market?.priceChange24h)} accent={(market?.priceChange24h ?? 0) >= 0} />
            <Metric label="Объём 24ч" value={formatUsd(market?.volume24hUsd)} />
            <Metric label="Ранг" value={market?.rank ? `#${market.rank}` : "—"} />
            <Metric label="В обращении" value={formatAmount(market?.circulatingSupply)} />
            <Metric label="Макс. предложение" value={formatAmount(market?.maxSupply ?? market?.totalSupply)} />
            <Metric label="Исторический максимум" value={formatUsd(market?.athUsd)} />
            <Metric label="Исторический минимум" value={formatUsd(market?.atlUsd)} />
            <OpenInterestMetric
              usd={oi?.usd}
              change1h={oi?.change1h}
              change4h={oi?.change4h}
              change24h={oi?.change24h}
            />
            <Metric label="Funding" value={formatFunding(funding?.rate)} detail={formatNextFunding(funding?.nextFundingTime)} tone={changeTone(funding?.rate)} />
            <LiquidationsMetric totalUsd={liquidations?.totalUsd ?? 0} venues={liquidations?.byExchange ?? []} />
            <CexLiquidityMetric exchange={exchange} liquidity={cexLiquidity} />
          </div>
        </section>

        <section className="rounded-2xl border border-border bg-secondary p-4">
          <div className="flex items-center justify-between gap-3">
            <div className="text-xs uppercase tracking-wide text-muted-foreground">DEX-ликвидность</div>
            <div className="text-[10px] text-muted-foreground">DexScreener</div>
          </div>
          {dexQuery.isPending ? (
            <div className="mt-3 text-xs text-muted-foreground">Загрузка пулов...</div>
          ) : dex ? (
            <>
              <div className="mt-3 grid grid-cols-2 gap-2 sm:grid-cols-4">
                <Metric label="Ликвидность" value={formatUsd(dex.totalLiquidityUsd)} accent />
                <Metric label="Объём 24ч" value={formatUsd(dex.volume24hUsd)} />
                <Metric label="Пулов" value={String(dex.pairCount)} />
                <Metric label="Сделки 24ч" value={`${dex.buys24h} / ${dex.sells24h}`} />
                <Metric label="Цена DEX" value={formatUsd(dex.priceUsd)} />
                <Metric label="Изменение 24ч" value={formatPercent(dex.priceChange24h)} />
                <Metric label="Капитализация" value={formatUsd(dex.marketCapUsd)} />
                <Metric label="FDV" value={formatUsd(dex.fullyDilutedValuationUsd)} />
              </div>
              <div className="mt-3 flex flex-wrap items-center gap-2 text-xs text-muted-foreground">
                <span>{dex.chain}</span>
                <span>•</span>
                <span>{dex.dex}</span>
                <span>•</span>
                {dex.pairUrl ? (
                  <a href={dex.pairUrl} target="_blank" rel="noreferrer" className="font-semibold text-primary hover:text-primary">
                    {dex.pairLabel}
                  </a>
                ) : <span>{dex.pairLabel}</span>}
              </div>
            </>
          ) : (
            <div className="mt-3 text-xs leading-5 text-muted-foreground">
              Подтверждённые DEX-пулы для этой монеты не найдены.
            </div>
          )}
        </section>
      </div>

      <section className="rounded-2xl border border-border bg-secondary p-4">
        <div className="flex items-center justify-between gap-3">
          <div className="text-xs uppercase tracking-wide text-muted-foreground">Последние новости</div>
          <div className="text-[10px] text-muted-foreground">RSS и официальные источники</div>
        </div>
        {newsQuery.isPending ? (
          <div className="mt-3 text-xs text-muted-foreground">Загрузка новостей...</div>
        ) : news.length > 0 ? (
          <div className="mt-3 grid gap-2 md:grid-cols-2 xl:grid-cols-3">
            {news.map((item) => (
              <a
                key={item.id}
                href={item.url}
                target="_blank"
                rel="noreferrer"
                className="group rounded-xl border border-border bg-card p-3 transition-colors hover:border-border-strong hover:bg-secondary"
              >
                <div className="flex items-center justify-between gap-3 text-[10px] text-muted-foreground">
                  <span className="truncate">{item.source || "Источник"}</span>
                  <time className="shrink-0" dateTime={item.publishedAt}>{newsDate.format(new Date(item.publishedAt))}</time>
                </div>
                <div className="mt-2 line-clamp-2 text-sm font-semibold leading-5 text-foreground group-hover:text-primary">
                  {item.title}
                </div>
                {item.summary ? (
                  <div className="mt-1 line-clamp-2 text-xs leading-5 text-muted-foreground">{item.summary}</div>
                ) : null}
              </a>
            ))}
          </div>
        ) : (
          <div className="mt-3 text-xs leading-5 text-muted-foreground">
            Свежих новостей, напрямую связанных с {base}, пока нет.
          </div>
        )}
      </section>
    </div>
  );
}
