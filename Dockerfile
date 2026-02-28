# syntax=docker/dockerfile:1
FROM golang:alpine AS builder
WORKDIR $GOPATH/src/app
ADD . ./
ENV GO111MODULE=on
ENV GOPROXY="https://goproxy.cn,direct"
RUN go build -o ThingsPanel-Go .

FROM alpine:latest
LABEL description="ThingsPanel Go Backend"
WORKDIR /go/src/app
RUN apk update && apk add --no-cache tzdata
COPY --from=builder /go/src/app .
EXPOSE 9999
RUN chmod +x ThingsPanel-Go
RUN pwd
RUN ls -lrt
ENTRYPOINT [ "./ThingsPanel-Go" ]