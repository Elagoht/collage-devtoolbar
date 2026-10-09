# elagoht/devtoolbar

A collage plugin that shows a small panel at the bottom of every page in
development: which page rendered, in which locale, with what status, how long the
render and each of its fragments took, which fragments failed, the dependency tags
the render depended on, its `Cache-Control` and `ETag`, its size, and how many
findings the checking plugins reported. In production and in a static build it does nothing at
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

Requires collage v0.57.0 or later.

## The panel

Collapsed, it is one line in the corner:

```
collage  home (en) · 200 · 1.84ms · 1 failed fragment · 3 findings
```

Opened, it lists:

| | |
| --- | --- |
| Page | the page's name and locale, or "not a page render" for a document, an asset, a handler |
| Strategy | `static`, `dynamic` or `incremental` |
| Status | the response's status |
| Render time | `X-Collage-Render-Time`, which collage sets on a fresh render in development |
| Fragments | how many ran and how many failed, then one line each: its time, and for a failed one whether its fallback was shown and the error. In the order they were entered, so a fragment comes before the ones in its slots, whose time its own includes |
| Dependency tags | the tags the render depended on — the page's own and those its data handlers returned — or "none" |
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
`</body>`. What only the render knows — the page, the locale, the fragments, the tags, the findings — the
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

## Local peers only

The panel shows every visitor how the site is built: its pages, fragments, timings,
dependency tags and failures. It also takes the conditional headers off every
request, so each one is rendered in full. It is meant for the developer, so in
development the plugin answers only a peer on the developer's machine or a
private network:

- loopback (`127.0.0.0/8`, `::1`);
- private networks (`10/8`, `172.16/12`, `192.168/16`, IPv6 `fc00::/7`), which
  covers a Docker bridge and a phone on the LAN;
- link-local addresses (`169.254/16`, `fe80::/10`).

`collage dev` reaches the application from loopback, so it is covered.

A request from any other address means development mode is serving the public, by
mistake or through a host added to `COLLAGE_DEV_HOST`. Such a response goes out
exactly as it would without the plugin, and the first one logs a warning:

```
WARN devtoolbar: DevMode is serving a non-local peer; the panel is off for it peer=203.0.113.5:4000
```

The address checked is `collage.ClientIP`'s. That is the connection's own address,
or, behind a proxy listed in `Server.TrustedProxies`, the client that proxy
forwarded for. A development server behind nginx on the same machine therefore
shows the panel only to local clients, provided the proxy is listed. Without
`TrustedProxies`, `X-Forwarded-For` is never read, because a client can write it.

A tailnet or carrier-grade NAT address (`100.64.0.0/10`) is not a private network,
so a phone reaching the machine over Tailscale gets no panel. To show it to
remote peers, turn the check off:

```go
devtoolbar.NewWith(devtoolbar.Options{AllowRemote: true})
```

```json
{ "elagoht/devtoolbar": { "allowRemote": true } }
```

This sits behind collage's own guard: since v0.56.0, development mode refuses a
`Host` that does not name the machine.

If collage-secure's policy is on, it is report-only in development, so the panel's
inline style and close button work; the browser's console will mention them.

## Limitations

What a plugin can observe is what is on the response and on the `AfterRender`
event. The panel cannot show:

- **Cache hits.** Development never reads the page cache, so every page is a fresh
  render; the panel says nothing about how production would serve it.
- **Where a fragment's time went.** A fragment's line is its wall-clock time,
  slots included; its data handler and its template are not timed apart, and a
  fragment rejected before it ran has no line.
- **A document's render.** Fragments and tags come from a page's `AfterRender`; a
  document, an asset or a handler shows only what its response carries.

Because the whole HTML response is held back to add the panel, a page that streams
its HTML arrives at once in development.

## Changes

### v0.3.0

- **Local peers only.** In development the panel is drawn only for a peer on the
  developer's machine or a private network (loopback, RFC 1918, IPv6
  unique-local, link-local). Any other peer gets the response untouched, and the
  first one logs a warning. `Options.AllowRemote` (`allowRemote`) turns the check
  off.
- `NewWith(Options)`; the plugin reads its section of the plugin configuration.
- Requires collage v0.57.0.

### v0.2.4

- Requires collage v0.49.0. Tests only: the test site gives its fragments
  typed data with `collage.Load` and `collage.DataHandler`, since
  `WithDataHandler` is gone. The plugin itself is unchanged.

### v0.2.3

- A page written without a `Content-Type` — a handler mounted with `app.Handle`,
  say — is given the panel. The type is sniffed from the first bytes, as net/http
  does.

### v0.2.2

- The panel lands before `</body>` after non-ASCII text, such as Turkish İ.

### v0.2.1

- `collage.json`: the plugin described to editors — its template functions,
  snippets and configuration schema — for the Collage Snippets & Highlighter
  extension and any tool reading it.

### v0.2.0

- The panel lists every fragment of the render with its time, and for one that
  failed whether its fallback was shown and the error; the summary line counts
  failed fragments. From collage v0.24.0's `AfterRenderEvent.Fragments`.
- The panel shows the dependency tags the render depended on, from
  `AfterRenderEvent.DependencyTags`.
- Requires collage v0.24.0.
