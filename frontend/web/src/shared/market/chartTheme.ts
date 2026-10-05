/** Theme colors for canvas charts, which cannot resolve CSS variables themselves. */
export function readChartTheme() {
  const styles = getComputedStyle(document.documentElement);
  const color = (name: string) => styles.getPropertyValue(name).trim();
  return {
    surface: color("--card"),
    foreground: color("--muted-foreground"),
    grid: color("--border"),
    crosshair: color("--muted-foreground"),
    label: color("--secondary"),
    up: color("--primary"),
    down: color("--destructive"),
  };
}
