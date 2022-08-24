# Ekster

A [Microsub](https://indieweb.org/Microsub) server.

## Installation

The Docker image `ghci.io/pstuifzand/ekster:dev` contains the development version
of this server. It has dependencies on Postgresql and Redis.

Use the `docker-compose.yml` file to get an idea of how to use Ekster in Docker.

Start the server with database and redis using `docker-compose`.

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
