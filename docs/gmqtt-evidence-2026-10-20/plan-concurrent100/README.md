# Plan authorization concurrency component evidence

Community-only real private PostgreSQL schema. Two existing store tests were enlarged to 100 callers; no production code or DB-pool limit changed (pool maximum 16). All 100 callers report ready before a shared start barrier opens; this is concurrent callers, not 100 simultaneously executing SQL statements.

Same idempotency key: 100 successes, 99 duplicate results, one command ID, one command row and one log. One hundred distinct keys on one authorization: exactly one success, 99 ErrRejected, no other error, one command/log and plan link to the same ID. Count/Take errors are checked.

Luna targeted race and root whole brokercommands package race ran source SHA `e0c42955872f9548543e6c0686b11bf3992eaf6a1aec35678e76366166e40352`: two targeted tests and all 23 package test events pass respectively, zero skip/fail. Root then strengthened the same-key check to reject an empty returned command ID. Final source SHA `46dde49030f3e4b1796e518d44829928baf3c607a0e3009413925e23315b462e`; both targeted tests passed again under race, zero skip/fail. The original logs and exact source status files are retained rather than claiming one final-source whole-package run. DSN was injected from the private mode-0600 QA file and not logged.

This proves store-layer atomic/idempotent plan consumption only. It is not full HTTP/identity-revocation acceptance, a model/human workflow, dispatch/crash certification or physical effect. AC-A02 stays PENDING for the original formal suite. The user subsequently requested structure plus smoke acceptance for current development delivery and deferred expanded testing to their team; do not use this evidence to justify more stress testing.
