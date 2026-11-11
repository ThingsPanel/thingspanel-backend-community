"""Plan native tenant identities using the existing ThingModel v2 namespace.

This is a pure reconciliation step. It never rewrites a source tenant, changes
ownership, or grants access. A source snapshot must include every raw tenant ID
and every existing native owner before its plan can be accepted for conversion.
"""
import hashlib
import json
import uuid


def canonical_identity(kind, raw):
    if not raw or not raw.strip():
        raise ValueError('missing identity requires explicit quarantine')
    # Authentication already disallows surrounding whitespace; do not turn it
    # into another identity silently during an offline conversion.
    if raw != raw.strip():
        raise ValueError('identity has surrounding whitespace')
    try:
        identity = uuid.UUID(raw)
    except ValueError:
        identity = uuid.uuid5(uuid.NAMESPACE_OID, 'thingspanel/community/' + kind + '/' + raw)
    if identity.int == 0:
        raise ValueError('zero identity')
    return str(identity)


def tenant_identity_plan(raw_ids, existing_native_owners):
    rows, owners, errors = [], {}, []
    for raw in sorted(set(raw_ids)):
        if not raw or not raw.strip():
            # Missing-tenant resources are accounted for separately; there is
            # deliberately no shared/default tenant for them.
            continue
        try:
            native = canonical_identity('tenant', raw)
        except ValueError:
            errors.append({'reason':'invalid_source_tenant','source_id':raw})
            continue
        if native in owners:
            errors.append({'reason':'canonical_identity_collision','source_ids':[owners[native],raw]})
        owners[native] = raw
        rows.append({'source_id':raw,'native_id':native})
    for owner in sorted(set(existing_native_owners)):
        if owner not in owners:
            errors.append({'reason':'existing_native_owner_unresolved','native_id':owner})
    return {'format_version':1,'mapping':'thingspanel/community/tenant UUIDv5 OID; UUID passthrough',
            'ready_for_conversion':not errors,'migration_complete':False,'entries':rows,
            'errors':errors,'entries_sha256':hashlib.sha256(json.dumps(rows,sort_keys=True,separators=(',',':')).encode()).hexdigest()}
