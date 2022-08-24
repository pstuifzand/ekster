FROM golang:1.18-alpine AS build-env
RUN apk --no-cache add ca-certificates
COPY go.mod go.sum /src/
COPY cmd /src/cmd/
COPY pkg /src/pkg/
WORKDIR /src
RUN ls -lr
RUN go build ./cmd/eksterd

FROM scratch
COPY --from=build-env /src/eksterd /
COPY --from=build-env /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
ENTRYPOINT ["/eksterd"]
