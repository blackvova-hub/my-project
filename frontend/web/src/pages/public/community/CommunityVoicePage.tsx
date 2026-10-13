import { useEffect, useMemo, useState } from "react";
import { Link, useSearchParams } from "react-router-dom";
import { ConnectionState } from "livekit-client";
import {
  ChevronRight,
  Headphones,
  HeadphoneOff,
  Info,
  LoaderCircle,
  Mic,
  MicOff,
  PhoneOff,
  Search,
  Users,
  Volume2,
  X,
} from "lucide-react";
import {
  fetchVoicePairs,
  fetchVoiceRooms,
  searchPairs,
  voiceError,
  type VoiceCatalogue,
} from "./voiceApi";
import { useVoiceRoom } from "./useVoiceRoom";
import "./voice.css";

export default function CommunityVoicePage() {
  const [catalogue, setCatalogue] = useState<VoiceCatalogue | null>(null);
  const [catalogueError, setCatalogueError] = useState(""),
    [roomError, setRoomError] = useState("");
  const [active, setActive] = useState<Record<string, number>>({});
  const [search, setSearch] = useState(""),
    [occupied, setOccupied] = useState(false),
    [limit, setLimit] = useState(100);
  const [retry, setRetry] = useState(0),
    [devices, setDevices] = useState<MediaDeviceInfo[]>([]);
  const [params, setParams] = useSearchParams();
  const selectedID = params.get("pair") || "BTC-USDT";
  const { setAudioHost, ...voice } = useVoiceRoom();
  useEffect(() => {
    const controller = new AbortController();
    let timer: ReturnType<typeof setTimeout>;
    const load = async () => {
      try {
        const result = await fetchVoicePairs(controller.signal);
        if (!controller.signal.aborted) {
          setCatalogue(result);
          setCatalogueError("");
          timer = setTimeout(load, 30 * 60_000);
        }
      } catch (e) {
        if (!controller.signal.aborted) {
          setCatalogueError(
            voiceError(e, "Каталог недоступен. Повторите попытку."),
          );
          timer = setTimeout(load, 10_000);
        }
      }
    };
    void load();
    return () => {
      controller.abort();
      clearTimeout(timer);
    };
  }, [retry]);
  useEffect(() => {
    const controller = new AbortController();
    let timer: ReturnType<typeof setTimeout>;
    const load = async () => {
      if (!document.hidden) {
        try {
          const result = await fetchVoiceRooms(controller.signal);
          if (!controller.signal.aborted) {
            setActive(result.rooms);
            setRoomError("");
          }
        } catch (e) {
          if (!controller.signal.aborted)
            setRoomError(voiceError(e, "Список комнат временно недоступен."));
        }
      }
      if (!controller.signal.aborted) timer = setTimeout(load, 2000);
    };
    void load();
    return () => {
      controller.abort();
      clearTimeout(timer);
    };
  }, [retry]);
  useEffect(() => {
    let live = true;
    const update = async () => {
      try {
        const result = await navigator.mediaDevices?.enumerateDevices();
        if (live && result) setDevices(result);
      } catch {
        /* The page can still listen without enumerating devices. */
      }
    };
    void update();
    navigator.mediaDevices?.addEventListener("devicechange", update);
    return () => {
      live = false;
      navigator.mediaDevices?.removeEventListener("devicechange", update);
    };
  }, [voice.mic]);
  const filtered = useMemo(
    () => searchPairs(catalogue?.items ?? [], search, active, occupied),
    [catalogue, search, active, occupied],
  );
  const selected =
    catalogue?.items.find((p) => p.id === selectedID) ?? catalogue?.items[0];
  const inSelected = voice.pair === selected?.id;
  const connected = voice.state === ConnectionState.Connected;
  const connecting =
    voice.connecting ||
    voice.state === ConnectionState.Reconnecting ||
    voice.state === ConnectionState.SignalReconnecting;
  const title = selected
    ? `${selected.base} / ${selected.quote}`
    : "Выберите криптопару";
  const supportsOutput =
    typeof HTMLMediaElement !== "undefined" &&
    "setSinkId" in HTMLMediaElement.prototype;
  const partial = catalogue?.sources.filter((s) => !s.complete) ?? [];
  const changeSearch = (value: string) => {
    setSearch(value);
    setLimit(100);
  };
  const choosePair = (id: string) => {
    setParams({ pair: id }, { replace: true });
    voice.setError("");
  };

  return (
    <div className="voice-page" data-voice-page>
      <nav className="voice-breadcrumb" aria-label="Хлебные крошки">
        <Link to="/community">Комьюнити</Link>
        <span>/</span>
        <span>Голосовой чат</span>
      </nav>
      <h1>Голосовой чат</h1>
      <p className="voice-description">
        Одна комната на криптопару — для всех бирж и рынков.
      </p>
      {!window.isSecureContext && (
        <div className="voice-notice">
          <Info size={18} />
          <p>
            Для микрофона нужен HTTPS.{" "}
            <a
              href={`https://${window.location.host}${window.location.pathname}${window.location.search}`}
            >
              Открыть защищённую страницу
            </a>
            .{" "}
            <a href="/voice-local-ca.crt" download>
              Сертификат локального сервера
            </a>{" "}
            нужно добавить в доверенные на устройстве.
          </p>
        </div>
      )}
      <div className="voice-workspace">
        <aside className="voice-sidebar" aria-label="Каталог криптопар">
          <div className="voice-sidebar-tools">
            <h2>Криптопары</h2>
            <label className="voice-search">
              <Search size={18} />
              <input
                aria-label="Найти пару"
                placeholder="Найти пару"
                value={search}
                onChange={(e) => changeSearch(e.target.value)}
              />
              {search && (
                <button
                  aria-label="Очистить поиск"
                  onClick={() => changeSearch("")}
                >
                  <X size={15} />
                </button>
              )}
            </label>
            <div className="voice-tabs" role="group" aria-label="Фильтр комнат">
              <button
                aria-pressed={!occupied}
                onClick={() => {
                  setOccupied(false);
                  setLimit(100);
                }}
              >
                Все
              </button>
              <button
                aria-pressed={occupied}
                onClick={() => {
                  setOccupied(true);
                  setLimit(100);
                }}
              >
                С участниками
              </button>
            </div>
          </div>
          <div className="voice-pairs" data-voice-pairs>
            {!catalogue ? (
              <div className="voice-list-message" role="status">
                {catalogueError || "Загружаем криптопары…"}
                <button
                  className="voice-text-button"
                  onClick={() => setRetry((x) => x + 1)}
                >
                  Повторить
                </button>
              </div>
            ) : filtered.length ? (
              filtered.slice(0, limit).map((p) => (
                <button
                  key={p.id}
                  className="voice-pair"
                  data-pair={p.id}
                  aria-pressed={selected?.id === p.id}
                  onClick={() => choosePair(p.id)}
                >
                  <Volume2 size={18} />
                  <span>
                    {p.base}{" "}
                    <span className="voice-pair-quote">/ {p.quote}</span>
                  </span>
                  {active[p.id] > 0 && !roomError ? (
                    <span
                      className="voice-occupancy"
                      aria-label={`${active[p.id]} участников`}
                    >
                      <Users size={13} />
                      {active[p.id]}
                    </span>
                  ) : (
                    <ChevronRight size={16} />
                  )}
                </button>
              ))
            ) : (
              <div className="voice-list-message">
                {occupied && !search
                  ? "Сейчас нет занятых комнат. Выберите пару во вкладке «Все» и войдите первым."
                  : "Пары не найдены. Попробуйте другой запрос."}
              </div>
            )}
            {filtered.length > limit && (
              <button
                className="voice-load-more"
                onClick={() => setLimit((x) => x + 100)}
              >
                Показать ещё {Math.min(100, filtered.length - limit)}
              </button>
            )}
          </div>
          <div className="voice-catalogue-summary">
            {catalogue
              ? `${catalogue.items.length.toLocaleString("ru-RU")} уникальных пар`
              : "Каталог бирж"}
          </div>
        </aside>
        <section className="voice-room" aria-label="Голосовая комната">
          <header className="voice-room-heading">
            <div>
              <h2>{title}</h2>
              <p>
                {active[selected?.id ?? ""] > 0 && !roomError
                  ? `${active[selected!.id]} участников в комнате`
                  : "Комната появится после входа"}
              </p>
            </div>
            <span
              className="voice-status"
              role="status"
              data-voice-state={voice.state}
            >
              {connecting ? (
                <>
                  <LoaderCircle size={14} className="animate-spin" />
                  Подключение…
                </>
              ) : inSelected && connected ? (
                <>
                  <span className="voice-status-dot" />
                  Подключено
                </>
              ) : (
                "Не подключено"
              )}
            </span>
          </header>
          {(voice.error || roomError) && (
            <div className="voice-error" role="alert">
              <Info size={18} />
              <p>{voice.error || roomError}</p>
              <button
                className="voice-text-button"
                onClick={() => {
                  voice.setError("");
                  setRetry((x) => x + 1);
                }}
              >
                Повторить
              </button>
            </div>
          )}
          {voice.pair && !inSelected && (
            <div className="voice-current">
              <Headphones size={16} />
              <span>Вы в комнате {voice.pair.replace("-", " / ")}</span>
              <button
                className="voice-text-button"
                onClick={() => choosePair(voice.pair!)}
              >
                Вернуться
              </button>
              <button
                className="voice-text-button"
                onClick={() => void voice.leave()}
              >
                Выйти
              </button>
            </div>
          )}
          <div className="voice-room-content">
            {inSelected && voice.members.length ? (
              <>
                <div className="voice-members-heading">
                  <Users size={17} />
                  Участники · {voice.members.length}
                </div>
                <ul className="voice-members">
                  {voice.members.map((m) => (
                    <li
                      key={m.identity}
                      data-voice-member={m.identity}
                      data-speaking={m.speaking}
                    >
                      <div className="voice-avatar" aria-hidden="true">
                        {m.name.slice(0, 2).toUpperCase()}
                      </div>
                      <div className="voice-member-name">
                        <strong>
                          {m.name}
                          {m.local && <span> · Вы</span>}
                        </strong>
                        <small>
                          {m.speaking
                            ? "Говорит"
                            : m.microphone
                              ? "Микрофон включён"
                              : "Слушает"}
                          {m.quality === "poor" ? " · Слабое соединение" : ""}
                        </small>
                      </div>
                      {m.microphone ? (
                        <Mic size={18} aria-label="Микрофон включён" />
                      ) : (
                        <MicOff size={18} aria-label="Микрофон выключен" />
                      )}
                    </li>
                  ))}
                </ul>
              </>
            ) : (
              <div className="voice-empty">
                <Headphones size={56} strokeWidth={1.4} />
                <h3>Обсуждайте рынок голосом</h3>
                <p>
                  Войдите в комнату, чтобы слушать участников.
                  <br />
                  Микрофон включается отдельно.
                </p>
                <button
                  className="voice-primary"
                  disabled={
                    !selected || voice.connecting || !catalogue?.configured
                  }
                  onClick={() => selected && void voice.join(selected.id)}
                >
                  {voice.connecting ? (
                    <LoaderCircle size={19} className="animate-spin" />
                  ) : (
                    <Headphones size={19} />
                  )}
                  {voice.connecting
                    ? "Подключаемся…"
                    : voice.pair
                      ? "Перейти в эту комнату"
                      : "Войти в комнату"}
                </button>
                {voice.connecting && (
                  <button
                    className="voice-text-button"
                    onClick={() => void voice.leave()}
                  >
                    Отменить
                  </button>
                )}
                {catalogue && !catalogue.configured && (
                  <p>Голосовой сервер пока не настроен.</p>
                )}
              </div>
            )}
          </div>
          <footer className="voice-controls">
            {inSelected && (
              <div className="voice-call-controls">
                <button
                  aria-label={
                    voice.mic ? "Выключить микрофон" : "Включить микрофон"
                  }
                  aria-pressed={voice.mic}
                  disabled={!connected || voice.busy || voice.deaf}
                  onClick={() => void voice.toggleMic()}
                >
                  {voice.mic ? <Mic size={18} /> : <MicOff size={18} />}
                  {voice.mic ? "Микрофон включён" : "Микрофон выключен"}
                </button>
                <button
                  aria-label={voice.deaf ? "Включить звук" : "Выключить звук"}
                  aria-pressed={voice.deaf}
                  disabled={!connected || voice.busy}
                  onClick={() => void voice.toggleDeaf()}
                >
                  {voice.deaf ? (
                    <HeadphoneOff size={18} />
                  ) : (
                    <Headphones size={18} />
                  )}
                  {voice.deaf ? "Звук выключен" : "Звук включён"}
                </button>
                <button
                  className="voice-leave"
                  onClick={() => void voice.leave()}
                >
                  <PhoneOff size={18} />
                  Выйти
                </button>
              </div>
            )}
            {voice.audioBlocked && (
              <button
                className="voice-text-button"
                onClick={() => void voice.startAudio()}
              >
                Разрешить воспроизведение звука
              </button>
            )}
            <p className="voice-mic-note">
              <Info size={17} />
              {voice.deaf
                ? "Звук и микрофон выключены. При включении звука микрофон останется выключенным."
                : "Микрофон выключен при входе."}
            </p>
            <h3>Устройства звука</h3>
            <div className="voice-devices">
              <label>
                <span>Микрофон</span>
                <select
                  aria-label="Микрофон"
                  value={voice.input}
                  onChange={(e) =>
                    void voice.switchDevice("audioinput", e.target.value)
                  }
                >
                  <option value="default">По умолчанию</option>
                  {devices
                    .filter(
                      (d) =>
                        d.kind === "audioinput" &&
                        d.deviceId &&
                        d.deviceId !== "default",
                    )
                    .map((d, i) => (
                      <option key={d.deviceId} value={d.deviceId}>
                        {d.label || `Микрофон ${i + 1}`}
                      </option>
                    ))}
                </select>
              </label>
              <label>
                <span>Выходной звук</span>
                <select
                  aria-label="Выходной звук"
                  value={voice.output}
                  disabled={!supportsOutput}
                  onChange={(e) =>
                    void voice.switchDevice("audiooutput", e.target.value)
                  }
                >
                  <option value="default">По умолчанию</option>
                  {devices
                    .filter(
                      (d) =>
                        d.kind === "audiooutput" &&
                        d.deviceId &&
                        d.deviceId !== "default",
                    )
                    .map((d, i) => (
                      <option key={d.deviceId} value={d.deviceId}>
                        {d.label || `Динамики ${i + 1}`}
                      </option>
                    ))}
                </select>
                {!supportsOutput && (
                  <small>Выбор устройства доступен в настройках браузера</small>
                )}
              </label>
              <label className="voice-volume">
                <span>Громкость · {Math.round(voice.volume * 100)}%</span>
                <div>
                  <Volume2 size={18} />
                  <input
                    aria-label="Громкость участников"
                    type="range"
                    min="0"
                    max="100"
                    value={Math.round(voice.volume * 100)}
                    onChange={(e) =>
                      voice.setVolume(Number(e.target.value) / 100)
                    }
                  />
                </div>
              </label>
            </div>
          </footer>
        </section>
      </div>
      <p className="voice-footnote">
        Комната создаётся при входе первого участника и удаляется после выхода
        последнего.{" "}
        {partial.length > 0 &&
          `Часть рынков недоступна: ${partial.map((s) => s.name).join(", ")}. Каталог обновится автоматически.`}
        {catalogueError &&
          catalogue &&
          " Обновить каталог пока не удалось; показаны последние полученные пары."}
      </p>
      <div ref={setAudioHost} hidden data-voice-audio-host />
    </div>
  );
}
