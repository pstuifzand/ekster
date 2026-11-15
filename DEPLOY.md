# Deploying Ekster

Ekster is one Go binary (`eksterd`) with two dependencies: PostgreSQL and Redis.
The container image is `scratch`-based — there is no shell inside it, so inspect
state through the database container or from the host.

This document covers a single-host Docker Compose deployment behind a TLS
reverse proxy, which is how `microsub.stuifzandapp.com` runs.

## What runs

| Service    | Image                              | Purpose                                        |
|------------|------------------------------------|------------------------------------------------|
| `web`      | `ghcr.io/pstuifzand/ekster:dev`    | `eksterd`: API, reader UI, Micropub, MCP source |
| `database` | `postgres:14`                      | items, channels, feeds, sources, migrations     |
| `redis`    | `redis:5`                          | sessions, IndieAuth tokens                      |

The `web` container also holds a Bleve full-text index in `items.bleve`, relative
to its working directory. It is a single-writer index: **never run two `web`
containers against the same volume**, and treat that directory as state, not
scratch.

## Requirements

- Docker and `docker-compose`. The p83 host has `docker-compose` 1.28.5; there is
  no `docker compose` v2 plugin there.
- A public HTTPS hostname. Micropub, IndieAuth and Microsub clients all use the
  base URL, so `http://localhost` only works for local experiments.
- Outbound HTTPS from the host: Ekster fetches feeds and sends WebSub requests.
- Roughly 1 GB RAM for the stack; the image itself is a few tens of MB.

## Configuration

Flags (all have defaults; set them in `command:`):

| Flag        | Default                                                              | Meaning |
|-------------|----------------------------------------------------------------------|---------|
| `-port`     | `80`                                                                 | listen port inside the container |
| `-auth`     | `true`                                                               | require IndieAuth for `/microsub/`; `false` only for local work |
| `-headless` | `false`                                                              | serve the API without the reader UI and admin pages |
| `-redis`    | `redis:6379`                                                         | Redis address |
| `-db`       | `host=database user=postgres password=… dbname=ekster sslmode=disable` | Postgres DSN |
| `-baseurl`  | —                                                                    | external base URL, including trailing slash |

Environment variables:

| Variable                         | Meaning |
|----------------------------------|---------|
| `EKSTER_BASEURL`                 | same as `-baseurl`; the server exits if neither is set |
| `EKSTER_USER_AGENT`              | User-Agent for feed fetches (identify yourself to publishers) |
| `OTEL_EXPORTER_OTLP_ENDPOINT`    | tracing collector; empty disables tracing |
| `OTEL_EXPORTER_OTLP_PROTOCOL`    | `http/protobuf` |
| `OTEL_SERVICE_NAME`              | `ekster` |

`EKSTER_BASEURL` must be the URL users and clients actually see, e.g.
`https://microsub.example.com/`. It ends up in Micropub `Location` headers,
IndieAuth redirects and WebSub callbacks; a wrong base URL produces links that
only work on the machine itself.

### Production compose

```yaml
version: '2'
services:
  web:
    image: ghcr.io/pstuifzand/ekster:dev
    command: -port 80
    working_dir: /opt/microsub
    volumes:
      - search-data:/opt/microsub
    environment:
      - EKSTER_BASEURL=https://microsub.example.com/
      - EKSTER_USER_AGENT=ekster (https://microsub.example.com/)
      - OTEL_EXPORTER_OTLP_ENDPOINT=
    ports:
      - 8089:80
    depends_on:
      - database
      - redis
    restart: unless-stopped

  database:
    image: postgres:14
    volumes:
      - database-data:/var/lib/postgresql/data
    environment:
      POSTGRES_DB: ekster
      POSTGRES_USER: postgres
      POSTGRES_PASSWORD: change-me
    restart: unless-stopped

  redis:
    image: redis:5
    restart: unless-stopped

volumes:
  database-data:
  search-data:
```

Put the `web` service on the reverse proxy's Docker network (on p83 that is
`nginxproxy_default`) so the proxy can reach port 80; the published `8089` port
is only needed if the proxy runs on the host instead.

### Reverse proxy

Terminate TLS in front of `web` and pass the standard `Host`/`X-Forwarded-*`
headers. Two things matter beyond that:

- Disable response buffering for `text/event-stream`: the reader subscribes to
  `GET /microsub/?action=events` and never sees new items through a buffering
  proxy.
- Allow large request bodies for `/micropub` (photo and content uploads) and
  keep the read timeout generous; the events endpoint stays open.

## First run

```sh
docker-compose up -d
docker-compose logs -f web
```

Watch for:

```
Running migrations
Migrations are up
Listening on port 80
```

Then open the base URL. The first login creates the user and shows the Microsub
endpoint to paste into a reader. Keep `-auth=true` for anything reachable from
outside; `-auth=false` skips IndieAuth and is only for development. Admin pages
still require a session, so a local `-auth=false` instance needs a seeded Redis
session to show them.

## Deploying a new version

Deploying means the commit at `HEAD` of the branch CI built — never the working
tree. `git status --short` in a development checkout usually shows uncommitted
edits that are not in any image; say so when reporting a deploy.

1. Push to `dev`. The Docker workflow
   (`.github/workflows/docker-publish.yml`) builds and pushes
   `ghcr.io/pstuifzand/ekster:dev`.
2. Confirm the **Docker** job succeeded for that commit — the `Go` job passing
   does not mean an image was published:

   ```sh
   gh run list --branch dev --limit 5
   ```

3. On the host, pull and check the revision label before touching the running
   container:

   ```sh
   cd /home/microsub/microsub
   docker-compose pull web
   docker image inspect ghcr.io/pstuifzand/ekster:dev --format \
     '{{index .Config.Labels "org.opencontainers.image.revision"}}'
   ```

   The printed SHA must equal `git rev-parse HEAD`. `pull` reporting "downloaded
   newer image" proves nothing; the label is the only real check.

4. Tag the outgoing image for rollback **before** recreating:

   ```sh
   docker inspect microsub_web_1 --format '{{.Image}}'   # image being replaced
   docker tag "$(docker inspect microsub_web_1 --format '{{.Image}}')" rollback-<old-sha>
   ```

   Tagging `ghcr.io/pstuifzand/ekster:dev` after `pull` tags the *new* image, not
   the running one — that mistake silently destroys your rollback path.

5. Recreate only the web service:

   ```sh
   docker-compose up -d --no-deps web
   ```

6. Verify, in this order:

   ```sh
   docker inspect microsub_web_1 --format \
     '{{.State.Status}} restarts={{.RestartCount}} rev={{index .Config.Labels "org.opencontainers.image.revision"}}'
   docker logs --tail 30 microsub_web_1
   ```

   Expect `running`, `restarts=0`, the new SHA, `Migrations are up` and
   `Listening on port 80`. A container that starts is not a deploy: finish with
   an API call (`./ek channels`, `./ek timeline home`) and, for user-facing work,
   a check in the browser.

## Migrations

Migrations are embedded in the binary (`cmd/eksterd/db/migrations/*.sql`) and run
on every start; there is no separate migrate step. `golang-migrate` records state
in `schema_migrations` (`version`, `dirty`):

```sh
docker exec microsub_database_1 psql -U postgres -d ekster -A -F'|' \
  -c 'SELECT version, dirty FROM schema_migrations'
```

- Migrations are forward-only. Rolling the image back does not undo a schema
  change, so a rollback across a migration is only safe if the older binary
  tolerates the newer schema.
- `dirty = t` means a migration failed halfway and the server will refuse to
  start (crash loop with `Dirty database version N`). Inspect the schema, finish
  or undo the partial change by hand, then clear the flag:
  `UPDATE schema_migrations SET dirty = false`. Never clear it without looking
  first — the flag exists because the schema is neither old nor new.

## Data and backups

- `database-data` holds everything that matters: users, channels, feeds, items,
  Micropub sources. Back it up before migrations and before deploys that touch
  schema:

  ```sh
  docker exec microsub_database_1 pg_dump -U postgres ekster | gzip > ekster-$(date +%F).sql.gz
  ```

- The Bleve index (`/opt/microsub/items.bleve` in production) is derived data:
  losing it does not lose items, but there is no rebuild command, so search only
  covers items indexed after the loss. Keep the volume across recreations.
- The index is single-writer and lock-failing. Two containers on the same volume
  produce a startup error about a locked index, not a shared index.

## Rollback

```sh
docker tag rollback-<old-sha> ghcr.io/pstuifzand/ekster:dev
docker-compose up -d --no-deps web
docker inspect microsub_web_1 --format '{{index .Config.Labels "org.opencontainers.image.revision"}}'
```

Verify the label matches the SHA you intended, then exercise the API as after a
normal deploy. For a quick pre-release trial, build locally with the revision
label set, `docker save | ssh host docker load`, tag the current image as
`rollback-<sha>` first, then tag and recreate:

```sh
docker build -t ekster:trial --label org.opencontainers.image.revision=$(git rev-parse HEAD) .
```

## Troubleshooting

| Symptom | Check |
|---------|-------|
| Container restarts or the proxy answers 502 | `docker logs microsub_web_1`: a `Dirty database version` or a Bleve lock ends the process at startup |
| `EKSTER_BASEURL environment variable not found` and immediate exit | Set the env var or pass `-baseurl` |
| Micropub `Location` or IndieAuth redirects point at the wrong host | Base URL is wrong or missing the trailing slash |
| Revision label shows the previous SHA after a deploy | The Docker CI job did not publish for that commit — re-run it, do not debug the code |
| New items never appear in an open reader | Proxy buffering on `action=events` (SSE) |
| Search returns nothing after a restart | The index volume is not mounted, so `items.bleve` was recreated empty |
| A channel shows unread items paging never returns | Timestamp ties at a page boundary; see the cursor rules in the developer notes |
| Feed follow fails with a 500 | The response body carries the reason (`feeds.url` is unique across the instance); the server may not log it |
