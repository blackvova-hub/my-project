import { useQuery } from "@tanstack/react-query";

export type CoinInfo = {
  id: string;
  name: string;
  symbol: string;
  description: string;
  homepage?: string;
  image?: string;
  genesisDate?: string;
  hashingAlgorithm?: string;
  categories?: string[];
  countryOrigin?: string;
  github?: string;
  twitter?: string;
  subreddit?: string;
  telegram?: string;
  facebook?: string;
  discord?: string;
  contracts: Array<{ platform: string; address: string }>;
  market: {
    rank?: number;
    priceUsd?: number;
    marketCapUsd?: number;
    fullyDilutedValuationUsd?: number;
    volume24hUsd?: number;
    circulatingSupply?: number;
    totalSupply?: number;
    maxSupply?: number;
    athUsd?: number;
    atlUsd?: number;
    priceChange24h?: number;
  };
};

type CoinGeckoDetail = {
  description?: { ru?: string; en?: string };
  links?: {
    homepage?: string[];
    repos_url?: { github?: string[] };
    twitter_screen_name?: string;
    facebook_username?: string;
    telegram_channel_identifier?: string;
    subreddit_url?: string;
    chat_url?: string[];
  };
  image?: { small?: string };
  genesis_date?: string;
  hashing_algorithm?: string;
  categories?: string[];
  country_origin?: string;
  platforms?: Record<string, string>;
  market_cap_rank?: number;
  market_data?: {
    current_price?: { usd?: number };
    market_cap?: { usd?: number };
    fully_diluted_valuation?: { usd?: number };
    total_volume?: { usd?: number };
    circulating_supply?: number;
    total_supply?: number;
    max_supply?: number;
    ath?: { usd?: number };
    atl?: { usd?: number };
    price_change_percentage_24h?: number;
  };
};

function telegramURL(identifier?: string) {
  const value = identifier?.trim();
  if (!value) return undefined;
  if (/^https?:\/\//i.test(value)) return value;
  return `https://t.me/${value.replace(/^@/, "")}`;
}

export function useCoinInfo(base: string, enabled: boolean) {
  return useQuery({
    queryKey: ["coinInfo", base.toLowerCase()] as const,
    queryFn: async ({ signal }): Promise<CoinInfo> => {
      const searchResponse = await fetch(
        `https://api.coingecko.com/api/v3/search?query=${encodeURIComponent(base)}`,
        { signal },
      );
      if (!searchResponse.ok) throw new Error("coingecko_search_failed");
      const searchData = (await searchResponse.json()) as {
        coins?: Array<{ id: string; symbol: string; name: string }>;
      };
      const normalizedBase = base.toLowerCase();
      const coin =
        searchData.coins?.find((item) => item.symbol.toLowerCase() === normalizedBase) ??
        searchData.coins?.find((item) => item.name.toLowerCase().includes(normalizedBase)) ??
        searchData.coins?.[0];
      if (!coin) throw new Error("coin_not_found");

      const detailResponse = await fetch(
        `https://api.coingecko.com/api/v3/coins/${coin.id}?localization=false&tickers=false&market_data=true&community_data=true&developer_data=false&sparkline=false`,
        { signal },
      );
      if (!detailResponse.ok) throw new Error("coingecko_details_failed");
      const detail = (await detailResponse.json()) as CoinGeckoDetail;
      const descriptionRaw =
        (detail.description?.ru as string | undefined) ||
        (detail.description?.en as string | undefined) ||
        "";
      const twitterHandle = detail.links?.twitter_screen_name as string | undefined;
      const facebookHandle = detail.links?.facebook_username?.trim();
      const discord = detail.links?.chat_url?.find((url) => /discord\.(gg|com)/i.test(url));
      const contracts = Object.entries(detail.platforms ?? {}).flatMap(([platform, address]) => {
        const normalizedAddress = address.trim();
        return normalizedAddress ? [{ platform, address: normalizedAddress }] : [];
      });

      return {
        id: coin.id,
        name: coin.name,
        symbol: coin.symbol.toUpperCase(),
        description: descriptionRaw.replace(/<[^>]+>/g, "").trim(),
        homepage: Array.isArray(detail.links?.homepage)
          ? (detail.links.homepage as string[]).find(Boolean)
          : undefined,
        image: detail.image?.small as string | undefined,
        genesisDate: detail.genesis_date as string | undefined,
        hashingAlgorithm: detail.hashing_algorithm as string | undefined,
        categories: Array.isArray(detail.categories)
          ? (detail.categories as string[]).filter(Boolean)
          : undefined,
        countryOrigin: detail.country_origin as string | undefined,
        github: Array.isArray(detail.links?.repos_url?.github)
          ? (detail.links.repos_url.github as string[]).find(Boolean)
          : undefined,
        twitter: twitterHandle ? `https://twitter.com/${twitterHandle}` : undefined,
        subreddit: detail.links?.subreddit_url as string | undefined,
        telegram: telegramURL(detail.links?.telegram_channel_identifier),
        facebook: facebookHandle ? `https://www.facebook.com/${facebookHandle}` : undefined,
        discord,
        contracts,
        market: {
          rank: detail.market_cap_rank,
          priceUsd: detail.market_data?.current_price?.usd,
          marketCapUsd: detail.market_data?.market_cap?.usd,
          fullyDilutedValuationUsd: detail.market_data?.fully_diluted_valuation?.usd,
          volume24hUsd: detail.market_data?.total_volume?.usd,
          circulatingSupply: detail.market_data?.circulating_supply,
          totalSupply: detail.market_data?.total_supply,
          maxSupply: detail.market_data?.max_supply,
          athUsd: detail.market_data?.ath?.usd,
          atlUsd: detail.market_data?.atl?.usd,
          priceChange24h: detail.market_data?.price_change_percentage_24h,
        },
      };
    },
    enabled: enabled && Boolean(base),
    staleTime: 10 * 60_000,
    gcTime: 30 * 60_000,
    refetchOnWindowFocus: false,
    retry: 1,
  });
}
