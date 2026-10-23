import type {
  Envelope,
  Page,
  NotificationView,
  NotificationListQuery,
  NotificationSubmit,
  PluginReceiptV1,
  PluginGrantUpdate,
  PluginGrantView,
  PluginView,
  SecretPatch,
  SourceEventV1
} from "./notification-v2.types";

export const blockedListQuery: NotificationListQuery = { page: 1, pageSize: 20, intakeStatus: "blocked", sourceType: "alarm" };
export const blockedNotificationList: Envelope<Page<NotificationView>> = {
  code: 200, message: "ok", requestId: "query-fixture-1",
  data: {items: [{id: "blocked-fixture-1", source: {type: "alarm", id: "event-fixture-1"}, intakeStatus: "blocked", blockedReason: "group_revision_unavailable", createdAt: "2026-10-09T00:00:00Z", deliveries: []}], page: 1, pageSize: 20, total: 1}
};

// Mirrors fixtures/submit-request.json and compiles against the public
// camelCase business consumer declarations.
export const submitFixture = {
  source: { type: "alarm", id: "alarm-fixture-001" },
  deliveries: [
    {
      instanceId: "sms-fixture-001",
      recipient: { kind: "phone", address: "+15550000000" },
      content: { kind: "text", title: "Fixture", text: "No external send" }
    }
  ],
  expiresAt: "2030-01-01T00:00:00Z"
} satisfies NotificationSubmit;

// Mirrors fixtures/source-event.json, including provider/source JSON nulls.
export const sourceEventFixture = {
  schemaVersion: "1.0",
  sourceDeploymentId: "community-fixture",
  sourceEventId: "alarm-fixture-001",
  actionId: "notify-action-fixture",
  tenantId: "tenant-fixture",
  notificationGroupId: "group-fixture",
  groupRevision: 1,
  occurredAt: "2026-10-09T00:00:00Z",
  expiresAt: "2030-01-01T00:00:00Z",
  payload: {
    subject: "Fixture alert",
    text: "Mock only, never send externally.",
    variables: {
      "device.name": "cold-room-fixture",
      "device.extra": { nullable: null, labels: ["cold", null] }
    },
    legacyAlertJson: {
      device: { description: null, metadata: { owner: null } },
      samples: [null, { value: 4 }]
    }
  }
} satisfies SourceEventV1;

export const setSecretFixture: SecretPatch = {
  set: { accessToken: "FAKE_SECRET_SENTINEL_ROTATE" }
};

export const clearSecretFixture: SecretPatch = {
  clear: ["accessToken"]
};

// Mirrors the explicit tenant grant request/response fixtures. expectedVersion
// zero creates only a missing grant; tenantId is opaque and administrator-supplied.
export const pluginGrantCreate: PluginGrantUpdate = {
  pluginRegistrationId: "plugin-registration-fixture",
  tenantId: "tenant-opaque-fixture",
  enabled: true,
  expectedVersion: 0
};

export const pluginGrantView: PluginGrantView = {
  pluginRegistrationId: "plugin-registration-fixture",
  tenantId: "tenant-opaque-fixture",
  enabled: true,
  version: 1,
  createdAt: "2026-10-09T12:00:00Z",
  updatedAt: "2026-10-09T12:00:00Z"
};

export const pluginGrantListFixture: Envelope<Page<PluginGrantView>> = {
  code: 200,
  message: "ok",
  requestId: "grant-list-fixture",
  data: {
    items: [{
      pluginRegistrationId: "plugin-registration-fixture",
      tenantId: "tenant-opaque-fixture",
      enabled: false,
      version: 2,
      createdAt: "2026-10-09T12:00:00Z",
      updatedAt: "2026-10-09T12:05:00Z"
    }],
    page: 1,
    pageSize: 20,
    total: 1
  }
};

// @ts-expect-error Tenant identity is required and cannot be inferred or omitted.
export const pluginGrantWithoutTenant: PluginGrantUpdate = {
  pluginRegistrationId: "plugin-registration-fixture",
  enabled: true,
  expectedVersion: 0
};

// Plugin protocol remains snake_case while the business DTOs above use
// camelCase.
export const pluginReceiptFixture: PluginReceiptV1 = {
  event_id: "receipt-fixture-001",
  provider_message_id: "provider-message-fixture-001",
  status: "delivered",
  occurred_at: "2026-10-09T00:00:00Z"
};

export const businessSnakeCaseDrift: SourceEventV1 = {
  ...sourceEventFixture,
  // @ts-expect-error Business v2 uses sourceDeploymentId, not source_deployment_id.
  source_deployment_id: "wrong-wire-name"
};

export const pluginCamelCaseDrift: PluginReceiptV1 = {
  ...pluginReceiptFixture,
  // @ts-expect-error Plugin receipts retain snake_case and reject camelCase fields.
  eventId: "wrong-wire-name"
};

// @ts-expect-error SecretPatch is exactly one of set or clear.
export const ambiguousSecretPatch: SecretPatch = {
  set: { accessToken: "FAKE_SECRET_SENTINEL_ROTATE" },
  clear: ["accessToken"]
};

export const declaredIdentityPluginManifest: PluginView["manifest"] = {
  pluginId: "example.notification",
  name: "Example notification plugin",
  pluginVersion: "1.0.1",
  identityFields: ["account_id", "region"],
  channels: ["webhook"],
  contentModes: ["text"],
  configSchema: { type: "object" },
  recipientSchema: { type: "object" },
  secretFields: ["password"],
  capabilities: { deliveryReceipts: false }
};

export const legacyPluginManifestSummary: PluginView["manifest"] = {
  pluginId: "example.legacy",
  name: "Legacy notification plugin",
  pluginVersion: "1.0.0",
  channels: ["webhook"],
  contentModes: ["text"],
  configSchema: { type: "object" },
  recipientSchema: { type: "object" },
  secretFields: [],
  capabilities: { deliveryReceipts: false }
};

export const pluginManifestSnakeCaseDrift: PluginView["manifest"] = {
  ...legacyPluginManifestSummary,
  // @ts-expect-error Safe business summaries map optional identity_fields to identityFields.
  identity_fields: ["account_id"]
};
