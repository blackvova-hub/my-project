import type { DrawingTool } from "./chartDrawings";

export function ToolIcon({ tool }: { tool: DrawingTool }) {
  if (tool === "cursor") {
    return (
      <svg viewBox="0 0 20 20" aria-hidden="true" className="h-[18px] w-[18px]">
        <path d="M3.5 2.8 15 10l-5.1 1.1-2.7 4.5L3.5 2.8Z" fill="none" stroke="currentColor" strokeWidth="1.5" strokeLinejoin="round" />
      </svg>
    );
  }
  if (tool === "horizontal") {
    return (
      <svg viewBox="0 0 20 20" aria-hidden="true" className="h-[18px] w-[18px]">
        <path d="M3 10h14" fill="none" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" />
        <circle cx="4" cy="10" r="1.4" fill="currentColor" />
        <circle cx="16" cy="10" r="1.4" fill="currentColor" />
      </svg>
    );
  }
  if (tool === "vertical") {
    return (
      <svg viewBox="0 0 20 20" aria-hidden="true" className="h-[18px] w-[18px]">
        <path d="M10 3v14" fill="none" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" />
        <circle cx="10" cy="4" r="1.4" fill="currentColor" />
        <circle cx="10" cy="16" r="1.4" fill="currentColor" />
      </svg>
    );
  }
  if (tool === "trend") {
    return (
      <svg viewBox="0 0 20 20" aria-hidden="true" className="h-[18px] w-[18px]">
        <path d="m4 15 12-10" fill="none" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" />
        <circle cx="4" cy="15" r="1.8" fill="#101913" stroke="currentColor" strokeWidth="1.4" />
        <circle cx="16" cy="5" r="1.8" fill="#101913" stroke="currentColor" strokeWidth="1.4" />
      </svg>
    );
  }
  if (tool === "ray") {
    return (
      <svg viewBox="0 0 20 20" aria-hidden="true" className="h-[18px] w-[18px]">
        <path d="M3.5 15.5 17 4" fill="none" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" />
        <circle cx="4" cy="15" r="1.7" fill="#101913" stroke="currentColor" strokeWidth="1.4" />
        <path d="m13.5 4 3.5.1-.3 3.4" fill="none" stroke="currentColor" strokeWidth="1.25" strokeLinecap="round" strokeLinejoin="round" />
      </svg>
    );
  }
  if (tool === "rectangle") {
    return (
      <svg viewBox="0 0 20 20" aria-hidden="true" className="h-[18px] w-[18px]">
        <rect x="3.5" y="4.5" width="13" height="11" rx="1" fill="none" stroke="currentColor" strokeWidth="1.4" />
      </svg>
    );
  }
  if (tool === "fibonacci") {
    return (
      <svg viewBox="0 0 20 20" aria-hidden="true" className="h-[18px] w-[18px]">
        <path d="M3 4h14M3 8h14M3 12h14M3 16h14" fill="none" stroke="currentColor" strokeWidth="1.2" strokeLinecap="round" />
        <path d="M5 3v14M15 3v14" fill="none" stroke="currentColor" strokeWidth="1" opacity=".55" />
      </svg>
    );
  }
  if (tool === "brush") {
    return (
      <svg viewBox="0 0 20 20" aria-hidden="true" className="h-[18px] w-[18px]">
        <path d="m3.4 14.2 10.2-10.2 2.4 2.4L5.8 16.6 3 17l.4-2.8Z" fill="none" stroke="currentColor" strokeWidth="1.35" strokeLinejoin="round" />
        <path d="m12.5 5.1 2.4 2.4M3.4 14.2l2.4 2.4" fill="none" stroke="currentColor" strokeWidth="1.2" strokeLinecap="round" />
      </svg>
    );
  }
  if (tool === "text") {
    return (
      <svg viewBox="0 0 20 20" aria-hidden="true" className="h-[18px] w-[18px]">
        <path d="M4 4h12M10 4v12M7 16h6" fill="none" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" />
      </svg>
    );
  }
  return (
    <svg viewBox="0 0 20 20" aria-hidden="true" className="h-[18px] w-[18px]">
      <rect x="2.5" y="6.5" width="15" height="7" rx="1" fill="none" stroke="currentColor" strokeWidth="1.35" />
      <path d="M5 7v3M8 7v2M11 7v3M14 7v2" fill="none" stroke="currentColor" strokeWidth="1.15" strokeLinecap="round" />
    </svg>
  );
}

