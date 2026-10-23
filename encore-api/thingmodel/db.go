package thingmodel

import "encore.dev/storage/sqldb"

// Reference the single existing resource and migration ledger. This does not
// import or start its declaring service; release builds select this narrow host.
var db = sqldb.Named("device_metadata")
