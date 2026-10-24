import { useState } from "react";
import { useMutation, useQuery } from "@tanstack/react-query";
import { ApiError, api, speciesLabel } from "../api";
import type { Animal, DoseResult, Drug, Prescription } from "../types";
import { Badge, Card, ErrorBox, Field, Page, Query, SuccessBox } from "../ui";

export default function Prescribe() {
  const drugs = useQuery({ queryKey: ["drugs"], queryFn: () => api<{ items: Drug[] }>("/api/drugs") });
  const [f, setF] = useState({
    animal_id: "", drug_id: "", mg_per_kg: "", doses_per_day: "1", duration_days: "5", dilute_to_mg_per_ml: "", override_reason: "",
  });
  const animal = useQuery({
    queryKey: ["animal", Number(f.animal_id)],
    queryFn: () => api<Animal>(`/api/animals/${f.animal_id}`),
    enabled: Number(f.animal_id) > 0,
  });
  const body = () => ({
    animal_id: Number(f.animal_id), drug_id: Number(f.drug_id),
    mg_per_kg: f.mg_per_kg || null, doses_per_day: Number(f.doses_per_day), duration_days: Number(f.duration_days),
    dilute_to_mg_per_ml: f.dilute_to_mg_per_ml || null, override_reason: f.override_reason,
  });
  const calc = useMutation({ mutationFn: () => api<DoseResult>("/api/dosage/calculate", { method: "POST", body: body() }) });
  const save = useMutation({ mutationFn: () => api<Prescription>("/api/prescriptions", { method: "POST", body: body() }) });
  const set = (k: keyof typeof f) => (e: { target: { value: string } }) => {
    setF({ ...f, [k]: e.target.value });
    calc.reset();
    save.reset();
  };
  const drug = drugs.data?.items.find((d) => d.id === Number(f.drug_id));
  const species = animal.data?.species;
  const range = drug && species ? drug.dosing[species] : undefined;
  const needsOverride = calc.error instanceof ApiError && calc.error.code === "max_dose_exceeded";

  return (
    <Page title="Reçete ve doz hesabı">
      <div className="grid grid-2">
        <Card title="Girdi">
          <form className="form" onSubmit={(e) => { e.preventDefault(); calc.mutate(); }}>
            <div className="form-row">
              <Field label="Hasta no" hint={animal.data ? `${animal.data.name} · ${speciesLabel[animal.data.species]} · ${animal.data.breed} · ${animal.data.weight_kg ?? "?"} kg` : "ör. 4 (Zeytin, Collie)"}>
                <input type="number" value={f.animal_id} onChange={set("animal_id")} required />
              </Field>
              <Field label="İlaç">
                <Query q={drugs}>
                  {(d) => (
                    <select value={f.drug_id} onChange={set("drug_id")} required>
                      <option value="">Seçin…</option>
                      {d.items.map((x) => <option key={x.id} value={x.id}>{x.name}</option>)}
                    </select>
                  )}
                </Query>
              </Field>
            </div>
            {drug && species && (
              <div className="small muted">
                {range ? `Önerilen aralık (${speciesLabel[species]}): ${range.mg_per_kg_min}–${range.mg_per_kg_max} mg/kg` + (range.max_single_dose_mg ? `, tek doz en fazla ${range.max_single_dose_mg} mg` : "") : "Bu tür için onaylı doz yok."}
                {drug.contraindications.filter((c) => c.species === species).map((c) => (
                  <div key={c.reason}><Badge value="emergency" text="Kontrendikasyon" /> {c.breed ? `${c.breed}: ` : ""}{c.reason}</div>
                ))}
              </div>
            )}
            <div className="form-row">
              <Field label="Doz oranı (mg/kg)" hint="Boşsa alt sınır"><input inputMode="decimal" value={f.mg_per_kg} onChange={set("mg_per_kg")} /></Field>
              <Field label="Günde"><input type="number" min={1} max={6} value={f.doses_per_day} onChange={set("doses_per_day")} /></Field>
              <Field label="Gün"><input type="number" min={1} max={90} value={f.duration_days} onChange={set("duration_days")} /></Field>
              <Field label="Seyreltme (mg/mL)" hint="İsteğe bağlı"><input inputMode="decimal" value={f.dilute_to_mg_per_ml} onChange={set("dilute_to_mg_per_ml")} /></Field>
            </div>
            {(needsOverride || f.override_reason) && (
              <Field label="Sınır aşımı gerekçesi" hint="En az 10 karakter; denetim kaydına yazılır.">
                <textarea value={f.override_reason} onChange={set("override_reason")} />
              </Field>
            )}
            <div className="row">
              <button className="btn" disabled={calc.isPending}>Hesapla</button>
              <button type="button" className="btn btn-primary" disabled={!calc.data || save.isPending} onClick={() => save.mutate()}>
                Reçeteyi kaydet
              </button>
            </div>
          </form>
        </Card>
        <Card title="Hesap (adım adım)">
          {!calc.data && !calc.error && <div className="state state-empty">Hesaplamak için girdileri doldurun.</div>}
          <ErrorBox error={calc.error} />
          {needsOverride && <p className="small muted">Doz azami sınırı aşıyor. Gerekçe yazarak yeniden hesaplayabilirsiniz; 2 kat üstü hiçbir koşulda yazılamaz.</p>}
          {calc.data && (
            <div className="stack">
              <ol className="steps">
                {calc.data.steps.map((s, i) => (
                  <li key={i}>
                    <span className="n">{i + 1}</span>
                    <span><strong>{s.label}</strong><div className="formula">{s.formula}</div></span>
                    <span className="result">{s.result}</span>
                  </li>
                ))}
              </ol>
              {calc.data.warnings.map((w) => <div key={w} className="state state-error">{w}</div>)}
              <div className="row">
                <Badge value="info" text={`Doz: ${calc.data.dose_mg} mg`} />
                {calc.data.volume_ml && <Badge value="info" text={`${calc.data.volume_ml} mL`} />}
                {calc.data.tablets && <Badge value="info" text={`${calc.data.tablets} tablet`} />}
                <Badge value="muted" text={`Teslim: ${calc.data.dispense_quantity} ${calc.data.dispense_unit}`} />
              </div>
            </div>
          )}
          <ErrorBox error={save.error} />
          {save.data && (
            <SuccessBox>
              Reçete #{save.data.id} kaydedildi. Hesap {save.data.weight_kg_snapshot} kg ile sabitlendi; hayvanın kilosu sonradan değişse de bu reçete değişmez.
            </SuccessBox>
          )}
        </Card>
      </div>
    </Page>
  );
}
