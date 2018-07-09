FROM ubuntu
RUN apt-get -y update && apt-get install -y ca-certificates
RUN ["mkdir", "/usr/share/eksterd"]
ADD ./eksterd /usr/local/bin
ADD ./templates /usr/share/eksterd
EXPOSE 80
ENV EKSTER_TEMPLATES "/usr/share/eksterd"
ENTRYPOINT ["/usr/local/bin/eksterd"]
