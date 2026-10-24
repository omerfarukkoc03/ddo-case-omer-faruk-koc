import { useState, type FormEvent } from "react";
import { login } from "../api";
import { ErrorBox, Field } from "../ui";

const DEMO = [
  { email: "vet@patiklinik.test", label: "Veteriner" },
  { email: "tekniker@patiklinik.test", label: "Teknisyen" },
  { email: "resepsiyon@patiklinik.test", label: "Resepsiyon" },
  { email: "eczane@patiklinik.test", label: "Eczane / depo" },
  { email: "sahip@patiklinik.test", label: "Hayvan sahibi" },
  { email: "sahip2@patiklinik.test", label: "Ortak sahip" },
];
const DEMO_PASSWORD = "Demo123!";

export default function Login() {
  const [email, setEmail] = useState("");
  const [password, setPassword] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<unknown>(null);

  async function submit(e?: FormEvent, creds?: { email: string; password: string }) {
    e?.preventDefault();
    setBusy(true);
    setError(null);
    try {
      await login(creds?.email ?? email, creds?.password ?? password);
    } catch (err) {
      setError(err);
    } finally {
      setBusy(false);
    }
  }

  return (
    <div className="login">
      <div className="card">
        <div className="brand" style={{ padding: 0, marginBottom: 4 }}>🐾 PatiKlinik</div>
        <p className="muted" style={{ marginTop: 0 }}>Veteriner klinik hasta, aşı ve randevu sistemi</p>
        <form className="form" onSubmit={submit}>
          <Field label="E-posta">
            <input type="email" autoComplete="username" value={email} onChange={(e) => setEmail(e.target.value)} required />
          </Field>
          <Field label="Parola">
            <input type="password" autoComplete="current-password" value={password} onChange={(e) => setPassword(e.target.value)} required />
          </Field>
          <ErrorBox error={error} />
          <button className="btn btn-primary" disabled={busy}>{busy ? "Giriş yapılıyor…" : "Giriş yap"}</button>
        </form>
        <hr style={{ border: 0, borderTop: "1px solid var(--border)", margin: "16px 0" }} />
        <p className="small muted" style={{ marginTop: 0 }}>
          Demo hesapları (parola <code>{DEMO_PASSWORD}</code>):
        </p>
        <div className="demo-accounts">
          {DEMO.map((d) => (
            <button key={d.email} className="btn btn-sm" disabled={busy} onClick={() => submit(undefined, { email: d.email, password: DEMO_PASSWORD })}>
              {d.label}
            </button>
          ))}
        </div>
      </div>
    </div>
  );
}
