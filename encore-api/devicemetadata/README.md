## Device Metadata Service

This Encore service is the new thing-model backend for ThingsPanel.

Responsibilities:

- thing models and thing model items
- device templates, products, and devices
- thing model version snapshots
- built-in metadata dictionaries
- MQTT devices topic ingress and command downlink

Conventions locked by the ThingsModel task set:

- no `v2`, `next`, or `new` naming in service paths
- formal item types are `PROPERTY`, `EVENT`, and `COMMAND`
- formal MQTT topics are `devices/{sn}/property/up`, `devices/{sn}/event/up`,
  `devices/{sn}/command/up`, `devices/{sn}/command/down`, and `devices/{sn}/status/up`
- compatibility with older topics must stay isolated in the legacy adapter path

Local development:

```bash
cd encore-api
encore run
```

Key endpoints:

- `GET /api/device-metadata/healthz`
- `POST /api/thing-models`
- `POST /api/devices/:id/actuate`

Guardrails:

- handlers stay thin and delegate to persistence/helpers
- do not route by matching URI strings
- tenant scope comes from the Encore auth handler
