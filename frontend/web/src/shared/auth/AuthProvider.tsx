import React, { useCallback, useEffect, useMemo, useState } from "react";
import {
  apiConfirmResetPassword,
  apiLogin,
  apiLogout,
  apiMe,
  apiRegister,
  apiResendVerification,
  apiResetPassword,
  apiVerifyTwoFALogin,
  apiRequestTwoFAEnable,
  apiConfirmTwoFAEnable,
  apiPing,
  apiUpdateTwoFA,
  apiRequestTwoFADisable,
  apiUpdateProfile,
  apiVerifyEmail,
  type ApiUser,
} from "./authApi";
import { AuthContext, type AuthContextValue } from "./AuthContext";
import {
  normalizePrimaryExchange,
  readGuestPrimaryExchange,
  writeGuestPrimaryExchange,
  type PrimaryExchange,
} from "../exchange/primaryExchange";

export default function AuthProvider({ children }: { children: React.ReactNode }) {
  const [user, setUser] = useState<ApiUser | null>(null);
  const [isLoading, setIsLoading] = useState(true);
  const [guestPrimaryExchange, setGuestPrimaryExchange] = useState<PrimaryExchange | null>(() =>
    readGuestPrimaryExchange()
  );

  const refresh = useCallback(async () => {
    setIsLoading(true);
    try {
      const { user } = await apiMe();
      setUser(user);
    } catch {
      setUser(null);
    } finally {
      setIsLoading(false);
    }
  }, []);

  useEffect(() => {
    // при старте приложения узнаём, есть ли сессия
    void refresh();
  }, [refresh]);

  useEffect(() => {
    if (!user?.id) return;
    let timer: ReturnType<typeof setInterval> | null = null;
    let pingInFlight = false;

    const doPing = async () => {
      if (pingInFlight) return;
      pingInFlight = true;
      try {
        await apiPing();
      } catch {
        // игнорируем — статус обновится при следующем успешном действии
      } finally {
        pingInFlight = false;
      }
    };

    void doPing();
    timer = setInterval(doPing, 60_000);

    const handleVisibility = () => {
      if (document.visibilityState === "visible") {
        void doPing();
      }
    };
    document.addEventListener("visibilitychange", handleVisibility);

    return () => {
      if (timer) clearInterval(timer);
      document.removeEventListener("visibilitychange", handleVisibility);
    };
  }, [user?.id]);

  const login = useCallback(async (email: string, password: string, captchaToken?: string) => {
    const res = await apiLogin(email, password, captchaToken);
    if (res.requiresTwoFA) {
      return { requiresTwoFA: true, challenge: res.challenge };
    }
    if (res.user) {
      setUser(res.user);
    }
    return { requiresTwoFA: false };
  }, []);

  const verifyTwoFALogin = useCallback(async (email: string, code: string, challenge: string) => {
    const { user } = await apiVerifyTwoFALogin(email, code, challenge);
    setUser(user);
  }, []);

  const register = useCallback(async (email: string, password: string, captchaToken?: string) => {
    const res = await apiRegister(email, password, captchaToken);
    return res;
  }, []);

  const verifyEmail = useCallback(async (email: string, code: string) => {
    const { user } = await apiVerifyEmail(email, code);
    setUser(user);
  }, []);

  const resendVerification = useCallback(async (email: string) => {
    await apiResendVerification(email);
  }, []);

  const resetPassword = useCallback(async (email: string, captchaToken?: string) => {
    return await apiResetPassword(email, captchaToken);
  }, []);

  const confirmResetPassword = useCallback(async (token: string, newPassword: string) => {
    await apiConfirmResetPassword(token, newPassword);
  }, []);

  const logout = useCallback(async () => {
    try {
      await apiLogout();
    } finally {
      setUser(null);
    }
  }, []);

  const updateProfile = useCallback(async (input: {
    displayName?: string;
    avatarUrl?: string;
    primaryExchange?: PrimaryExchange;
  }) => {
    const { user } = await apiUpdateProfile(input);
    setUser(user);
  }, []);

  const isAuth = Boolean(user);

  const setPrimaryExchange = useCallback(async (exchange: PrimaryExchange) => {
    if (isAuth) {
      const { user: updated } = await apiUpdateProfile({ primaryExchange: exchange });
      setUser(updated);
      return;
    }
    writeGuestPrimaryExchange(exchange);
    setGuestPrimaryExchange(exchange);
  }, [isAuth]);

  const updateTwoFA = useCallback(async (enabled: boolean, password?: string, code?: string) => {
    const { user } = await apiUpdateTwoFA(enabled, password, code);
    setUser(user);
  }, []);

  const requestTwoFADisable = useCallback(async () => {
    await apiRequestTwoFADisable();
  }, []);

  const requestTwoFAEnable = useCallback(async () => {
    await apiRequestTwoFAEnable();
  }, []);

  const confirmTwoFAEnable = useCallback(async (code: string) => {
    const { user } = await apiConfirmTwoFAEnable(code);
    setUser(user);
  }, []);

  const primaryExchange = user
    ? normalizePrimaryExchange(user.primaryExchange)
    : guestPrimaryExchange;

  const value = useMemo<AuthContextValue>(() => ({
    isAuth,
    user,
    isLoading,
    primaryExchange,
    setPrimaryExchange,
    login,
    verifyTwoFALogin,
    register,
    verifyEmail,
    resendVerification,
    resetPassword,
    logout,
    updateProfile,
    updateTwoFA,
    requestTwoFAEnable,
    requestTwoFADisable,
    confirmTwoFAEnable,
    refresh,
    confirmResetPassword,
  }), [
    isAuth,
    user,
    isLoading,
    primaryExchange,
    setPrimaryExchange,
    login,
    verifyTwoFALogin,
    register,
    verifyEmail,
    resendVerification,
    resetPassword,
    logout,
    updateProfile,
    updateTwoFA,
    requestTwoFAEnable,
    requestTwoFADisable,
    confirmTwoFAEnable,
    refresh,
    confirmResetPassword,
  ]);

  return <AuthContext.Provider value={value}>{children}</AuthContext.Provider>;
}
