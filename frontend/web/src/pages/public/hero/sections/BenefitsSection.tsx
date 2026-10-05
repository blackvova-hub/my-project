import type { ReactNode } from "react"

export function BenefitsSection() {
  return (
    <section id="benefits" className="bg-background text-foreground">
      <div className="mx-auto max-w-6xl px-6 py-16">
        <div data-reveal className="flex flex-col items-start justify-between gap-6 md:flex-row md:items-end">
          <div>
            <h2 className="text-3xl font-extrabold tracking-tight sm:text-4xl">
              Что вы получаете?
            </h2>
            <p className="mt-4 max-w-2xl text-muted-foreground">
              Не просто сигналы. Вы получаете систему: анализ, уровни, контроль риска и поддержку
              принятия решений.
            </p>
          </div>

          <div className="flex gap-3">
          </div>
        </div>

        <div className="mt-10 grid grid-cols-1 gap-6 sm:grid-cols-2 lg:grid-cols-3">
          <BenefitCard
            title="Сигналы с уровнем уверенности"
            desc="Каждый сигнал содержит уровни входа, стопа, тейков и краткое объяснение логики."
            icon={<IconLightning />}
          />
          <BenefitCard
            title="Скринеры и алерты"
            desc="Фильтры по тренду, объёму, импульсу, уровням. Не пропускаете лучшие сетапы."
            icon={<IconSearch />}
          />
          <BenefitCard
            title="Графики и зоны"
            desc="Разметка уровней: поддержки или сопротивления, ликвидность, ключевые зоны входа."
            icon={<IconChart />}
          />
          <BenefitCard
            title="Риск-менеджмент"
            desc="Подсказки по размеру позиции, правила защиты депозита и контроль просадки."
            icon={<IconShield />}
          />
          <BenefitCard
            title="Обучение и разборы"
            desc="Короткие уроки и примеры сделок, чтобы вы понимали рынок, а не просто копировали."
            icon={<IconGraduation />}
          />
          <BenefitCard
            title="Сообщество и поддержка"
            desc="Обсуждения, разборы ситуаций, ответы на вопросы и рост вместе с командой."
            icon={<IconUsers />}
          />
        </div>

      </div>
    </section>
  )
}

function BenefitCard(props: {
  title: string
  desc: string
  icon: ReactNode
}) {
  return (
    <div data-reveal className="group relative overflow-hidden rounded-3xl border border-border/50 bg-card p-6 text-card-foreground transition-colors hover:border-border-strong">
      <div className="relative">
        <div className="flex items-center gap-3">
          <div className="grid h-11 w-11 place-items-center rounded-2xl border border-border/50 bg-secondary text-secondary-foreground">
            {props.icon}
          </div>
          <div className="text-lg font-bold">{props.title}</div>
        </div>

        <div className="mt-3 text-sm leading-relaxed text-muted-foreground">{props.desc}</div>

        <div className="mt-6 h-px w-full bg-border/50" />

        <div className="mt-4 flex items-center justify-between text-sm text-muted-foreground">
          <span className="inline-flex items-center gap-2">
            <span className="h-2 w-2 rounded-full bg-primary" />
            Включено
          </span>
          <span className="text-muted-foreground transition group-hover:text-card-foreground">
            Подробнее →
          </span>
        </div>
      </div>
    </div>
  )
}

/* ===== Icons (inline SVG) ===== */
function IconLightning() {
  return (
    <svg width="22" height="22" viewBox="0 0 24 24" fill="none">
      <path
        d="M13 2L4 14H11L10 22L20 10H13L13 2Z"
        stroke="currentColor"
        strokeWidth="2"
        strokeLinejoin="round"
      />
    </svg>
  )
}

function IconSearch() {
  return (
    <svg width="22" height="22" viewBox="0 0 24 24" fill="none">
      <path
        d="M10.5 18C14.6421 18 18 14.6421 18 10.5C18 6.35786 14.6421 3 10.5 3C6.35786 3 3 6.35786 3 10.5C3 14.6421 6.35786 18 10.5 18Z"
        stroke="currentColor"
        strokeWidth="2"
      />
      <path
        d="M16 16L21 21"
        stroke="currentColor"
        strokeWidth="2"
        strokeLinecap="round"
      />
    </svg>
  )
}

function IconChart() {
  return (
    <svg width="22" height="22" viewBox="0 0 24 24" fill="none">
      <path d="M4 19V5" stroke="currentColor" strokeWidth="2" strokeLinecap="round" />
      <path d="M4 19H20" stroke="currentColor" strokeWidth="2" strokeLinecap="round" />
      <path
        d="M7 15L11 11L14 14L19 8"
        stroke="currentColor"
        strokeWidth="2"
        strokeLinecap="round"
        strokeLinejoin="round"
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

function IconGraduation() {
  return (
    <svg width="22" height="22" viewBox="0 0 24 24" fill="none">
      <path
        d="M12 3L2 8L12 13L22 8L12 3Z"
        stroke="currentColor"
        strokeWidth="2"
        strokeLinejoin="round"
      />
      <path
        d="M6 10.5V16C6 16 8 18 12 18C16 18 18 16 18 16V10.5"
        stroke="currentColor"
        strokeWidth="2"
        strokeLinejoin="round"
      />
      <path d="M22 8V16" stroke="currentColor" strokeWidth="2" strokeLinecap="round" />
    </svg>
  )
}

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
