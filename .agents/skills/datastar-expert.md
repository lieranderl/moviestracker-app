# Skill: Datastar Expert

## Purpose & Scope
This guide provides deep technical instructions for generating, refactoring, and maintaining **Datastar v1.0** reactive hypermedia code.

---

## 1. Core Directives

### `data-signals`
Initializes reactive signals scoped to an element and its descendants.
```html
<!-- Object syntax -->
<div data-signals="{ count: 0, user: { name: 'Alex' } }"></div>

<!-- Specific signal syntax -->
<div data-signals:count="0"></div>

<!-- If missing modifier (preserves existing client signal if already present) -->
<div data-signals:count__ifmissing="10"></div>
```

### `data-bind`
Sets up two-way data binding between an HTML input and a signal.
```html
<input type="text" data-bind:name />
<input type="checkbox" data-bind:agree />
<select data-bind:country>
  <option value="us">United States</option>
  <option value="eu">Europe</option>
</select>
```

### `data-text` & `data-show`
Reactive text output and conditional visibility.
```html
<span data-text="$count"></span>
<div data-show="$count > 0" class="alert">Count is positive</div>
```

### `data-init` & Persistent SSE Streaming (Best Practice)
For continuous real-time feeds without repeated HTTP polling handshakes:
```html
<!-- Automatically connects to the streaming SSE endpoint on mount -->
<div data-init="@get('/api/live-stats?stream=true')">
  <div id="live-stats-container">...</div>
</div>
```
When the element unmounts or the user navigates away, Datastar automatically aborts the fetch stream, signaling the backend via `r.Context().Done()` to terminate cleanly.

### `data-on:<event>`
Event listeners that execute Datastar expressions or backend actions.
```html
<button data-on:click="$count++">Increment</button>
<button data-on:click="@post('/api/save')">Save</button>
<!-- Note: data-on:submit automatically calls preventDefault() on HTMLFormElements -->
<form data-on:submit="@post('/api/login')"></form>
```

Modifiers (Use double underscore `__` syntax):
- `__prevent` (e.g. `data-on:click__prevent`, note: `data-on:submit` already prevents default on forms)
- `__stop` (stops event propagation)
- `__debounce.300ms`
- `__throttle.500ms`
- `__window` / `__document`
- `__outside`

### `data-on-interval`
Triggers periodic actions without client JavaScript timers. Use for periodic refreshes where a persistent stream is not needed.
```html
<div data-on-interval__duration.5s="@get('/api/live-stats')"></div>
```

---

## 2. Server-Sent Events (SSE) Protocol

Datastar expects the backend to stream events using `Content-Type: text/event-stream`.

### Patch Elements (`datastar-patch-elements`)
Morphs HTML fragments into the DOM by ID matching:
```
event: datastar-patch-elements
data: elements <div id="target-id">New Content</div>

```

Optional modes (default is `outer` morphing):
- `outer` (default): Morphs entire element, preserving state.
- `inner`: Morphs child contents.
- `append` / `prepend`: Inserts at end / start.
- `replace`: Replaces entire node without morphing.
- `remove`: Removes target element.

### Patch Signals (`datastar-patch-signals`)
Updates client reactive signal values from the server:
```
event: datastar-patch-signals
data: signals {"count": 42, "status": "Ready"}

```

### Execute Script via SSE
To execute client-side scripts (e.g. redirects or client alerts), the SDK sends a `<script>` tag inside `datastar-patch-elements` targeting `body` with `mode append`:
```go
_ = sse.Redirect("/dashboard")
```

---

## 3. Keep Datastar Code DRY Rules

Based on [Keep Datastar Code DRY](https://data-star.dev/how_tos/keep_datastar_code_dry):

1. **Avoid Repeating Actions**: Never attach identical `@get()` or `@post()` calls across sibling elements.
2. **Leverage Event Bubbling**:
   ```html
   <!-- DRY: One listener on parent container handles all items -->
   <div data-on:click="evt.target.closest('button')?.dataset.op && @post('/api/counter?op=' + evt.target.closest('button').dataset.op)">
     <button data-op="decrement">- 1</button>
     <button data-op="reset">Reset</button>
     <button data-op="increment">+ 1</button>
   </div>
   ```
3. **Server-Side Template DRYness**: Group repetitive markup in reusable server helper functions (components) with type-safe parameters.
