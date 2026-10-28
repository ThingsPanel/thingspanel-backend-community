/* Contract-aligned TypeScript mapping for notification-v2.yaml.
 * This is a checked-in consumer snapshot, not a generated Encore client.
 * Keep in sync through the T01 contract conformance check.
 */

export type Channel = "email" | "sms" | "voice" | "im" | "webhook";
export type DispatchStatus = "queued" | "sending" | "accepted" | "failed" | "unknown";
export type DeliveryStatus = "unsupported" | "pending" | "delivered" | "failed" | "unknown";
export type IntakeStatus = "ready" | "blocked";

export interface Envelope<T> {
  code: 200;
  message: "ok";
  requestId: string;
  data: T;
}

export interface DomainError {
  code: 400 | 401 | 403 | 404 | 409 | 422 | 429 | 503;
  message: string;
  details: { reason: string; fields?: Array<{ field: string; reason: string }> };
  requestId: string;
}

export interface RuntimeError {
  code: string;
  message: string;
  details?: null | Record<string, unknown> | unknown[];
}

export interface PluginRegistration {
  pluginId: string;
  pluginVersion: string;
  origin: string;
  authSecret: string;
  manifestDigest: string;
}

export interface PluginUpdate {
  expectedVersion: number;
  enabled: boolean;
}

export interface PluginGrantUpdate {
  pluginRegistrationId: string;
  tenantId: string;
  enabled: boolean;
  /** Zero creates a missing grant at version 1; otherwise match the current version. */
  expectedVersion: number;
}

export interface PluginGrantView {
  pluginRegistrationId: string;
  tenantId: string;
  enabled: boolean;
  version: number;
  createdAt: string;
  updatedAt: string;
}

export interface PluginGrantListQuery {
  pluginRegistrationId?: string;
  page: number;
  pageSize: number;
}

export type PluginGrantPage = Page<PluginGrantView>;

export interface PluginView {
  id: string;
  pluginId: string;
  pluginVersion: string;
  origin: string;
  manifestDigest: string;
  manifest: {
    pluginId: string;
    name: string;
    pluginVersion: string;
    identityFields?: string[];
    channels: Channel[];
    contentModes: Array<"text" | "template">;
    configSchema: Record<string, unknown>;
    recipientSchema: Record<string, unknown>;
    secretFields: string[];
    capabilities: { deliveryReceipts: boolean };
  };
  enabled: boolean;
  health: "unknown" | "healthy" | "unhealthy";
  version: number;
}

export type SecretPatch =
  | { set: Record<string, string>; clear?: never }
  | { clear: string[]; set?: never };

export interface InstanceCreate {
  pluginRegistrationId: string;
  name: string;
  channel: Channel;
  config: Record<string, unknown>;
  providerIdentity: Record<string, string>;
}

export interface InstanceUpdate {
  expectedVersion: number;
  name?: string;
  configPatch?: Record<string, unknown>;
  secrets?: SecretPatch;
  enabled?: boolean;
}

export type InstanceValidate =
  | { instanceId: string; expectedVersion: number; configPatch: Record<string, unknown> }
  | { draft: { pluginRegistrationId: string; channel: Channel; config: Record<string, unknown> } };

export interface TestSendRequest {
  recipient: Recipient;
  content: Content;
  expiresAt: string;
}

export interface ReceiptCredentialResponse {
  credential: string;
  expiresAt: string;
}

export interface InstanceView {
  id: string;
  pluginRegistrationId: string;
  name: string;
  channel: Channel;
  config: Record<string, unknown>;
  providerIdentity: Record<string, string>;
  enabled: boolean;
  version: number;
  configVersion: number;
  secretState: Record<string, boolean>;
}

export interface Recipient {
  kind: "email" | "phone" | "chat_id" | "user_id" | "webhook";
  address: string;
}

export type Content =
  | { kind: "text"; title?: string; text: string }
  | { kind: "template"; title?: string; template: { id: string; locale?: string; params: Record<string, unknown> } };

export interface SourceRef {
  type: "alarm" | "automation" | "manual" | "test";
  id: string;
}

export interface NotificationSubmit {
  source: SourceRef;
  deliveries: Array<{ instanceId: string; recipient: Recipient; content: Content }>;
  expiresAt: string;
}

export interface AcceptedData {
  accepted: true;
  notificationId: string;
  deliveryIds: string[];
  intakeStatus: IntakeStatus;
  blockedReason?: string;
}

export type RecipientSource =
  | { kind: "literal"; recipient: Recipient }
  | { kind: "member"; userId: string; contactField: "email" | "phone" | "applicationUserId" };

export type ContentBinding =
  | { kind: "text"; title: string; text: string }
  | { kind: "template"; templateId: string; locale: string; paramsMapping: Record<string, string> };

export interface GroupBinding {
  bindingId: string;
  instanceId: string;
  recipientSource: RecipientSource;
  contentBinding: ContentBinding;
}

export interface GroupCreate {
  name: string;
  enabled: boolean;
  bindings: GroupBinding[];
}

export interface GroupUpdate extends GroupCreate {
  expectedVersion: number;
}

export interface GroupView extends GroupCreate {
  id: string;
  revision: number;
  version: number;
  migrationState: "native" | "legacy_unmigrated" | "projection_pending";
  sourceProjections?: SourceGroupProjection[];
}

export interface SourceGroupProjectionRequest {
  sourceDeploymentId: string;
  tenantId: string;
  legacyGroupId: string;
  notificationGroupId: string;
  groupRevision: number;
}

export interface SourceGroupProjection extends SourceGroupProjectionRequest {
  createdAt: string;
}

export interface Page<T> {
  items: T[];
  page: number;
  pageSize: number;
  total: number;
}

export interface ValidationResult {
  valid: boolean;
  errors: Array<{ field: string; reason: string }>;
}

export interface DeliverySummary {
  deliveryId: string;
  dispatchStatus: DispatchStatus;
  deliveryStatus: DeliveryStatus;
  attemptCount: number;
  providerMessageId?: string;
}

export interface DeliveryView extends DeliverySummary {
  notificationId: string;
  instanceId: string;
  configVersion: number;
  recipient: { kind: Recipient["kind"]; display: string };
  source: SourceRef;
  createdAt: string;
  error?: { code: string; message: string; reason?: string };
}

export interface NotificationView {
  id: string;
  source: SourceRef;
  intakeStatus: IntakeStatus;
  blockedReason?: string;
  createdAt: string;
  deliveries: DeliverySummary[];
}

export interface NotificationListQuery {
  page?: number;
  pageSize?: number;
  sourceType?: SourceRef["type"];
  sourceId?: string;
  intakeStatus?: IntakeStatus;
  from?: string;
  to?: string;
}

export interface SourceEventV1 {
  schemaVersion: "1.0";
  sourceDeploymentId: string;
  sourceEventId: string;
  actionId: string;
  tenantId: string;
  notificationGroupId: string;
  groupRevision: number;
  occurredAt: string;
  expiresAt: string;
  payload: {
    subject: string;
    text: string;
    variables: Record<string, unknown>;
    legacyAlertJson?: Record<string, unknown>;
  };
}

export interface PluginReceiptV1 {
  event_id: string;
  provider_message_id: string;
  delivery_id?: string;
  status: "delivered" | "failed" | "unknown";
  occurred_at: string;
  provider_code?: string;
  message?: string;
}

export interface ReceiptAck {
  accepted: true;
}
