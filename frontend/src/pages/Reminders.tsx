import { useInfiniteQuery, useMutation, useQueryClient } from "@tanstack/react-query";
import { api, date, dateTime, type List } from "../api";
import { useHasRole } from "../auth";
import { Badge, Card, ErrorBox, Loading, Page, SuccessBox } from "../ui";

interface Reminder {
  id: number;
  owner_name: string;
  animal_name: string;
  vaccine_name: string;
  kind: string;
  due_date: string;
  created_at: string;
  message: string;
}

const kindText: Record<string, string> = { upcoming: "Yaklaşan", due: "Bugün", overdue: "Gecikmiş" };

export default function Reminders() {
  const qc = useQueryClient();
  const canRun = useHasRole("reception", "vet");
  const list = useInfiniteQuery({
    queryKey: ["reminders"],
    initialPageParam: "",
    queryFn: ({ pageParam }) => api<List<Reminder>>(`/api/reminders?limit=30&cursor=${pageParam}`),
    getNextPageParam: (l) => l.next_cursor ?? undefined,
  });
  const run = useMutation({
    mutationFn: () => api<{ created: number; skipped_concurrent_run: boolean }>("/api/jobs/reminders/run", { method: "POST" }),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["reminders"] }),
  });
  const items = list.data?.pages.flatMap((p) => p.items) ?? [];
  return (
    <Page
      title="Aşı hatırlatmaları"
      actions={canRun && <button className="btn" onClick={() => run.mutate()} disabled={run.isPending}>Hatırlatma job'ını çalıştır</button>}
    >
      <div className="stack">
        {run.data && (
          <SuccessBox>
            {run.data.created} yeni hatırlatma oluştu. Job idempotent: tekrar çalıştırmak aynı hatırlatmayı ikinci kez üretmez.
          </SuccessBox>
        )}
        <ErrorBox error={run.error} />
        <Card>
          {list.isPending ? <Loading /> : list.isError ? <ErrorBox error={list.error} /> : items.length === 0 ? (
            <div className="state state-empty">Hatırlatma yok.</div>
          ) : (
            <>
              <div className="table-wrap">
                <table>
                  <thead><tr><th>Tür</th><th>Vade</th><th>E-posta şablonu</th><th>Oluşturma</th></tr></thead>
                  <tbody>
                    {items.map((r) => (
                      <tr key={r.id}>
                        <td><Badge value={r.kind === "overdue" ? "overdue" : r.kind === "due" ? "checked_in" : "planned"} text={kindText[r.kind]} /></td>
                        <td>{date(r.due_date)}</td>
                        <td className="small">{r.message}</td>
                        <td className="small muted">{dateTime(r.created_at)}</td>
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
      </div>
    </Page>
  );
}
