export function TeamSection() {
  return (
    <section id="team" className="relative bg-background py-12 sm:py-14">
      <div className="pointer-events-none absolute inset-x-0 top-0 h-px bg-border/30" />

      <div className="mx-auto max-w-6xl px-6">
        <div data-reveal className="flex flex-col gap-4">
          <h2 className="max-w-3xl text-2xl font-extrabold tracking-tight sm:text-3xl">
            Мы - небольшая команда из двух разработчиков
          </h2>
          <p className="max-w-3xl text-sm leading-relaxed text-muted-foreground">
            Платформа - результат долгой проектировки, тестирования и системной
            разработки. Мы сами проходим весь цикл: от аналитики и прототипов до релизов и
            поддержки.
          </p>
        </div>

        <div data-reveal-scope className="mt-8 grid gap-6 md:grid-cols-2">
          <div className="relative overflow-hidden rounded-3xl border border-border/50 bg-card p-6">
            <div className="relative">
              <div className="text-sm font-semibold text-card-foreground/80">Состав команды</div>
              <div className="mt-2 text-lg font-bold text-card-foreground">2 разработчика</div>
              <p className="mt-2 text-sm text-muted-foreground">
                Закрываем фронтенд, бэкенд, дизайн, аналитику и инфраструктуру - поэтому
                фокусируемся на качестве и устойчивости.
              </p>
            </div>
          </div>

          <div className="relative overflow-hidden rounded-3xl border border-border/50 bg-card p-6">
            <div className="relative">
              <div className="text-sm font-semibold text-card-foreground/80">Проектировка и разработка</div>
              <div className="mt-2 text-lg font-bold text-card-foreground">Долгий цикл зрелости</div>
              <p className="mt-2 text-sm text-muted-foreground">
                Мы не спешим ради громких релизов. Каждая функция проходит проектировку,
                проверку на рынке и доводится до стабильного состояния.
              </p>
            </div>
          </div>
        </div>

      </div>
    </section>
  )
}
