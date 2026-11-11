package storage

import "gorm.io/gorm"

// TelemetryHistoryStore is the seam through which accepted telemetry points reach
// durable history. The only implementation today is the PostgreSQL one, which
// writes history, the current snapshot and the winner check inside the caller's
// transaction. Other backends (Timescale profile, MySQL, TDengine) must not be
// plugged in behind this transactional method: they cannot join a relational
// transaction, so they need the intent/replay design in the closeout notes.
type TelemetryHistoryStore interface {
	// AppendInTx writes points using the caller-owned transaction; it never commits.
	AppendInTx(tx *gorm.DB, points []TelemetryData) (int, error)
}

type postgresHistoryStore struct{}

func (postgresHistoryStore) AppendInTx(tx *gorm.DB, points []TelemetryData) (int, error) {
	return writeTelemetryWinners(tx, points)
}

// historyStore falls back to PostgreSQL for writers built without one (tests, zero values).
func (w *telemetryWriter) historyStore() TelemetryHistoryStore {
	if w.history == nil {
		return postgresHistoryStore{}
	}
	return w.history
}
