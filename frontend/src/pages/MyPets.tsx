import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { api, speciesLabel, type List } from "../api";
import type { Animal } from "../types";
import { Page, Query } from "../ui";
import { AnimalView } from "./AnimalDetail";

// Sahip portalı: kendi hayvanlarının sağlık karnesi (bonus). Sunucu yalnız sahibin hayvanlarını döndürür.
export default function MyPets() {
  const list = useQuery({ queryKey: ["my-animals"], queryFn: () => api<List<Animal>>("/api/animals?limit=50") });
  const [sel, setSel] = useState<number | null>(null);
  return (
    <Page title="Sağlık karnesi">
      <Query q={list} isEmpty={(d) => d.items.length === 0} empty="Kayıtlı hayvanınız yok.">
        {(d) => {
          const id = sel ?? d.items[0].id;
          return (
            <div className="stack">
              <div className="row">
                {d.items.map((a) => (
                  <button key={a.id} className={`btn ${a.id === id ? "btn-primary" : ""}`} onClick={() => setSel(a.id)}>
                    {a.name} <span className="small">({speciesLabel[a.species]})</span>
                  </button>
                ))}
              </div>
              <Detail id={id} />
            </div>
          );
        }}
      </Query>
    </Page>
  );
}

function Detail({ id }: { id: number }) {
  const q = useQuery({ queryKey: ["animal", id], queryFn: () => api<Animal>(`/api/animals/${id}`) });
  return <Query q={q}>{(a) => <AnimalView a={a} />}</Query>;
}
