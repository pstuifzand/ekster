# Tracing with Tempo

Tempo runs on the `p83.nl` host (`/root/observability`) next to Grafana. It keeps
traces for **72 hours** and has **no authentication**, so its OTLP receivers are
never published to the internet — only Docker networks and a loopback-style
bridge address.

## Addressing

| sender | endpoint |
|---|---|
| container on the `nginxproxy_default` network (every web app here is on it) | `http://tempo:4318` (HTTP) / `tempo:4317` (gRPC) |
| process running directly on the host | `http://172.17.0.1:4318` / `172.17.0.1:4317` |
| container on any other bridge network | not reachable — add `nginxproxy_default` to it |
| your laptop | `http://localhost:4318` through an SSH tunnel |

Docker's bridge isolation blocks traffic between separate networks, so a
container that only joins its own compose network cannot reach `172.17.0.1`.
That is why Tempo is attached to `nginxproxy_default`: it reaches every app that
already sits behind the reverse proxy, without opening a public port.

Tunnel for local development:

```sh
ssh -N -L 4318:172.17.0.1:4318 -L 4317:172.17.0.1:4317 root@p83.nl
```

Grafana: https://grafana.p83.nl — Tempo is the default datasource (`uid: tempo`).

## Environment

On a container (no compose or network change needed if it is on
`nginxproxy_default`):

```sh
OTEL_EXPORTER_OTLP_ENDPOINT=http://tempo:4318
OTEL_EXPORTER_OTLP_PROTOCOL=http/protobuf
OTEL_SERVICE_NAME=ekster            # ekster | p83 | microsub | ...
OTEL_RESOURCE_ATTRIBUTES=deployment.environment=production
```

Running the binary on the host itself, use `http://172.17.0.1:4318`. With the
tunnel open, use `http://localhost:4318`. Set `OTEL_SDK_DISABLED=true` in tests
and CI so no spans leave the machine.

## Minimal wiring

```go
exp, err := otlptracehttp.New(ctx) // reads the OTEL_EXPORTER_OTLP_* variables
if err != nil { return err }

// NewSchemaless, not NewWithAttributes(semconv.SchemaURL, ...): the default
// resource already carries a schema URL and Merge refuses two different ones
// ("conflicting Schema URL"), which silently disables tracing.
res, _ := resource.Merge(resource.Default(),
    resource.NewSchemaless(semconv.ServiceName("ekster")))

tp := sdktrace.NewTracerProvider(
    sdktrace.WithBatcher(exp),
    sdktrace.WithResource(res),
)
otel.SetTracerProvider(tp)
defer tp.Shutdown(context.Background()) // flushes the batch queue
```

Instrument `net/http` with `otelhttp.NewHandler` and `otelhttp.NewTransport` so
incoming requests and outgoing calls produce spans without hand-written code.
Trace IDs propagate over the standard `traceparent` header, so two services
using `otelhttp` chain up automatically.

## Verify

Grafana → Explore → datasource **Tempo** → TraceQL:

```
{ resource.service.name = "ekster" }
```

Nothing there? Spans are batched, so give it ~10 s; anything older than 72 h is
deleted; and `connection refused` means the sender is on a network without a
route — check which networks the container is attached to.

## Notes

- `ekster` used to declare `go 1.16` in `go.mod`; the OpenTelemetry Go SDK
  needs Go 1.21+, so the module is now `go 1.23`, the Dockerfile builds on
  `golang:1.23-alpine`, and CI pins the same. Keep those three in step.
- Tracing in ekster is opt-in: nothing is created and nothing is sent unless
  `OTEL_EXPORTER_OTLP_ENDPOINT` is set. The unit tests instrument with an
  in-memory recorder, so they never touch a collector either.
- Tempo's storage, memory caps and compose file live in `/root/observability` on
  the server. Trace blocks sit on the Docker volume under
  `/mnt/volume-ams3-02` and are not backed up — deliberate for a 72 h buffer.

## What ekster emits

Span names are routes and operations; unbounded values (feed URL, channel, ids)
live in attributes so the trace list stays readable.

| span | where | useful attributes |
|---|---|---|
| `GET /microsub/`, `POST /micropub`, `POST /incoming/`, `GET /` | inbound HTTP | method, path, status code |
| `feeds.refresh` | every one-minute refresh cycle | `feeds.count`, `feeds.processed` |
| `feed.refresh` | one per feed, child of the cycle | `feed.url`, `feed.channel`, `feed.id`, `feed.tier` |
| `GET <host><path>` | every outgoing request | method, host, path, status, `http.cache.hit` |
| `feed.process` | storing a feed's items | `feed.channel`, `feed.id`, `feed.changed` |
| `feed.parse` | header + items for one document | `feed.format`, `feed.items`, `feed.name` |
| `feed.parse.header` | feed metadata | `feed.content_type` |
| `feed.parse.items` | entries | `feed.content_type`, `feed.items` |
| `feeds.search`, `feed.preview` | reader search and preview | query, url |

Outgoing HTTP is instrumented once, by wrapping `http.DefaultTransport` in
`pkg/observability.Setup`: every `http.Client{}` without an explicit transport
is covered, so the feed fetcher, WebSub hub calls, IndieAuth endpoint discovery
and rss enclosure downloads all appear without per-call-site code. A redis HTTP
cache hit is marked `http.cache.hit=true`, which tells a fast trace apart from a
fast network.

The `action=events` SSE connection is deliberately not traced — its span would
only end when the reader closes the page.

TraceQL starting points:

```
{ resource.service.name = "ekster" && name = "feed.refresh" && duration > 2s }
{ resource.service.name = "ekster" && span.feed.changed = true }
```
