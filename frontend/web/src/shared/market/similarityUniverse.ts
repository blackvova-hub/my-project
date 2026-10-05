// Explicit preset, not a live market-cap ranking. The server still filters
// these symbols by the selected market and availability of its asset catalog.
export const majorSymbols = ["BTCUSDT", "ETHUSDT", "SOLUSDT", "BNBUSDT", "XRPUSDT"];

const sectorLabels: Record<string, string> = {
  meme: "Мемкоинов",
  defi: "DeFi",
  ai: "ИИ / AI",
  gaming: "Игровых проектов / Gaming",
  l1: "Блокчейнов L1",
  l2: "Решений L2",
  rwa: "Реальных активов / RWA",
};

type SearchScope = { scope: string; sector?: string; symbols?: string[] };

export function universeFromRequest(request?: SearchScope): string {
  if (request?.scope === "sector" && request.sector) return `sector:${request.sector.toLowerCase()}`;
  if (request?.scope === "custom") {
    const symbols = new Set(request.symbols);
    if (symbols.size === majorSymbols.length && majorSymbols.every((symbol) => symbols.has(symbol))) return "majors";
    return "custom";
  }
  return request && ["same_asset", "all_crypto", "alts"].includes(request.scope) ? request.scope : "same_asset";
}

export function universeRequest(value: string, custom: string): SearchScope {
  if (value.startsWith("sector:")) return { scope: "sector", sector: value.slice(7) };
  if (value === "majors") return { scope: "custom", symbols: [...majorSymbols] };
  if (value === "custom") return { scope: "custom", symbols: custom.toUpperCase().split(/[\s,;]+/).filter(Boolean) };
  return { scope: value };
}

export function universeOptions(sectors: string[]) {
  const available = [...new Set(sectors.map((sector) => sector.toLowerCase()))];
  const ordered = [...Object.keys(sectorLabels).filter((key) => available.includes(key)), ...available.filter((key) => !(key in sectorLabels)).sort()];
  return [
    { value: "same_asset", label: "Этой монеты" },
    { value: "all_crypto", label: "Всех монет" },
    { value: "majors", label: "Основных монет" },
    { value: "alts", label: "Альткоинов" },
    ...ordered.map((sector) => ({ value: `sector:${sector}`, label: sectorLabels[sector] ?? sector.toUpperCase() })),
    { value: "custom", label: "Своего списка" },
  ];
}
