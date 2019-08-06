FROM alpine:3.7
RUN apk --no-cache add ca-certificates
WORKDIR /opt/micropub
EXPOSE 80
COPY ./eksterd /app/
COPY ./templates /app/templates
ENTRYPOINT ["/app/eksterd"]
