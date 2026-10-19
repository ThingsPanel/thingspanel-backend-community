# Mosquitto Linux/arm64 build evidence

Date: 2026-10-19. Docker Desktop/OrbStack built the owned `deploy/mosquitto-community/Dockerfile` from the pinned source checkout `/tmp/tp-mqtt-20261019/mosquitto`, tag `v2.1.2`, commit `99fa50f30e325609394c324c8ff71cfbbe95d8ab`.

- Image tag: `tp-mosquitto:2.1.2-qa`
- Image ID: `sha256:c103ceeb61129b0dae7e5c54d8545587b225956ceaed313e32f720aec122360e`
- Platform: `linux/arm64`
- Runtime smoke: invoking `/usr/local/sbin/mosquitto -h` in the image printed `mosquitto version 2.1.2`.
- Scope: broker image build and binary version only. No container listener or plugin was started for this smoke. Linux plugin ABI and MQTT behavior inside the container remain unverified.
- Docker Hub `eclipse-mosquitto:2.1.2` remained unavailable in the local tag lookup; this image was built from the pinned source.

This is a local build artifact, not production certification.
