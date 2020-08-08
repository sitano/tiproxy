FROM pingcap/tidb:latest

ADD bin/tiproxy /tiproxy

RUN chmod +x /tiproxy

WORKDIR /

EXPOSE 4000 10080

ENTRYPOINT ["/tiproxy"]
