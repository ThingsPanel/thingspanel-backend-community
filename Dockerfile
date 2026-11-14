# syntax=docker/dockerfile:1
FROM --platform=$BUILDPLATFORM golang:alpine AS builder
WORKDIR /src
COPY . ./
ENV GO111MODULE=on
ENV GOPROXY="https://goproxy.cn,direct"
ARG VERSION=dev
ARG TARGETOS
ARG TARGETARCH
RUN CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} go build -trimpath -ldflags "-X project/pkg/global.SYSTEM_VERSION=${VERSION}" -o /out/ThingsPanel-Go .

FROM alpine:latest
ARG VERSION=dev
LABEL description="ThingsPanel Go Backend"
WORKDIR /go/src/app
RUN apk update && apk add --no-cache tzdata
COPY --from=builder /out/ThingsPanel-Go ./ThingsPanel-Go
COPY sql ./sql
COPY static ./static
COPY files/iphone-1.png files/iphone-2.png files/logo.png ./files/
COPY configs/conf.yml configs/casbin.conf configs/messages.yaml configs/messages_str.yaml ./configs/
COPY configs/rsa_key/private_key.pem configs/rsa_key/public.pem ./configs/rsa_key/
RUN mkdir -p files/logs
EXPOSE 9999
RUN chmod +x ThingsPanel-Go && test "$(./ThingsPanel-Go --version)" = "$VERSION"
ENTRYPOINT [ "./ThingsPanel-Go" ]
