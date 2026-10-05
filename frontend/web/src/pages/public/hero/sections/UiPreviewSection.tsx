import { useEffect, useRef, useState } from "react"

export function UiPreviewSection() {

  const VIDEO_SRC = "/videos/screener-preview.mp4"

  const sectionRef = useRef<HTMLElement | null>(null)
  const videoRef = useRef<HTMLVideoElement | null>(null)
  const [isReady, setIsReady] = useState(false)

  useEffect(() => {
    const sectionEl = sectionRef.current
    const videoEl = videoRef.current

    if (!sectionEl || !videoEl) return

    const onLoadedMeta = () => setIsReady(true)
    videoEl.addEventListener("loadedmetadata", onLoadedMeta)

    const observer = new IntersectionObserver(
      async (entries) => {
        const entry = entries[0]
        if (!videoEl) return

        if (entry.isIntersecting && entry.intersectionRatio >= 0.45) {
          try {
            if (!isReady) return
            await videoEl.play()
          } catch {
            // autoplay может быть заблокирован браузером
          }
        } else {
          if (!videoEl.paused) videoEl.pause()
        }
      },
      {
        threshold: [0, 0.25, 0.45, 0.65, 0.85],
      }
    )

    observer.observe(sectionEl)

    return () => {
      videoEl.removeEventListener("loadedmetadata", onLoadedMeta)
      observer.disconnect()
    }
  }, [isReady])

  return (
    <section ref={sectionRef} id="ui-preview" className="relative bg-background py-16 text-foreground sm:py-20">
      {/* subtle separator */}
      <div className="pointer-events-none absolute inset-x-0 top-0 h-px bg-border/40" />

      <div className="mx-auto max-w-6xl px-6">
        {/* Header */}
        <div className="flex flex-col gap-6 md:flex-row md:items-end md:justify-between">
          <div>
            <div className="inline-flex items-center gap-2 rounded-full border border-border/60 bg-secondary px-4 py-2 text-xs text-secondary-foreground">
              <span className="h-2 w-2 rounded-full bg-primary" />
              UI Preview
            </div>

            <h2 className="mt-5 max-w-3xl text-3xl font-extrabold tracking-tight sm:text-4xl">
              Как выглядит наш скринер внутри платформы
            </h2>

            <p className="mt-4 max-w-2xl text-muted-foreground leading-relaxed">
              Показаны таблица кандидатов, фильтры, и быстрый переход к графику. Видео запускается
              автоматически, когда ты доходишь до секции.
            </p>
          </div>

          <div className="flex flex-col gap-3 sm:flex-row">
            <a
              href="#screens"
              className="rounded-full px-8 py-3 text-sm font-semibold text-center
                         border border-border bg-secondary text-secondary-foreground hover:bg-accent transition-colors focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-ring"
            >
              Перейти к скринерам
            </a>

            <a
              href="#about"
              className="rounded-full px-8 py-3 text-sm font-semibold text-center
                         bg-primary text-primary-foreground hover:bg-primary-hover transition-colors focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-ring"
            >
              Узнать о платформе
            </a>
          </div>
        </div>

        {/* Video Card */}
        <div className="mt-12">
          <div className="group relative overflow-hidden rounded-3xl border border-border/60 bg-card text-card-foreground">

            <div className="relative p-4 sm:p-6">
              {/* Video container */}
              <div className="relative overflow-hidden rounded-2xl border border-border/60 bg-background">
                {/* Top bar (like app window) */}
                <div className="flex items-center gap-2 border-b border-border/60 bg-surface-raised px-4 py-3">
                  <span className="h-2.5 w-2.5 rounded-full bg-muted-foreground/40" />
                  <span className="h-2.5 w-2.5 rounded-full bg-muted-foreground/40" />
                  <span className="h-2.5 w-2.5 rounded-full bg-muted-foreground/40" />
                  <div className="ml-3 text-xs text-muted-foreground">
                    Short&amp;Long — Screener Preview
                  </div>
                </div>

                <video
                  ref={videoRef}
                  className="aspect-video w-full object-cover"
                  src={VIDEO_SRC}
                  muted
                  playsInline
                  loop
                  preload="metadata"
                  controls={false}
                />

              </div>

              {/* Bottom info strip */}
              <div className="mt-6 grid gap-4 rounded-2xl border border-border/60 bg-surface-raised p-5 sm:grid-cols-3">
                <Meta
                  title="Фильтры"
                  desc="Тренд, объём, импульс, уровни"
                />
                <Meta
                  title="Скорость"
                  desc="Список кандидатов за секунды"
                />
                <Meta
                  title="Алерты"
                  desc="Уведомления на важные события"
                />
              </div>

              <div className="mt-6 text-xs text-muted-foreground">
                Примечание: на мобильных autoplay работает только с выключенным звуком (muted).
              </div>
            </div>
          </div>
        </div>

        {/* bottom separator */}
        <div className="mt-16 h-px w-full bg-border/40" />
      </div>
    </section>
  )
}

function Meta(props: { title: string; desc: string }) {
  return (
    <div className="rounded-2xl border border-border/60 bg-card p-4">
      <div className="text-sm font-semibold text-card-foreground">{props.title}</div>
      <div className="mt-1 text-sm text-muted-foreground">{props.desc}</div>
    </div>
  )
}
