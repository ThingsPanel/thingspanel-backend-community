package store

import "encore.app/automation/domain"

func domainSignal(s string) domain.SignalKind { return domain.SignalKind(s) }

func domainQuality(q string) domain.Quality { return domain.Quality(q) }
