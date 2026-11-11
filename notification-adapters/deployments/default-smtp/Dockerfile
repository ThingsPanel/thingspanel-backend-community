FROM golang:1.23-alpine AS build
WORKDIR /src
COPY go.mod ./
COPY sdk ./sdk
COPY smtp ./smtp
COPY cmd/smtp ./cmd/smtp
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags='-s -w' -o /out/notification-smtp ./cmd/smtp

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/notification-smtp /notification-smtp
EXPOSE 5005
ENTRYPOINT ["/notification-smtp"]
