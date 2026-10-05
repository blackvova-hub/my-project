import type { HttpError } from "../../shared/api/http";

// API identifiers remain unchanged; only their presentation is localized.
const labels: Record<string, string> = {
  "Main account": "Основной счёт",
  "Futures account": "Фьючерсный счёт",
  "7D": "7 дней",
  "30D": "30 дней",
  "90D": "90 дней",
  YTD: "С начала года",
  "1Y": "1 год",
  ALL: "Всё время",
  LONG: "Лонг",
  SHORT: "Шорт",
  BUY: "Покупка",
  SELL: "Продажа",
  Entry: "Вход",
  Add: "Увеличение",
  "Partial close": "Частичное закрытие",
  Exit: "Выход",
  bybit: "Bybit",
  binance: "Binance",
  Other: "Прочее",
  "1–3x": "1–3×",
  "3–5x": "3–5×",
  "5–10x": "5–10×",
  "10x+": "От 10×",
  deterministic: "Пояснение по правилам",
  "deterministic · daily AI limit": "Пояснение по правилам · дневной лимит ИИ",
  "deterministic · AI unavailable": "Пояснение по правилам · ИИ недоступен",
  "AI · cached": "Пояснение ИИ · сохранённый ответ",
  "AI explanation": "Пояснение ИИ",
};
export const displayLabel = (value: string | undefined) =>
  value ? (labels[value] ?? value) : "—";

const messages: Record<string, string> = {
  "Sign in to access analytics.": "Войдите в аккаунт, чтобы открыть аналитику.",
  "Cross-site request rejected.":
    "Запрос с другого сайта отклонён. Обновите страницу и повторите действие.",
  "JSON required.": "Неверный формат запроса. Обновите страницу.",
  "Invalid request.": "Проверьте заполненные поля и повторите запрос.",
  "Choose an exchange, name and read-only API credentials.":
    "Выберите биржу, укажите название и API-ключи с доступом только для чтения.",
  "Credential encryption is not configured on the server.":
    "Шифрование ключей на сервере не настроено. Обратитесь к администратору.",
  "Encryption is not configured on the server.":
    "Шифрование на сервере не настроено.",
  "Credential encryption unavailable.":
    "Шифрование ключей временно недоступно.",
  "Unable to decrypt this connection. Reconnect it.":
    "Не удалось расшифровать ключи. Подключите счёт заново.",
  "Unable to read connections.": "Не удалось загрузить подключения.",
  "Maximum of 10 connections per account.":
    "Можно добавить не более 10 подключений.",
  "Unable to encrypt credentials.": "Не удалось зашифровать ключи.",
  "This API key is already connected.": "Этот API-ключ уже подключён.",
  "Unable to save connection.": "Не удалось сохранить подключение.",
  "Unable to queue sync.": "Не удалось запустить синхронизацию.",
  "Connection is syncing, was just synced, or no longer exists.":
    "Счёт уже обновляется, недавно обновлён или отключён.",
  "Unable to disconnect.": "Не удалось отключить счёт.",
  "Connection not found.": "Подключение не найдено.",
  "Analytics could not be loaded. Please retry.":
    "Не удалось загрузить аналитику. Повторите попытку.",
  "Tag and strategy must be under 80 characters.":
    "Метка и стратегия должны содержать не более 80 символов.",
  "Unable to load trades.": "Не удалось загрузить сделки.",
  "Trade not found.": "Сделка не найдена.",
  "Unable to save annotation.": "Не удалось сохранить пометку.",
  "Unable to load trade.": "Не удалось загрузить сделку.",
  "This trade exceeds the 30,000-minute chart limit.":
    "Длительность сделки превышает ограничение графика в 30 000 минут.",
  "Trade exceeds the 30,000 minute chart limit.":
    "Длительность сделки превышает ограничение графика в 30 000 минут.",
  "Mark-price history is unavailable. MFE and MAE cannot be calculated.":
    "История цены маркировки недоступна. Максимальные прибыль и убыток внутри сделки не рассчитаны.",
  "Unable to save excursion measurements.":
    "Не удалось сохранить оценку движения цены внутри сделки.",
  "1-minute mark-price samples; execution candles excluded. Gross PnL basis.":
    "Оценка по минутным ценам маркировки, без свечей с исполнениями. Результат до вычета расходов.",
  "Exchange is unavailable. Try again later.":
    "Биржа временно недоступна. Повторите попытку позже.",
  "Only read-only API keys are accepted. Disable trading and withdrawal permissions.":
    "Нужен ключ только для чтения. Отключите торговлю и вывод средств.",
  "Only read-only API keys are accepted. Disable trading, transfer and withdrawal permissions.":
    "Нужен ключ только для чтения. Отключите торговлю, переводы и вывод средств.",
  "Enable read access for this API key.":
    "Включите право чтения для этого API-ключа.",
  "Exchange returned a repeated pagination cursor.":
    "Биржа повторно вернула ту же страницу истории. Повторите синхронизацию позже.",
  "History page limit reached; coverage was not advanced.":
    "Достигнут лимит страниц истории. Загрузка не завершена.",
  "Unified account balance unavailable.": "Баланс единого счёта недоступен.",
  "Income history page limit reached.":
    "Достигнут лимит страниц истории начислений.",
  "Too many executions at one timestamp; import coverage is incomplete.":
    "Слишком много исполнений с одинаковым временем. История загружена не полностью.",
  "Trade history page limit reached.":
    "Достигнут лимит страниц истории сделок.",
  "Account exceeds the interactive 200,000-event limit; narrow history in the server configuration before analytics can be displayed.":
    "История превышает лимит 200 000 событий. Для отображения аналитики администратору нужно ограничить период загрузки.",
  "Trade analytics cover USDT linear contracts. Other wallet currencies remain visible in the source ledger; their historical USD conversion is unavailable.":
    "Аналитика сделок охватывает линейные контракты USDT. Другие валюты остаются в исходном журнале, но их историческая оценка в долларах недоступна.",
  "Historical leverage, stop-loss and take-profit are unavailable unless recorded at entry.":
    "Историческое плечо, стоп-лосс и тейк-профит доступны только при сохранении в момент входа.",
  "Coverage: Binance USD-M futures. Spot, options and coin-margined accounts are not included.":
    "Поддерживаются фьючерсы Binance USD-M. Спот, опционы и счета с обеспечением в криптовалюте не включены.",
  "Historical leverage, stop-loss and take-profit are not returned by the trade-history API.":
    "API истории сделок не возвращает историческое плечо, стоп-лосс и тейк-профит.",
  "Non-stable collateral requires currency valuation; USD reconciliation is incomplete.":
    "Обеспечение в других криптовалютах требует переоценки. Сверка в долларах неполна.",
  "Non-USDT contracts are retained in the ledger but excluded from trade reconstruction.":
    "Контракты не в USDT сохраняются в журнале, но не участвуют в восстановлении сделок.",
  "Select a supported pattern with affected trades.":
    "Выберите закономерность со связанными сделками.",
  "Unable to load calculation evidence.":
    "Не удалось загрузить данные для пояснения.",
  "No complete owned trades match this pattern.":
    "Для этой закономерности не найдено ваших сделок с полной историей.",
  "Unable to reserve explanation.": "Не удалось начать подготовку пояснения.",
  "Unable to check explanation budget.":
    "Не удалось проверить лимит пояснений.",
};

export function localizeMessage(message: string): string {
  if (messages[message]) return messages[message];
  const bybit = /^Bybit error (\d+)\./.exec(message);
  if (bybit)
    return `Ошибка Bybit ${bybit[1]}. Проверьте права ключа, разрешённые IP-адреса и тип счёта.`;
  const http = /Exchange rejected request \(HTTP (\d+)\)/.exec(message);
  if (http)
    return `Биржа отклонила запрос (код ${http[1]}). Проверьте права ключа, разрешённые IP-адреса и регион.`;
  if (/timeout|deadline exceeded/i.test(message))
    return "Время ожидания истекло. Повторите попытку позже.";
  if (/failed to fetch|network|fetch failed/i.test(message))
    return "Не удалось связаться с сервером. Проверьте соединение и повторите попытку.";
  if (/[а-яё]/i.test(message)) return message;
  return "Не удалось выполнить операцию. Повторите попытку; если ошибка сохраняется, обратитесь к администратору.";
}

export function errorMessage(error: unknown) {
  if (error instanceof Error) {
    if (messages[error.message]) return messages[error.message];
    const status = (error as HttpError).status;
    if (status === 401) return "Сессия истекла. Войдите в аккаунт снова.";
    if (status === 429)
      return "Слишком много запросов. Подождите немного и повторите попытку.";
    return localizeMessage(error.message);
  }
  return "Не удалось выполнить операцию. Повторите попытку.";
}
