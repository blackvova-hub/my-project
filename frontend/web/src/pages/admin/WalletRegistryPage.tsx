import { useCallback, useEffect, useState } from "react";
import { http } from "../../shared/api/http";

type Wallet = {
  chain: string;
  address: string;
  entityName: string;
  entityType: string;
  status: string;
  largestTxUsd: number;
  knownBalanceUsd: number;
  significantTxCount: number;
  explorerUrl: string;
  arkhamUrl: string;
  notes: string;
  verificationSourceUrl: string;
};

const emptyWallet: Wallet = {
  chain: "ethereum",
  address: "",
  entityName: "",
  entityType: "unknown",
  status: "probable",
  largestTxUsd: 0,
  knownBalanceUsd: 0,
  significantTxCount: 0,
  explorerUrl: "",
  arkhamUrl: "",
  notes: "",
  verificationSourceUrl: "",
};

export default function WalletRegistryPage() {
  const [items, setItems] = useState<Wallet[]>([]);
  const [draft, setDraft] = useState<Wallet>(emptyWallet);
  const [status, setStatus] = useState("unknown");
  const [q, setQ] = useState("");
  const [appliedQuery, setAppliedQuery] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");

  const load = useCallback(async () => {
    const params = new URLSearchParams({ status, q: appliedQuery });
    const response = await http<{ items: Wallet[] }>(`/api/admin/wallet-registry?${params}`);
    setItems(response.items || []);
  }, [appliedQuery, status]);

  useEffect(() => {
    void load();
  }, [load]);

  const runSearch = () => {
    if (q === appliedQuery) {
      void load();
      return;
    }
    setAppliedQuery(q);
  };

  const create = async () => {
    setBusy(true);
    setError("");
    try {
      await http("/api/admin/wallet-registry", {
        method: "POST",
        body: JSON.stringify(draft),
      });
      setDraft(emptyWallet);
      await load();
    } catch {
      setError("Не удалось добавить адрес. Проверь сеть, адрес и название.");
    } finally {
      setBusy(false);
    }
  };

  const update = async (wallet: Wallet) => {
    setBusy(true);
    setError("");
    try {
      await http(`/api/admin/wallet-registry/${wallet.chain}/${wallet.address}`, {
        method: "PATCH",
        body: JSON.stringify(wallet),
      });
      await load();
    } catch {
      setError("Не удалось сохранить метку кошелька.");
    } finally {
      setBusy(false);
    }
  };

  const changeItem = (index: number, patch: Partial<Wallet>) =>
    setItems((current) => current.map((wallet, itemIndex) => (itemIndex === index ? { ...wallet, ...patch } : wallet)));

  return (
    <main className="mx-auto max-w-7xl bg-background p-6 text-foreground">
      <div className="mb-6 flex items-center justify-between">
        <div>
          <h1 className="text-2xl font-semibold">Wallet Registry</h1>
          <p className="text-sm text-muted-foreground">Неизвестные адреса и ручная доказательная атрибуция</p>
        </div>
        <a className="text-sm text-primary hover:underline focus-visible:outline-2 focus-visible:outline-ring" href="/admin">Назад в админку</a>
      </div>

      <section className="mb-6 rounded-lg border border-border bg-card p-4 text-card-foreground">
        <h2 className="mb-3 text-lg font-medium">Добавить известный кошелёк</h2>
        <div className="grid gap-3 md:grid-cols-2 lg:grid-cols-4">
          <select className="rounded border border-input bg-background px-3 py-2 text-foreground outline-none focus:border-ring focus-visible:ring-2 focus-visible:ring-ring/30" value={draft.chain} onChange={(event) => setDraft({ ...draft, chain: event.target.value })}>
            <option value="ethereum">Ethereum</option>
            <option value="bitcoin">Bitcoin</option>
            <option value="solana">Solana</option>
            <option value="tron">Tron</option>
          </select>
          <input className="rounded border border-input bg-background px-3 py-2 text-foreground outline-none focus:border-ring focus-visible:ring-2 focus-visible:ring-ring/30" value={draft.address} onChange={(event) => setDraft({ ...draft, address: event.target.value })} placeholder="Адрес кошелька" />
          <input className="rounded border border-input bg-background px-3 py-2 text-foreground outline-none focus:border-ring focus-visible:ring-2 focus-visible:ring-ring/30" value={draft.entityName} onChange={(event) => setDraft({ ...draft, entityName: event.target.value })} placeholder="Название: US Government" />
          <input className="rounded border border-input bg-background px-3 py-2 text-foreground outline-none focus:border-ring focus-visible:ring-2 focus-visible:ring-ring/30" value={draft.entityType} onChange={(event) => setDraft({ ...draft, entityType: event.target.value })} placeholder="Тип: government / exchange" />
          <input className="rounded border border-input bg-background px-3 py-2 text-foreground outline-none focus:border-ring focus-visible:ring-2 focus-visible:ring-ring/30" type="number" min="0" value={draft.knownBalanceUsd} onChange={(event) => setDraft({ ...draft, knownBalanceUsd: Math.max(0, Number(event.target.value) || 0) })} placeholder="Известный баланс, USD" />
          <select className="rounded border border-input bg-background px-3 py-2 text-foreground outline-none focus:border-ring focus-visible:ring-2 focus-visible:ring-ring/30" value={draft.status} onChange={(event) => setDraft({ ...draft, status: event.target.value })}>
            <option value="probable">Probable</option>
            <option value="verified">Verified</option>
            <option value="unknown">Unknown</option>
            <option value="ignore">Ignore</option>
          </select>
          <input className="rounded border border-input bg-background px-3 py-2 text-foreground outline-none focus:border-ring focus-visible:ring-2 focus-visible:ring-ring/30 lg:col-span-2" value={draft.verificationSourceUrl} onChange={(event) => setDraft({ ...draft, verificationSourceUrl: event.target.value })} placeholder="URL источника подтверждения" />
          <button disabled={busy || !draft.address.trim() || !draft.entityName.trim()} className="rounded bg-primary px-4 py-2 text-primary-foreground hover:bg-primary-hover focus-visible:outline-2 focus-visible:outline-ring disabled:opacity-50" onClick={() => void create()}>Добавить</button>
        </div>
      </section>

      {error && <p className="mb-4 rounded border border-destructive/30 bg-destructive/10 p-3 text-sm text-destructive">{error}</p>}

      <div className="mb-4 flex gap-2">
        <input className="min-w-0 flex-1 rounded border border-input bg-background px-3 py-2 text-foreground outline-none focus:border-ring focus-visible:ring-2 focus-visible:ring-ring/30 sm:flex-none" value={q} onChange={(event) => setQ(event.target.value)} onKeyDown={(event) => { if (event.key === "Enter") runSearch(); }} placeholder="Адрес или entity" />
        <select className="rounded border border-input bg-background px-3 py-2 text-foreground outline-none focus:border-ring focus-visible:ring-2 focus-visible:ring-ring/30" value={status} onChange={(event) => setStatus(event.target.value)}>
          <option value="">Все</option><option value="unknown">Unknown</option><option value="probable">Probable</option><option value="verified">Verified</option><option value="ignore">Ignore</option>
        </select>
        <button className="rounded border border-border bg-secondary px-4 py-2 text-secondary-foreground hover:bg-accent focus-visible:outline-2 focus-visible:outline-ring" onClick={runSearch}>Поиск</button>
      </div>

      <div className="overflow-x-auto rounded border border-border bg-card text-card-foreground">
        <table className="min-w-full text-sm">
          <thead className="bg-secondary"><tr><th className="p-2 text-left">Адрес</th><th className="p-2 text-left">Название</th><th className="p-2 text-left">Тип</th><th className="p-2 text-left">Статус</th><th className="p-2 text-right">Known balance USD</th><th className="p-2 text-right">Largest USD</th><th className="p-2">Ссылки</th><th className="p-2">Сохранить</th></tr></thead>
          <tbody>{items.map((wallet, index) => <tr key={`${wallet.chain}:${wallet.address}`} className="border-t border-border hover:bg-secondary">
            <td className="p-2"><div>{wallet.chain}</div><code>{wallet.address}</code></td>
            <td className="p-2"><input className="w-44 rounded border border-input bg-background px-2 py-1 text-foreground outline-none focus:border-ring focus-visible:ring-2 focus-visible:ring-ring/30" value={wallet.entityName} onChange={(event) => changeItem(index, { entityName: event.target.value })} /></td>
            <td className="p-2"><input className="w-36 rounded border border-input bg-background px-2 py-1 text-foreground outline-none focus:border-ring focus-visible:ring-2 focus-visible:ring-ring/30" value={wallet.entityType} onChange={(event) => changeItem(index, { entityType: event.target.value })} /></td>
            <td className="p-2"><select className="rounded border border-input bg-background text-foreground outline-none focus:border-ring focus-visible:ring-2 focus-visible:ring-ring/30" value={wallet.status} onChange={(event) => changeItem(index, { status: event.target.value })}><option>unknown</option><option>probable</option><option>verified</option><option>ignore</option></select></td>
            <td className="p-2"><input className="w-36 rounded border border-input bg-background px-2 py-1 text-right text-foreground outline-none focus:border-ring focus-visible:ring-2 focus-visible:ring-ring/30" type="number" min="0" value={wallet.knownBalanceUsd} onChange={(event) => changeItem(index, { knownBalanceUsd: Math.max(0, Number(event.target.value) || 0) })} /></td>
            <td className="p-2 text-right">{Math.round(wallet.largestTxUsd).toLocaleString()}</td>
            <td className="p-2"><a className="mr-2 text-primary hover:underline focus-visible:outline-2 focus-visible:outline-ring" href={wallet.explorerUrl} target="_blank" rel="noreferrer">Explorer</a><a className="text-primary hover:underline focus-visible:outline-2 focus-visible:outline-ring" href={wallet.arkhamUrl} target="_blank" rel="noreferrer">Arkham</a></td>
            <td className="p-2"><button disabled={busy} className="rounded bg-primary px-3 py-1 text-primary-foreground hover:bg-primary-hover focus-visible:outline-2 focus-visible:outline-ring disabled:opacity-50" onClick={() => void update(wallet)}>Сохранить</button></td>
          </tr>)}</tbody>
        </table>
      </div>
    </main>
  );
}
