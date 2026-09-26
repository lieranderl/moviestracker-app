# GitHub Copilot instructions

Follow [AGENTS.md](../AGENTS.md): the project's architecture, UI rules
(Datastar and DaisyUI first, no inline styles), test-first workflow and
commands. The guides in [docs/](../docs) cover the details.

- Go 1.27 with Templ, Datastar v1 and DaisyUI 5, streaming state over
  Server-Sent Events; no client application framework.
- Run `make ci` before concluding work.
- Changes reach `main` only through pull requests titled as Conventional
  Commits ([docs/MAINTAINING.md](../docs/MAINTAINING.md)).
