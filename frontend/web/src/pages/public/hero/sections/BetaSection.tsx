export function BetaSection() {
  return (
    <section id="beta" className="relative bg-background py-14 sm:py-18">
      <div className="pointer-events-none absolute inset-x-0 top-0 h-px bg-border/30" />

      <div className="mx-auto max-w-6xl px-6">
        <div data-reveal className="relative overflow-hidden rounded-[28px] border border-border/50 bg-card text-card-foreground p-6 sm:p-10">

          <div className="relative flex flex-col gap-6 md:flex-row md:items-center md:justify-between">
            <div className="max-w-3xl">
              <div className="inline-flex items-center gap-2 rounded-full border border-border/50 bg-secondary/40 px-4 py-2 text-xs text-card-foreground/80">
                <span className="h-2 w-2 rounded-full bg-primary" />
                Бета-тестирование
              </div>
              <h3 className="mt-4 text-2xl font-extrabold tracking-tight sm:text-3xl">
                Сейчас платформа в активной бете
              </h3>
              <p className="mt-3 text-sm leading-relaxed text-muted-foreground">
                Мы открыли ранний доступ, чтобы быстрее довести продукт до стабильности.
                Сейчас идёт отладка логики сигналов, интерфейса и потоков данных - и нам
                важно получать обратную связь от реальных пользователей.
              </p>
              <p className="mt-3 text-sm leading-relaxed text-muted-foreground">
                Если заметили баг или  ошибку в данных - напишите
                в Telegram. Мы фиксируем проблемы и выпускаем обновления максимально быстро.
              </p>

              <div className="mt-5 flex flex-wrap gap-3 text-xs text-muted-foreground">
                <span className="rounded-full border border-border/50 bg-secondary/40 px-3 py-1">
                  Быстрые фиксы
                </span>
                <span className="rounded-full border border-border/50 bg-secondary/40 px-3 py-1">
                  Прозрачный трекинг
                </span>
                <span className="rounded-full border border-border/50 bg-secondary/40 px-3 py-1">
                  Регулярные апдейты
                </span>
              </div>
            </div>

            <div className="flex flex-col gap-3 sm:flex-row">
              <a
                href="#"
                className="w-full rounded-full px-8 py-3 text-sm font-semibold text-center
                           border border-border/70 bg-secondary/40 hover:bg-accent hover:text-accent-foreground focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-ring transition"
              >
                Сообщить о проблеме
              </a>
            </div>
          </div>
        </div>
      </div>
    </section>
  )
}
