# ekster

a [Microsub](https://indieweb.org/Microsub) server

## Installing and running ekster

There are two methods for installing and running ekster.

### Method 1: From binaries

Download the binaries from the [latest release](https://github.cGom/pstuifzand/ekster/releases/) on Github.

### Method 2: Install ekster from source with Go

ekster is build using [go](https://golang.org). To be able to install ekster
you need a Go environment. Use these commands to install the programs.

    go get -u p83.nl/go/ekster/cmd/eksterd
    go get -u p83.nl/go/ekster/cmd/ek

`eksterd` uses [Redis](https://redis.io/) as the database to temporarily save
the items and feeds. The more permanent information is saved in `backend.json`.

#### Running eksterd

Run both Redis and `eksterd`.

Generate the configuration file "backend.json". Run this command only once, as
it will regenerate the configuration from scratch. See **Configuration** below for
how to set up the json file.

    eksterd new

Start redis

    redis --port 6379

Start eksterd and pass the redis and port arguments.

    EKSTER_TEMPLATES=$GOPATH/src/p83.nl/go/ekster/templates EKSTER_BASEURL=https://example.com eksterd -redis localhost:6379 -port 8090

You can now access `eksterd` on port `8090`. To really use it, you should proxy
`eksterd` behind a HTTP reverse proxy on port 80, or 443.

### Method 3: Using Docker / Docker Compose

It's now also possible to use docker-compose to start an ekster server. Create an empty directory.
Download [docker-compose.yml](https://raw.githubusercontent.com/pstuifzand/ekster/master/docker-compose.yml) from Github
and run the following commands in that directory.

    docker-compose pull
    docker-compose run web new
    # edit the backend.json file according to the instructions
    docker-compose up

This will first pull the Docker image from the Docker hub. Then run the image to generate a default backend.json file.
After editing, you can run `docker-compose up` to start the server. This will start Redis and ekster in such a way
so that you can run the program without problems. By default it will choose a random port, to run the server.
To make it really useful, you need to run this on an internet connected server and choose a fixed port.

The nicest way to run this docker-compose environment is with a proxy in the front. You can run ekster behind
[nginx-proxy](https://github.com/jwilder/nginx-proxy).

## When ekster is running

Add a link in the `<head>` tag to let the microsub client know where to find your server.

    <link rel="microsub" href="https://microsub.example.com/microsub">

The domain name `microsub.example.com` needs to be replaced with the vhost that
you use to proxy the server.

The microsub server responds to the `/microsub` url with the micropub protocol.
You can use `ek` to talk to the endpoint.

It's also possible to visit the microsub server with your browser, there are a few ways to
change settings.

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

    Ek is a tool for managing Microsub servers.

    Usage:

        ek [global arguments] command [arguments]

    Commands:

        connect URL                  login to Indieauth url

        channels                     list channels
        channels NAME                create channel with NAME
        channels UID NAME            update channel UID with NAME
        channels -delete UID         delete channel with UID

        timeline UID                 show posts for channel UID
        timeline UID -after AFTER    show posts for channel UID starting from AFTER
        timeline UID -before BEFORE  show posts for channel UID ending at BEFORE

        search QUERY                 search for feeds from QUERY

        preview URL                  show items from the feed at URL

        follow UID                   show follow list for channel uid
        follow UID URL               follow url on channel uid

        unfollow UID URL             unfollow url on channel uid

        export opml                  export feeds as opml
        import opml FILENAME         import opml feeds

        export json                  export feeds as json
        import json FILENAME         import json feeds

    global arguments:

      -verbose
            show verbose logging

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

## Support me

[![ko-fi](https://www.ko-fi.com/img/githubbutton_sm.svg)](https://ko-fi.com/V7V7ZUS1)

## Other Microsub projects

* <https://indieweb.org/Microsub>
* Aperture: [code](https://github.com/aaronpk/Aperture), [hosted](https://aperture.p3k.io)
