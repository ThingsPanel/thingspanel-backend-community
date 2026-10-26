# Mosquitto shared-subscription prefix control

Date: 2026-10-19. A no-plugin Mosquitto 2.1.2 process (same pinned native binary, loopback port 32883) was used to isolate broker topic matching from the Go authorization plugin.

| Shared filter | Published topic | Result |
| --- | --- | --- |
| `$share/qa/$tp/#` | `$tp/ingress/v1/probe` | No delivery in 2 seconds; subscriber timed out (exit 27). |
| `$share/qa/tp-internal/#` | `tp-internal/ingress/v1/probe` | Delivered to shared subscriber. |
| `$share/qa/plain/#` | `plain/probe` | Delivered to shared subscriber. |

All publishers completed successfully. This confirms the `$`-root matching issue in the broker independent of plugin ACL decisions; it does not compare ingress transformations because there was no plugin. The subsequent plugin-enabled MQTT 3.1.1/MQTT 5 run confirms delivery on the protected `tp-internal/` prefix and explicit denial of the old `$tp/` namespace.

The control uses QoS 0 on this no-plugin local-only broker and is evidence of subscription matching behavior only.
