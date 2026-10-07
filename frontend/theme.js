// Applies the persisted theme before first paint to avoid a flash of the wrong
// theme. Loaded synchronously in <head>; keep this file tiny.
try {
  let theme = localStorage.getItem("theme");
  // The themes were named githublight / githubdark before they were retuned.
  const renamed = { githublight: "light", githubdark: "dark" }[theme];
  if (renamed) {
    theme = renamed;
    localStorage.setItem("theme", theme);
  }
  if (theme && theme !== "system") {
    document.documentElement.setAttribute("data-theme", theme);
  }
} catch {
  // Storage unavailable (private mode / blocked): fall back to system theme.
}
