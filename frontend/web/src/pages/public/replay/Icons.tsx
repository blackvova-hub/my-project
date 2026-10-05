export function Icon({ name }: { name: string }) {
  const paths: Record<string, string> = {
    cursor: "M12 2v8M12 14v8M2 12h8M14 12h8",
    calendar: "M4 5h16v16H4ZM8 3v4M16 3v4M4 10h16M8 14h3M14 14h2M8 17h3",
    fullscreen: "M3 9V3h6M15 3h6v6M21 15v6h-6M9 21H3v-6",
    chevron: "m7 10 5 5 5-5",
    trend: "M4 19 20 5M3 17v3h3M18 4h3v3",
    horizontal: "M3 12h18M4 10v4M20 10v4",
    vertical: "M12 3v18M10 4h4M10 20h4",
    ray: "M3 20 20 3M15 3h5v5",
    rectangle: "M4 5h16v14H4Z",
    fibonacci: "M3 5h18M3 10h18M3 14h18M3 19h18M6 3v18",
    brush: "m4 17 12-12 3 3L7 20l-4 1Z",
    text: "M4 5h16M12 5v15M8 20h8",
    ruler: "M4 17 17 4l3 3L7 20l-3-3ZM8 15l2 2m1-5 2 2m1-5 2 2",
    undo: "m8 4-5 5 5 5M3 9h10a6 6 0 0 1 0 12",
    trash: "M3 6h18M9 6V3h6v3M6 6l1 15h10l1-15M10 10v7M14 10v7",
    clear: "M4 5h16M6 9h12M9 13h6M10 17h4M11 21h2",
    lock: "M5 10h14v11H5zM8 10V7a4 4 0 0 1 8 0v3",
    unlock: "M5 10h14v11H5zM8 10V7a4 4 0 0 1 7-2",
    eye: "M2 12s4-7 10-7 10 7 10 7-4 7-10 7S2 12 2 12ZM12 9a3 3 0 1 0 0 6 3 3 0 0 0 0-6",
    eyeOff: "M3 3l18 18M9 5.5A11 11 0 0 1 12 5c6 0 10 7 10 7a15 15 0 0 1-3 3.6M6 6.5C3.5 8.2 2 12 2 12s4 7 10 7c1.2 0 2.4-.3 3.4-.7",
    magnet: "M5 3v9a7 7 0 0 0 14 0V3h-4v9a3 3 0 0 1-6 0V3H5ZM5 7h4M15 7h4",
    previous: "M6 5v14M19 5 9 12l10 7Z",
    target: "M12 2v4M12 18v4M2 12h4M18 12h4M12 5a7 7 0 1 0 0 14 7 7 0 0 0 0-14",
    close: "m5 5 14 14M19 5 5 19",
    return:
      "M20 8a8 8 0 0 0-14-2L3 9M3 3v6h6M4 16a8 8 0 0 0 14 2l3-3M21 21v-6h-6",
    indicators: "M4 18V9M10 18V4M16 18v-6M22 18V6",
    arrow: "M4 12h16m-6-6 6 6-6 6",
  };
  return (
    <svg
      viewBox="0 0 24 24"
      width="20"
      height="20"
      aria-hidden="true"
      fill="none"
      stroke="currentColor"
      strokeWidth="1.7"
      strokeLinecap="round"
      strokeLinejoin="round"
    >
      {name === "play" ? (
        <path d="m8 4 12 8-12 8Z" fill="currentColor" stroke="none" />
      ) : name === "pause" ? (
        <path
          d="M6 5h4v14H6zM14 5h4v14h-4z"
          fill="currentColor"
          stroke="none"
        />
      ) : name === "next" ? (
        <path d="m5 5 10 7-10 7ZM18 5v14" fill="currentColor" />
      ) : (
        <path d={paths[name] ?? paths.target} />
      )}
    </svg>
  );
}
