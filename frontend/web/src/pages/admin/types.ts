export type AdminUser = {
  id: string;
  num_id: number;
  public_id: number;
  email: string;
  displayName: string;
  lastIp: string;
  plan: string;
  subscriptionExpiresAt?: string | null;
  subscriptionFrozenAt?: string | null;
  subscriptionFrozenDaysRemaining?: number;
  subscriptionDaysRemaining?: number;
  subscriptionActive?: boolean;
  isAdmin: boolean;
  twoFAEnabled: boolean;
  emailVerified: boolean;
  lastSeenAt?: string | null;
  telegramLinked?: boolean;
  scannerSlots?: string[];
  createdAt: string;
  updatedAt: string;
};

export type AdminScannerRule = {
  id: number;
  scanner_slot: string;
  symbol: string;
  window_minutes: number;
  enabled: boolean;
  conditions: unknown;
};
