# Template custom-field PATCH kernel implementation

Date: 2026-10-27
Scope: kernel only; no OAS/frontend/backend contract edits, migrations, old-suite reruns, CLI/HTTP reruns, image rebuild, or production operations.

## Implementation

- Added optional strict `customFieldChanges` parsing and canonicalization to the native template PATCH input. It requires both arrays, 1–100 operations, unique keys, disjoint upsert/remove keys, and rejects simultaneous `configuration` plus `customFieldChanges`.
- Merge uses raw JSON values and changes only `configuration.customFieldDefinitions`. Existing definition order is retained, new definitions append in upsert order, explicit fields overwrite only those fields, and untouched raw values retain numeric precision. Key-only upsert of an existing definition is a no-op; incomplete new definitions fail the existing configuration validator.
- Existing-value protection compares complete old/new definitions in both PATCH forms. It protects removal of an existing definition, incompatible type/enum changes, and re-enabling a definition with incompatible saved values. Name/description/default changes, disabling, enum option labels/order changes, and removal of a key absent from the previous definition set do not cause false conflicts.
- Live devices are scanned through `device_access_bindings JOIN devices`, scoped to tenant/template and excluding deleted devices. JSON object membership means `0`, `false`, and `null` values are all recognized. Conflict errors include only field key and affected device count, never stored values. The existing raw handler maps business `FailedPrecondition` to 409 and ETag mismatch to 412.
- Device PATCH reads current template definitions with `FOR SHARE`. Template update holds the existing template `FOR UPDATE` lock and scans device rows without locking them, preserving a single lock order and serializing definition changes against device-value writes.

## Targeted verification

Command:

```text
encore test ./devicemetadata -run 'TestParseNativeTemplate(CustomFieldChanges|Patch)|TestMergeNativeCustomFieldChanges|TestNativeTemplateCustomFieldChangesProtectLiveDeviceValues|TestNativeDeviceCustomFieldWriteSerializesWithDefinitionRemoval|TestNativeTemplateMutationProblemKeepsConditionalAndBusinessConflictsSeparate' -count=1
```

Result: `ok encore.app/devicemetadata 0.883s`.

Coverage includes parser rejection/precision, raw merge preservation, 0/false/null removal conflicts, incompatible type and enum changes, compatible rename/default/disable and empty-field removal, same-key/body replay, stale ETag, complete-configuration bypass prevention, failed-operation receipt/audit rollback, 409-vs-412 HTTP mapping, and a concurrent device PATCH waiting on template `FOR SHARE` then rejecting the removed field without changing stored device values.

## Preserved failures and corrections

1. First database-targeted run exposed an assumed `devices.template_id` column (`SQLSTATE 42703`). The implementation was corrected to use the established `device_access_bindings` relation; a source scan confirmed no such old-column reference remains in the new scan path.
2. A first test assertion treated a field key in the safe conflict message as if it were a stored value; the assertion was narrowed to actual fixture values.
3. A first type-change test changed the type but left an invalid default, so configuration validation correctly returned 422 before the existing-value check. The test now supplies a valid new default and verifies the intended 409 value-compatibility conflict.

## Changed-file fingerprints

Worktree: `/Users/junhong/.codex/worktrees/device-metadata-native-cutover-1.3/kernel`

```text
9055e70739bacd368cfe604258022142dc43fff110f39a309460112419fcede0  encore-api/devicemetadata/native_template_input.go
0e84185951e9c12f49c65f5832ee9d4a377880e06c01a7b5395b08048e37d29b  encore-api/devicemetadata/native_custom_fields.go
3939e53509609e7c7848be574a5cf3809ecf4129627c2299158bda6607d08d7d  encore-api/devicemetadata/native_template_write.go
3c50fdded25a615b2946c364802761d8a7779cf45ac6e58e4daad73f7411c9d7  encore-api/devicemetadata/native_device_write.go
b0ab0bcf09d0111345618f2e26a54c9d36a963c960fa83dc84174796362b6402  encore-api/devicemetadata/native_template_input_test.go
2768faf7a8323d051bbb45bd9e5a4a7a905e1aa3ba15775a0546ba4ca81b6cf3  encore-api/devicemetadata/native_template_write_test.go
```

No secrets, credentials, request bodies containing private material, or runtime values are included.

## Final strict-contract, build, runtime, and HTTP evidence (2026-10-27)

The earlier `0.883s` targeted run above is historical and predates the final strict decode additions. The final required Encore targeted set, including missing-required-field, unknown-enum-property, live-value guard, raw HTTP status mapping, and concurrency coverage, passed as `ok encore.app/devicemetadata 1.103s`. Exact command and output are in `evidence/2026-10-27-pause/resume-20261027/template-custom-field-final-targeted-test.log`.

### Frozen source and build

- Verified baseline 479 manifest/archive: `backend-source-manifest-credentials-groups.json` SHA-256 `9654b44a418d1f76c9b0be296361956f42bd8cb45e3ceeb5de2281f81f31e734`; `backend-source-credentials-groups-479.tar.gz` SHA-256 `de80594fb88be5335208097494e075076d7b5b6b6b72c26d80e37f60c2fc1916`. The shared 479 manifest and archive match all 479 files. A separate scratch-root 477 manifest was not used.
- Final extension freeze: `backend-source-manifest-template-custom-field-changes-480.json` SHA-256 `8e8ef99be664e57456b8199f701a9d23e45347ba82a33bdc52237f0abbeb38ea`; `backend-source-template-custom-field-changes-480.tar.gz` SHA-256 `5ef78af75db18c930afd8dfea60bb36894b0c8cc7de7e90ba5861152a15f3d3c`. Manifest/archive verified 480/480. The existing owned ordinary clone matched baseline 479 with zero mismatches before overlay; six implementation/test files were overlaid, then all 480 matched.
- Built with Encore CLI `1.58.6`, Go `1.26.2`, target `linux/arm64`, using the original private infra profile path and no `--skip-config`. Image `codex-device-management-20261027-7312c5b5:template-custom-field-changes-480`, ID `sha256:cc422dadc7014c26e4af8c8880b6afb149f19cc8504795baf85e17d4bea3e3ad`. The first wrapper's post-build summarizer hit zsh's read-only `status` variable; image inspection and successful-export build log confirmed the build, and it was not repeated.
- New image runs as owned app on `127.0.0.1:55067`. Previous 479 app was stopped and renamed `codex-dm-template-read-7312c5b5-67dee72a-app-before-custom-field`; its image remains available for rollback. The owned PG/stub/network remain running. No migrations, schema changes, production calls, or external pushes occurred.
- Safe build log copy: `evidence/2026-10-27-pause/resume-20261027/build-template-custom-fields.log` SHA-256 `9b652295fe81b7cf0b89d2e5a3b155d76ebcf422337af3b8b050a7e4a529dbbc`.

### Runtime permissions and first failures

- The first HTTP harness attempt read `Etag` case-sensitively and omitted `If-Match`; the API correctly returned 428 before any write. The harness now reads response header names case-insensitively and reuses the same unchanged HTTP fixture.
- The next real write returned 503. Effective privilege checks showed the runtime role already had `UPDATE(id)` but lacked the 12 columns used by `saveAccessTemplateConfigurationTx`. A no-execution `EXPLAIN UPDATE ... WHERE false` reproduced SQLSTATE `42501` (`permission_denied`).
- Granted only `UPDATE(name,description,brand,product_name,model_number,image_asset_id,tags,configuration,administrative_state,updated_by,resource_version,updated_at)` on `public.device_access_templates`. Post-check: all 12 target columns effective, table-level UPDATE remains false, `id` remains the pre-existing lock privilege, and no permission was added for `tenant_id` or `deleted_at`. No schema or source change was needed for the 503.
- Permission evidence is in `evidence/2026-10-27-pause/resume-20261027/template-custom-field-runtime-permissions.json`.

### Real HTTP result

The final independent owned fixture passed all 17 HTTP checks. It covers full-configuration removal of an assigned `false` value (409 with ETag preserved), deleting assigned `null` and `0` values (409), enum contraction against a saved legacy value (409), upsert plus removal of an unassigned field (200), same-key/same-body replay (byte-identical 200), same-key/different-body conflict (409), stale ETag (412), missing required `enabled`, unknown enum property, and simultaneous full configuration plus `customFieldChanges` (all 422), plus preservation of a large integer in unrelated access parameters, an untouched high-precision numeric default, and unrelated configuration sections. Safe summary: `template-custom-field-http-summary.json`; request log has only methods, paths, statuses, ETags and body hashes in `template-custom-field-http-requests.redacted.jsonl`.

- HTTP fixture retained: template `f64aa1a5-d9ca-43b7-801f-ccc2aba828cf`, device `2cd3eef4-54a1-44bc-9484-4e3f28011a3c`.
- UI fixture retained separately and was not modified by these HTTP tests: template `101ac1e2-b45c-4128-9b50-604a19c7496f`, device `3d630172-2996-4e42-9705-851417bd9aed`. It has a saved enum value and a saved numeric zero for UI 409 checks. A read-only GET confirmed the UI's pending key `uiNullNumber20261027` does not yet exist; template resourceVersion is 1.

### Synthetic-session containment

During private JSON schema inspection, four owned synthetic bearer tokens were inadvertently emitted as token-keyed object keys in a local tool diagnostic. No production credential was involved. The original four-entry mapping was overwritten with fresh random sessions; the current UI session was rotated to a matching principal/tenant/authority and the owned stub was restarted using the same mount. Private session/profile files remain mode 0600 and were not copied into shared evidence. The stub lookup is an exact active-token-key match using constant-time comparison and has no fallback. A validly signed but unregistered synthetic token received HTTP 401; the rotated UI session received HTTP 200. The exact old four bearer values were not retained, so no per-token 401 claim is made. Safe facts are in `synthetic-session-rotation-containment.json`.

Full safe runtime/image/source details are in `template-custom-field-runtime-summary.json`. Private configuration files remain in their original scratch locations and are referenced by path only.
