package notification

var (
	_ InstanceRepository               = (*Store)(nil)
	_ GroupResolver                    = (*Store)(nil)
	_ SourceProjectionStore            = (*Store)(nil)
	_ SubmissionStore                  = (*Store)(nil)
	_ SubmissionReplayStore            = (*Store)(nil)
	_ DeliveryStore                    = (*Store)(nil)
	_ DeliveryQueryStore               = (*Store)(nil)
	_ ReceiptStore                     = (*Store)(nil)
	_ GroupManagementRepository        = (*Store)(nil)
	_ PluginManagementRepository       = (*Store)(nil)
	_ InstanceManagementRepository     = (*Store)(nil)
	_ ManagementReplayStore            = (*Store)(nil)
	_ NotificationCredentialRepository = (*Store)(nil)
	_ PluginRuntimeResolver            = (*Store)(nil)
)
