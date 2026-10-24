import { createContext, useContext, useEffect, useState, type ReactNode } from "react";
import { loadSession, onSessionChange, type Role, type User } from "./api";

const AuthContext = createContext<User | null>(null);

export function AuthProvider({ children }: { children: ReactNode }) {
  const [user, setUser] = useState<User | null>(() => loadSession()?.user ?? null);
  useEffect(() => {
    const off = onSessionChange((s) => setUser(s?.user ?? null));
    return () => {
      off();
    };
  }, []);
  return <AuthContext.Provider value={user}>{children}</AuthContext.Provider>;
}

export const useUser = () => useContext(AuthContext);

export function useHasRole(...roles: Role[]) {
  const u = useUser();
  return !!u && roles.includes(u.role);
}
