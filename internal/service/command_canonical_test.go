package service

import (
	"bytes"
	"project/internal/model"
	"testing"
)

func TestCommandIdempotencyKeepsLargeIntegerPrecisionAndIgnoresDerivedRoute(t *testing.T) {
	value := `{"counter":9007199254740992}`
	req := &model.PutMessageForCommand{DeviceID: "logical", Identify: "setCounter", Value: &value}
	a, err := canonicalizeCommand(req, "root-before")
	if err != nil {
		t.Fatal(err)
	}
	b, err := canonicalizeCommand(req, "root-after")
	if err != nil || !bytes.Equal(a, b) {
		t.Fatal("route change altered unchanged client request")
	}
	value = `{"counter":9007199254740993}`
	c, err := canonicalizeCommand(req, "root-before")
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(a, c) {
		t.Fatal("distinct large integers share idempotency canonical input")
	}
}

func TestCommandCanonicalRejectsAmbiguousDuplicateParameters(t *testing.T) {
	for _, value := range []string{`{"value":1,"value":2}`, `{"a":1,"\u0061":2}`, `{"items":[{"x":1,"x":2}]}`, `{"x":1} {"x":2}`} {
		req := &model.PutMessageForCommand{DeviceID: "device", Identify: "set", Value: &value}
		if _, err := canonicalizeCommand(req, ""); err == nil {
			t.Fatalf("ambiguous command parameters accepted: %s", value)
		}
	}
}

func TestCommandCanonicalDistinguishesExplicitNullAndOmittedParams(t *testing.T) {
	req := &model.PutMessageForCommand{DeviceID: "device", Identify: "set"}
	omitted, err := canonicalizeCommand(req, "")
	if err != nil {
		t.Fatal(err)
	}
	value := "null"
	req.Value = &value
	explicit, err := canonicalizeCommand(req, "")
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(omitted, explicit) {
		t.Fatal("null setter and omitted parameters share idempotency input")
	}
}
