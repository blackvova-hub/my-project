import { createContext, useContext } from "react";
import type { ApiUser } from "./authApi";
import type { PrimaryExchange } from "../exchange/primaryExchange";

export type AuthContextValue = {
  isAuth: boolean;
  user: ApiUser | null;
  isLoading: boolean;
  primaryExchange: PrimaryExchange | null;
  setPrimaryExchange: (exchange: PrimaryExchange) => Promise<void>;
  login: (email: string, password: string, captchaToken?: string) => Promise<{ requiresTwoFA: boolean; challenge?: string }>;

  verifyTwoFALogin: (email: string, code: string, challenge: string) => Promise<void>;
  register: (email: string, password: string, captchaToken?: string) => Promise<{ requiresVerification: boolean }>;

  verifyEmail: (email: string, code: string) => Promise<void>;
  resendVerification: (email: string) => Promise<void>;
  resetPassword: (email: string, captchaToken?: string) => Promise<{ message?: string } | void>;
  confirmResetPassword: (token: string, newPassword: string) => Promise<void>;
  logout: () => Promise<void>;
  updateProfile: (input: { displayName?: string; avatarUrl?: string; primaryExchange?: PrimaryExchange }) => Promise<void>;
  updateTwoFA: (enabled: boolean, password?: string, code?: string) => Promise<void>;
  requestTwoFAEnable: () => Promise<void>;
  requestTwoFADisable: () => Promise<void>;
  confirmTwoFAEnable: (code: string) => Promise<void>;
  refresh: () => Promise<void>;
};

export const AuthContext = createContext<AuthContextValue | null>(null);

export function useAuth() {
  const ctx = useContext(AuthContext);
  if (!ctx) throw new Error("useAuth must be used within AuthProvider");
  return ctx;
}
