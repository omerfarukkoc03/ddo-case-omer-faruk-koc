import { useState, type ReactElement } from "react";
import { NavLink, Navigate, Route, Routes, useLocation } from "react-router-dom";
import { useQueryClient } from "@tanstack/react-query";
import { logout, roleLabel, type Role } from "./api";
import { useUser } from "./auth";
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
  if (!user) return <Login />;
  const pages = PAGES.filter((p) => p.roles.includes(user.role));
  const home = user.role === "owner" ? "/my" : "/";

  return (
    <div className="app">
      <div className="topbar">
        <button className="btn btn-sm" onClick={() => setMenu(!menu)} aria-label="Menü">☰</button>
        <strong>🐾 PatiKlinik</strong>
        <span className="small muted">{roleLabel[user.role]}</span>
      </div>
      <aside className={`sidebar ${menu ? "open" : ""}`} onClick={() => setMenu(false)}>
        <div className="brand">🐾 PatiKlinik</div>
        <nav className="nav">
          {pages.filter((p) => p.nav).map((p) => (
            <NavLink key={p.path} to={p.path} end={p.path === "/"}>
              <span aria-hidden>{p.nav!.icon}</span> {p.nav!.label}
            </NavLink>
          ))}
        </nav>
        <div className="userbox">
          <div><strong>{user.name}</strong></div>
          <div className="role">{roleLabel[user.role]}</div>
          <button
            className="btn btn-sm"
            style={{ marginTop: 8 }}
            onClick={async () => {
              await logout();
              qc.clear();
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
