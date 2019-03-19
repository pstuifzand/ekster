FROM alpine
RUN apk --no-cache add ca-certificates
WORKDIR /opt/micropub
EXPOSE 80
COPY /go/bin/eksterd /app/
COPY /go/src/p83.nl/go/ekster/templates /app/templates
ENTRYPOINT ["/app/eksterd"]
