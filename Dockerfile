# syntax=docker/dockerfile:1
FROM --platform=$BUILDPLATFORM golang:1.23-alpine AS builder
WORKDIR /src
COPY . .
ARG VERSION=dev
ARG TARGETOS
ARG TARGETARCH
ENV GO111MODULE=on
ENV GOPROXY="https://goproxy.cn,direct"
ENV CGO_ENABLED=0
ENV GOOS=$TARGETOS
ENV GOARCH=$TARGETARCH
RUN go build -ldflags "-X project/pkg/global.SYSTEM_VERSION=${VERSION}" -o ThingsPanel-Go .

FROM --platform=$TARGETPLATFORM alpine:3.20
ARG TARGETARCH
LABEL description="ThingsPanel Go Backend linux/$TARGETARCH"
WORKDIR /go/src/app
RUN apk add --no-cache tzdata ca-certificates
COPY --from=builder /src/ .
EXPOSE 9999
RUN chmod +x ThingsPanel-Go
ENTRYPOINT [ "./ThingsPanel-Go" ]
