import { useQuery } from "@tanstack/react-query";

type DexPair = {
  chainId?: string;
  dexId?: string;
  url?: string;
  pairAddress?: string;
  baseToken?: { address?: string; name?: string; symbol?: string };
  quoteToken?: { address?: string; name?: string; symbol?: string };
  priceUsd?: string | null;
  txns?: { h24?: { buys?: number; sells?: number } };
  volume?: { h24?: number };
  priceChange?: { h24?: number } | null;
  liquidity?: { usd?: number } | null;
  fdv?: number | null;
  marketCap?: number | null;
};

export type DexLiquidity = {
  pairCount: number;
  totalLiquidityUsd: number;
  volume24hUsd: number;
  buys24h: number;
  sells24h: number;
  priceUsd?: number;
  priceChange24h?: number;
  marketCapUsd?: number;
  fullyDilutedValuationUsd?: number;
  chain: string;
  dex: string;
  pairLabel: string;
  pairUrl?: string;
};

const COINGECKO_TO_DEX_CHAIN: Record<string, string> = {
  ethereum: "ethereum",
  "binance-smart-chain": "bsc",
  "polygon-pos": "polygon",
  "arbitrum-one": "arbitrum",
  "optimistic-ethereum": "optimism",
  base: "base",
  avalanche: "avalanche",
  solana: "solana",
  fantom: "fantom",
  cronos: "cronos",
  linea: "linea",
  "zksync-era": "zksync",
  sui: "sui",
  "the-open-network": "ton",
  tron: "tron",
  pulsechain: "pulsechain",
  mantle: "mantle",
  scroll: "scroll",
  celo: "celo",
};

function finiteNumber(value: unknown): number | undefined {
  const parsed = typeof value === "number" ? value : Number(value);
  return Number.isFinite(parsed) ? parsed : undefined;
}

async function fetchContractPairs(
  platform: string,
  address: string,
  signal: AbortSignal,
): Promise<DexPair[]> {
  const chain = COINGECKO_TO_DEX_CHAIN[platform];
  if (!chain) return [];
  const response = await fetch(
    `https://api.dexscreener.com/token-pairs/v1/${encodeURIComponent(chain)}/${encodeURIComponent(address)}`,
    { signal },
  );
  if (!response.ok) return [];
  const data = (await response.json()) as DexPair[];
  return Array.isArray(data) ? data : [];
}

async function searchPairs(symbol: string, name: string, signal: AbortSignal): Promise<DexPair[]> {
  const response = await fetch(
    `https://api.dexscreener.com/latest/dex/search?q=${encodeURIComponent(`${symbol} ${name}`.trim())}`,
    { signal },
  );
  if (!response.ok) return [];
  const data = (await response.json()) as { pairs?: DexPair[] };
  const normalizedSymbol = symbol.toUpperCase();
  const normalizedName = name.trim().toLowerCase();
  return (data.pairs ?? []).filter((pair) => {
    const symbols = [pair.baseToken?.symbol, pair.quoteToken?.symbol]
      .filter(Boolean)
      .map((value) => value!.toUpperCase());
    const names = [pair.baseToken?.name, pair.quoteToken?.name]
      .filter(Boolean)
      .map((value) => value!.trim().toLowerCase());
    return symbols.includes(normalizedSymbol) && (!normalizedName || names.includes(normalizedName));
  });
}

function summarizePairs(pairs: DexPair[]): DexLiquidity | null {
  const uniquePairs = new Map<string, DexPair>();
  for (const pair of pairs) {
    const key = `${pair.chainId ?? ""}:${pair.pairAddress ?? pair.url ?? ""}`;
    if (key !== ":") uniquePairs.set(key, pair);
  }
  const rows = [...uniquePairs.values()];
  if (rows.length === 0) return null;

  let totalLiquidityUsd = 0;
  let volume24hUsd = 0;
  let buys24h = 0;
  let sells24h = 0;
  let topPair = rows[0];
  let topLiquidity = -1;

  for (const pair of rows) {
    const liquidity = finiteNumber(pair.liquidity?.usd) ?? 0;
    totalLiquidityUsd += liquidity;
    volume24hUsd += finiteNumber(pair.volume?.h24) ?? 0;
    buys24h += finiteNumber(pair.txns?.h24?.buys) ?? 0;
    sells24h += finiteNumber(pair.txns?.h24?.sells) ?? 0;
    if (liquidity > topLiquidity) {
      topPair = pair;
      topLiquidity = liquidity;
    }
  }

  const base = topPair.baseToken?.symbol ?? "?";
  const quote = topPair.quoteToken?.symbol ?? "?";
  return {
    pairCount: rows.length,
    totalLiquidityUsd,
    volume24hUsd,
    buys24h,
    sells24h,
    priceUsd: finiteNumber(topPair.priceUsd),
    priceChange24h: finiteNumber(topPair.priceChange?.h24),
    marketCapUsd: finiteNumber(topPair.marketCap),
    fullyDilutedValuationUsd: finiteNumber(topPair.fdv),
    chain: topPair.chainId ?? "—",
    dex: topPair.dexId ?? "—",
    pairLabel: `${base}/${quote}`,
    pairUrl: topPair.url,
  };
}

export function useDexLiquidity(options: {
  symbol: string;
  name: string;
  contracts: Array<{ platform: string; address: string }>;
  enabled?: boolean;
}) {
  const { symbol, name, contracts, enabled = true } = options;
  const contractKey = contracts.map(({ platform, address }) => `${platform}:${address}`).join("|");

  return useQuery({
    queryKey: ["dexLiquidity", symbol.toUpperCase(), name, contractKey] as const,
    queryFn: async ({ signal }): Promise<DexLiquidity | null> => {
      const supportedContracts = contracts
        .filter(({ platform, address }) => Boolean(COINGECKO_TO_DEX_CHAIN[platform] && address))
        .slice(0, 6);
      const results = await Promise.allSettled(
        supportedContracts.map(({ platform, address }) => fetchContractPairs(platform, address, signal)),
      );
      const contractPairs = results.flatMap((result) =>
        result.status === "fulfilled" ? result.value : [],
      );
      const pairs = contractPairs.length > 0
        ? contractPairs
        : await searchPairs(symbol, name, signal);
      return summarizePairs(pairs);
    },
    enabled: enabled && Boolean(symbol),
    staleTime: 60_000,
    gcTime: 10 * 60_000,
    refetchOnWindowFocus: false,
    retry: 1,
  });
}
