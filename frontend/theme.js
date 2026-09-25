// Applies the persisted theme before first paint to avoid a flash of the wrong
// theme. Loaded synchronously in <head>; keep this file tiny.
try {
  const theme = localStorage.getItem("theme");
  if (theme && theme !== "system") {
    document.documentElement.setAttribute("data-theme", theme);
  }
} catch {
  // Storage unavailable (private mode / blocked): fall back to system theme.
}
