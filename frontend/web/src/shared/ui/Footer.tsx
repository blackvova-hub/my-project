import { Link } from "react-router-dom"
import { useAuth } from "../auth/AuthContext"

export function Footer() {
  const { isAuth } = useAuth();
  return (
    <footer className="relative border-t border-border bg-background pt-16 pb-10 text-foreground">

      <div className="mx-auto max-w-6xl px-6">
        {/* Top */}
        <div className="grid gap-10 md:grid-cols-12">
          <div className="md:col-span-5">
            <div className="text-xl font-extrabold tracking-tight">Short&amp;Long</div>
            <p className="mt-4 max-w-md text-sm leading-relaxed text-muted-foreground">
              Аналитическая платформа для трейдинга: скринеры, фильтры, уровни и риск-контроль.
              Мы строим систему, которая помогает принимать решения быстрее и спокойнее.
            </p>

            {!isAuth ? (
              <div className="mt-6 flex flex-wrap gap-3">
                <Link
                  to="/login"
                  className="rounded-full bg-primary px-7 py-3 text-sm font-semibold text-primary-foreground transition hover:bg-primary-hover focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-ring"
                >
                  Получить доступ
                </Link>

                <Link
                  to="/#news"
                  className="rounded-full border border-border bg-secondary px-7 py-3 text-sm font-semibold text-secondary-foreground transition hover:bg-accent hover:text-accent-foreground focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-ring"
                >
                  Новости
                </Link>
              </div>
            ) : null}
          </div>

          <div className="md:col-span-7">
            <div className="grid gap-8 sm:grid-cols-3">
              <FooterCol title="Платформа">
                <FooterLink to="/#screens" label="Скринеры" />
                <FooterLink to="/#ui-preview" label="UI Preview" />
                <FooterLink to="/#benefits" label="Что внутри" />
                <FooterLink to="/#about" label="О нас" />
              </FooterCol>

              <FooterCol title="Документы">
                <FooterLink to="/terms" label="Условия" />
                <FooterLink to="/privacy" label="Конфиденциальность" />
                <FooterLink to="/cookies" label="Cookies" />
              </FooterCol>

              <FooterCol title="Контакты">
                <ExternalLink href="https://t.me/short_long_crypto" label="Telegram" />
                <ExternalLink href="https://www.instagram.com/short_and_long_off" label="Instagram" />
                <ExternalLink href="https://www.tiktok.com/@shortandlongoff" label="TikTok" />
                <ExternalLink href="https://www.youtube.com/@short-and-long_OFF" label="YouTube" />
              </FooterCol>
            </div>
          </div>
        </div>

        {/* Bottom */}
        <div className="mt-14 border-t border-border pt-8 text-xs text-muted-foreground">
          <div>© {new Date().getFullYear()} Short&amp;Long. All rights reserved.</div>
        </div>
      </div>
    </footer>
  )
}

function FooterCol({ title, children }: { title: string; children: React.ReactNode }) {
  return (
    <div>
      <div className="text-sm font-semibold text-card-foreground">
        {title}
      </div>
      <div className="mt-4 flex flex-col gap-3">{children}</div>
    </div>
  )
}

function FooterLink({ to, label }: { to: string; label: string }) {
  return (
    <Link
      to={to}
      className="text-sm text-muted-foreground transition hover:text-primary focus-visible:outline-2 focus-visible:outline-ring"
    >
      {label}
    </Link>
  )
}

function ExternalLink({ href, label }: { href: string; label: string }) {
  return (
    <a
      href={href}
      target="_blank"
      rel="noreferrer"
      className="text-sm text-muted-foreground transition hover:text-primary focus-visible:outline-2 focus-visible:outline-ring"
    >
      {label}
    </a>
  )
}
