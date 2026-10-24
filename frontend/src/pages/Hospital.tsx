import { useState } from "react";
import { Link } from "react-router-dom";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { api, dateTime } from "../api";
import { useHasRole } from "../auth";
import type { Cage } from "../types";
import { Badge, Card, ErrorBox, Field, Page, Query, label } from "../ui";

export default function Hospital() {
  const qc = useQueryClient();
  const isVet = useHasRole("vet");
  const canTransition = useHasRole("vet", "technician");
  const cages = useQuery({ queryKey: ["cages"], queryFn: () => api<{ items: Cage[] }>("/api/cages"), refetchInterval: 15_000 });
  const refresh = () => qc.invalidateQueries({ queryKey: ["cages"] });
  const transition = useMutation({
    mutationFn: ({ id, to }: { id: number; to: string }) => api(`/api/cages/${id}/transition`, { method: "POST", body: { to } }),
    onSuccess: refresh,
  });
  const discharge = useMutation({
    mutationFn: (hid: number) => api(`/api/hospitalizations/${hid}/discharge`, { method: "POST" }),
    onSuccess: refresh,
  });

  return (
    <Page title="Yatan hasta ve kafesler">
      <div className="stack">
        {isVet && <AdmitForm onDone={refresh} />}
        <p className="small muted" style={{ margin: 0 }}>
          Kafes durum makinesi: boş → dolu → temizlik → hazır → dolu. Geçersiz geçişler sunucuda (uygulama + veritabanı tetikleyicisi) reddedilir.
        </p>
        <ErrorBox error={transition.error ?? discharge.error} />
        <Query q={cages} isEmpty={(d) => d.items.length === 0} empty="Kafes tanımlı değil.">
          {(d) => (
            <div className="cages">
              {d.items.map((c) => (
                <div key={c.id} className={`cage ${c.state}`}>
                  <div className="row">
                    <strong>{c.code}</strong>
                    <span className="small muted">{c.size} · {c.ward === "isolation" ? "izolasyon" : "genel"}</span>
                    <span className="spacer" />
                    <Badge value={c.state} />
                  </div>
                  {c.animal_name && (
                    <div className="small">
                      <Link to={`/animals/${c.animal_id}`}>{c.animal_name}</Link>
                      <div className="muted">{c.reason} · {dateTime(c.admitted_at)}</div>
                    </div>
                  )}
                  <div className="row">
                    {isVet && c.hospitalization_id && (
                      <button className="btn btn-sm" onClick={() => discharge.mutate(c.hospitalization_id!)} disabled={discharge.isPending}>Taburcu et</button>
                    )}
                    {canTransition && !c.hospitalization_id && c.allowed_next.filter((s) => s !== "occupied").map((s) => (
                      <button key={s} className="btn btn-sm" onClick={() => transition.mutate({ id: c.id, to: s })} disabled={transition.isPending}>
                        → {label(s)}
                      </button>
                    ))}
                  </div>
                </div>
              ))}
            </div>
          )}
        </Query>
      </div>
    </Page>
  );
}

function AdmitForm({ onDone }: { onDone: () => void }) {
  const [f, setF] = useState({ animal_id: "", cage_id: "", reason: "" });
  const m = useMutation({
    mutationFn: () =>
      api<{ id: number; cage_id: number }>("/api/hospitalizations", {
        method: "POST",
        body: { animal_id: Number(f.animal_id), cage_id: f.cage_id ? Number(f.cage_id) : null, reason: f.reason },
      }),
    onSuccess: onDone,
  });
  const set = (k: keyof typeof f) => (e: { target: { value: string } }) => setF({ ...f, [k]: e.target.value });
  return (
    <Card title="Hasta yatır">
      <form className="form-row" onSubmit={(e) => { e.preventDefault(); m.mutate(); }}>
        <Field label="Hasta no"><input type="number" value={f.animal_id} onChange={set("animal_id")} required /></Field>
        <Field label="Kafes no" hint="Boşsa uygun ilk kafes"><input type="number" value={f.cage_id} onChange={set("cage_id")} /></Field>
        <Field label="Neden"><input value={f.reason} onChange={set("reason")} required /></Field>
        <div className="field" style={{ justifyContent: "flex-end" }}><button className="btn btn-primary" disabled={m.isPending}>Yatır</button></div>
      </form>
      <ErrorBox error={m.error} />
      {m.data && <p className="small">Yatış #{m.data.id}, kafes #{m.data.cage_id}.</p>}
    </Card>
  );
}
