FROM golang:1.24-alpine AS build
WORKDIR /src
COPY go.mod ./
COPY sdk ./sdk
COPY unipush ./unipush
COPY cmd/unipush ./cmd/unipush
RUN CGO_ENABLED=0 go build -trimpath -ldflags='-s -w' -o /notification-unipush ./cmd/unipush

FROM gcr.io/distroless/static-debian12:nonroot
ARG SOURCE_REVISION=development
LABEL org.opencontainers.image.source="https://github.com/ThingsPanel/thingspanel-backend-community"
LABEL org.opencontainers.image.revision=$SOURCE_REVISION
COPY --from=build /notification-unipush /notification-unipush
ENV NOTIFICATION_PLUGIN_LISTEN=0.0.0.0:5009
EXPOSE 5009
ENTRYPOINT ["/notification-unipush"]
