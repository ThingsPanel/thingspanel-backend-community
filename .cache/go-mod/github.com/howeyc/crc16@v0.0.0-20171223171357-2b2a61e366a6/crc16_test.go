// Copyright 2013 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package crc16

import (
	"bytes"
	"encoding"
	"testing"
)

type testCase struct {
	Message []byte
	CRC     uint16
}

func TestARC(t *testing.T) {
	tests := []testCase{
		{[]byte("123456789"), 0xBB3D}}
	table := MakeTable(IBM)
	for _, testcase := range tests {
		result := ^Update(0xFFFF, table, testcase.Message)
		if testcase.CRC != result {
			t.Fatalf("ARC CRC-16 value is incorrect, expected %x, received %x.", testcase.CRC, result)
		}
	}
}

func TestModbus(t *testing.T) {
	tests := []testCase{
		{[]byte{0xEA, 0x03, 0x00, 0x00, 0x00, 0x64}, 0x3A53},
		{[]byte{0x4B, 0x03, 0x00, 0x2C, 0x00, 0x37}, 0xBFCB},
		{[]byte("123456789"), 0x4B37},
		{[]byte{0x0D, 0x01, 0x00, 0x62, 0x00, 0x33}, 0x0DDD},
		{[]byte{0x01, 0x03, 0x00, 0x85, 0x00, 0x01}, 0xE395},
		}
	for _, testcase := range tests {
		result := ^ChecksumIBM(testcase.Message)
		if testcase.CRC != result {
			t.Fatalf("Modbus CRC-16 value is incorrect, expected %X, received %X.", testcase.CRC, result)
		}
	}
}

func BenchmarkChecksumIBM(b *testing.B) {
	for i := 0; i < b.N; i++ {
		ChecksumIBM([]byte{0xEA, 0x03, 0x00, 0x00, 0x00, 0x64})
	}
}

func BenchmarkMakeTable(b *testing.B) {
	for i := 0; i < b.N; i++ {
		MakeTable(IBM)
	}
}

func TestCCITTFalse(t *testing.T) {
	data := []byte("testdata")
	target := uint16(0xDC7C)

	actual := ChecksumCCITTFalse(data)
	if actual != target {
		t.Fatalf("CCITT checksum did not return the correct value, expected %x, received %x", target, actual)
	}
}

func TestBinaryMarshal(t *testing.T) {
	input1 := "The tunneling gopher digs downwards, "
	input2 := "unaware of what he will find."

	first := New(IBMTable)
	firstrev := New(MBusTable)
	first.Write([]byte(input1))
	firstrev.Write([]byte(input1))

	marshaler, ok := first.(encoding.BinaryMarshaler)
	if !ok {
		t.Fatal("first does not implement encoding.BinaryMarshaler")
	}
	state, err := marshaler.MarshalBinary()
	if err != nil {
		t.Fatal("unable to marshal hash:", err)
	}
	marshalerrev, rok := firstrev.(encoding.BinaryMarshaler)
	if !rok {
		t.Fatal("firstrev does not implement encoding.BinaryMarshaler")
	}
	staterev, err := marshalerrev.MarshalBinary()
	if err != nil {
		t.Fatal("unable to marshal hash:", err)
	}

	second := New(IBMTable)
	secondrev := New(MBusTable)

	unmarshaler, ok := second.(encoding.BinaryUnmarshaler)
	if !ok {
		t.Fatal("second does not implement encoding.BinaryUnmarshaler")
	}
	if err := unmarshaler.UnmarshalBinary(state); err != nil {
		t.Fatal("unable to unmarshal hash:", err)
	}

	unmarshalerrev, rok := secondrev.(encoding.BinaryUnmarshaler)
	if !rok {
		t.Fatal("secondrev does not implement encoding.BinaryUnmarshaler")
	}
	if err := unmarshalerrev.UnmarshalBinary(staterev); err != nil {
		t.Fatal("unable to unmarshal hash:", err)
	}

	first.Write([]byte(input2))
	second.Write([]byte(input2))
	firstrev.Write([]byte(input2))
	secondrev.Write([]byte(input2))

	if !bytes.Equal(first.Sum(nil), second.Sum(nil)) {
		t.Fatalf("does not match!")
	}
	if !bytes.Equal(firstrev.Sum(nil), secondrev.Sum(nil)) {
		t.Fatalf("does not match!")
	}
	if bytes.Equal(first.Sum(nil), secondrev.Sum(nil)) {
		t.Fatalf("should not match!")
	}
}
