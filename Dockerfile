FROM --platform=$BUILDPLATFORM golang:1.23-alpine AS builder
WORKDIR /src
COPY . .
ARG TARGETOS
ARG TARGETARCH
ENV GO111MODULE=on
ENV GOPROXY="https://goproxy.cn,direct"
ENV CGO_ENABLED=0
ENV GOOS=$TARGETOS
ENV GOARCH=$TARGETARCH
RUN go build -o ThingsPanel-Go .

FROM --platform=$TARGETPLATFORM alpine:3.20
ARG TARGETARCH
LABEL description="ThingsPanel Go Backend linux/$TARGETARCH"
WORKDIR /go/src/app
RUN apk add --no-cache tzdata ca-certificates
COPY --from=builder /src/ .
EXPOSE 9999
RUN chmod +x ThingsPanel-Go
ENTRYPOINT [ "./ThingsPanel-Go" ]
