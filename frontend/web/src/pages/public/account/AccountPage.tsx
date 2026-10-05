import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { useNavigate } from "react-router-dom";
import { useAuth } from "../../../shared/auth/AuthContext";
import { AnimatedNativeSelect } from "../../../shared/ui/AnimatedSelect";
import {
  apiSessions,
  apiTelegramLink,
  apiTelegramStatus,
  apiTelegramToggle,
  apiTelegramUnlink,
  type ApiSession,
  type TelegramLink,
  type TelegramStatus,
} from "../../../shared/auth/authApi";
import {
  useAllTrades,
  useTradeStrategies,
  useDeleteTradeStrategy,
  useDeleteTradeStrategyPair,
} from "../scanners/queries";
import type { ApiTrade } from "../scanners/types";
import AvatarModal from "./AvatarModal";
import TradeDetailsModal from "./TradeDetailsModal";
import TradeHistoryTable from "./TradeHistoryTable";
import StrategyPairRow from "./StrategyPairRow";

const ACCOUNT_PANEL_CLASS =
  "rounded-2xl border border-border bg-card p-5 md:p-6";
const ACCOUNT_FIELD_CLASS =
  "mt-2 min-h-11 w-full rounded-xl border border-border bg-background px-3.5 py-2.5 text-foreground outline-none transition placeholder:text-muted-foreground focus:border-ring focus:ring-2 focus:ring-ring";
const ACCOUNT_READONLY_FIELD_CLASS =
  "mt-2 min-h-11 w-full rounded-xl border border-border bg-background px-3.5 py-2.5 text-muted-foreground outline-none";
const ACCOUNT_SECTION_CLASS =
  "mt-5 rounded-2xl border border-border bg-background p-4 sm:p-5";

type ProfileDraft = {
  displayName: string;
  avatarUrl: string;
  country: string;
  about: string;
};

type UAParserConstructor = new (ua?: string) => {
  getDevice: () => { vendor?: string; model?: string };
  getOS: () => { name?: string; version?: string };
  getBrowser: () => { name?: string; version?: string };
};

function profileSnapshot(profile: ProfileDraft) {
  return JSON.stringify(profile);
}

function accountProfileStorageKey(userID: string, field: "country" | "about") {
  return `account_profile:${userID}:v1:${field}`;
}

function readAccountProfileField(userID: string, field: "country" | "about") {
  try {
    const key = accountProfileStorageKey(userID, field);
    const stored = localStorage.getItem(key);
    if (stored !== null) return stored;
    const legacy = localStorage.getItem(`account_${field}`) ?? "";
    if (legacy) localStorage.setItem(key, legacy);
    return legacy;
  } catch {
    return "";
  }
}

function writeAccountProfileField(userID: string, field: "country" | "about", value: string) {
  try {
    localStorage.setItem(accountProfileStorageKey(userID, field), value);
  } catch {
    // The server-backed profile still remains usable when storage is unavailable.
  }
}

function Pill({ label, tone }: { label: string; tone: "success" | "warning" | "neutral" }) {
  const cls =
    tone === "success"
      ? "border-border-strong bg-accent text-primary"
      : tone === "warning"
        ? "border-amber-400/20 bg-amber-500/10 text-amber-700 dark:text-amber-200"
        : "border-border bg-card text-muted-foreground";
  return (
    <span className={`inline-flex items-center rounded-full border px-3 py-1 text-xs ${cls}`}>
      {label}
    </span>
  );
}

function formatDate(value?: string | null) {
  if (!value) return "—";
  const d = new Date(value);
  if (Number.isNaN(d.getTime())) return "—";
  return d.toLocaleString();
}

function formatNumber(value?: number | null, digits = 2) {
  if (value === null || value === undefined) return "—";
  if (!Number.isFinite(value)) return "—";
  return value.toFixed(digits);
}

function formatPercent(value?: number | null) {
  if (value === null || value === undefined) return "—";
  if (!Number.isFinite(value)) return "—";
  return `${value.toFixed(2)}%`;
}

function formatUsd(value?: number | null) {
  if (value === null || value === undefined) return "—";
  if (!Number.isFinite(value)) return "—";
  return `$${value.toFixed(2)}`;
}

function subscriptionSummary(user: {
  plan?: string | null;
  subscriptionExpiresAt?: string | null;
  subscriptionFrozenAt?: string | null;
  subscriptionDaysRemaining?: number;
  subscriptionFrozenDaysRemaining?: number;
}) {
  const plan = String(user.plan ?? "free").toLowerCase();
  const frozen = Boolean(user.subscriptionFrozenAt);
  if (frozen) {
    const frozenDays = Math.max(0, Number(user.subscriptionFrozenDaysRemaining ?? 0));
    return {
      status: "Заморожена",
      daysText: `${frozenDays} дн. на паузе`,
      expiresText: "Дата окончания обновится после разморозки",
    };
  }

  const days = Math.max(0, Number(user.subscriptionDaysRemaining ?? 0));
  if ((plan === "pro" || plan === "standard") && days > 0) {
    return {
      status: "Активна",
      daysText: `${days} дн. осталось`,
      expiresText: formatDate(user.subscriptionExpiresAt),
    };
  }

  return {
    status: "Неактивна",
    daysText: "0 дн.",
    expiresText: "—",
  };
}

function TabButton({
  active,
  label,
  onClick,
}: {
  active: boolean;
  label: string;
  onClick: () => void;
}) {
  return (
    <button
      type="button"
      onClick={onClick}
      aria-pressed={active}
      className={
        "flex min-h-11 w-full items-center justify-start rounded-xl border px-4 py-2.5 text-left text-sm font-semibold transition focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring " +
        (active
          ? "border-border-strong bg-accent text-primary"
          : "border-border bg-secondary text-secondary-foreground hover:bg-accent")
      }
    >
      {label}
    </button>
  );
}

export default function AccountPage() {
  const {
    user,
    logout,
    updateProfile,
    updateTwoFA,
    requestTwoFAEnable,
    requestTwoFADisable,
    confirmTwoFAEnable,
    refresh,
    primaryExchange,
    setPrimaryExchange,
  } = useAuth();
  const navigate = useNavigate();
  const userKey = user?.id ?? "anon";
  const profileUserID = user?.id;
  const profileDisplayName = user?.displayName;
  const profileAvatarURL = user?.avatarUrl;
  const tradeStrategies = useTradeStrategies(userKey);
  const allTrades = useAllTrades(userKey);
  const closedTrades = useMemo(
    () => allTrades.data?.filter((trade) => trade.status === "CLOSED"),
    [allTrades.data],
  );
  const deleteStrategy = useDeleteTradeStrategy(userKey);
  const deletePair = useDeleteTradeStrategyPair(userKey);
  const [status, setStatus] = useState<string>("");
  const [isSaving, setIsSaving] = useState(false);
  const [securityStatus, setSecurityStatus] = useState<string>("");
  const [twoFAStage, setTwoFAStage] = useState<"idle" | "code-sent">("idle");
  const [twoFACode, setTwoFACode] = useState("");
  const [twoFALoading, setTwoFALoading] = useState(false);
  const [twoFAConfirming, setTwoFAConfirming] = useState(false);
  const [activeTab, setActiveTab] = useState<"custom" | "settings" | "stats">("custom");
  const [selectedTrade, setSelectedTrade] = useState<ApiTrade | null>(null);
  const [sessions, setSessions] = useState<ApiSession[]>([]);
  const [sessionsLoading, setSessionsLoading] = useState(false);
  const [sessionsError, setSessionsError] = useState("");
  const [uaParserCtor, setUaParserCtor] = useState<UAParserConstructor | null>(null);
  const [telegramStatus, setTelegramStatus] = useState<TelegramStatus | null>(null);
  const [telegramLink, setTelegramLink] = useState<TelegramLink | null>(null);
  const [telegramLoading, setTelegramLoading] = useState(false);
  const [telegramError, setTelegramError] = useState("");
  const [form, setForm] = useState({
    displayName: "",
    avatarUrl: "",
    country: "",
    about: "",
  });
  const [countrySearch, setCountrySearch] = useState("");
  const [countryOpen, setCountryOpen] = useState(false);
  const [avatarModalOpen, setAvatarModalOpen] = useState(false);
  const closeAvatarModal = useCallback(() => setAvatarModalOpen(false), []);
  const closeSelectedTrade = useCallback(() => setSelectedTrade(null), []);
  const openSelectedTrade = useCallback((trade: ApiTrade) => setSelectedTrade(trade), []);
  const lastSavedProfileRef = useRef("");
  const autosaveTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null);
  const countries = useMemo(
    () => [
      { code: "RU", name: "Россия" },
      { code: "BY", name: "Беларусь" },
      { code: "KZ", name: "Казахстан" },
      { code: "UA", name: "Украина" },
      { code: "GE", name: "Грузия" },
      { code: "AM", name: "Армения" },
      { code: "AZ", name: "Азербайджан" },
      { code: "UZ", name: "Узбекистан" },
      { code: "KG", name: "Кыргызстан" },
      { code: "TJ", name: "Таджикистан" },
      { code: "MD", name: "Молдова" },
      { code: "LV", name: "Латвия" },
      { code: "LT", name: "Литва" },
      { code: "EE", name: "Эстония" },
      { code: "PL", name: "Польша" },
      { code: "DE", name: "Германия" },
      { code: "FR", name: "Франция" },
      { code: "ES", name: "Испания" },
      { code: "IT", name: "Италия" },
      { code: "GB", name: "Великобритания" },
      { code: "US", name: "США" },
      { code: "CA", name: "Канада" },
      { code: "MX", name: "Мексика" },
      { code: "BR", name: "Бразилия" },
      { code: "AR", name: "Аргентина" },
      { code: "CL", name: "Чили" },
      { code: "TR", name: "Турция" },
      { code: "AE", name: "ОАЭ" },
      { code: "IL", name: "Израиль" },
      { code: "SA", name: "Саудовская Аравия" },
      { code: "EG", name: "Египет" },
      { code: "IN", name: "Индия" },
      { code: "CN", name: "Китай" },
      { code: "JP", name: "Япония" },
      { code: "KR", name: "Южная Корея" },
      { code: "VN", name: "Вьетнам" },
      { code: "TH", name: "Таиланд" },
      { code: "ID", name: "Индонезия" },
      { code: "MY", name: "Малайзия" },
      { code: "SG", name: "Сингапур" },
      { code: "AU", name: "Австралия" },
      { code: "NZ", name: "Новая Зеландия" },
      { code: "ZA", name: "ЮАР" },
      { code: "NG", name: "Нигерия" },
      { code: "KE", name: "Кения" },
    ],
    []
  );

  const sub = useMemo(() => (user ? subscriptionSummary(user) : null), [user]);

  useEffect(() => {
    if (!profileUserID) return;
    const storedCountry = readAccountProfileField(profileUserID, "country");
    const storedAbout = readAccountProfileField(profileUserID, "about");
    const nextForm = {
      displayName: profileDisplayName ?? "",
      avatarUrl: profileAvatarURL ?? "",
      country: storedCountry,
      about: storedAbout,
    };
    lastSavedProfileRef.current = profileSnapshot(nextForm);
    setForm(nextForm);
  }, [profileUserID, profileDisplayName, profileAvatarURL]);

  useEffect(() => {
    if (!user?.id || activeTab !== "settings") return;
    let cancelled = false;
    setSessionsLoading(true);
    setSessionsError("");
    apiSessions()
      .then(({ sessions }) => {
        if (cancelled) return;
        setSessions(sessions);
      })
      .catch((err) => {
        if (cancelled) return;
        const message = err instanceof Error ? err.message : "Не удалось загрузить устройства";
        setSessionsError(message);
      })
      .finally(() => {
        if (cancelled) return;
        setSessionsLoading(false);
      });
    return () => {
      cancelled = true;
    };
  }, [user?.id, activeTab]);

  useEffect(() => {
    if (activeTab !== "settings" || uaParserCtor) return;
    let cancelled = false;
    void import("ua-parser-js").then((module) => {
      const imported = module as unknown as {
        default?: UAParserConstructor;
        UAParser?: UAParserConstructor;
      };
      const Parser = imported.default ?? imported.UAParser;
      if (!cancelled && Parser) setUaParserCtor(() => Parser);
    });
    return () => {
      cancelled = true;
    };
  }, [activeTab, uaParserCtor]);

  useEffect(() => {
    if (!user?.id || activeTab !== "settings") return;
    let cancelled = false;
    setTelegramLoading(true);
    setTelegramError("");
    apiTelegramStatus()
      .then((data) => {
        if (cancelled) return;
        setTelegramStatus(data);
        if (!data.linked) {
          setTelegramLink(null);
        }
      })
      .catch((err) => {
        if (cancelled) return;
        const message = err instanceof Error ? err.message : "Не удалось загрузить Telegram статус";
        setTelegramError(message);
      })
      .finally(() => {
        if (cancelled) return;
        setTelegramLoading(false);
      });
    return () => {
      cancelled = true;
    };
  }, [user?.id, activeTab]);

  useEffect(() => {
    setAvatarModalOpen(false);
  }, [activeTab]);

  const initials = useMemo(() => {
    const name = form.displayName || user?.displayName || "User";
    const parts = name.trim().split(/\s+/);
    const a = parts[0]?.[0] ?? "U";
    const b = parts[1]?.[0] ?? "";
    return (a + b).toUpperCase();
  }, [form.displayName, user?.displayName]);

  const selectedCountry = useMemo(
    () => countries.find((c) => c.name === form.country),
    [countries, form.country]
  );

  const filteredCountries = useMemo(() => {
    const search = countrySearch.trim().toLowerCase();
    if (!search) return countries;
    return countries.filter(
      (c) => c.name.toLowerCase().includes(search) || c.code.toLowerCase().includes(search)
    );
  }, [countries, countrySearch]);

  function flagEmoji(code: string) {
    return code
      .toUpperCase()
      .replace(/./g, (char) => String.fromCodePoint(127397 + char.charCodeAt(0)));
  }

  const online = Boolean(user);

  function describeSession(ua: string) {
    if (!ua) {
      return {
        deviceLabel: "Неизвестное устройство",
        browserLabel: "Неизвестный браузер",
        osLabel: "",
      };
    }
    if (!uaParserCtor) {
      return {
        deviceLabel: "Устройство",
        browserLabel: ua,
        osLabel: "",
      };
    }
    const parser = new uaParserCtor(ua);
    const device = parser.getDevice();
    const os = parser.getOS();
    const browser = parser.getBrowser();
    const deviceLabel = [device.vendor, device.model].filter(Boolean).join(" ");
    const osLabel = [os.name, os.version].filter(Boolean).join(" ");
    const browserLabel = [browser.name, browser.version].filter(Boolean).join(" ");
    return {
      deviceLabel: deviceLabel || osLabel || "Неизвестное устройство",
      browserLabel: browserLabel || "Неизвестный браузер",
      osLabel,
    };
  }

  useEffect(() => {
    if (!profileUserID) return;
    const snapshot = profileSnapshot(form);
    if (snapshot === lastSavedProfileRef.current) {
      return;
    }
    if (autosaveTimerRef.current) {
      clearTimeout(autosaveTimerRef.current);
    }
    autosaveTimerRef.current = setTimeout(async () => {
      setStatus("");
      setIsSaving(true);
      try {
        await updateProfile({
          displayName: form.displayName,
          avatarUrl: form.avatarUrl,
        });
        writeAccountProfileField(profileUserID, "country", form.country.trim());
        writeAccountProfileField(profileUserID, "about", form.about.trim());
        lastSavedProfileRef.current = snapshot;
        setStatus("Сохранено");
      } catch (err) {
        const message = err instanceof Error ? err.message : "Ошибка сохранения";
        setStatus(`Не удалось сохранить: ${message}`);
      } finally {
        setIsSaving(false);
      }
    }, 600);
    return () => {
      if (autosaveTimerRef.current) {
        clearTimeout(autosaveTimerRef.current);
      }
    };
  }, [profileUserID, form, updateProfile]);

  async function handleEnableTwoFA() {
    if (!user) return;
    setSecurityStatus("");
    setTwoFALoading(true);
    try {
      await requestTwoFAEnable();
      setTwoFAStage("code-sent");
      setSecurityStatus("Код отправлен на email. Введи его ниже.");
    } catch (err) {
      const message = err instanceof Error ? err.message : "Ошибка 2FA";
      setSecurityStatus(`Не удалось отправить код: ${message}`);
    } finally {
      setTwoFALoading(false);
    }
  }

  async function handleConfirmTwoFA() {
    if (!user) return;
    if (!twoFACode.trim()) {
      setSecurityStatus("Введи код подтверждения.");
      return;
    }
    setSecurityStatus("");
    setTwoFAConfirming(true);
    try {
      await confirmTwoFAEnable(twoFACode.trim());
      await refresh();
      setTwoFAStage("idle");
      setTwoFACode("");
      setSecurityStatus("2FA включена.");
    } catch (err) {
      const message = err instanceof Error ? err.message : "Ошибка 2FA";
      setSecurityStatus(`Не удалось включить 2FA: ${message}`);
    } finally {
      setTwoFAConfirming(false);
    }
  }

  async function handleDisableTwoFA() {
    if (!user) return;
    setSecurityStatus("");
    try {
      await requestTwoFADisable();
      const password = window.prompt("??????? ??????? ?????? ??? ?????????? 2FA:") || "";
      const code = window.prompt("??????? OTP-??? ?? ??????:") || "";
      await updateTwoFA(false, password, code);
      await refresh();
      setTwoFAStage("idle");
      setTwoFACode("");
      setSecurityStatus("2FA выключена.");
    } catch (err) {
      const message = err instanceof Error ? err.message : "Ошибка 2FA";
      setSecurityStatus(`Не удалось выключить 2FA: ${message}`);
    }
  }
  useEffect(() => {
    if (user?.twoFAEnabled) {
      setTwoFAStage("idle");
      setTwoFACode("");
    }
  }, [user?.twoFAEnabled]);

  async function handleTelegramLink() {
    setTelegramError("");
    setTelegramLoading(true);
    try {
      const data = await apiTelegramLink();
      setTelegramLink(data);
    } catch (err) {
      const message = err instanceof Error ? err.message : "Не удалось получить код";
      setTelegramError(message);
    } finally {
      setTelegramLoading(false);
    }
  }

  async function handleTelegramToggle(enabled: boolean) {
    setTelegramError("");
    setTelegramLoading(true);
    try {
      const res = await apiTelegramToggle(enabled);
      setTelegramStatus((prev) => (prev ? { ...prev, enabled: res.enabled, linked: true } : prev));
    } catch (err) {
      const message = err instanceof Error ? err.message : "Не удалось обновить Telegram";
      setTelegramError(message);
    } finally {
      setTelegramLoading(false);
    }
  }

  async function handleTelegramUnlink() {
    setTelegramError("");
    setTelegramLoading(true);
    try {
      await apiTelegramUnlink();
      setTelegramStatus({ linked: false, enabled: false, username: "" });
      setTelegramLink(null);
    } catch (err) {
      const message = err instanceof Error ? err.message : "Не удалось отвязать Telegram";
      setTelegramError(message);
    } finally {
      setTelegramLoading(false);
    }
  }

  async function handleLogout() {
    await logout();
    navigate("/", { replace: true });
  }

  async function handleDeleteStrategy(id: string, name: string) {
    if (!confirm(`Удалить стратегию "${name}"? Все сделки останутся, но стратегия исчезнет.`)) {
      return;
    }
    try {
      await deleteStrategy.mutateAsync(id);
    } catch (err) {
      const message = err instanceof Error ? err.message : "Не удалось удалить стратегию";
      alert(message);
    }
  }

  async function handleDeletePair(strategyId: string, symbol: string) {
    if (!confirm(`Удалить все сделки по паре ${symbol}?`)) {
      return;
    }
    try {
      await deletePair.mutateAsync({ id: strategyId, symbol });
    } catch (err) {
      const message = err instanceof Error ? err.message : "Не удалось удалить пару";
      alert(message);
    }
  }

  function handleAvatarFile(file: File | null) {
    if (!file) return;
    if (file.size > 1_500_000) {
      setStatus("Файл слишком большой. До 1.5MB.");
      return;
    }
    const reader = new FileReader();
    reader.onload = () => {
      const result = typeof reader.result === "string" ? reader.result : "";
      if (result) {
        setForm((prev) => ({ ...prev, avatarUrl: result }));
      }
    };
    reader.readAsDataURL(file);
  }

  if (!user) {
    return (
      <div className="min-h-[calc(100vh-120px)] text-foreground">
        <div className="mx-auto max-w-4xl px-4 py-12">Загрузка профиля...</div>
      </div>
    );
  }

  return (
    <div className="min-h-[calc(100vh-120px)] text-foreground">
      <div className="mx-auto w-full max-w-[1450px] px-4 py-8 sm:px-6 lg:px-8 lg:py-10">
        <div className="flex flex-col gap-4 md:flex-row md:items-start md:justify-between">
          <div>
            <div className="flex items-center gap-3">
              <h1 className="text-2xl font-semibold tracking-tight md:text-3xl">Аккаунт</h1>
              <Pill label={online ? "Online" : "Offline"} tone={online ? "success" : "neutral"} />
            </div>
            <p className="mt-2 text-muted-foreground">Профиль пользователя, подписка и безопасность.</p>
          </div>
        </div>

        <div className="mt-6 grid items-start gap-5 lg:grid-cols-[280px_minmax(0,1fr)_210px] xl:grid-cols-[300px_minmax(0,1fr)_230px]">
          <div className="order-2 min-w-0 lg:order-1 lg:sticky lg:top-24">
            <div className={ACCOUNT_PANEL_CLASS}>
              <div className="flex items-start gap-4">
                <button
                  type="button"
                  onClick={() => setAvatarModalOpen(true)}
                  className="relative h-16 w-16 overflow-hidden rounded-2xl border border-border bg-card transition hover:border-border-strong"
                  title="Сменить аватар"
                >
                  {form.avatarUrl ? (
                    <img src={form.avatarUrl} alt="avatar" className="h-full w-full object-cover" />
                  ) : (
                    <div className="flex h-full w-full items-center justify-center text-lg font-semibold">
                      {initials}
                    </div>
                  )}
                  <div className="absolute -bottom-1 -right-1 h-5 w-5 rounded-full border border-border bg-accent" />
                </button>
                <div className="min-w-0">
                  <div className="text-xl font-semibold leading-tight">
                    {form.displayName || "User"}
                    {selectedCountry?.code ? (
                      <span className="ml-2 text-base" title={selectedCountry.name}>
                        {flagEmoji(selectedCountry.code)}
                      </span>
                    ) : null}
                  </div>
                  <div className="mt-1 truncate text-sm text-muted-foreground">{user.email}</div>
                  <div className="mt-1 text-xs text-muted-foreground">
                    ID пользователя: <span className="text-foreground font-semibold">{user.public_id || "—"}</span>
                  </div>
                  <div className="mt-3 flex flex-wrap gap-2">
                    <Pill label={`Plan: ${user.plan || "Free"}`} tone="neutral" />
                    {sub ? (
                      <Pill
                        label={`Подписка: ${sub.status}`}
                        tone={sub.status === "Активна" ? "success" : sub.status === "Заморожена" ? "warning" : "neutral"}
                      />
                    ) : null}
                    {user.isAdmin ? <Pill label="Admin" tone="success" /> : null}
                    <Pill label={`2FA: ${user.twoFAEnabled ? "On" : "Off"}`} tone={user.twoFAEnabled ? "success" : "warning"} />
                  </div>
                </div>
              </div>

              <div className="mt-5 grid gap-2.5">
                <div className="rounded-xl border border-border bg-background p-4">
                  <div className="text-xs text-muted-foreground">Зарегистрирован</div>
                  <div className="mt-1 text-sm">{formatDate(user.createdAt)}</div>
                </div>
                <div className="rounded-xl border border-border bg-background p-4">
                  <div className="text-xs text-muted-foreground">Последняя активность</div>
                  <div className="mt-1 text-sm">{formatDate(user.lastSeenAt)}</div>
                </div>
                <div className="rounded-xl border border-border bg-background p-4">
                  <div className="text-xs text-muted-foreground">Подписка</div>
                  <div className="mt-1 text-sm">{sub?.status ?? "—"}</div>
                  <div className="mt-1 text-xs text-muted-foreground">{sub?.daysText ?? "—"}</div>
                  <div className="mt-1 text-xs text-muted-foreground">До: {sub?.expiresText ?? "—"}</div>
                </div>
              </div>
            </div>
          </div>

          <div className="order-3 min-w-0 lg:order-2">
            {activeTab === "custom" && (
              <div className={ACCOUNT_PANEL_CLASS}>
                <div className="text-xl font-semibold tracking-tight">Кастомизация</div>
                <div className="mt-1.5 text-sm leading-6 text-muted-foreground">
                  Настрой имя и внешний вид профиля.
                </div>

                <div className="mt-5 grid gap-4 sm:grid-cols-2">
                  <label className="text-sm text-muted-foreground">
                    Имя пользователя
                    <input
                      value={form.displayName}
                      onChange={(e) => setForm((prev) => ({ ...prev, displayName: e.target.value }))}
                      className={ACCOUNT_FIELD_CLASS}
                      placeholder="User123"
                    />
                  </label>

                  <div className="text-sm text-muted-foreground">
                    Страна
                    <div className="relative mt-2">
                      <button
                        type="button"
                        onClick={() => setCountryOpen((prev) => !prev)}
                        className="flex min-h-11 w-full items-center justify-between rounded-xl border border-border bg-background px-3.5 py-2.5 text-left text-foreground outline-none transition focus:border-ring focus:ring-2 focus:ring-ring"
                      >
                        <span className="flex items-center gap-2 text-sm">
                          {selectedCountry?.code ? (
                            <span className="text-base">{flagEmoji(selectedCountry.code)}</span>
                          ) : null}
                          {form.country || "Выбери страну"}
                        </span>
                        <span className="text-xs text-muted-foreground">▼</span>
                      </button>
                      {countryOpen && (
                        <div className="absolute z-10 mt-2 w-full rounded-xl border border-border-strong bg-background/95 p-2 shadow-xl">
                          <input
                            value={countrySearch}
                            onChange={(e) => setCountrySearch(e.target.value)}
                            className="mb-2 min-h-10 w-full rounded-lg border border-border-strong bg-background px-3 py-2 text-sm text-foreground outline-none placeholder:text-muted-foreground focus:border-ring"
                            placeholder="Поиск страны..."
                          />
                          <div className="max-h-44 overflow-auto">
                            {filteredCountries.length === 0 && (
                              <div className="px-2 py-2 text-xs text-muted-foreground">Ничего не найдено</div>
                            )}
                            {filteredCountries.map((c) => (
                              <button
                                key={c.code}
                                type="button"
                                onClick={() => {
                                  setForm((prev) => ({ ...prev, country: c.name }));
                                  setCountrySearch("");
                                  setCountryOpen(false);
                                }}
                                className={
                                  "flex w-full items-center justify-between rounded-lg px-2 py-2 text-left text-sm transition hover:bg-accent " +
                                  (form.country === c.name
                                    ? "bg-accent text-primary"
                                    : "text-muted-foreground")
                                }
                              >
                                <span className="flex items-center gap-2">
                                  <span className="text-base">{flagEmoji(c.code)}</span>
                                  {c.name}
                                </span>
                                <span className="text-xs text-muted-foreground">{c.code}</span>
                              </button>
                            ))}
                          </div>
                        </div>
                      )}
                    </div>
                  </div>

                  <label className="text-sm text-muted-foreground">
                    Ваша основная биржа
                    <AnimatedNativeSelect
                      value={primaryExchange ?? "bybit"}
                      onChange={(e) => {
                        const next = e.target.value === "binance" ? "binance" : "bybit";
                        setStatus("");
                        setIsSaving(true);
                        void setPrimaryExchange(next)
                          .then(() => setStatus("Сохранено"))
                          .catch((err) => {
                            const message = err instanceof Error ? err.message : "Ошибка сохранения";
                            setStatus(`Не удалось сохранить: ${message}`);
                          })
                          .finally(() => setIsSaving(false));
                      }}
                      className={ACCOUNT_FIELD_CLASS}
                    >
                      <option value="bybit">Bybit</option>
                      <option value="binance">Binance</option>
                    </AnimatedNativeSelect>
                    <span className="mt-1.5 block text-xs leading-5 text-muted-foreground">
                      Автоматически используется в сканерах, тикере, watchlist и графиках.
                    </span>
                  </label>

                  <label className="text-sm text-muted-foreground sm:col-span-2">
                    Немного о себе
                    <textarea
                      value={form.about}
                      onChange={(e) => setForm((prev) => ({ ...prev, about: e.target.value }))}
                      rows={4}
                      className={`${ACCOUNT_FIELD_CLASS} min-h-28 resize-y`}
                      placeholder="Расскажи пару слов о себе"
                    />
                  </label>
                </div>

                <div className="mt-5 grid gap-4 sm:grid-cols-2">
                  <label className="text-sm text-muted-foreground">
                    План
                    <input
                      value={user.plan || "Free"}
                      readOnly
                      className={ACCOUNT_READONLY_FIELD_CLASS}
                    />
                  </label>

                  <label className="text-sm text-muted-foreground">
                    Остаток подписки
                    <input
                      value={sub?.daysText ?? "—"}
                      readOnly
                      className={ACCOUNT_READONLY_FIELD_CLASS}
                    />
                  </label>

                  <label className="text-sm text-muted-foreground">
                    Дата окончания подписки
                    <input
                      value={sub?.expiresText ?? "—"}
                      readOnly
                      className={ACCOUNT_READONLY_FIELD_CLASS}
                    />
                  </label>

                  <label className="text-sm text-muted-foreground sm:col-span-2">
                    Email (только чтение)
                    <input
                      value={user.email}
                      readOnly
                      className={ACCOUNT_READONLY_FIELD_CLASS}
                    />
                  </label>
                </div>

                <div className="mt-5 flex flex-wrap items-center gap-3">
                  {isSaving && <div className="text-sm text-muted-foreground">Сохранение...</div>}
                  {!isSaving && status && <div className="text-sm text-muted-foreground">{status}</div>}
                </div>

              </div>
            )}

            {activeTab === "settings" && (
              <div className={ACCOUNT_PANEL_CLASS}>
                <div className="text-xl font-semibold tracking-tight">Настройки аккаунта</div>
                <div className="mt-1.5 text-sm leading-6 text-muted-foreground">
                  Контакты, подписка и безопасность.
                </div>

                <div className={ACCOUNT_SECTION_CLASS}>
                  <div className="text-sm font-semibold text-foreground">Безопасность</div>
                  <div className="mt-2 text-sm text-muted-foreground">
                    2FA: {user.twoFAEnabled ? "Включена" : "Выключена"}
                  </div>
                  {!user.emailVerified && (
                    <div className="mt-3 rounded-xl border border-amber-400/30 bg-amber-400/10 px-3 py-2 text-xs text-amber-700 dark:text-amber-200">
                      Подтверди email, чтобы включить 2FA.
                    </div>
                  )}
                  <div className="mt-4 flex flex-wrap items-center gap-3">
                    {user.twoFAEnabled ? (
                      <button
                        type="button"
                        onClick={handleDisableTwoFA}
                        className="rounded-xl border border-border bg-card px-4 py-2 text-sm hover:bg-secondary transition"
                      >
                        Выключить 2FA
                      </button>
                    ) : (
                      <button
                        type="button"
                        onClick={handleEnableTwoFA}
                        disabled={!user.emailVerified || twoFALoading}
                        className="rounded-xl border border-border bg-card px-4 py-2 text-sm hover:bg-secondary transition disabled:opacity-50"
                      >
                        {twoFALoading ? "Отправляем код..." : "Включить 2FA"}
                      </button>
                    )}
                    {securityStatus && <div className="text-sm text-muted-foreground">{securityStatus}</div>}
                  </div>

                  {!user.twoFAEnabled && twoFAStage === "code-sent" && (
                    <div className="mt-4 grid gap-3 sm:max-w-md">
                      <label className="text-sm text-muted-foreground">
                        Код подтверждения
                        <input
                          value={twoFACode}
                          onChange={(e) => setTwoFACode(e.target.value)}
                          className={`${ACCOUNT_FIELD_CLASS} text-center tracking-[0.3em]`}
                          placeholder="000000"
                          maxLength={6}
                          inputMode="numeric"
                        />
                      </label>
                      <div className="flex flex-wrap gap-2">
                        <button
                          type="button"
                          onClick={handleConfirmTwoFA}
                          disabled={twoFAConfirming}
                          className="rounded-xl bg-primary px-4 py-2 text-sm font-semibold text-primary-foreground hover:bg-primary-hover transition disabled:opacity-60"
                        >
                          {twoFAConfirming ? "Проверяем..." : "Подтвердить 2FA"}
                        </button>
                        <button
                          type="button"
                          onClick={handleEnableTwoFA}
                          disabled={twoFALoading}
                          className="rounded-xl border border-border bg-card px-4 py-2 text-sm hover:bg-secondary transition disabled:opacity-60"
                        >
                          Отправить код ещё раз
                        </button>
                      </div>
                    </div>
                  )}
                </div>

                <div className={ACCOUNT_SECTION_CLASS}>
                  <div className="text-sm font-semibold text-foreground">Telegram уведомления</div>
                  <div className="mt-1 text-sm text-muted-foreground">
                    Подключи Telegram, чтобы получать сигналы в боте.
                  </div>

                  {telegramLoading && (
                    <div className="mt-3 text-sm text-muted-foreground">Загрузка Telegram...</div>
                  )}
                  {telegramError && (
                    <div className="mt-3 text-sm text-amber-700 dark:text-amber-200">{telegramError}</div>
                  )}

                  {telegramStatus?.linked ? (
                    <div className="mt-3 grid gap-3">
                      <div className="text-sm text-muted-foreground">
                        Статус: {telegramStatus.enabled ? "включены" : "выключены"}
                        {telegramStatus.username
                          ? ` · @${telegramStatus.username}`
                          : ""}
                      </div>
                      <div className="flex flex-wrap gap-2">
                        <button
                          type="button"
                          onClick={() => handleTelegramToggle(!telegramStatus.enabled)}
                          className="rounded-xl border border-border bg-card px-4 py-2 text-sm hover:bg-secondary transition"
                        >
                          {telegramStatus.enabled ? "Выключить уведомления" : "Включить уведомления"}
                        </button>
                        <button
                          type="button"
                          onClick={handleTelegramUnlink}
                          className="rounded-xl border border-border bg-card px-4 py-2 text-sm hover:bg-secondary transition"
                        >
                          Отвязать Telegram
                        </button>
                      </div>
                    </div>
                  ) : (
                    <div className="mt-3 grid gap-3">
                      <button
                        type="button"
                        onClick={handleTelegramLink}
                        className="w-fit rounded-xl bg-primary px-4 py-2 text-sm font-semibold text-primary-foreground hover:bg-primary-hover transition"
                      >
                        Получить код для привязки
                      </button>
                      {telegramLink && (
                        <div className="rounded-xl border border-border bg-background p-4 text-sm text-muted-foreground">
                          <div>1) Открой бота {telegramLink.botUsername}</div>
                          <div className="mt-1">2) Нажми “Start” или перейди по ссылке:</div>
                          <button
                            type="button"
                            onClick={() => window.open(telegramLink.deepLink, "_blank")}
                            className="mt-2 rounded-lg border border-border bg-card px-3 py-2 text-sm hover:bg-secondary transition"
                          >
                            Открыть Telegram
                          </button>
                          <div className="mt-2 text-xs text-muted-foreground">
                            Код: {telegramLink.token} · Истекает: {formatDate(telegramLink.expiresAt)}
                          </div>
                        </div>
                      )}
                    </div>
                  )}
                </div>

                <div className={ACCOUNT_SECTION_CLASS}>
                  <div className="text-sm font-semibold text-foreground">Устройства входа</div>
                  <div className="mt-1 text-sm text-muted-foreground">
                    Последние входы в аккаунт с разных устройств и браузеров.
                  </div>

                  {sessionsLoading ? (
                    <div className="mt-3 text-sm text-muted-foreground">Загрузка устройств...</div>
                  ) : sessionsError ? (
                    <div className="mt-3 text-sm text-amber-700 dark:text-amber-200">{sessionsError}</div>
                  ) : sessions.length === 0 ? (
                    <div className="mt-3 text-sm text-muted-foreground">Пока нет данных по устройствам.</div>
                  ) : (
                    <div className="mt-4 grid gap-4">
                      {(() => {
                        const grouped = sessions.reduce((acc, session) => {
                          const meta = describeSession(session.userAgent);
                          const key = `${meta.deviceLabel}|${meta.browserLabel}|${meta.osLabel}|${session.ip || ""}`;
                          const existing = acc.get(key);
                          if (!existing) {
                            acc.set(key, {
                              key,
                              meta,
                              ip: session.ip || "—",
                              latestAt: session.createdAt,
                              isActive: session.isActive,
                              isCurrent: session.isCurrent,
                              count: 1,
                            });
                            return acc;
                          }
                          existing.count += 1;
                          if (new Date(session.createdAt).getTime() > new Date(existing.latestAt).getTime()) {
                            existing.latestAt = session.createdAt;
                            existing.isActive = session.isActive;
                            existing.isCurrent = session.isCurrent;
                          }
                          return acc;
                        }, new Map<
                          string,
                          {
                            key: string;
                            meta: ReturnType<typeof describeSession>;
                            ip: string;
                            latestAt: string;
                            isActive: boolean;
                            isCurrent: boolean;
                            count: number;
                          }
                        >());

                        const groupedList = Array.from(grouped.values()).sort(
                          (a, b) => new Date(b.latestAt).getTime() - new Date(a.latestAt).getTime()
                        );
                        const latestLogin = groupedList[0]?.latestAt;

                        const renderGroup = (item: (typeof groupedList)[number]) => {
                          const statusLabel = item.isCurrent
                            ? "Текущая сессия"
                            : item.isActive
                              ? "Активная"
                              : "Истекла";
                          return (
                            <div className="rounded-xl border border-border bg-background px-4 py-3" key={item.key}>
                              <div className="flex flex-wrap items-center justify-between gap-2">
                                <div>
                                  <div className="text-sm font-semibold text-foreground">
                                    {item.meta.deviceLabel}
                                    {item.count > 1 ? ` · ${item.count}` : ""}
                                  </div>
                                  <div className="mt-1 text-xs text-muted-foreground">
                                    {item.meta.browserLabel}
                                    {item.meta.osLabel ? ` · ${item.meta.osLabel}` : ""}
                                  </div>
                                  <div className="mt-1 text-[11px] text-muted-foreground">IP: {item.ip}</div>
                                </div>
                                <span className="rounded-full border border-border bg-card px-3 py-1 text-xs text-muted-foreground">
                                  {statusLabel}
                                </span>
                              </div>
                              <div className="mt-2 text-xs text-muted-foreground">
                                Последний вход: {formatDate(item.latestAt)}
                              </div>
                            </div>
                          );
                        };

                        return (
                          <>
                            <div className="text-xs text-muted-foreground">
                              Последний вход: {latestLogin ? formatDate(latestLogin) : "—"}
                            </div>
                            <div className="mt-3 grid gap-3">
                              {groupedList.map(renderGroup)}
                            </div>
                          </>
                        );
                      })()}
                    </div>
                  )}
                </div>

                <div className="mt-5 flex flex-wrap items-center gap-3">
                  {isSaving && <div className="text-sm text-muted-foreground">Сохранение...</div>}
                  {!isSaving && status && <div className="text-sm text-muted-foreground">{status}</div>}
                </div>

                <div className="mt-6 border-t border-border pt-6">
                  <button
                    type="button"
                    onClick={handleLogout}
                    className="rounded-xl bg-primary px-4 py-2 text-sm font-semibold text-primary-foreground hover:bg-primary-hover transition"
                  >
                    Выйти из аккаунта
                  </button>
                </div>
              </div>
            )}

            {activeTab === "stats" && (
              <div className={ACCOUNT_PANEL_CLASS}>
                <div className="text-xl font-semibold tracking-tight">Статистика сделок</div>
                <div className="mt-1.5 text-sm leading-6 text-muted-foreground">
                  Разделение по стратегиям. Новая стратегия появляется автоматически после закрытия сделки.
                </div>

                {tradeStrategies.isLoading ? (
                  <div className="mt-6 text-sm text-muted-foreground">Загрузка...</div>
                ) : tradeStrategies.data && tradeStrategies.data.length > 0 ? (
                  <div className="mt-6 grid gap-6">
                    {tradeStrategies.data.map((strategy) => (
                      <section
                        key={strategy.id}
                        className="rounded-2xl border border-border bg-background p-4 sm:p-5"
                      >
                        <div className="flex flex-wrap items-start justify-between gap-3">
                          <div>
                            <div className="text-xs uppercase tracking-wide text-muted-foreground">Стратегия</div>
                            <div className="mt-1 text-lg font-semibold text-foreground">
                              {strategy.name}
                            </div>
                          </div>
                          <button
                            type="button"
                            onClick={() => handleDeleteStrategy(strategy.id, strategy.name)}
                            disabled={deleteStrategy.isPending}
                            className="rounded-lg border border-rose-500/30 bg-rose-500/10 px-3 py-2 text-xs font-semibold text-destructive hover:bg-rose-500/20"
                          >
                            Удалить стратегию
                          </button>
                        </div>

                        <div className="mt-4 grid gap-3 sm:grid-cols-2 lg:grid-cols-4">
                          <div className="rounded-xl border border-border bg-background p-3">
                            <div className="text-xs text-muted-foreground">Сделок</div>
                            <div className="mt-1 text-base font-semibold">{strategy.total_trades}</div>
                          </div>
                          <div className="rounded-xl border border-border bg-background p-3">
                            <div className="text-xs text-muted-foreground">Закрыто</div>
                            <div className="mt-1 text-base font-semibold">{strategy.closed_trades}</div>
                          </div>
                          <div className="rounded-xl border border-border bg-background p-3">
                            <div className="text-xs text-muted-foreground">Win rate</div>
                            <div className="mt-1 text-base font-semibold">
                              {formatPercent(strategy.win_rate ?? null)}
                            </div>
                          </div>
                          <div className="rounded-xl border border-border bg-background p-3">
                            <div className="text-xs text-muted-foreground">Сумма прибыли (%)</div>
                            <div className="mt-1 text-base font-semibold">
                              {formatPercent(strategy.sum_profit_percent ?? null)}
                            </div>
                          </div>
                          <div className="rounded-xl border border-border bg-background p-3">
                            <div className="text-xs text-muted-foreground">Сумма прибыли ($)</div>
                            <div className="mt-1 text-base font-semibold">
                              {formatUsd(strategy.sum_profit_usd ?? null)}
                            </div>
                          </div>
                          <div className="rounded-xl border border-border bg-background p-3">
                            <div className="text-xs text-muted-foreground">Средняя прибыль (%)</div>
                            <div className="mt-1 text-base font-semibold">
                              {formatPercent(strategy.avg_profit_percent ?? null)}
                            </div>
                          </div>
                          <div className="rounded-xl border border-border bg-background p-3">
                            <div className="text-xs text-muted-foreground">Средняя прибыль ($)</div>
                            <div className="mt-1 text-base font-semibold">
                              {formatUsd(strategy.avg_profit_usd ?? null)}
                            </div>
                          </div>
                          <div className="rounded-xl border border-border bg-background p-3">
                            <div className="text-xs text-muted-foreground">Средняя длительность (мин)</div>
                            <div className="mt-1 text-base font-semibold">
                              {formatNumber(strategy.avg_duration_minutes ?? null, 0)}
                            </div>
                          </div>
                        </div>

                        <div className="mt-5 overflow-hidden rounded-2xl border border-border">
                          <div className="bg-card px-4 py-3 text-xs font-semibold uppercase tracking-wide text-muted-foreground">
                            Пары
                          </div>
                          {strategy.pairs && strategy.pairs.length > 0 ? (
                            <div className="overflow-x-auto">
                              <table className="w-full text-left text-sm text-foreground">
                                <thead className="bg-background text-xs uppercase text-muted-foreground">
                                  <tr>
                                    <th className="px-4 py-3">Пара</th>
                                    <th className="px-4 py-3">Сделок</th>
                                    <th className="px-4 py-3">Win rate</th>
                                    <th className="px-4 py-3">Σ %</th>
                                    <th className="px-4 py-3">Σ $</th>
                                    <th className="px-4 py-3">AVG мин</th>
                                    <th className="px-4 py-3"></th>
                                  </tr>
                                </thead>
                                <tbody>
                                  {strategy.pairs.map((pair) => (
                                    <StrategyPairRow
                                      key={pair.symbol}
                                      strategyId={strategy.id}
                                      strategyName={strategy.name}
                                      pair={pair}
                                      trades={allTrades.data}
                                      deletePending={deletePair.isPending}
                                      onDelete={() => handleDeletePair(strategy.id, pair.symbol)}
                                      onSelectTrade={openSelectedTrade}
                                    />
                                  ))}
                                </tbody>
                              </table>
                            </div>
                          ) : (
                            <div className="px-4 py-4 text-sm text-muted-foreground">
                              По этой стратегии пока нет пар.
                            </div>
                          )}
                        </div>
                      </section>
                    ))}
                  </div>
                ) : (
                  <div className="mt-6 rounded-2xl border border-dashed border-border bg-background px-5 py-8 text-center text-sm text-muted-foreground">
                    Стратегий ещё нет. Закрой первую сделку и укажи стратегию.
                  </div>
                )}

                <TradeHistoryTable
                  trades={closedTrades}
                  isLoading={allTrades.isLoading}
                  onSelect={openSelectedTrade}
                />
              </div>
            )}


          </div>
          <aside className="order-1 min-w-0 lg:order-3 lg:sticky lg:top-24">
            <nav aria-label="Разделы аккаунта" className="grid gap-2 rounded-2xl border border-border bg-card p-2">
              <TabButton active={activeTab === "custom"} label="Кастомизация" onClick={() => setActiveTab("custom")} />
              <TabButton active={activeTab === "settings"} label="Настройки аккаунта" onClick={() => setActiveTab("settings")} />
              <TabButton active={activeTab === "stats"} label="Статистика сделок" onClick={() => setActiveTab("stats")} />
            </nav>
          </aside>
        </div>

        {avatarModalOpen ? (
          <AvatarModal
            avatarUrl={form.avatarUrl}
            initials={initials}
            status={status}
            onDismiss={closeAvatarModal}
            onFile={handleAvatarFile}
          />
        ) : null}

        {selectedTrade ? (
          <TradeDetailsModal trade={selectedTrade} onDismiss={closeSelectedTrade} />
        ) : null}
      </div>
    </div>
  );
}

