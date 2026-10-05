import { Link } from "react-router-dom"

export function HeroSection() {
  return (
    <section id="top" className="relative min-h-screen overflow-hidden text-media-foreground">
      {/* Background image (ТОЛЬКО ДЛЯ HERO) */}
      <div className="absolute inset-0 bg-[url('/images/hero-bg.jpeg')] bg-cover bg-top bg-no-repeat brightness-95 contrast-110" />
  <div className="pointer-events-none absolute inset-x-0 bottom-0 h-64 bg-gradient-to-b from-transparent via-emerald-950/15 to-emerald-950/25" />
  <div className="pointer-events-none absolute inset-x-0 bottom-0 h-48 bg-gradient-to-b from-transparent via-[#111b17]/55 to-[#0f1714]" />

      {/* Dark overlay + vignette */}
      <div className="absolute inset-0 bg-gradient-to-b from-emerald-950/35 via-black/45 to-black/70" />
      <div className="absolute inset-0 bg-[radial-gradient(ellipse_at_center,rgba(16,185,129,0.08)_0%,rgba(0,0,0,0.45)_70%,rgba(0,0,0,0.75)_100%)]" />

      {/* Content */}
      <div className="relative z-10">
        <main className="mx-auto max-w-6xl px-6">
          {/* ===== HERO ===== */}
          <section className="flex min-h-[calc(100vh-76px)] flex-col items-center justify-center text-center">
            <h1 data-reveal className="max-w-4xl text-balance text-4xl font-extrabold leading-tight sm:text-5xl md:text-6xl">
              Прибыльные сделки начинаются здесь
            </h1>

            <p data-reveal className="mt-6 max-w-2xl text-pretty text-base leading-relaxed text-media-muted-foreground sm:text-lg">
              С нашим сервисом вы сможете облегчить и ускорить анализ данных на любых финансовых биржах.
            </p>

            <div data-reveal className="mt-10">
              <Link
                to="/login"
                className="inline-flex items-center justify-center rounded-full px-10 py-4 text-sm font-semibold
                           bg-primary text-primary-foreground hover:bg-primary-hover transition
                           focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-ring"
              >
                Получить PRO на месяц
              </Link>
            </div>

            {/* Scroll hint */}
            <div className="mt-16 flex items-center justify-center">
              <a href="#about" aria-label="О платформе" className="hero-scroll-hint text-media-muted-foreground focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-ring">
                <svg
                  width="28"
                  height="28"
                  viewBox="0 0 24 24"
                  fill="none"
                  xmlns="http://www.w3.org/2000/svg"
                  aria-hidden="true"
                >
                  <path
                    d="M12 5V19"
                    stroke="currentColor"
                    strokeWidth="2"
                    strokeLinecap="round"
                  />
                  <path
                    d="M7 14L12 19L17 14"
                    stroke="currentColor"
                    strokeWidth="2"
                    strokeLinecap="round"
                    strokeLinejoin="round"
                  />
                </svg>
              </a>
            </div>
          </section>
        </main>
      </div>
    </section>
  )
}
