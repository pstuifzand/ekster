# Ek Reader

`ek reader` starts the embedded Ekstereye SPA on a random loopback port and
opens it in the default browser. The local server owns IndieAuth tokens and
forwards Microsub and Micropub requests to the endpoint saved by Ek. The browser
only uses same-origin local URLs; the server refuses non-loopback clients and
unexpected Host headers.

## Updating the embedded UI

The UI source lives in the sibling `~/work/ekstereye` checkout. After changing
it, run `npm run lint`, `npm test`, and `npm run build` there. Copy its `dist/`
contents into `cmd/ek/reader/dist/` in this repository before building Ek:

```sh
cp -a ~/work/ekstereye/dist/. cmd/ek/reader/dist/
go test ./...
go build ./cmd/ek
```

## Connection and proxy routes

- `GET/POST/DELETE /_ek/api/connection`: status, start IndieAuth, disconnect.
- `GET/POST /_ek/api/microsub`: authenticated Microsub proxy (including SSE).
- `GET/POST /_ek/api/micropub`: authenticated Micropub proxy.
- `/_ek/callback`: loopback IndieAuth callback.

Upstream URLs always come from discovered and saved endpoints; callers cannot
supply an upstream target. The access token is attached by the Go server and is
never returned to the reader UI.
