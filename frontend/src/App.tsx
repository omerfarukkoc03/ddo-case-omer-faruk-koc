import { useState } from "react";
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

interface NavItem {
  to: string;
  label: string;
  icon: string;
  roles: Role[];
}

// Menü rol bazlıdır; asıl yetki kontrolü sunucuda yapılır (her uç noktada RBAC).
const NAV: NavItem[] = [
  { to: "/", label: "Pano", icon: "📊", roles: ["vet", "technician", "reception", "pharmacy"] },
  { to: "/my", label: "Hayvanlarım", icon: "🐾", roles: ["owner"] },
  { to: "/animals", label: "Hasta kayıtları", icon: "🐾", roles: ["vet", "technician", "reception"] },
  { to: "/appointments", label: "Randevular", icon: "📅", roles: ["vet", "technician", "reception", "owner"] },
  { to: "/prescribe", label: "Reçete & doz", icon: "💊", roles: ["vet"] },
  { to: "/pharmacy", label: "Eczane & stok", icon: "📦", roles: ["pharmacy", "vet"] },
  { to: "/hospital", label: "Yatan hasta", icon: "🏥", roles: ["vet", "technician", "reception"] },
  { to: "/billing", label: "Fatura & tahsilat", icon: "🧾", roles: ["reception", "owner"] },
  { to: "/merge", label: "Mükerrer kayıtlar", icon: "🔀", roles: ["reception"] },
  { to: "/reminders", label: "Hatırlatmalar", icon: "🔔", roles: ["reception", "vet", "owner"] },
];

export default function App() {
  const user = useUser();
  const qc = useQueryClient();
  const [menu, setMenu] = useState(false);
  const loc = useLocation();
  if (!user) return <Login />;
  const items = NAV.filter((n) => n.roles.includes(user.role));
  const home = user.role === "owner" ? "/my" : "/";
  const allowed = (path: string) => items.some((n) => n.to === path);

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
          {items.map((n) => (
            <NavLink key={n.to} to={n.to} end={n.to === "/"}>
              <span aria-hidden>{n.icon}</span> {n.label}
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
          <Route path="/" element={allowed("/") ? <Dashboard /> : <Navigate to={home} replace />} />
          <Route path="/my" element={<MyPets />} />
          <Route path="/animals" element={<Animals />} />
          <Route path="/animals/:id" element={<AnimalDetail />} />
          <Route path="/appointments" element={<Appointments />} />
          <Route path="/prescribe" element={<Prescribe />} />
          <Route path="/pharmacy" element={<Pharmacy />} />
          <Route path="/hospital" element={<Hospital />} />
          <Route path="/billing" element={<Billing />} />
          <Route path="/merge" element={<Merge />} />
          <Route path="/reminders" element={<Reminders />} />
          <Route path="*" element={<Navigate to={home} replace />} />
        </Routes>
      </main>
    </div>
  );
}
