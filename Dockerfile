FROM ubuntu
RUN apt-get -y update && apt-get install -y ca-certificates
ADD ./server /usr/local/bin
EXPOSE 80
ENTRYPOINT ["/usr/local/bin/server"]
