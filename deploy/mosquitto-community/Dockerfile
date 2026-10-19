FROM debian:bookworm-slim AS build

ARG MOSQUITTO_COMMIT=99fa50f30e325609394c324c8ff71cfbbe95d8ab
WORKDIR /src
RUN apt-get update && apt-get install -y --no-install-recommends \
    build-essential cmake git libcjson-dev libsqlite3-dev libssl-dev pkg-config \
    && rm -rf /var/lib/apt/lists/*
COPY . /src
RUN test "$(git describe --tags --exact-match)" = "v2.1.2" \
    && test "$(git rev-parse HEAD)" = "${MOSQUITTO_COMMIT}" \
    && cmake -S /src -B /build -DCMAKE_BUILD_TYPE=Release \
       -DCMAKE_INSTALL_PREFIX=/usr/local -DWITH_DOCS=OFF -DWITH_TESTS=OFF \
       -DWITH_BUNDLED_DEPS=ON -DWITH_TLS=ON -DWITH_TLS_PSK=ON \
       -DWITH_WEBSOCKETS=ON -DWITH_WEBSOCKETS_BUILTIN=ON \
    && cmake --build /build --parallel 4 \
    && cmake --install /build

FROM debian:bookworm-slim
RUN apt-get update && apt-get install -y --no-install-recommends \
    ca-certificates libcjson1 libsqlite3-0 libssl3 \
    && rm -rf /var/lib/apt/lists/* \
    && groupadd --system --gid 1883 mosquitto \
    && useradd --system --uid 1883 --gid mosquitto --no-create-home \
       --shell /usr/sbin/nologin mosquitto \
    && mkdir -p /mosquitto/config /mosquitto/data /mosquitto/plugins \
    && chown -R mosquitto:mosquitto /mosquitto
COPY --from=build /usr/local/ /usr/local/
RUN ldconfig
VOLUME ["/mosquitto/data"]
EXPOSE 1883
ENTRYPOINT ["/usr/local/sbin/mosquitto", "-c", "/mosquitto/config/mosquitto.conf"]
