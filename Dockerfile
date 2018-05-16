FROM ubuntu
RUN apt-get -y update && apt-get install -y ca-certificates
ADD ./eksterd /usr/local/bin
EXPOSE 80
ENTRYPOINT ["/usr/local/bin/eksterd"]
