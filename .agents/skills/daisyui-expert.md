# Skill: DaisyUI 5 & Tailwind CSS Expert

## Purpose & Scope
Guidelines for writing clean, accessible, and high-performance UI using **DaisyUI 5** and **Tailwind CSS 4**.

---

## 1. DaisyUI 5 Component Conventions

### Navbar & Blurred Toolbar
```html
<header class="sticky top-0 z-50 w-full backdrop-blur-md bg-base-100/80 border-b border-base-content/10">
  <div class="navbar max-w-7xl mx-auto px-4 sm:px-8">
    <div class="navbar-start">...</div>
    <div class="navbar-center hidden md:flex">...</div>
    <div class="navbar-end">...</div>
  </div>
</header>
```

### Popover Dropdowns (Standard HTML Popover API)
DaisyUI 5 standardizes dropdown menus using native HTML popover attributes:
```html
<!-- Trigger -->
<button
  class="btn btn-ghost btn-circle avatar"
  popovertarget="my-dropdown"
  style="anchor-name:--my-dropdown"
>
  <div class="w-10 rounded-full overflow-hidden">
    <img width="40" height="40" src="/avatar.jpg" alt="User" class="object-cover w-full h-full" />
  </div>
</button>

<!-- Dropdown content -->
<div
  class="dropdown dropdown-end menu menu-sm bg-base-100/95 backdrop-blur-xl border border-base-content/10 rounded-2xl z-50 p-3 shadow-2xl"
  popover
  id="my-dropdown"
  style="position-anchor:--my-dropdown"
>
  ...
</div>
```

### Inputs & Forms
- In DaisyUI 5, use `input` directly.
- **Do NOT** use `input-bordered` (deprecated in v5).
- Input groups:
  ```html
  <div class="space-y-1.5">
    <label class="block text-xs font-semibold text-base-content/80 uppercase" for="email">Email</label>
    <input id="email" type="email" class="input w-full rounded-xl bg-base-200/50 focus:bg-base-100" />
  </div>
  ```

### Cards & Glassmorphism
```html
<div class="card bg-base-100/80 backdrop-blur-xl border border-base-content/10 shadow-2xl rounded-3xl">
  <div class="card-body p-8">
    <h2 class="card-title">Title</h2>
    <p>Content</p>
  </div>
</div>
```

### Color Semantics & Anti-Patterns
- **Use**: `bg-primary`, `text-primary`, `bg-base-100`, `bg-base-200`, `text-base-content`, `alert-error`, `badge-soft`.
- **Avoid**: Hardcoded hex colors (e.g. `#1f2937`) or inline CSS styles for colors.
- **Avoid**: `transition-all`. Always specify the property: `transition-colors`, `transition-opacity`, or `transition-transform`.
- **Images**: Always include `width`, `height`, `alt`, and `class="object-cover w-full h-full"`.
