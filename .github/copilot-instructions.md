# GitHub Copilot Custom Instructions

Please follow the architecture, coding standards, and best practices detailed in [AGENTS.md](../AGENTS.md).

### Core Priorities:
1. **Hypermedia First**: Use Datastar v1.0 reactive signals (`data-signals`, `data-bind`, `data-on`, `@get`, `@post`) and Server-Sent Events (`datastar-patch-elements`, `datastar-patch-signals`). Do not introduce client-side SPAs (React, Vue, etc.).
2. **Keep Datastar DRY**: Always use parent event bubbling delegation for repetitive actions instead of duplicating `@action()` on every element.
3. **DaisyUI 5 Styling**: Use modern DaisyUI 5 utility classes and HTML Popover API for dropdowns. Avoid legacy DaisyUI 4 names like `input-bordered`. Avoid `transition-all`.
4. **Bun Server Runtime**: Leverage Bun's native HTTP streaming and web-standard Request/Response APIs.
5. **Quality & Tests**: Run `bun test` and `bun x tsc --noEmit` before concluding work.
