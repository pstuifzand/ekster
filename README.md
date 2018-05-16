# ekster

a microsub server



## Warning!

Very alpha: no warranty.

## Install ekster

ekster is build using [go](https://golang.org). To be able to install ekster
you need a Go environment. Use these commands to install the programs.

    go get -u github.com/pstuifzand/ekster/cmd/eksterd
    go get -u github.com/pstuifzand/ekster/cmd/ek

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

## backend.json

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

## Using Docker / Docker Compose

It's now also possible to use docker-compose to start a ekster server.

    docker-compose pull
    docker-compose run web new
    docker-compose up

