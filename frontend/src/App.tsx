import { useState, type ReactElement } from "react";
import { NavLink, Navigate, Route, Routes, useLocation, useNavigate } from "react-router-dom";
import { useQueryClient } from "@tanstack/react-query";
import { logout, roleLabel, type Role } from "./api";
import { useUser } from "./auth";
import { initials } from "./ui";
import Login from "./pages/Login";
import Dashboard from "./pages/Dashboard";
import Animals from "./pages/Animals";
import AnimalDetail from "./pages/AnimalDetail";
import Appointments from "./pages/Appointments";
import Prescribe from "./pages/Prescribe";
import Pharmacy from "./pages/Pharmacy";
import Hospital from "./pages/Hospital";
import Billing from "./pages/Billing";
import Merge from "./pages/Merge";
import MyPets from "./pages/MyPets";
import Reminders from "./pages/Reminders";

interface Page {
  path: string;
  element: ReactElement;
  roles: Role[];
  // nav doluysa sayfa menüde görünür.
  nav?: { label: string; icon: string };
}

const ALL: Role[] = ["owner", "vet", "technician", "reception", "pharmacy"];

// Tek kaynak: hem menü hem rota koruması bu tablodan üretilir; menüde görünmeyen bir sayfaya adres çubuğundan
// girilirse kullanıcı kendi ana sayfasına yönlenir. Asıl yetki kontrolü yine sunucudadır (her uç noktada RBAC);
// bu katman yalnız kullanıcıya yetkisi olmayan ekranın hiç çizilmemesini sağlar.
const PAGES: Page[] = [
  { path: "/", element: <Dashboard />, roles: ["vet", "technician", "reception", "pharmacy"], nav: { label: "Pano", icon: "📊" } },
  { path: "/my", element: <MyPets />, roles: ["owner"], nav: { label: "Hayvanlarım", icon: "🐾" } },
  { path: "/animals", element: <Animals />, roles: ["vet", "technician", "reception"], nav: { label: "Hasta kayıtları", icon: "🐾" } },
  // Sahip de hasta detayına erişebilir; sunucu yalnız kendi hayvanını döndürür.
  { path: "/animals/:id", element: <AnimalDetail />, roles: ALL },
  { path: "/appointments", element: <Appointments />, roles: ["vet", "technician", "reception", "owner"], nav: { label: "Randevular", icon: "📅" } },
  { path: "/prescribe", element: <Prescribe />, roles: ["vet"], nav: { label: "Reçete & doz", icon: "💊" } },
  { path: "/pharmacy", element: <Pharmacy />, roles: ["pharmacy", "vet"], nav: { label: "Eczane & stok", icon: "📦" } },
  { path: "/hospital", element: <Hospital />, roles: ["vet", "technician", "reception"], nav: { label: "Yatan hasta", icon: "🏥" } },
  { path: "/billing", element: <Billing />, roles: ["reception", "owner"], nav: { label: "Fatura & tahsilat", icon: "🧾" } },
  { path: "/merge", element: <Merge />, roles: ["reception"], nav: { label: "Mükerrer kayıtlar", icon: "🔀" } },
  { path: "/reminders", element: <Reminders />, roles: ["reception", "vet", "owner"], nav: { label: "Hatırlatmalar", icon: "🔔" } },
];

export default function App() {
  const user = useUser();
  const qc = useQueryClient();
  const [menu, setMenu] = useState(false);
  const loc = useLocation();
  const navigate = useNavigate();
  if (!user) return <Login />;
  const pages = PAGES.filter((p) => p.roles.includes(user.role));
  const home = user.role === "owner" ? "/my" : "/";

  return (
    <div className="app">
      <div className="topbar">
        <button className="btn btn-sm" onClick={() => setMenu(!menu)} aria-label="Menü">☰</button>
        <strong className="row" style={{ gap: 6 }}><span aria-hidden>🐾</span> PatiKlinik</strong>
        <span className="avatar" title={`${user.name} · ${roleLabel[user.role]}`}>{initials(user.name)}</span>
      </div>
      <div className={`scrim ${menu ? "open" : ""}`} onClick={() => setMenu(false)} />
      <aside className={`sidebar ${menu ? "open" : ""}`} onClick={(e) => (e.target as HTMLElement).closest("a") && setMenu(false)}>
        <div className="brand">
          <span className="brand-mark" aria-hidden>🐾</span>
          <span>PatiKlinik<small>Veteriner klinik sistemi</small></span>
        </div>
        <div className="nav-title">Menü</div>
        <nav className="nav">
          {pages.filter((p) => p.nav).map((p) => (
            <NavLink key={p.path} to={p.path} end={p.path === "/"}>
              <span className="ico" aria-hidden>{p.nav!.icon}</span> {p.nav!.label}
            </NavLink>
          ))}
        </nav>
        <div className="userbox">
          <div className="who">
            <span className="avatar" aria-hidden>{initials(user.name)}</span>
            <div style={{ minWidth: 0 }}>
              <div className="name">{user.name}</div>
              <span className="badge badge-muted">{roleLabel[user.role]}</span>
            </div>
          </div>
          <button
            className="btn btn-sm"
            style={{ marginTop: 10, width: "100%" }}
            onClick={async () => {
              await logout();
              qc.clear();
              // Sonraki kullanıcı, öncekinin açık bıraktığı sayfaya değil kendi ana sayfasına girsin.
              navigate("/", { replace: true });
            }}
          >
            Çıkış yap
          </button>
        </div>
      </aside>
      <main className="main" key={loc.pathname}>
        <Routes>
          {PAGES.map((p) => (
            <Route key={p.path} path={p.path} element={p.roles.includes(user.role) ? p.element : <Navigate to={home} replace />} />
          ))}
          <Route path="*" element={<Navigate to={home} replace />} />
        </Routes>
      </main>
    </div>
  );
}
