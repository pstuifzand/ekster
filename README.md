# ekster

a microsub server


## Warning!

Very alpha: no warranty.

## Installing and running ekster

### Method 1: Install ekster (from source)

ekster is build using [go](https://golang.org). To be able to install ekster
you need a Go environment. Use these commands to install the programs.

    go get -u github.com/pstuifzand/ekster/cmd/eksterd
    go get -u github.com/pstuifzand/ekster/cmd/ek

`eksterd` uses [Redis](https://redis.io/) as the database, to temporarily save
the items and feeds. The more permanent information is saved in `backend.json`.

#### Running eksterd

Run both Redis and `eksterd`.

Generate the configuration file "backend.json". Run this command only once, as
it will regenerate the configuration, from scratch. See **Configuration** for
how to set up the json file.

    eksterd new

Start redis

    redis --port 6379

Start eksterd and pass the redis and port arguments.

    EKSTER_BASEURL=https://example.com eksterd -redis localhost:6379 -port 8090

You can now access `eksterd` on port `8090`. To really use it, you should proxy
`eksterd` behind a HTTP reverse proxy on port 80, or 443.

### Method 2: Using Docker / Docker Compose

It's now also possible to use docker-compose to start a ekster server.

    docker-compose pull
    docker-compose run web new
    docker-compose up

## When ekster is running

Add a link in the `<head>` tag to let the microsub client know where to find your server.

   <link rel="microsub" href="https://microsub.example.com/microsub">

The domain name `microsub.example.com` needs to be replaced with the vhost that
you use to proxy the server.

The microsub server responds to the `/microsub` url with the micropub protocol.
You can use `ek` to talk to the endpoint.

## Commands

### `eksterd`

The command `eksterd` is the main server program. It will run a Microsub server.
`eksterd` also needs a Redis server. It's used to temporarily remember the items.

The first time you should call the command

    eksterd new

This will generate a configuration file `backend.json` where it remembers the feeds.

### `ek`

The command `ek` is the command line client for Microsub server. It is able to
call the different functions of the Microsub server. It isn't needed to use `eksterd`, but
it can be useful. It can also be used with other servers that implement Microsub.

    ek connect <url>

Connect with `ek` to you microsub server. After that you can call `ek` to
control your microsub server. It should even work with other servers that
support microsub.

    ek <command> options...

    Commands:

      channels                     list channels
      channels NAME                create channel with NAME
      channels UID NAME            update channel UID with NAME
      channels -delete UID         delete channel with UID

      timeline UID                 show posts for channel UID
      timeline UID -after AFTER    show posts for channel UID starting from AFTER
      timeline UID -before BEFORE  show posts for channel UID ending at BEFORE

      search QUERY                 search for feeds from QUERY

      preview URL                  show items from the feed at URL

      follow UID                   show follow list for channel UID
      follow UID URL               follow URL on channel UID

      unfollow UID URL             unfollow URL on channel UID


## Configuration: backend.json

The `backend.json` file contains all information about channels, feeds and authentication.
When the server is not running you can make changes to this file to add or remove feeds.
This is not the easiest way, but it's possible.

When generating this file for the first time. It will contain a default
configuration. This can be changed (and perhaps should be changed).
The two parts that should be changed are:

   "Me": "...",
   "TokenEndpoint": "...",


The `Me` value should be set to the URL you use to sign into Monocle, or
Micropub client.

`TokenEndpoint` should be the `token_endpoint` you use for that domain,
`ekster` will check every 10 minutes, if the token is still valid. This could
be retrieved automatically, but this doesn't happen at the moment.

