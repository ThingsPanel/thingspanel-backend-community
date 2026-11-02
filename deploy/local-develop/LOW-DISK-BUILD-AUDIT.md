# Low-disk source build alternatives — read-only audit

2026-11-02. No build, new container, migration, image deletion, volume deletion or
storage relocation was performed. Existing images were inspected as metadata only.

## Encore 1.58.6

The installed official CLI reports `encore version v1.58.6`. Its local
`encore build docker --help` establishes:

- architecture defaults to **amd64**, even on this arm64 host;
- `--arch arm64` and `--os linux` are supported;
- `--base` defaults to **scratch**;
- only image-tag output is exposed; no binary, tar-file or local-directory output
  flag exists in this CLI help;
- `encore build --help` exposes only the docker subcommand.

The launcher now explicitly passes arm64/linux/scratch. This avoids accidental
amd64 compilation and establishes the smallest supported base without a derived
image or an old image pretending to contain develop. The existing v1.3 Encore
image is 66.1 MB, but that is historical evidence, not a bound on this build's
intermediate storage or final image size.

The installed CLI executable contains ENCORE_CACHE_DIR, ENCORE_CONFIG_DIR,
ENCORE_DAEMON_LOG_PATH and ENCORE_GOROOT settings. The launcher directs cache,
configuration, daemon log, Go caches/modules and temporary paths to the fixture
on 80G. This confirms names are recognized by this executable; it does not prove
all daemon/library writes are redirected. Build-process and daemon behavior still
needs observation during the approved build window. Do not restart/stop an
existing user Encore daemon as an implicit optimization.

The image-writing implementation and peak host/VM scratch usage were not proven
from local installed source (the distribution contains executables and runtimes,
not the CLI build implementation). Therefore the 12 GiB internal Encore preflight
budget is retained. No unsupported export flag, post-build extraction or bypass of
the disk floor is introduced. Container creation just to extract a binary is also
outside the current read-only window.

## Broker owner/plugin overlay

A Go owner binary can be cross-compiled with CGO=0 into 80G. The native Mosquitto
plugin cannot: it requires Linux arm64 CGo, GCC/libcjson development headers and
headers from the exact pinned Mosquitto source tree. Existing production build
contracts pin source `99fa50f30e325609394c324c8ff71cfbbe95d8ab` (v2.1.2), compile
broker and plugin against that same source, and use Go 1.26.2/bookworm.

Local image inventory contains a 152 MB v1.3 broker runtime (linux/arm64) and a
111 MB QA broker runtime, but no identifiable golang/compiler builder image.
The v1.3 runtime image metadata has no source/ABI provenance labels. Merely seeing
version 2.1.2 or plugin API version 5 would not establish the exact headers and
runtime ABI linkage required by the repository's production contract.

A reliable overlay path would require all of the following before execution:

1. An already-local audited Linux arm64 builder with Go 1.26.2, C compiler,
   libcjson headers and required shared development libraries; its exact image ID.
2. Mosquitto headers from a verified clean checkout at the pinned commit on 80G.
3. A verified runtime build manifest establishing the same Mosquitto commit/ABI,
   libc/json compatibility and exact runtime image ID (or a newly built matched
   broker binary alongside the plugin, adding the native compiler requirements).
4. Read-only repository input, cache/output/temp paths bound to 80G and a small
   bounded/tmpfs container writable layer; no apt or module cache in its overlay.
5. Owner/readiness/plugin artifact hashes, source SHA and read-only mount checks
   at startup, followed by real native auth/connect/message/command acceptance.

Those prerequisites are not currently demonstrated. The launcher therefore does
not substitute a new owner plus old plugin, install a compiler into a runtime
layer, claim runtime-version equality proves ABI, or lower the 16 GiB native build
budget. Existing source-image build remains the reviewed path, currently blocked
by internal capacity. Nothing needs to be moved/deleted to preserve this result.
