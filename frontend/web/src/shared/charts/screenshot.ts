import type { DrawingRuntime } from "./drawings/DrawingEditor";
export async function downloadChartScreenshot(
  api: DrawingRuntime,
  root: HTMLElement,
) {
  const source = api.chart.takeScreenshot(),
    canvas = document.createElement("canvas"),
    ctx = canvas.getContext("2d")!;
  canvas.width = source.width;
  canvas.height = source.height;
  ctx.drawImage(source, 0, 0);
  const overlays = root.querySelectorAll<SVGSVGElement>(
    ".ct-smart-overlay,.ct-drawing-overlay",
  );
  for (const overlay of overlays) {
    const copy = overlay.cloneNode(true) as SVGSVGElement;
    const styles = getComputedStyle(document.documentElement);
    copy.setAttribute("xmlns", "http://www.w3.org/2000/svg");
    copy.setAttribute("width", String(api.chart.timeScale().width()));
    copy.setAttribute("height", String(api.chart.panes()[0].getHeight()));
    let svg = new XMLSerializer().serializeToString(copy);
    svg = svg.replace(/var\((--[a-z-]+)\)/g, (_, name) =>
      styles.getPropertyValue(name).trim(),
    );
    const url = URL.createObjectURL(new Blob([svg], { type: "image/svg+xml" }));
    try {
      const img = new Image();
      await new Promise<void>((resolve, reject) => {
        img.onload = () => resolve();
        img.onerror = reject;
        img.src = url;
      });
      ctx.drawImage(
        img,
        0,
        0,
        (img.width * source.width) / api.chart.chartElement().clientWidth,
        (img.height * source.height) / api.chart.chartElement().clientHeight,
      );
    } finally {
      URL.revokeObjectURL(url);
    }
  }
  const a = document.createElement("a");
  a.href = canvas.toDataURL("image/png");
  a.download = "shortlong-chart.png";
  a.click();
}
