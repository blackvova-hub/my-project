export type MarketplaceType = "strategy" | "education" | "community";

export type MarketplaceItem = {
  id: string;
  type: MarketplaceType;
  title: string;
  author: string;
  category: string;
  price: number;
  currency: "USDT";
  rating: number;
  users: number;
  description: string;
  tags: string[];
  top?: boolean;
};

export const MARKETPLACE_CATEGORIES = [
  { id: "all", label: "Все" },
  { id: "strategy", label: "Стратегии" },
  { id: "education", label: "Обучение" },
  { id: "community", label: "Сообщество" },
];

export const MARKETPLACE_ITEMS: MarketplaceItem[] = [
  {
    id: "strat-1",
    type: "strategy",
    title: "Momentum Breakout 4H",
    author: "Vova Signals",
    category: "Фьючерсы",
    price: 79,
    currency: "USDT",
    rating: 4.8,
    users: 312,
    description: "Импульсная стратегия по пробоям с фильтром объёма и авто-менеджментом риска.",
    tags: ["Bybit", "4H", "Breakout"],
    top: true,
  },
  {
    id: "strat-2",
    type: "strategy",
    title: "Smart Liquidity Sweep",
    author: "Aleks Trade",
    category: "Скальпинг",
    price: 49,
    currency: "USDT",
    rating: 4.6,
    users: 180,
    description: "Охота за ликвидностью с точечными входами и строгими лимитами просадки.",
    tags: ["1H", "Liquidity"],
    top: true,
  },
  {
    id: "strat-3",
    type: "strategy",
    title: "Grid Hybrid",
    author: "Grid Masters",
    category: "Спот",
    price: 25,
    currency: "USDT",
    rating: 4.4,
    users: 420,
    description: "Гибридная сетка с динамическими уровнями и защитой от трендовых движений.",
    tags: ["Grid", "BTC"],
  },
  {
    id: "edu-1",
    type: "education",
    title: "Курс: Быстрый старт в деривативах",
    author: "Trader Lab",
    category: "Фьючерсы",
    price: 120,
    currency: "USDT",
    rating: 4.9,
    users: 540,
    description: "5 модулей: маржа, риск-менеджмент, стратегия, психология и практика.",
    tags: ["Video", "Practice"],
    top: true,
  },
  {
    id: "edu-2",
    type: "education",
    title: "Пакет: Индикаторы и сетапы",
    author: "Crypto Edu",
    category: "Тех-анализ",
    price: 60,
    currency: "USDT",
    rating: 4.5,
    users: 210,
    description: "Практические сетапы с примерами сделок и чек-листом входа.",
    tags: ["TA", "Indicators"],
    top: true,
  },
  {
    id: "edu-3",
    type: "education",
    title: "Обучение: Алготрейдинг для трейдера",
    author: "Algo House",
    category: "Алготрейдинг",
    price: 200,
    currency: "USDT",
    rating: 4.7,
    users: 98,
    description: "Основы автоматизации и тестирования идей на исторических данных.",
    tags: ["Backtest", "Bots"],
  },
  {
    id: "com-1",
    type: "community",
    title: "Private Swing Club",
    author: "Swing Room",
    category: "Закрытое сообщество",
    price: 39,
    currency: "USDT",
    rating: 4.6,
    users: 640,
    description: "Еженедельные обзоры рынка, live-разборы и общий watchlist.",
    tags: ["Weekly", "Signals"],
    top: true,
  },
  {
    id: "com-3",
    type: "community",
    title: "Pro Market Radar",
    author: "Radar Team",
    category: "Аналитика",
    price: 70,
    currency: "USDT",
    rating: 4.8,
    users: 260,
    description: "Утренние обзоры, идеи по альтам и контроль риска.",
    tags: ["Radar", "Analytics"],
  },
];

export const MARKETPLACE_LABELS: Record<MarketplaceType, string> = {
  strategy: "Стратегия",
  education: "Обучение",
  community: "Сообщество",
};

export function formatMarketplacePrice(price: number, currency: "USDT") {
  return `${price} ${currency}`;
}
