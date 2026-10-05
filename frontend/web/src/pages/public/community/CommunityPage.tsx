import { Link } from "react-router-dom";

export default function CommunityPage() {
  return (
    <div className="mx-auto max-w-6xl px-4 py-10 md:py-14">
      <div className="rounded-3xl border border-border/60 bg-card p-8 text-card-foreground md:p-10">
        <div className="flex flex-col items-start justify-between gap-6 md:flex-row">
          <div>
            <div className="inline-flex items-center rounded-full border border-border/60 bg-secondary px-3 py-1 text-xs text-secondary-foreground">
              Комьюнити
            </div>
            <h1 className="mt-4 text-3xl md:text-4xl font-extrabold tracking-tight">
              Пространство сообщества
            </h1>
            <p className="mt-3 max-w-2xl text-sm text-muted-foreground">
              Выбери раздел: каналы для подписок и ленту для обновлений.
            </p>
          </div>
        </div>

        <div className="mt-8 grid gap-4 md:grid-cols-2">
          <Link
            to="/community/channels"
            className="rounded-2xl border border-border/60 bg-secondary/40 p-6 transition-colors hover:border-border hover:bg-accent focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-ring"
          >
            <div className="text-sm font-semibold text-card-foreground">Каналы</div>
            <div className="mt-2 text-sm text-muted-foreground">
              Список каналов, подборки и теги.
            </div>
          </Link>
          <Link
            to="/community/feed"
            className="rounded-2xl border border-border/60 bg-secondary/40 p-6 transition-colors hover:border-border hover:bg-accent focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-ring"
          >
            <div className="text-sm font-semibold text-card-foreground">Лента</div>
            <div className="mt-2 text-sm text-muted-foreground">
              Обновления, обсуждения и активность сообщества.
            </div>
          </Link>
        </div>
      </div>
    </div>
  );
}
