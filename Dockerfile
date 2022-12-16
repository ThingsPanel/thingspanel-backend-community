# syntax=docker/dockerfile:1
FROM golang:alpine AS builder
WORKDIR $GOPATH/src/app
ADD . ./
ENV GO111MODULE=on
ENV GOPROXY="https://goproxy.io"
RUN go build

FROM alpine:latest
ENV TP_PG_IP=172.19.0.4
ENV TP_PG_PORT=5432
ENV TP_MQTT_HOST=172.19.0.5:1883
ENV MQTT_HTTP_HOST=172.19.0.5:8083
ENV TP_REDIS_HOST=172.19.0.6:6379
ENV PLUGIN_HTTP_HOST=172.19.0.8:503
WORKDIR /go/src/app
COPY --from=builder /go/src/app .
EXPOSE 9999
EXPOSE 9998
RUN chmod +x ThingsPanel-Go
RUN pwd
RUN ls -lrt
ENTRYPOINT [ "./ThingsPanel-Go" ]