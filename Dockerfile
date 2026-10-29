# syntax=docker/dockerfile:1
FROM --platform=$BUILDPLATFORM golang:alpine AS builder
WORKDIR $GOPATH/src/app
ADD . ./
ENV GO111MODULE=on
ENV GOPROXY="https://goproxy.cn,direct"
ARG VERSION=dev
ARG TARGETOS
ARG TARGETARCH
RUN CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} go build -ldflags "-X project/pkg/global.SYSTEM_VERSION=${VERSION}" -o ThingsPanel-Go .

FROM alpine:latest
ARG VERSION=dev
LABEL description="ThingsPanel Go Backend"
WORKDIR /go/src/app
RUN apk update && apk add --no-cache tzdata
COPY --from=builder /go/src/app .
EXPOSE 9999
RUN chmod +x ThingsPanel-Go && test "$(./ThingsPanel-Go --version)" = "$VERSION"
RUN pwd
RUN ls -lrt
ENTRYPOINT [ "./ThingsPanel-Go" ]
