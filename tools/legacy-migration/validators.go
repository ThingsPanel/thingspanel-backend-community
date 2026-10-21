package main

import "strings"

type ValidationReport struct {
	SampleSize int      `json:"sample_size"`
	Matched    int      `json:"matched"`
	Missing    []string `json:"missing,omitempty"`
	Mismatched []string `json:"mismatched,omitempty"`
	MatchRate  float64  `json:"match_rate"`
}

func buildValidationReport(expected map[string][]string, actual map[string][]string) *ValidationReport {
	report := &ValidationReport{}
	for sn, exp := range expected {
		report.SampleSize++
		act, ok := actual[sn]
		if !ok {
			report.Missing = append(report.Missing, sn)
			continue
		}
		if identifiersMatch(exp, act) {
			report.Matched++
			continue
		}
		report.Mismatched = append(report.Mismatched, sn)
	}
	if report.SampleSize > 0 {
		report.MatchRate = float64(report.Matched) / float64(report.SampleSize)
	}
	return report
}

func identifiersMatch(expected, actual []string) bool {
	if len(expected) != len(actual) {
		return false
	}
	set := make(map[string]struct{}, len(expected))
	for _, item := range expected {
		set[item] = struct{}{}
	}
	for _, item := range actual {
		if _, ok := set[item]; ok {
			continue
		}
		trimmed := strings.TrimSuffix(strings.TrimSuffix(strings.TrimSuffix(item, "_property"), "_event"), "_command")
		if _, ok := set[trimmed]; ok {
			continue
		}
		return false
	}
	return true
}
