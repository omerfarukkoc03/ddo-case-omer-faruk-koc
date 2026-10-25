import { useState, type FormEvent } from "react";
import { activateAccount, login } from "../api";
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
  const [mode, setMode] = useState<"login" | "activate">("login");
  return (
    <div className="login">
      <div className="card">
        <div className="brand" style={{ padding: 0, marginBottom: 4 }}>🐾 PatiKlinik</div>
        <p className="muted" style={{ marginTop: 0 }}>Veteriner klinik hasta, aşı ve randevu sistemi</p>
        {mode === "login" ? <LoginForm onActivate={() => setMode("activate")} /> : <ActivateForm onBack={() => setMode("login")} />}
      </div>
    </div>
  );
}

function LoginForm({ onActivate }: { onActivate: () => void }) {
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
    <>
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
      <p className="small" style={{ marginBottom: 0 }}>
        Hayvan sahibi misiniz ve klinikten etkinleştirme kodu aldınız mı?{" "}
        <button className="btn btn-ghost btn-sm" style={{ color: "var(--brand)", padding: 0 }} onClick={onActivate}>
          Hesabımı etkinleştir →
        </button>
      </p>
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
    </>
  );
}

// Sahip portal hesabı yalnız klinikte kimliği doğrulandıktan sonra resepsiyonun verdiği kodla açılır; böylece
// başkasının telefonunu ya da e-postasını bilen biri onun hayvanlarının kayıtlarına erişemez.
function ActivateForm({ onBack }: { onBack: () => void }) {
  const [f, setF] = useState({ email: "", code: "", password: "", password2: "" });
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<unknown>(null);
  const mismatch = f.password2 !== "" && f.password !== f.password2;

  async function submit(e: FormEvent) {
    e.preventDefault();
    if (f.password !== f.password2) return;
    setBusy(true);
    setError(null);
    try {
      await activateAccount(f.email, f.code, f.password);
    } catch (err) {
      setError(err);
    } finally {
      setBusy(false);
    }
  }
  const set = (k: keyof typeof f) => (e: { target: { value: string } }) => setF({ ...f, [k]: e.target.value });

  return (
    <form className="form" onSubmit={submit}>
      <h2>Hesabımı etkinleştir</h2>
      <p className="small muted" style={{ margin: 0 }}>
        Kliniğe kayıt sırasında resepsiyon size 8 karakterlik bir etkinleştirme kodu verir (48 saat geçerli, tek kullanımlık).
        Kodu almadıysanız kliniği arayın.
      </p>
      <Field label="E-posta" hint="Kliniğe verdiğiniz e-posta adresi">
        <input type="email" autoComplete="username" value={f.email} onChange={set("email")} required />
      </Field>
      <Field label="Etkinleştirme kodu">
        <input value={f.code} onChange={set("code")} placeholder="XXXX-XXXX" autoCapitalize="characters" className="mono" required />
      </Field>
      <Field label="Yeni parola" hint="En az 8 karakter, en az bir harf ve bir rakam">
        <input type="password" autoComplete="new-password" value={f.password} onChange={set("password")} minLength={8} required />
      </Field>
      <Field label="Yeni parola (tekrar)">
        <input type="password" autoComplete="new-password" value={f.password2} onChange={set("password2")} required />
      </Field>
      {mismatch && <div className="state state-error">Parolalar eşleşmiyor.</div>}
      <ErrorBox error={error} />
      <button className="btn btn-primary" disabled={busy || mismatch}>{busy ? "Etkinleştiriliyor…" : "Hesabımı etkinleştir ve giriş yap"}</button>
      <button type="button" className="btn btn-ghost btn-sm" onClick={onBack}>← Girişe dön</button>
    </form>
  );
}
