import { useState } from "react";
import { useParams } from "react-router-dom";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { api, date, dateTime, speciesLabel } from "../api";
import { useHasRole } from "../auth";
import type { Animal, Prescription, Vaccinations } from "../types";
import { Badge, Card, ErrorBox, Field, Page, Query, WeightChart } from "../ui";

export default function AnimalDetail() {
  const id = Number(useParams().id);
  const animal = useQuery({ queryKey: ["animal", id], queryFn: () => api<Animal>(`/api/animals/${id}`) });
  return (
    <Query q={animal}>
      {(a) => (
        <Page title={`${a.name} · ${speciesLabel[a.species]}`}>
          <AnimalView a={a} />
        </Page>
      )}
    </Query>
  );
}

// AnimalView personel detay ekranı ve sahibin sağlık karnesi tarafından ortak kullanılır.
export function AnimalView({ a }: { a: Animal }) {
  const canSeeRx = useHasRole("vet", "pharmacy");
  return (
    <div className="stack">
      <div className="grid grid-2">
        <Card title="Kimlik">
          <dl className="small" style={{ margin: 0, display: "grid", gridTemplateColumns: "auto 1fr", gap: "4px 12px" }}>
            <dt className="muted">Hasta no</dt><dd style={{ margin: 0 }}>#{a.id}</dd>
            <dt className="muted">Irk</dt><dd style={{ margin: 0 }}>{a.breed}</dd>
            <dt className="muted">Mikroçip</dt><dd style={{ margin: 0 }} className="mono">{a.microchip ?? "çipsiz"}</dd>
            <dt className="muted">Doğum</dt>
            <dd style={{ margin: 0 }}>{date(a.birth_date)} {a.birth_date_estimated && <Badge value="warn" text="tahmini yaştan" />}</dd>
            <dt className="muted">Güncel kilo</dt><dd style={{ margin: 0 }}>{a.weight_kg ? `${a.weight_kg} kg` : "—"}</dd>
            {a.merged_into && (<><dt className="muted">Durum</dt><dd style={{ margin: 0 }}><Badge value="cancelled" text={`#${a.merged_into} ile birleştirildi`} /></dd></>)}
          </dl>
        </Card>
        <OwnersCard a={a} />
      </div>
      <VaccinationsCard animalId={a.id} />
      <div className="grid grid-2">
        <WeightsCard animalId={a.id} />
        <PackagesCard animalId={a.id} />
      </div>
      <ExamsCard animalId={a.id} />
      {canSeeRx && <PrescriptionsCard animalId={a.id} />}
    </div>
  );
}

function OwnersCard({ a }: { a: Animal }) {
  const qc = useQueryClient();
  const isReception = useHasRole("reception");
  const m = useMutation({
    mutationFn: (ownerId: number) => api(`/api/animals/${a.id}/billing-owner`, { method: "PUT", body: { owner_id: ownerId } }),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["animal", a.id] }),
  });
  return (
    <Card title="Sahipler">
      <ul className="events">
        {a.owners?.map((o) => (
          <li key={o.owner_id}>
            <span>
              <strong>{o.full_name}</strong> <span className="muted small">#{o.owner_id} · {o.phone} · {o.relation === "owner" ? "sahip" : o.relation === "co_owner" ? "ortak sahip" : "bakıcı"}</span>
            </span>
            {o.is_billing ? (
              <Badge value="info" text="Fatura sorumlusu" />
            ) : (
              isReception && <button className="btn btn-sm" onClick={() => m.mutate(o.owner_id)} disabled={m.isPending}>Fatura sorumlusu yap</button>
            )}
          </li>
        ))}
      </ul>
      <p className="small muted">
        Fatura kuralı: elle seçilen sahip → fatura sorumlusu → ilk kayıtlı sahip.
      </p>
      <ErrorBox error={m.error} />
    </Card>
  );
}

function VaccinationsCard({ animalId }: { animalId: number }) {
  const qc = useQueryClient();
  const canAdminister = useHasRole("vet", "technician");
  const q = useQuery({ queryKey: ["vacc", animalId], queryFn: () => api<Vaccinations>(`/api/animals/${animalId}/vaccinations`) });
  const [batch, setBatch] = useState("");
  const [usePackage, setUsePackage] = useState(false);
  const m = useMutation({
    mutationFn: (doseId: number) =>
      api(`/api/vaccine-doses/${doseId}/administer`, { method: "POST", body: { batch_no: batch || "LOT-MANUEL", use_package: usePackage } }),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ["vacc", animalId] });
      qc.invalidateQueries({ queryKey: ["packages", animalId] });
    },
  });
  return (
    <Card title="Aşı takvimi">
      <Query q={q} isEmpty={(v) => v.doses.length === 0} empty="Bu tür için aşı protokolü yok.">
        {(v) => (
          <div className="stack">
            {canAdminister && (
              <div className="form-row">
                <Field label="Aşı seri/lot no"><input value={batch} onChange={(e) => setBatch(e.target.value)} placeholder="ör. DHPP-2026-0042" /></Field>
                <label className="field" style={{ justifyContent: "flex-end" }}>
                  <span className="row"><input type="checkbox" style={{ width: "auto" }} checked={usePackage} onChange={(e) => setUsePackage(e.target.checked)} /> Aşı paketinden düş</span>
                </label>
              </div>
            )}
            <ErrorBox error={m.error} />
            <div className="table-wrap">
              <table>
                <thead><tr><th>Aşı</th><th>Doz</th><th>Tarih</th><th>Durum</th><th>Not</th>{canAdminister && <th />}</tr></thead>
                <tbody>
                  {v.doses.map((d) => {
                    const overdue = d.status === "planned" && d.due_date <= v.today;
                    return (
                      <tr key={d.id}>
                        <td><strong>{d.vaccine_code}</strong> <span className="muted small">{d.vaccine_name}</span></td>
                        <td>{d.dose_number}. <Badge value={d.kind} /></td>
                        <td>{d.status === "administered" ? date(d.administered_at) : date(d.due_date)}</td>
                        <td><Badge value={overdue ? "overdue" : d.status} text={overdue && d.due_date < v.today ? "Gecikmiş" : overdue ? "Bugün" : undefined} /></td>
                        <td className="small muted">{d.batch_no}{d.package_used && " · paketten"}</td>
                        {canAdminister && (
                          <td>{d.status === "planned" && <button className="btn btn-sm" onClick={() => m.mutate(d.id)} disabled={m.isPending}>Uygula</button>}</td>
                        )}
                      </tr>
                    );
                  })}
                </tbody>
              </table>
            </div>
            <details>
              <summary className="small">Plan nasıl hesaplandı? (motor açıklaması)</summary>
              <ul className="small">
                {v.plans.map((p) => (
                  <li key={p.vaccine_code}>
                    <strong>{p.vaccine_code}</strong>
                    {p.late_start && " · geç başlangıç serisi"}
                    {p.estimated_age && " · tahmini yaşa göre"}
                    {p.series_complete && " · seri tamam"}
                    <ul>
                      {p.planned.map((d) => (<li key={d.number}>{date(d.due_date)} — {d.reason}</li>))}
                      {p.ignored_doses?.map((d) => (<li key={d.date} className="muted">Sayılmayan doz {date(d.date)}: {d.reason}</li>))}
                    </ul>
                  </li>
                ))}
              </ul>
            </details>
          </div>
        )}
      </Query>
    </Card>
  );
}

function WeightsCard({ animalId }: { animalId: number }) {
  const qc = useQueryClient();
  const canRecord = useHasRole("vet", "technician");
  const q = useQuery({ queryKey: ["weights", animalId], queryFn: () => api<{ items: { weight_kg: string; measured_at: string }[] }>(`/api/animals/${animalId}/weights`) });
  const [kg, setKg] = useState("");
  const m = useMutation({
    mutationFn: () => api(`/api/animals/${animalId}/weights`, { method: "POST", body: { weight_kg: kg } }),
    onSuccess: () => {
      setKg("");
      qc.invalidateQueries({ queryKey: ["weights", animalId] });
      qc.invalidateQueries({ queryKey: ["animal", animalId] });
    },
  });
  return (
    <Card title="Kilo takibi">
      <Query q={q} isEmpty={(d) => d.items.length === 0} empty="Kilo ölçümü yok.">
        {(d) => <WeightChart points={d.items} />}
      </Query>
      {canRecord && (
        <form className="row" style={{ marginTop: 8 }} onSubmit={(e) => { e.preventDefault(); m.mutate(); }}>
          <input style={{ maxWidth: 140 }} inputMode="decimal" placeholder="kg" value={kg} onChange={(e) => setKg(e.target.value)} required />
          <button className="btn btn-sm" disabled={m.isPending}>Kilo kaydet</button>
        </form>
      )}
      <ErrorBox error={m.error} />
    </Card>
  );
}

function PackagesCard({ animalId }: { animalId: number }) {
  const q = useQuery({
    queryKey: ["packages", animalId],
    queryFn: () => api<{ items: { id: number; package_name: string; sessions_total: number; sessions_left: number; purchased_at: string }[] }>(`/api/animals/${animalId}/packages`),
  });
  return (
    <Card title="Aşı paketleri">
      <Query q={q} isEmpty={(d) => d.items.length === 0} empty="Paket yok.">
        {(d) => (
          <ul className="events">
            {d.items.map((p) => (
              <li key={p.id}>
                <span>{p.package_name} <span className="muted small">· {date(p.purchased_at)}</span></span>
                <Badge value={p.sessions_left > 0 ? "ready" : "muted"} text={`${p.sessions_left}/${p.sessions_total} seans`} />
              </li>
            ))}
          </ul>
        )}
      </Query>
    </Card>
  );
}

function ExamsCard({ animalId }: { animalId: number }) {
  const qc = useQueryClient();
  const isVet = useHasRole("vet");
  const q = useQuery({
    queryKey: ["exams", animalId],
    queryFn: () => api<{ items: { id: number; vet_name: string; weight_kg: string | null; temperature_c: string | null; findings: string; diagnosis: string | null; created_at: string }[] }>(`/api/animals/${animalId}/examinations`),
  });
  const [f, setF] = useState({ findings: "", diagnosis: "", weight_kg: "", temperature_c: "" });
  const m = useMutation({
    mutationFn: () =>
      api("/api/examinations", {
        method: "POST",
        body: { animal_id: animalId, findings: f.findings, diagnosis: f.diagnosis || null, weight_kg: f.weight_kg || null, temperature_c: f.temperature_c || null },
      }),
    onSuccess: () => {
      setF({ findings: "", diagnosis: "", weight_kg: "", temperature_c: "" });
      qc.invalidateQueries({ queryKey: ["exams", animalId] });
      qc.invalidateQueries({ queryKey: ["weights", animalId] });
    },
  });
  return (
    <Card title="Muayeneler">
      {isVet && (
        <form className="form" style={{ marginBottom: 12 }} onSubmit={(e) => { e.preventDefault(); m.mutate(); }}>
          <div className="form-row">
            <Field label="Kilo (kg)"><input inputMode="decimal" value={f.weight_kg} onChange={(e) => setF({ ...f, weight_kg: e.target.value })} /></Field>
            <Field label="Ateş (°C)"><input inputMode="decimal" value={f.temperature_c} onChange={(e) => setF({ ...f, temperature_c: e.target.value })} /></Field>
            <Field label="Tanı"><input value={f.diagnosis} onChange={(e) => setF({ ...f, diagnosis: e.target.value })} /></Field>
          </div>
          <Field label="Bulgular"><textarea value={f.findings} onChange={(e) => setF({ ...f, findings: e.target.value })} required /></Field>
          <ErrorBox error={m.error} />
          <div><button className="btn btn-primary btn-sm" disabled={m.isPending}>Muayeneyi kaydet</button></div>
        </form>
      )}
      <Query q={q} isEmpty={(d) => d.items.length === 0} empty="Muayene kaydı yok.">
        {(d) => (
          <div className="table-wrap">
            <table>
              <thead><tr><th>Tarih</th><th>Veteriner</th><th>Bulgu</th><th>Tanı</th><th className="num">Kilo</th></tr></thead>
              <tbody>
                {d.items.map((e) => (
                  <tr key={e.id}>
                    <td>{dateTime(e.created_at)}</td><td>{e.vet_name}</td><td>{e.findings}</td><td>{e.diagnosis ?? "—"}</td>
                    <td className="num">{e.weight_kg ?? "—"}</td>
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

function PrescriptionsCard({ animalId }: { animalId: number }) {
  const q = useQuery({ queryKey: ["rx", animalId], queryFn: () => api<{ items: Prescription[] }>(`/api/prescriptions?animal_id=${animalId}&limit=50`) });
  return (
    <Card title="Reçeteler">
      <Query q={q} isEmpty={(d) => d.items.length === 0} empty="Reçete yok.">
        {(d) => (
          <div className="table-wrap">
            <table>
              <thead><tr><th>Tarih</th><th>İlaç</th><th className="num">Doz</th><th>Kilo (reçetedeki)</th><th>Durum</th></tr></thead>
              <tbody>
                {d.items.map((r) => (
                  <tr key={r.id}>
                    <td>{dateTime(r.created_at)}</td>
                    <td>{r.drug_name} {r.exceeds_max && <Badge value="emergency" text="sınır üstü" />}</td>
                    <td className="num">{r.dose_mg} mg</td>
                    <td>{r.weight_kg_snapshot} kg {r.weight_changed_note && <span className="small muted" title={r.weight_changed_note}>ⓘ</span>}</td>
                    <td><Badge value={r.status} /></td>
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
