export function AboutSection() {
  return (
  <section id="about" className="relative bg-background py-10 text-foreground sm:py-14">

  <div className="mx-auto max-w-6xl px-5 sm:px-6">
        {/* Header */}
        <div data-reveal className="flex flex-col gap-3">
          <div>
            <div className="inline-flex items-center gap-2 rounded-full border border-border/50 bg-secondary px-3.5 py-1.5 text-[11px] text-secondary-foreground">
              <span className="h-2 w-2 rounded-full bg-primary" />
              О платформе Short&amp;Long
            </div>

            <h2 className="mt-3 max-w-3xl text-2xl font-extrabold tracking-tight sm:text-3xl">
              Наш приоритет - ваше время и наименьший риск
            </h2>

          </div>
        </div>

        {/* Content grid */}
  <div className="mt-6 grid grid-cols-1 gap-4 lg:grid-cols-12">
          {/* Left: big card */}
          <div className="lg:col-span-7">
            <div data-reveal className="relative overflow-hidden rounded-3xl border border-border/50 bg-card p-5 text-card-foreground sm:p-6">
              <div className="relative">
                <h3 className="text-base font-bold">
                  Как это работает?
                </h3>

                <p className="mt-2.5 text-sm leading-relaxed text-muted-foreground">
                  Алгоритм анализирует рынок и оповещает вас о хорошей возможности входа. Решение же остаётся за вами.
                </p>

                <div className="mt-4 grid grid-cols-1 gap-3 sm:grid-cols-2">
                  <MiniStat
                    title="Контроль"
                    value="Индивидуальность"
                    hint="Можно настроить под себя"
                  />
                  <MiniStat
                    title="Прозрачность"
                    value="Логика и уровни"
                    hint="Почему вход именно здесь"
                  />
                  <MiniStat
                    title="Контроль риска"
                    value="Встроено"
                    hint="Правила защиты депозита"
                  />
                  <MiniStat
                    title="Скорость"
                    value="Скринеры"
                    hint="Быстрое анализирование рынка"
                  />
                </div>

                <div className="mt-5 rounded-2xl border border-border/50 bg-card p-4">
                  <div className="flex items-start gap-3">
                    <div className="mt-0.5 grid h-8 w-8 place-items-center rounded-2xl border border-border/50 bg-secondary text-secondary-foreground">
                      <IconQuote />
                    </div>
                    <div>
                      <div className="text-sm font-semibold text-card-foreground">
                        Наша цель
                      </div>
                      <div className="mt-1 text-[11px] leading-relaxed text-muted-foreground">
                        Понятный и удобный инструмент для ускорения анализа рынка: что смотреть, как фильтровать, где риск.
                      </div>
                    </div>
                  </div>
                </div>
              </div>
            </div>
          </div>

          {/* Right: stacked cards */}
          <div className="lg:col-span-5">
            <div className="grid gap-4">
              <InfoCard
                title="Для кого платформа?"
                desc="Для тех, кто хочет торговать системно: видеть сетапы, фильтровать шум и понимать риск."
                items={[
                  "Новички — чтобы не теряться",
                  "Опытные — чтобы ускорить анализ",
                  "Команда — чтобы работать по правилам",
                ]}
                icon={<IconUsers />}
              />

              <InfoCard
                title="Почему это работает?"
                desc="Потому что решения опираются на данные и контекст, а не на эмоции."
                items={[
                  "Скринеры и фильтры",
                  "Уровни и зоны",
                  "Риск-менеджмент",
                ]}
                icon={<IconShield />}
              />
            </div>
          </div>
        </div>

        {/* bottom separator */}
  <div className="mt-8 h-px w-full bg-border/50" />
      </div>
    </section>
  )
}

function MiniStat(props: { title: string; value: string; hint: string }) {
  return (
    <div className="rounded-2xl border border-border/50 bg-card p-3">
      <div className="text-[11px] text-muted-foreground">{props.title}</div>
      <div className="mt-1 text-sm font-bold text-card-foreground">{props.value}</div>
      <div className="mt-1 text-[11px] text-muted-foreground">{props.hint}</div>
    </div>
  )
}

function InfoCard(props: {
  title: string
  desc: string
  items: string[]
  icon: React.ReactNode
}) {
  return (
    <div data-reveal className="group relative overflow-hidden rounded-3xl border border-border/50 bg-card p-5 text-card-foreground transition-colors hover:border-border-strong">
      <div className="relative">
        <div className="flex items-center gap-3">
          <div className="grid h-10 w-10 place-items-center rounded-2xl border border-border/50 bg-secondary text-secondary-foreground">
            {props.icon}
          </div>
          <div className="text-base font-bold">{props.title}</div>
        </div>

        <p className="mt-2.5 text-sm leading-relaxed text-muted-foreground">{props.desc}</p>

        <div className="mt-4 space-y-1.5">
          {props.items.map((it) => (
            <div key={it} className="flex items-start gap-2.5 text-sm text-muted-foreground">
              <span className="mt-1.5 h-2 w-2 rounded-full bg-primary" />
              <span className="leading-relaxed">{it}</span>
            </div>
          ))}
        </div>
      </div>
    </div>
  )
}

/* ===== Icons ===== */
function IconUsers() {
  return (
    <svg width="22" height="22" viewBox="0 0 24 24" fill="none">
      <path
        d="M16 11C17.6569 11 19 9.65685 19 8C19 6.34315 17.6569 5 16 5C14.3431 5 13 6.34315 13 8C13 9.65685 14.3431 11 16 11Z"
        stroke="currentColor"
        strokeWidth="2"
      />
      <path
        d="M8 11C9.65685 11 11 9.65685 11 8C11 6.34315 9.65685 5 8 5C6.34315 5 5 6.34315 5 8C5 9.65685 6.34315 11 8 11Z"
        stroke="currentColor"
        strokeWidth="2"
      />
      <path
        d="M2 19C2 16.7909 4.79086 15 8 15C11.2091 15 14 16.7909 14 19"
        stroke="currentColor"
        strokeWidth="2"
        strokeLinecap="round"
      />
      <path
        d="M10 19C10 16.7909 12.7909 15 16 15C19.2091 15 22 16.7909 22 19"
        stroke="currentColor"
        strokeWidth="2"
        strokeLinecap="round"
      />
    </svg>
  )
}

function IconShield() {
  return (
    <svg width="22" height="22" viewBox="0 0 24 24" fill="none">
      <path
        d="M12 2L20 6V12C20 17 16.5 21 12 22C7.5 21 4 17 4 12V6L12 2Z"
        stroke="currentColor"
        strokeWidth="2"
        strokeLinejoin="round"
      />
      <path
        d="M9 12L11 14L15.5 9.5"
        stroke="currentColor"
        strokeWidth="2"
        strokeLinecap="round"
        strokeLinejoin="round"
      />
    </svg>
  )
}

function IconQuote() {
  return (
    <svg width="22" height="22" viewBox="0 0 24 24" fill="none">
      <circle cx="11" cy="11" r="6" stroke="currentColor" strokeWidth="2" />
      <path
        d="M16.5 16.5L21 21"
        stroke="currentColor"
        strokeWidth="2"
        strokeLinecap="round"
      />
    </svg>
  )
}
