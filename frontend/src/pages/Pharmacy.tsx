import { useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { api, date, dateTime, type List } from "../api";
import { useHasRole } from "../auth";
import type { Drug, Prescription } from "../types";
import { Badge, Card, ErrorBox, Field, Page, Query, SuccessBox, Toggle } from "../ui";

interface Batch {
  id: number;
  drug_name: string;
  unit: string;
  lot_no: string;
  expires_on: string;
  quantity: number;
  expired: boolean;
}

export default function Pharmacy() {
  const isPharmacy = useHasRole("pharmacy");
  return (
    <Page title="Eczane ve stok" subtitle="Reçeteler son kullanma tarihi en yakın partiden (FEFO) teslim edilir; süresi geçmiş parti kullanılmaz.">
      <div className="stack">
        <Queue canDispense={isPharmacy} />
        <Stock />
        {isPharmacy && <Toggle label="Stok girişi (yeni parti)"><ReceiveForm /></Toggle>}
      </div>
    </Page>
  );
}

function Queue({ canDispense }: { canDispense: boolean }) {
  const qc = useQueryClient();
  const q = useQuery({ queryKey: ["rx-queue"], queryFn: () => api<List<Prescription>>("/api/prescriptions?status=active&limit=50"), refetchInterval: 15_000 });
  const m = useMutation({
    mutationFn: (id: number) => api<{ batches: { lot_no: string; quantity: number }[] }>(`/api/prescriptions/${id}/dispense`, { method: "POST" }),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ["rx-queue"] });
      qc.invalidateQueries({ queryKey: ["stock"] });
    },
  });
  return (
    <Card title="Teslim bekleyen reçeteler">
      <ErrorBox error={m.error} />
      {m.data && <SuccessBox>Teslim edildi (FEFO): {m.data.batches.map((b) => `${b.lot_no} × ${b.quantity}`).join(", ")}</SuccessBox>}
      <Query q={q} isEmpty={(d) => d.items.length === 0} empty="Bekleyen reçete yok.">
        {(d) => (
          <div className="table-wrap">
            <table>
              <thead><tr><th>#</th><th>Tarih</th><th>Hasta</th><th>İlaç</th><th>Doz</th><th className="num">Miktar</th><th /></tr></thead>
              <tbody>
                {d.items.map((r) => (
                  <tr key={r.id}>
                    <td className="muted">{r.id}</td>
                    <td>{dateTime(r.created_at)}</td>
                    <td>{r.animal_name}</td>
                    <td>{r.drug_name} {r.exceeds_max && <Badge value="emergency" text="sınır üstü" />}</td>
                    <td className="small">{r.dose_mg} mg × {r.doses_per_day}/gün × {r.duration_days} gün</td>
                    <td className="num">{r.dispense_quantity} {r.calculation.dispense_unit}</td>
                    <td>{canDispense && <button className="btn btn-sm btn-primary" disabled={m.isPending} onClick={() => m.mutate(r.id)}>Teslim et</button>}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
      </Query>
    </Card>
  );
}

function Stock() {
  const q = useQuery({ queryKey: ["stock"], queryFn: () => api<{ items: Batch[] }>("/api/stock") });
  return (
    <Card title="Stok partileri (son kullanma tarihine göre)">
      <Query q={q} isEmpty={(d) => d.items.length === 0} empty="Stok yok.">
        {(d) => (
          <div className="table-wrap">
            <table>
              <thead><tr><th>İlaç</th><th>Parti</th><th>Son kullanma</th><th className="num">Miktar</th></tr></thead>
              <tbody>
                {d.items.map((b) => (
                  <tr key={b.id}>
                    <td>{b.drug_name}</td>
                    <td className="mono">{b.lot_no}</td>
                    <td>{date(b.expires_on)} {b.expired && <Badge value="emergency" text="süresi geçmiş — kullanılmaz" />}</td>
                    <td className="num">{b.quantity} {b.unit}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
      </Query>
    </Card>
  );
}

function ReceiveForm() {
  const qc = useQueryClient();
  const drugs = useQuery({ queryKey: ["drugs"], queryFn: () => api<{ items: Drug[] }>("/api/drugs") });
  const [f, setF] = useState({ drug_id: "", lot_no: "", expires_on: "", quantity: "" });
  const m = useMutation({
    mutationFn: () => api("/api/stock/batches", { method: "POST", body: { ...f, drug_id: Number(f.drug_id), quantity: Number(f.quantity) } }),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["stock"] }),
  });
  const set = (k: keyof typeof f) => (e: { target: { value: string } }) => setF({ ...f, [k]: e.target.value });
  return (
    <form className="form" onSubmit={(e) => { e.preventDefault(); m.mutate(); }}>
      <div className="form-row">
        <Field label="İlaç">
          <select value={f.drug_id} onChange={set("drug_id")} required>
            <option value="">Seçin…</option>
            {drugs.data?.items.map((d) => <option key={d.id} value={d.id}>{d.name}</option>)}
          </select>
        </Field>
        <Field label="Parti no"><input value={f.lot_no} onChange={set("lot_no")} required /></Field>
        <Field label="Son kullanma"><input type="date" value={f.expires_on} onChange={set("expires_on")} required /></Field>
        <Field label="Miktar"><input type="number" min={1} value={f.quantity} onChange={set("quantity")} required /></Field>
      </div>
      <ErrorBox error={m.error} />
      {m.isSuccess && <SuccessBox>Stok girişi kaydedildi.</SuccessBox>}
      <button className="btn btn-primary" disabled={m.isPending}>Kaydet</button>
    </form>
  );
}
