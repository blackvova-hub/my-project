import { Link } from "react-router-dom";
import { MARKETPLACE_ITEMS, formatMarketplacePrice } from "../../exchange/marketplaceData";

export function CommunityWidget() {
  const communities = MARKETPLACE_ITEMS.filter((item) => item.type === "community").slice(0, 3);

  return (
    <section className="rounded-2xl border border-border bg-card p-5 md:p-6">
      <div className="flex items-start justify-between gap-3">
        <div>
          <h2 className="text-lg font-semibold tracking-tight">Community</h2>
          <p className="mt-1 text-sm text-muted-foreground">Закрытые клубы, чаты и совместные разборы.</p>
        </div>

        <Link
          to="/exchange"
          className="rounded-xl border border-border bg-card px-3 py-2 text-xs font-semibold hover:bg-secondary transition"
        >
          Все сообщества
        </Link>
      </div>

      <div className="mt-4 grid gap-3">
        {communities.map((item) => (
          <div key={item.id} className="rounded-2xl border border-border bg-background p-4">
            <div className="flex items-start justify-between gap-3">
              <div>
                <div className="text-xs text-muted-foreground">{item.category}</div>
                <div className="mt-1 text-sm font-semibold text-foreground">{item.title}</div>
                <div className="mt-1 text-xs text-muted-foreground">Автор: {item.author}</div>
              </div>
              <div className="rounded-xl border border-border bg-card px-3 py-2 text-xs font-semibold text-foreground">
                {formatMarketplacePrice(item.price, item.currency)}
              </div>
            </div>
            <p className="mt-3 text-xs text-muted-foreground">{item.description}</p>
            <div className="mt-3 flex items-center justify-between text-xs text-muted-foreground">
              <span>⭐ {item.rating}</span>
              <span>{item.users} участников</span>
            </div>
          </div>
        ))}
      </div>
    </section>
  );
}
