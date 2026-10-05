export default function ExchangePage() {
  return (
    <div className="mx-auto max-w-6xl px-4 py-10 md:py-14">
      <div className="rounded-3xl border border-border/60 bg-card p-8 text-card-foreground md:p-10">
        <div className="flex flex-col items-start justify-between gap-6 md:flex-row">
          <div>
            <div className="inline-flex items-center rounded-full border border-border/60 bg-secondary px-3 py-1 text-xs text-secondary-foreground">
              Биржа
            </div>
            <h1 className="mt-4 text-3xl md:text-4xl font-extrabold tracking-tight">
              Раздел временно недоступен
            </h1>
            <p className="mt-3 max-w-2xl text-sm text-muted-foreground">
              Мы дорабатываем маркетплейс, чтобы добавить безопасные покупки, рейтинги и проверку
              контента. Сейчас функция отключена для всех пользователей.
            </p>
          </div>
        </div>

        <div className="mt-8 rounded-2xl border border-dashed border-border bg-secondary/40 p-6">
          <div className="text-sm font-semibold text-card-foreground">Скоро будет доступно</div>
          <ul className="mt-3 space-y-2 text-sm text-muted-foreground">
            <li>• Каталог стратегий и обучающих материалов</li>
            <li>• Подписка на сообщества и приватные каналы</li>
            <li>• Безопасные покупки и возвраты</li>
          </ul>
          <div className="mt-4 text-xs text-muted-foreground">
            Мы сообщим о запуске в новостях и уведомлениях.
          </div>
        </div>
      </div>
    </div>
  );
}
