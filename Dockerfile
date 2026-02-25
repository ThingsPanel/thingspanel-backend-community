# syntax=docker/dockerfile:1
FROM golang:alpine AS builder
WORKDIR $GOPATH/src/app
ADD . ./
ENV GO111MODULE=on
ENV GOPROXY="https://goproxy.io"
RUN go build -o ThingsPanel-Go .

FROM alpine:latest
LABEL description="ThingsPanel Go Backend"
WORKDIR /go/src/app
RUN apk update && apk add --no-cache tzdata
COPY --from=builder /go/src/app .
EXPOSE 9999
RUN apk --update add curl bash && 、
    mkdir /docker-entrypoint.d && \
    chmod +x ThingsPanel-Go docker-entrypoint.sh
// 增加预处理过程，方便后期调试，例如独立运行本镜像查看环境等
ENTRYPOINT ["/go/src/app/docker-entrypoint.sh"]
CMD [ "./ThingsPanel-Go" ]
