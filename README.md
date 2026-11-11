# Ekster

A [Microsub](https://indieweb.org/Microsub) server.

## Installation

The Docker image `ghci.io/pstuifzand/ekster:dev` contains the development version
of this server. It has dependencies on Postgresql and Redis.

Use the `docker-compose.yml` file to get an idea of how to use Ekster in Docker.

Start the server with database and redis using `docker-compose`.

Ek also includes the Ekstereye reader. Run `ek reader` to open it locally; it
uses Ek's saved IndieAuth connection and proxies Microsub requests through a
loopback-only server. If no account is connected, the reader offers sign-in.

Run `ek mcp` to expose the configured Microsub account to MCP clients over
stdio. It provides typed, structured, read-only tools for channels, timelines,
and search. See [docs/mcp.md](docs/mcp.md).

## Micropub sources

Services outside Ekster can post items into a channel through the Micropub
endpoint. Open `/settings/sources` to create a source per service and channel:
each source has its own token, so revoking one does not affect the others. The
page also shows when a source last delivered an item.

Send the token in the `Authorization` header using Bearer authentication, or as
an `access_token` field in the form body when the service cannot set headers.
Post `application/x-www-form-urlencoded` with `content`, `name`, `published` and
`category[]`, or send JF2 JSON with `Content-Type: application/jf2+json`.

A created post answers `201 Created` with a `Location` header pointing at
`/i/<uid>`. That permalink is private: it answers to the session of the user who
owns the channel, or to a micropub token for that channel, and returns 404 to
anyone else. `GET /micropub?q=config` describes the endpoint, and
`GET /micropub?q=source` lists what a token posted, or returns one item when
given `url=`.

```shell
docker-compose up -d
docker-compose logs -f web
```

This will start Ekster on [http://localhost:8089/](http://localhost:8089/).
When you log in for the first time. It will generate a user for you, and show
the microsub url that you can use.

> :warning: This will not work with Microsub readers that expect the server to be
> accessible on the internet. In that you should use a more advanced setup.

## Support me

[![ko-fi](https://www.ko-fi.com/img/githubbutton_sm.svg)](https://ko-fi.com/V7V7ZUS1)

## Other Microsub projects

* <https://indieweb.org/Microsub>
* Aperture: [code](https://github.com/aaronpk/Aperture), [hosted](https://aperture.p3k.io)
