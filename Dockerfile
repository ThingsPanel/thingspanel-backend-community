# syntax=docker/dockerfile:1
FROM golang:1.17-alpine
WORKDIR /app
RUN pwd
ENV TP_PG_IP=172.19.0.4
ENV TP_PG_PORT=5432
ENV TP_MQTT_HOST=172.19.0.5:1883
ENV TP_REDIS_HOST=172.19.0.6:6379
COPY . .
# RUN go build
EXPOSE 9999
EXPOSE 9998
RUN chmod +x ThingsPanel-Go
RUN pwd
RUN ls -lrt
CMD [ "./ThingsPanel-Go" ]