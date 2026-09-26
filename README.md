# elagoht/devtoolbar

A collage plugin that shows a small panel at the bottom of every page in
development: which page rendered, in which locale, with what status, how long the
render took, its `Cache-Control` and `ETag`, its size, and how many findings the
checking plugins reported. In production and in a static build it does nothing at
all.

```go
app, err := collage.New(&collage.Config{
	DevMode: devMode,
	Plugins: []collage.Plugin{
		htmlcheck.New(htmlcheck.Options{}),
		devtoolbar.New(), // last
	},
})
```

Requires collage v0.23.0 or later. It has nothing to configure.

## The panel

Collapsed, it is one line in the corner:

```
collage  home (en) · 200 · 1.84ms · 3 findings
```

Opened, it lists:

| | |
| --- | --- |
| Page | the page's name and locale, or "not a page render" for a document, an asset, a handler |
| Strategy | `static`, `dynamic` or `incremental` |
| Status | the response's status |
| Render time | `X-Collage-Render-Time`, which collage sets on a fresh render in development |
| Cache-Control, ETag | as collage sent them |
| Size | the body collage wrote, before the panel was added |
| Findings | errors and warnings the plugins before this one reported |
| Degraded | shown when a fragment failed, whether or not a fallback covered for it |

It is a `<details>` element, so it opens with a click, Enter or Space, and a close
button removes it from the page. Its styles are inline, scoped to its id, and
reset whatever the page's stylesheet gives its elements; it loads nothing — no script file, no stylesheet,
no font.

**Register it last.** A render's findings are those of the plugins whose
`OnAfterRender` ran before this one's, in registration order.

## How it works

The middleware holds back every HTML response and puts the panel before its last
`</body>`. What only the render knows — the page, the locale, the findings — the
plugin's `OnAfterRender` notes in a record carried in the request's context, which
the middleware reads once the response is complete. An event stream, an image, a
JSON document, a compressed body and a hijacked connection pass straight through.

In development, with the toolbar, every page is answered in full: the request's
`If-None-Match` and `If-Modified-Since` are dropped, because a `304` would have the
browser show the page it kept, with the timings of whichever request first fetched
it. The panel is added after the page cache, so what collage caches never carries
it.

## Development only

- On a server started without `DevMode`, `Init` installs no middleware and the hook
  returns at once.
- In a static build nothing is added, even from a development application: the
  hook sees `ev.Static` and returns, and the build never runs middleware.

If collage-secure's policy is on, it is report-only in development, so the panel's
inline style and close button work; the browser's console will mention them.

## Limitations

What a plugin can observe today is what is on the response and on the
`AfterRender` event. The panel cannot show:

- **Per-fragment timings.** collage reports them to `Metrics.FragmentDuration` only,
  which is the application's, not a plugin's, and carries no request to tie a
  timing to.
- **Dependency tags.** The tags a render depended on are known to the render engine
  and the cache, and are not on any page hook's event — only `RenderFragment`
  returns them, for a fragment a plugin renders itself.
- **Cache hits.** Development never reads the page cache, so every page is a fresh
  render; the panel says nothing about how production would serve it.

Each would need a core hook — a per-request render report on `AfterRenderEvent`,
with fragment timings and tags, would cover the first two.

Because the whole HTML response is held back to add the panel, a page that streams
its HTML arrives at once in development.
