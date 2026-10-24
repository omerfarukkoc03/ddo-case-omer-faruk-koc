import { useState, type FormEvent } from "react";
import { useInfiniteQuery, useMutation, useQueryClient } from "@tanstack/react-query";
import { useNavigate } from "react-router-dom";
import { api, date, speciesLabel, type List } from "../api";
import { useHasRole } from "../auth";
import type { Animal, Owner } from "../types";
import { Card, ErrorBox, Field, Loading, Page, SuccessBox, Toggle } from "../ui";

export default function Animals() {
  const [q, setQ] = useState("");
  const [chip, setChip] = useState("");
  const [search, setSearch] = useState({ q: "", chip: "" });
  const isReception = useHasRole("reception");
  const nav = useNavigate();

  const list = useInfiniteQuery({
    queryKey: ["animals", search],
    initialPageParam: "",
    queryFn: ({ pageParam }) =>
      api<List<Animal>>(`/api/animals?limit=25&q=${encodeURIComponent(search.q)}&microchip=${encodeURIComponent(search.chip)}&cursor=${pageParam}`),
    getNextPageParam: (last) => last.next_cursor ?? undefined,
  });
  const items = list.data?.pages.flatMap((p) => p.items) ?? [];

  return (
    <Page title="Hasta kayıtları">
      <div className="stack">
        {isReception && (
          <div className="grid grid-2">
            <Toggle label="Yeni sahip kaydı"><OwnerForm /></Toggle>
            <Toggle label="Yeni hayvan kaydı"><AnimalForm /></Toggle>
          </div>
        )}
        <Card>
          <form
            className="form-row"
            onSubmit={(e) => {
              e.preventDefault();
              setSearch({ q: q.trim(), chip: chip.trim() });
            }}
          >
            <Field label="Ad ile ara"><input value={q} onChange={(e) => setQ(e.target.value)} placeholder="ör. Karamel" /></Field>
            <Field label="Mikroçip no"><input value={chip} onChange={(e) => setChip(e.target.value)} inputMode="numeric" placeholder="15 hane" /></Field>
            <div className="field" style={{ justifyContent: "flex-end" }}><button className="btn btn-primary">Ara</button></div>
          </form>
        </Card>
        <Card>
          {list.isPending ? (
            <Loading />
          ) : list.isError ? (
            <ErrorBox error={list.error} />
          ) : items.length === 0 ? (
            <div className="state state-empty">Aramaya uyan hasta yok.</div>
          ) : (
            <>
              <div className="table-wrap">
                <table>
                  <thead>
                    <tr><th>#</th><th>Ad</th><th>Tür / ırk</th><th>Mikroçip</th><th>Doğum</th><th className="num">Kilo</th></tr>
                  </thead>
                  <tbody>
                    {items.map((a) => (
                      <tr key={a.id} className="clickable" onClick={() => nav(`/animals/${a.id}`)}>
                        <td className="muted">{a.id}</td>
                        <td><strong>{a.name}</strong></td>
                        <td>{speciesLabel[a.species]} · {a.breed}</td>
                        <td className="mono">{a.microchip ?? <span className="muted">çipsiz</span>}</td>
                        <td>{date(a.birth_date)}{a.birth_date_estimated && <span className="muted small"> (tahmini)</span>}</td>
                        <td className="num">{a.weight_kg ? `${a.weight_kg} kg` : "—"}</td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
              {list.hasNextPage && (
                <div className="row" style={{ marginTop: 12 }}>
                  <button className="btn" onClick={() => list.fetchNextPage()} disabled={list.isFetchingNextPage}>
                    {list.isFetchingNextPage ? "Yükleniyor…" : "Daha fazla yükle"}
                  </button>
                </div>
              )}
            </>
          )}
        </Card>
      </div>
    </Page>
  );
}

function OwnerForm() {
  const [f, setF] = useState({ full_name: "", phone: "", email: "" });
  const m = useMutation({
    mutationFn: () => api<Owner>("/api/owners", { method: "POST", body: { full_name: f.full_name, phone: f.phone, email: f.email || null } }),
  });
  return (
    <form className="form" onSubmit={(e: FormEvent) => { e.preventDefault(); m.mutate(); }}>
      <div className="form-row">
        <Field label="Ad soyad"><input value={f.full_name} onChange={(e) => setF({ ...f, full_name: e.target.value })} required /></Field>
        <Field label="Telefon"><input value={f.phone} onChange={(e) => setF({ ...f, phone: e.target.value })} placeholder="05XXXXXXXXX" required /></Field>
        <Field label="E-posta"><input type="email" value={f.email} onChange={(e) => setF({ ...f, email: e.target.value })} /></Field>
      </div>
      <ErrorBox error={m.error} />
      {m.data && <SuccessBox>Sahip kaydedildi: #{m.data.id} {m.data.full_name}</SuccessBox>}
      <button className="btn btn-primary" disabled={m.isPending}>Kaydet</button>
    </form>
  );
}

function AnimalForm() {
  const qc = useQueryClient();
  const nav = useNavigate();
  const [f, setF] = useState({
    name: "", species: "dog", breed: "", sex: "unknown", microchip: "", birth_date: "", estimated_age_months: "", weight_kg: "",
    owner_id: "", co_owner_id: "", billing: "owner",
  });
  const m = useMutation({
    mutationFn: () => {
      const owners = [{ owner_id: Number(f.owner_id), relation: "owner", is_billing: f.billing === "owner" }];
      if (f.co_owner_id) owners.push({ owner_id: Number(f.co_owner_id), relation: "co_owner", is_billing: f.billing === "co" });
      return api<Animal>("/api/animals", {
        method: "POST",
        body: {
          name: f.name, species: f.species, breed: f.breed, sex: f.sex,
          microchip: f.microchip || null,
          birth_date: f.birth_date || null,
          estimated_age_months: f.estimated_age_months ? Number(f.estimated_age_months) : null,
          weight_kg: f.weight_kg || null,
          owners,
        },
      });
    },
    onSuccess: (a) => {
      qc.invalidateQueries({ queryKey: ["animals"] });
      nav(`/animals/${a.id}`);
    },
  });
  const set = (k: keyof typeof f) => (e: { target: { value: string } }) => setF({ ...f, [k]: e.target.value });
  return (
    <form className="form" onSubmit={(e) => { e.preventDefault(); m.mutate(); }}>
      <div className="form-row">
        <Field label="Ad"><input value={f.name} onChange={set("name")} required /></Field>
        <Field label="Tür">
          <select value={f.species} onChange={set("species")}>
            <option value="dog">Köpek</option><option value="cat">Kedi</option><option value="rabbit">Tavşan</option>
          </select>
        </Field>
        <Field label="Irk"><input value={f.breed} onChange={set("breed")} placeholder="mixed" /></Field>
        <Field label="Cinsiyet">
          <select value={f.sex} onChange={set("sex")}>
            <option value="unknown">Bilinmiyor</option><option value="female">Dişi</option><option value="male">Erkek</option>
          </select>
        </Field>
      </div>
      <div className="form-row">
        <Field label="Mikroçip no" hint="15 hane (ISO 11784)"><input value={f.microchip} onChange={set("microchip")} inputMode="numeric" /></Field>
        <Field label="Doğum tarihi"><input type="date" value={f.birth_date} onChange={set("birth_date")} /></Field>
        <Field label="…ya da tahmini yaş (ay)" hint="Doğum tarihi bilinmiyorsa"><input type="number" min={0} value={f.estimated_age_months} onChange={set("estimated_age_months")} /></Field>
        <Field label="Kilo (kg)"><input inputMode="decimal" value={f.weight_kg} onChange={set("weight_kg")} /></Field>
      </div>
      <div className="form-row">
        <Field label="Sahip no"><input type="number" value={f.owner_id} onChange={set("owner_id")} required /></Field>
        <Field label="Ortak sahip no (isteğe bağlı)"><input type="number" value={f.co_owner_id} onChange={set("co_owner_id")} /></Field>
        <Field label="Fatura sorumlusu">
          <select value={f.billing} onChange={set("billing")}>
            <option value="owner">Sahip</option><option value="co">Ortak sahip</option><option value="none">Belirtme (ilk sahip)</option>
          </select>
        </Field>
      </div>
      <ErrorBox error={m.error} />
      <button className="btn btn-primary" disabled={m.isPending}>Kaydet ve aşı planını oluştur</button>
    </form>
  );
}
