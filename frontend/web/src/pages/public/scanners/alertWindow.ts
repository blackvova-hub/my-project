export const ALERT_WINDOW_MAX_MINUTES = 1440;

export function isValidAlertWindowMinutes(value: number, maximum: number): boolean {
  return Number.isFinite(value) && value >= 1 && value <= maximum;
}
