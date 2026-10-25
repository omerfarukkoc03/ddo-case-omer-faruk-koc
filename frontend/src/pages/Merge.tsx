import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { api, dateTime } from "../api";
import { Badge, Card, ErrorBox, Page, Query, SuccessBox } from "../ui";

interface Pair {
  survivor_id: number;
  survivor_label: string;
  duplicate_id: number;
  duplicate_label: string;
  reason: string;
}

interface MergeOp {
  id: number;
  entity: string;
  survivor_id: number;
  duplicate_id: number;
  performed_at: string;
  undone_at: string | null;
}

export default function Merge() {
  const qc = useQueryClient();
  const refresh = () => {
    qc.invalidateQueries({ queryKey: ["duplicates"] });
    qc.invalidateQueries({ queryKey: ["merges"] });
  };
  const merge = useMutation({
    mutationFn: (b: { entity: string; survivor_id: number; duplicate_id: number }) => api<MergeOp>("/api/merges", { method: "POST", body: b }),
    onSuccess: refresh,
  });
  const undo = useMutation({ mutationFn: (id: number) => api<MergeOp>(`/api/merges/${id}/undo`, { method: "POST" }), onSuccess: refresh });
  return (
    <Page title="Mükerrer kayıtlar" subtitle="Aynı telefonlu sahipler ve aynı ad/türdeki hayvanlar önerilir. Birleştirmede kayıt silinmez; her birleştirme geri alınabilir.">
      <div className="stack">
        <p className="small muted" style={{ margin: 0 }}>
          Birleştirmede hiçbir kayıt silinmez: mükerrer kayda bağlı satırlar hayatta kalan kayda taşınır ve taşınan her satır anlık
          görüntüye yazılır. Geri alma bu satırları, sonradan değişmedilerse, eski yerlerine döndürür.
        </p>
        <ErrorBox error={merge.error ?? undo.error} />
        {merge.data && <SuccessBox>Birleştirme #{merge.data.id} tamamlandı. Aşağıdan geri alınabilir.</SuccessBox>}
        <div className="grid grid-2">
          <Candidates entity="owners" onMerge={(p) => merge.mutate({ entity: "owner", survivor_id: p.survivor_id, duplicate_id: p.duplicate_id })} busy={merge.isPending} />
          <Candidates entity="animals" onMerge={(p) => merge.mutate({ entity: "animal", survivor_id: p.survivor_id, duplicate_id: p.duplicate_id })} busy={merge.isPending} />
        </div>
        <History onUndo={(id) => undo.mutate(id)} busy={undo.isPending} />
      </div>
    </Page>
  );
}

function Candidates({ entity, onMerge, busy }: { entity: string; onMerge: (p: Pair) => void; busy: boolean }) {
  const q = useQuery({ queryKey: ["duplicates", entity], queryFn: () => api<{ items: Pair[] }>(`/api/duplicates/${entity}`) });
  return (
    <Card title={entity === "owners" ? "Olası mükerrer sahipler" : "Olası mükerrer hayvanlar"}>
      <Query q={q} isEmpty={(d) => d.items.length === 0} empty="Mükerrer aday yok.">
        {(d) => (
          <div className="table-wrap">
            <table>
              <thead><tr><th>Kalacak kayıt</th><th>Mükerrer</th><th /></tr></thead>
              <tbody>
                {d.items.slice(0, 30).map((p) => (
                  <tr key={`${p.survivor_id}-${p.duplicate_id}`}>
                    <td>#{p.survivor_id} {p.survivor_label}</td>
                    <td>#{p.duplicate_id} {p.duplicate_label}<div className="small muted">{p.reason}</div></td>
                    <td><button className="btn btn-sm" disabled={busy} onClick={() => onMerge(p)}>Birleştir</button></td>
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

function History({ onUndo, busy }: { onUndo: (id: number) => void; busy: boolean }) {
  const q = useQuery({ queryKey: ["merges"], queryFn: () => api<{ items: MergeOp[] }>("/api/merges") });
  return (
    <Card title="Birleştirme geçmişi">
      <Query q={q} isEmpty={(d) => d.items.length === 0} empty="Henüz birleştirme yapılmadı.">
        {(d) => (
          <div className="table-wrap">
            <table>
              <thead><tr><th>#</th><th>Tür</th><th>Kayıtlar</th><th>Zaman</th><th>Durum</th><th /></tr></thead>
              <tbody>
                {d.items.map((m) => (
                  <tr key={m.id}>
                    <td>{m.id}</td>
                    <td>{m.entity === "owner" ? "Sahip" : "Hayvan"}</td>
                    <td>#{m.duplicate_id} → #{m.survivor_id}</td>
                    <td>{dateTime(m.performed_at)}</td>
                    <td>{m.undone_at ? <Badge value="cancelled" text={`geri alındı ${dateTime(m.undone_at)}`} /> : <Badge value="ok" text="aktif" />}</td>
                    <td>{!m.undone_at && <button className="btn btn-sm" disabled={busy} onClick={() => onUndo(m.id)}>Geri al</button>}</td>
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
