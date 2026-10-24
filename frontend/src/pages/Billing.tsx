import { useState } from "react";
import { useInfiniteQuery, useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { api, dateTime, money, type List } from "../api";
import { useHasRole } from "../auth";
import type { Invoice } from "../types";
import { Badge, Card, ErrorBox, Field, Loading, Page, Query, SuccessBox, Toggle, label } from "../ui";

export default function Billing() {
  const isReception = useHasRole("reception");
  const [status, setStatus] = useState("open");
  const [selected, setSelected] = useState<number | null>(null);
  const list = useInfiniteQuery({
    queryKey: ["invoices", status],
    initialPageParam: "",
    queryFn: ({ pageParam }) => api<List<Invoice>>(`/api/invoices?limit=25&status=${status}&cursor=${pageParam}`),
    getNextPageParam: (l) => l.next_cursor ?? undefined,
  });
  const items = list.data?.pages.flatMap((p) => p.items) ?? [];
  return (
    <Page
      title={isReception ? "Fatura ve tahsilat" : "Faturalarım"}
      actions={
        <select value={status} onChange={(e) => setStatus(e.target.value)} style={{ width: 160 }}>
          <option value="open">Açık</option><option value="paid">Ödenmiş</option><option value="">Tümü</option>
        </select>
      }
    >
      <div className="stack">
        {isReception && (
          <div className="grid grid-2">
            <Toggle label="Yeni fatura"><InvoiceForm onCreated={setSelected} /></Toggle>
            <Toggle label="Aşı paketi sat"><PackageForm onCreated={setSelected} /></Toggle>
          </div>
        )}
        <div className="grid grid-2">
          <Card title="Faturalar">
            {list.isPending ? <Loading /> : list.isError ? <ErrorBox error={list.error} /> : items.length === 0 ? (
              <div className="state state-empty">Fatura yok.</div>
            ) : (
              <>
                <div className="table-wrap">
                  <table>
                    <thead><tr><th>#</th><th>Hasta / sahip</th><th>Durum</th><th className="num">Kalan</th></tr></thead>
                    <tbody>
                      {items.map((i) => (
                        <tr key={i.id} className="clickable" onClick={() => setSelected(i.id)}>
                          <td className="muted">{i.id}</td>
                          <td>{i.animal_name} <div className="small muted">{i.owner_name}</div></td>
                          <td><Badge value={i.status} /></td>
                          <td className="num">{money(i.due_kurus)}</td>
                        </tr>
                      ))}
                    </tbody>
                  </table>
                </div>
                {list.hasNextPage && (
                  <button className="btn btn-sm" style={{ marginTop: 8 }} onClick={() => list.fetchNextPage()} disabled={list.isFetchingNextPage}>Daha fazla</button>
                )}
              </>
            )}
          </Card>
          {selected ? <InvoiceDetail id={selected} canPay={isReception} /> : <Card><div className="state state-empty">Ayrıntı için bir fatura seçin.</div></Card>}
        </div>
      </div>
    </Page>
  );
}

function InvoiceDetail({ id, canPay }: { id: number; canPay: boolean }) {
  const qc = useQueryClient();
  const q = useQuery({ queryKey: ["invoice", id], queryFn: () => api<Invoice>(`/api/invoices/${id}`) });
  const [amount, setAmount] = useState("");
  const [method, setMethod] = useState("card");
  const pay = useMutation({
    mutationFn: () => api(`/api/invoices/${id}/payments`, { method: "POST", body: { amount_kurus: Math.round(Number(amount.replace(",", ".")) * 100), method } }),
    onSuccess: () => {
      setAmount("");
      qc.invalidateQueries({ queryKey: ["invoice", id] });
      qc.invalidateQueries({ queryKey: ["invoices"] });
    },
  });
  return (
    <Card title={`Fatura #${id}`}>
      <Query q={q}>
        {(i) => (
          <div className="stack">
            <div className="small">
              <strong>{i.owner_name}</strong> adına ({label(i.billing_rule)}) · {i.animal_name} · {dateTime(i.created_at)}
            </div>
            <table>
              <tbody>
                {i.lines?.map((l, k) => (
                  <tr key={k}><td>{l.description} × {l.quantity}</td><td className="num">{money(l.line_total_kurus)}</td></tr>
                ))}
                <tr><td><strong>Toplam</strong></td><td className="num"><strong>{money(i.total_kurus)}</strong></td></tr>
                {i.payments?.map((p) => (
                  <tr key={p.id}><td className="muted">Ödeme · {p.method} · {dateTime(p.created_at)}</td><td className="num">−{money(p.amount_kurus)}</td></tr>
                ))}
                <tr><td><strong>Kalan</strong></td><td className="num"><strong>{money(i.due_kurus)}</strong></td></tr>
              </tbody>
            </table>
            {canPay && i.status === "open" && (
              <form className="form-row" onSubmit={(e) => { e.preventDefault(); pay.mutate(); }}>
                <Field label="Tutar (TL)"><input inputMode="decimal" value={amount} onChange={(e) => setAmount(e.target.value)} placeholder={(i.due_kurus / 100).toFixed(2)} required /></Field>
                <Field label="Yöntem">
                  <select value={method} onChange={(e) => setMethod(e.target.value)}>
                    <option value="card">Kart</option><option value="cash">Nakit</option><option value="transfer">Havale</option>
                  </select>
                </Field>
                <div className="field" style={{ justifyContent: "flex-end" }}><button className="btn btn-primary" disabled={pay.isPending}>Tahsil et</button></div>
              </form>
            )}
            <ErrorBox error={pay.error} />
          </div>
        )}
      </Query>
    </Card>
  );
}

function InvoiceForm({ onCreated }: { onCreated: (id: number) => void }) {
  const qc = useQueryClient();
  const [f, setF] = useState({ animal_id: "", owner_id: "", description: "Muayene ücreti", amount: "750" });
  const m = useMutation({
    mutationFn: () =>
      api<Invoice>("/api/invoices", {
        method: "POST",
        body: {
          animal_id: Number(f.animal_id), owner_id: f.owner_id ? Number(f.owner_id) : null,
          lines: [{ description: f.description, quantity: 1, unit_price_kurus: Math.round(Number(f.amount.replace(",", ".")) * 100) }],
        },
      }),
    onSuccess: (inv) => {
      qc.invalidateQueries({ queryKey: ["invoices"] });
      onCreated(inv.id);
    },
  });
  const set = (k: keyof typeof f) => (e: { target: { value: string } }) => setF({ ...f, [k]: e.target.value });
  return (
    <form className="form" onSubmit={(e) => { e.preventDefault(); m.mutate(); }}>
      <div className="form-row">
        <Field label="Hasta no"><input type="number" value={f.animal_id} onChange={set("animal_id")} required /></Field>
        <Field label="Fatura sahibi no" hint="Boşsa kural uygulanır"><input type="number" value={f.owner_id} onChange={set("owner_id")} /></Field>
        <Field label="Açıklama"><input value={f.description} onChange={set("description")} required /></Field>
        <Field label="Tutar (TL)"><input inputMode="decimal" value={f.amount} onChange={set("amount")} required /></Field>
      </div>
      <ErrorBox error={m.error} />
      {m.data && <SuccessBox>Fatura #{m.data.id}: {m.data.owner_name} ({label(m.data.billing_rule)}).</SuccessBox>}
      <button className="btn btn-primary" disabled={m.isPending}>Fatura kes</button>
    </form>
  );
}

function PackageForm({ onCreated }: { onCreated: (id: number) => void }) {
  const qc = useQueryClient();
  const pk = useQuery({ queryKey: ["packages-all"], queryFn: () => api<{ items: { id: number; name: string; sessions: number; price_kurus: number }[] }>("/api/packages") });
  const [f, setF] = useState({ package_id: "", animal_id: "" });
  const m = useMutation({
    mutationFn: () => api<{ purchase_id: number; invoice_id: number }>("/api/package-purchases", { method: "POST", body: { package_id: Number(f.package_id), animal_id: Number(f.animal_id) } }),
    onSuccess: (r) => {
      qc.invalidateQueries({ queryKey: ["invoices"] });
      onCreated(r.invoice_id);
    },
  });
  return (
    <form className="form" onSubmit={(e) => { e.preventDefault(); m.mutate(); }}>
      <div className="form-row">
        <Field label="Paket">
          <select value={f.package_id} onChange={(e) => setF({ ...f, package_id: e.target.value })} required>
            <option value="">Seçin…</option>
            {pk.data?.items.map((p) => <option key={p.id} value={p.id}>{p.name} · {p.sessions} seans · {money(p.price_kurus)}</option>)}
          </select>
        </Field>
        <Field label="Hasta no"><input type="number" value={f.animal_id} onChange={(e) => setF({ ...f, animal_id: e.target.value })} required /></Field>
      </div>
      <ErrorBox error={m.error} />
      {m.data && <SuccessBox>Paket satıldı (#{m.data.purchase_id}), fatura #{m.data.invoice_id}.</SuccessBox>}
      <button className="btn btn-primary" disabled={m.isPending}>Sat</button>
    </form>
  );
}
