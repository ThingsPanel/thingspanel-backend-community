"""No-skip gates for the reviewed uplink runtime and application assembly."""
import argparse
import json
from pathlib import Path

UPLINK_REQUIRED = ('TestTelemetryUplinkKeepsItsValues',
 'TestEventUplinkSeparatesIdentifierFromParams',
 'TestStatusUplinkIsAnOnlineBit',
 'TestAnUnknownTriggerTypeIsRefused',
 'TestAnEmptyTelemetryUplinkIsRefused',
 'TestTheForwarderPostsToTheIngressPath',
 'TestANativeInstallationStopsUsingTheBuiltInEngine',
 'TestNativeWithoutAnAddressFallsBackInsteadOfDroppingEverything',
 'TestALegacyInstallationKeepsTheBuiltInEngine',
 'TestRuntimeModeMatchesUplinkDecision',
 'TestPostgresPreparedNumericCapacityIsNotSchemaFailure',
 'TestPostgresRedisPreparedBusCommitRejectSiblingAndCapacity',
 'TestReliableDelayedOldAttributeReceiptCannotReplaceNewerTimestamp',
 'TestReliableSameTimestampAttributeUpdateRemainsPermitted',
 'TestReliableEventBusinessAndReceiptCommitTogether',
 'TestReliableEffectFailureRollsBackDomain',
 'TestReliableCommandAckRemainsScoped',
 'TestLateAckAfterCommandTimeoutIsConsumedWithoutChangingFailure',
 'TestReliableDurableCommandAckAssociatesLogicalChildAndRootTransport',
 'TestReliableCommandAckIsIdempotentForSameOutcomeAndRejectsConflict',
 'TestReliableChildCommandAckRejectsWrongDeviceTenantAndEpoch',
 'TestPostgresPreparedRejectionKeepsBindingLockThroughDeadCommit',
 'TestPostgresPreparedReliableSchemaRejectionRechecksBindingBeforeDead',
 'TestPostgresPreparedReliableRejectsInvalidWithoutBusinessOrEffects',
 'TestPostgresPreparedReliableGatewayAtomicAndBindingFence',
 'TestParseResponseRequiresKnownResult',
 'TestStatusUplinkBoundDeviceIgnoresLegacyExpiryAndUsesSQLActivity',
 'TestDecodeTelemetryPayloadWithoutDeviceConfigPassesRawPayloadThrough',
 'TestDecodeTelemetryPayloadWithDeviceConfigRetainsDecodeBehavior',
 'TestDecodeTelemetryPayloadPreservesProcessorFailureSignals',
 'TestTrustedTelemetryReceiptIDStablePerPacketAndScopedPerLogicalDevice',
 'TestTrustedTelemetryBuildsCommitRoutedStorageMessage',
 'TestTrustedTelemetryRejectsMalformedMetadataBeforeDeviceLookup',
 'TestTrustedTelemetryStorageSendStopsWithContext',
 'TestTrustedTelemetryProfileRecognitionIsExact',
 'TestTrustedTelemetryFinalBatchCommitsReceiptActivityAndEffectAtomically',
 'TestTrustedTelemetryRejectsStaleBindingsTenantAndReparentedChild',
 'TestTrustedTelemetryTransactionFailureRollsBackEveryTable',
 'TestTrustedTelemetryDuplicateReceiptAndOldSampleDoNotDuplicateEffectOrRegressLatest',
 'TestTrustedTelemetryMetricsCountInsertedRowsAndBatchDuplicates',
 'TestTrustedAndLegacyItemsUseSeparateCommitPaths',
 'TestTrustedTelemetryBindingFinalLockBlocksRouteMigration',
 'TestPostgresTelemetryModelFencePreventsHistoryCurrentAndEffect',
 'TestPostgresProcessOneInCellKeepsOlderOtherCellReceiptUntouched',
 'TestPostgresProcessOneInCellFencesRouteChangedReceiptAtOriginalCell',
 'TestPostgresProcessOneInCellRetryAndConcurrentCellsAreIsolated',
 'TestPostgresConnectingBindingAcceptsPendingButDoesNotProcess',
 'TestPostgresPreparedIngressRunsBeforeLocksAndRechecksRoute',
 'TestPostgresPreparedIngressPermanentRejectionIsSingleTerminalAttempt',
 'TestPostgresPreparedOutcomeOnlyAfterRealTerminalCommit',
 'TestPostgresQuarantineConcurrentDuplicatesShareStableRecord',
 'TestPostgresQuarantineTenantAndDeviceScopesAreIsolated',
 'TestPostgresQuarantineRejectsFencedAndInactiveBindings',
 'TestPostgresQuarantineCapacityRollsBackAndDuplicateBypassesNewSlot',
 'TestPostgresQuarantineQuotaSerializesDifferentDevicesInTenant',
 'TestPostgresQuarantineDatabaseFailureRollsBack',
 'TestQuarantineRejectsInvalidTopicReasonAndOversizeBody',
 'TestQuarantineIDIncludesEveryField',
 'TestPostgresAuditedRetirementBackpressureAndQuarantine',
 'TestPostgresRetiredQuarantineRequiresRouteHistoryAndExactSourceScope',
 'TestPostgresMigrationActivationGatesLedgerProcessing',
 'TestPostgresIngestionIDReceiptRetryScopeAndSourceDedup',
 'TestPostgresConcurrentDuplicateAcceptUsesOneStableReceipt',
 'TestPostgresSameSourceEventWithDifferentIngressTimestampIsDuplicate',
 'TestPostgresSameSamplesWithoutSourceIDRemainDistinct',
 'TestPostgresRejectsWrongEpochAndScopesReceiptReads',
 'TestPostgresHandlerRollbackRetryAndNoDuplicateProcessing',
 'TestPostgresHandlerFailuresBecomeAuditableDeadLetter',
 'TestPostgresRawPayloadLimitIsIndependentOfMetadataLimit',
 'TestPostgresCapacityAndOversizeRollBackAcceptance',
 'TestPostgresInactiveBindingRejectsAcceptance',
 'TestLogicalGatewayBatchHasOneDeadlineAndBoundedCommitQueue',
 'TestRedisLogicalOutcomesDoNotInferSuccessOrDoubleCount',
 'TestRedisLogicalObservationRetentionIsBounded',
 'TestPostgresUplinkBindingFencesMutations',
 'TestPostgresUplinkBindingColdHTTPDoesNotHoldWriteLock',
 'TestPostgresUplinkBindingWriteFenceHoldsUntilCommit',
 'TestUplinkContractColdSingleflightAndWarmZeroHTTP',
 'TestUplinkContractRejectsWrongPinsAndDoesNotCacheFailure',
 'TestUplinkContractCachePinsAndBoundedLRU',
 'TestUplinkContractCancelledWaiterDoesNotPoisonOtherWaiters',
 'TestUplinkContractColdLoadBoundAndNoExternalLocks',
 'TestDescribeReturnsExactPublishedSnapshotAndPins',
 'TestDescribeAcceptsExplicitZeroResourceRevisionButRejectsMissingOrNull',
 'TestNewClientRequiresHTTPSExceptExplicitLoopbackDevelopment',
 'TestDescribeRejectsRedirectAndIdentityOrSnapshotMismatch',
 'TestDescribePaginationBoundAndLegacyBindingFailClosed',
 'TestDescribeRejectsOversizeTimeoutCancellationAndDuplicateKeys',
 'TestValidateCommandEnumStillAppliesScalarConstraints',
 'TestValidateCommandBoundsNumberParsing',
 'TestValidateCommandUsesCoreUTF8ByteMaxLength',
 'TestValidateCommandRejectsNonFiniteConstraint',
 'TestNativeUplinkPublishedCoreParity',
 'TestNativeUplinkWholeMessageTypesAndParameters',
 'TestNativeUplinkContractFailsClosed',
 'TestNativeUplinkSameIdentifierAcrossFamilies',
 'TestNativeUplinkWireIdentifiersAreNotTrimmed')

ASSEMBLY_REQUIRED = ('TestBrokerIngressWorkerConfiguredCellUsesScopedStoreAPI',
 'TestBrokerIngressWorkerConfiguredCellFailsClosedWithoutScopedStoreAPI',
 'TestBrokerIngressAcceptorUsesStableScopedIngestionReceiptID',
 'TestBrokerIngressLegacyDoesNotRequireDBOrStartWorker',
 'TestBrokerIngressIngressV1FailsClosedWithoutDB',
 'TestBrokerIngressAcceptorRejectsUntrustedRouteEpoch',
 'TestBrokerIngressAcceptorUsesOnlyExplicitSourceEventID',
 'TestBrokerIngressAttributeSourceEventIDRemainsBusinessPayload',
 'TestBrokerIngressWorkerStopWaitsAndStartIsIdempotent',
 'TestBrokerIngressPreparedAssemblyNeverFallsBackToUnvalidatedProcess')

COMMAND_COMPAT_REQUIRED = ('TestValidateCommandKindNamespaceAndVerbatimIdentifiers', 'TestNativeCommandListPreservesPublishedIdentifierBytes')

def verify(path, required):
    rows=[json.loads(line) for line in Path(path).read_text(encoding='utf-8').splitlines() if line.strip()]
    for name in required:
        terminal=[r['Action'] for r in rows if r.get('Test')==name and r.get('Action') in ('pass','skip','fail')]
        if terminal!=['pass']:
            raise ValueError('Required test missing, skipped, failed or duplicated: '+name)
    if any(r.get('Action')=='fail' or r.get('Test') and r.get('Action')=='skip' for r in rows):
        raise ValueError('Test log contains failure or skipped test')
    if not any(r.get('Package') and not r.get('Test') and r.get('Action')=='pass' for r in rows):
        raise ValueError('No completed passing package')

if __name__=='__main__':
    p=argparse.ArgumentParser();p.add_argument('kind',choices=['uplink','assembly','command']);p.add_argument('log');a=p.parse_args()
    verify(a.log,{'uplink':UPLINK_REQUIRED,'assembly':ASSEMBLY_REQUIRED,'command':COMMAND_COMPAT_REQUIRED}[a.kind])
    print(a.kind+': all required tests passed, no skipped tests')
