export function SubscriptionsSection() {
  return (
    <section id="subscriptions" className="relative bg-background py-14 sm:py-18">
      <div className="pointer-events-none absolute inset-x-0 top-0 h-px bg-border/30" />

      <div className="mx-auto max-w-6xl px-6">
        <div className="relative overflow-hidden rounded-[28px] border border-border/50 bg-card text-card-foreground p-6 sm:p-10">

          <div data-reveal className="relative mb-10 text-center">
            <div className="text-xl font-extrabold tracking-tight sm:text-2xl">
              Подписки
            </div>
            <h3 className="mt-2 text-lg font-semibold text-card-foreground sm:text-xl">
              Выбери подходящий доступ
            </h3>
            <p className="mt-3 text-sm leading-relaxed text-muted-foreground">
              Для приобретения Standard и Pro напишите в Telegram:{" "}
              <a
                href="https://t.me/SL_manager_OFFICIAL"
                target="_blank"
                rel="noreferrer"
                className="text-primary hover:text-primary/80 focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-ring transition"
              >
                @SL_manager_OFFICIAL
              </a>
              .
            </p>
          </div>

          <div data-reveal-scope className="grid gap-6 md:grid-cols-3">
            <div className="flex h-full flex-col rounded-[26px] border border-border/50 bg-surface-raised p-6">
              <div className="text-xl font-semibold text-card-foreground sm:text-2xl">Free</div>
              <div className="mt-2 text-sm font-bold sm:text-base">0 ₽</div>
              <div className="mt-4 space-y-2 text-sm text-muted-foreground">
                <div className="flex items-start gap-2">
                  <span className="mt-1.5 h-2 w-2 shrink-0 rounded-full bg-primary" />
                  <span>Доступен 1 сканер</span>
                </div>
              </div>
            </div>

            <div className="flex h-full flex-col rounded-[26px] border border-border/50 bg-surface-raised p-6">
              <div className="text-xl font-semibold text-card-foreground sm:text-2xl">Standard</div>
              <div className="mt-2 flex items-center gap-2 text-sm font-bold sm:text-base">
                <span className="text-card-foreground">1500₽</span>
              </div>
              <div className="mt-4 space-y-2 text-sm text-muted-foreground">
                <div className="flex items-start gap-2">
                  <span className="mt-1.5 h-2 w-2 shrink-0 rounded-full bg-primary" />
                  <span>Доступно 2 сканера</span>
                </div>
                <div className="flex items-start gap-2">
                  <span className="mt-1.5 h-2 w-2 shrink-0 rounded-full bg-primary" />
                  <span>Доступ в  наше сообщество</span>
                </div>
              </div>
            </div>

            <div className="flex h-full flex-col rounded-[26px] border border-border/50 bg-surface-raised p-6">
              <div className="text-xl font-semibold text-card-foreground sm:text-2xl">Pro</div>
              <div className="mt-2 flex items-center gap-2 text-sm font-bold sm:text-base">
                <span className="text-card-foreground">3000₽</span>
              </div>
              <div className="mt-4 space-y-2 text-sm text-muted-foreground">
                <div className="flex items-start gap-2">
                  <span className="mt-1.5 h-2 w-2 shrink-0 rounded-full bg-primary" />
                  <span>Доступно 3 сканера</span>
                </div>
                <div className="flex items-start gap-2">
                  <span className="mt-1.5 h-2 w-2 shrink-0 rounded-full bg-primary" />
                  <span>Доступ в наше сообщество</span>
                </div>
                <div className="flex items-start gap-2">
                  <span className="mt-1.5 h-2 w-2 rounded-full bg-primary" />
                  <span>Раздел новости</span>
                </div>
                <div className="flex items-start gap-2 text-muted-foreground/80">
                  <span className="mt-1.5 h-2 w-2 shrink-0 rounded-full bg-primary" />
                  <span>В будущем планируется сканер для пампов</span>
                </div>
              </div>
            </div>
          </div>
        </div>
      </div>
    </section>
  )
}
