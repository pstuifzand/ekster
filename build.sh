#!/usr/bin/env bash

# STEP 1: Determinate the required values

GOOS=linux
GOARCH=amd64
CGO_ENABLED=0

PACKAGE="github.com/pstuifzand/ekster/cmd/eksterd"
VERSION="$(git describe --tags --always --abbrev=0 --match='v[0-9]*.[0-9]*.[0-9]*' 2> /dev/null | sed 's/^.//')"
COMMIT_HASH="$(git rev-parse --short HEAD)"
BUILD_TIMESTAMP=$(date '+%Y-%m-%dT%H:%M:%S')

# STEP 2: Build the ldflags

LDFLAGS=(
  "-X 'main.Version=${VERSION}'"
  "-X 'main.CommitHash=${COMMIT_HASH}'"
  "-X 'main.BuildTime=${BUILD_TIMESTAMP}'"
)

# STEP 3: Actual Go build process
date

go build -v -ldflags="${LDFLAGS[*]}" ${PACKAGE}

date
