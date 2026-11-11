# Plan: Local Microsub server with `ek serve`

**Status: Thinking about it**

## Goal

Run Ekster as a local Microsub server from the Ek executable. `ek serve` should use a user-selected data directory, persist its data in SQLite, fetch followed feeds in the background, and serve Ekstereye from the same local origin. In this mode, Ekstereye talks directly to the local Microsub server; it does not proxy to a separate Microsub service.

Keep the existing hosted Ekster deployment (PostgreSQL/Redis) working independently.

## Proposed user experience

```sh
ek serve --data ~/.local/share/ek --listen 127.0.0.1:8090
```

The command should create or migrate the data store, start the local Microsub server and feed refresh loop, then open the reader. Loopback should be the default; exposing the service on a LAN or public interface should require an explicit choice and appropriate authentication.

## Work areas

### 1. Make Ekster server code reusable

- Extract the `eksterd` app from `package main` into an importable package used by both `cmd/eksterd` and `cmd/ek`.
- Replace global `http.Handle` registrations with an explicit `http.ServeMux` owned by the app.
- Add a lifecycle API for initialization, serving, and graceful shutdown of feed/background workers.
- Preserve the existing hosted server command and its current configuration.

### 2. Add local SQLite storage

- Add a SQLite driver; prefer a pure-Go driver unless platform/build requirements suggest otherwise.
- Store the database under the configured data directory and apply versioned migrations on startup.
- Implement a SQLite-compatible timeline backend for items, paging, read state, and item lookup.
- Adapt existing PostgreSQL-specific queries and schema: JSON columns, generated IDs/`RETURNING`, placeholders, timestamps/intervals, array parameters, and PostgreSQL error handling.
- Keep database access behind a storage boundary so the hosted PostgreSQL backend remains supported.

### 3. Replace local Redis dependencies

- Inventory Redis use in sessions, IndieAuth state/token caching, feed-fetch caching, and other request paths.
- Use in-memory state for short-lived local sessions/cache where persistence is unnecessary; persist durable server state in SQLite.
- Ensure local mode does not require Redis to start or serve the Microsub protocol.

### 4. Local identity and protocol access

- Define a single-user local account/session model suitable for a loopback service.
- Let the embedded reader connect to the local Microsub endpoint without exposing credentials to browser code.
- Keep external IndieAuth/Microsub behavior available for hosted use; do not weaken authentication on non-loopback listeners.
- Ensure the local service exposes the expected Microsub protocol actions and any discovery metadata needed by clients.

### 5. Feed fetching and subscriptions

- Reuse the existing feed refresh and parsing pipeline.
- Fetch once at startup, then refresh on a configurable schedule with existing conditional requests and retry/backoff behavior.
- Make following a feed in the reader add it to the local server’s feed list so it is picked up by the refresh loop.
- Disable WebSub hub subscriptions by default in loopback mode because remote hubs generally cannot reach a local callback. Retain polling as the reliable local path.

### 6. Data directory and search index

- Put SQLite, the Bleve index, and any durable local state under the selected data directory rather than the current working directory.
- Define backup/export behavior and document which files can be regenerated (for example, the search index).

### 7. Reader integration

- Serve the built Ekstereye assets from the same local server and origin as the local Microsub API.
- Add a local-mode connection adapter that discovers/uses the local server, without the remote-server proxy path or browser-held upstream token.
- Keep the existing standalone reader and `ek reader` remote-proxy workflow working until local mode is complete.

### 8. CLI and operations

- Add `ek serve` flags for data directory, listen address, refresh interval, and opening the browser.
- Use loopback defaults, print the chosen reader/API URLs, and handle Ctrl-C with graceful shutdown.
- Keep logs useful for feed refresh and server startup without logging secrets.

## Suggested implementation order

1. Extract the Ekster app and explicit HTTP mux without changing hosted behavior.
2. Add a SQLite connection/migration path and prove startup against a temporary data directory.
3. Implement SQLite timeline/storage behavior and run protocol-level CRUD/read/paging tests.
4. Decouple local mode from Redis and remote IndieAuth assumptions.
5. Wire the existing fetch loop to the local store; test follow → fetch → timeline and restart persistence.
6. Serve Ekstereye and integrate its local connection mode.
7. Add CLI flags, graceful shutdown, documentation, and end-to-end tests.

## Acceptance criteria

- `ek serve --data <dir>` starts without PostgreSQL or Redis.
- Following a feed causes items to be fetched and appear in the reader.
- Channels, subscriptions, items, and read state persist after restart.
- Microsub timeline paging, mark-read, follow/unfollow, channels, and events work against the local service.
- The reader and API use the same origin; no upstream credential is exposed to JavaScript.
- Local mode listens on loopback by default and shuts down its workers cleanly.
- Existing hosted Ekster tests and deployment behavior continue to pass.
