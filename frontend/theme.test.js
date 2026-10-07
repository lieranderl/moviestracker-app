import { beforeEach, expect, test } from "bun:test";

let store;
let applied;

beforeEach(() => {
  store = new Map();
  applied = null;
  globalThis.localStorage = {
    getItem: (k) => (store.has(k) ? store.get(k) : null),
    setItem: (k, v) => store.set(k, String(v)),
  };
  globalThis.document = {
    documentElement: { setAttribute: (name, value) => (applied = { name, value }) },
  };
});

let run = 0;
const applyTheme = () => import(`./theme.js?run=${run++}`);

test("a saved dark theme is applied before first paint", async () => {
  store.set("theme", "dark");
  await applyTheme();
  expect(applied).toEqual({ name: "data-theme", value: "dark" });
});

test("the system theme leaves the page to prefers-color-scheme", async () => {
  store.set("theme", "system");
  await applyTheme();
  expect(applied).toBeNull();
});

test("a theme saved under its old GitHub name keeps working", async () => {
  store.set("theme", "githubdark");
  await applyTheme();
  expect(applied).toEqual({ name: "data-theme", value: "dark" });
  expect(store.get("theme")).toBe("dark");

  store.set("theme", "githublight");
  await applyTheme();
  expect(applied).toEqual({ name: "data-theme", value: "light" });
  expect(store.get("theme")).toBe("light");
});
