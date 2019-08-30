FROM golang:1.12

ADD build/dcos-log2es-linux-amd64 /

ENV ELASTICSEARCH_URL="http://coordinator.elastic.l4lb.thisdcos.directory:9200"
ENV DCOS_LOG_API="http://localhost:61001/system/v1/logs/v1/stream/?skip_prev=10"
ENV LOGGING_ENABLED="false"
ENV LOGGING_PREFIX="/"

CMD ["/dcos-log2es-linux-amd64"]