// Copyright 2020 The Prometheus Authors
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
// http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package promlog

import (
	"fmt"
	"testing"

	"github.com/go-kit/log/level"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v2"
)

// Make sure creating and using a logger with an empty configuration doesn't
// result in a panic.
func TestDefaultConfig(t *testing.T) {
	logger := New(&Config{})

	err := logger.Log("hello", "world")
	require.NoError(t, err)
}

func TestUnmarshallLevel(t *testing.T) {
	l := &AllowedLevel{}
	err := yaml.Unmarshal([]byte(`debug`), l)
	if err != nil {
		t.Error(err)
	}
	if l.s != "debug" {
		t.Errorf("expected %s, got %s", "debug", l.s)
	}
}

func TestUnmarshallEmptyLevel(t *testing.T) {
	l := &AllowedLevel{}
	err := yaml.Unmarshal([]byte(``), l)
	if err != nil {
		t.Error(err)
	}
	if l.s != "" {
		t.Errorf("expected empty level, got %s", l.s)
	}
}

func TestUnmarshallBadLevel(t *testing.T) {
	l := &AllowedLevel{}
	err := yaml.Unmarshal([]byte(`debugg`), l)
	if err == nil {
		t.Error("expected error")
	}
	expErr := `unrecognized log level "debugg"`
	if err.Error() != expErr {
		t.Errorf("expected error %s, got %s", expErr, err.Error())
	}
	if l.s != "" {
		t.Errorf("expected empty level, got %s", l.s)
	}
}

type recordKeyvalLogger struct {
	count int
}

func (r *recordKeyvalLogger) Log(keyvals ...interface{}) error {
	for _, v := range keyvals {
		if fmt.Sprintf("%v", v) == "Log level changed" {
			return nil
		}
	}
	r.count++
	return nil
}

func TestDynamic(t *testing.T) {
	logger := NewDynamic(&Config{})

	debugLevel := &AllowedLevel{}
	err := debugLevel.Set("debug")
	require.NoError(t, err)
	infoLevel := &AllowedLevel{}
	err = infoLevel.Set("info")
	require.NoError(t, err)

	recorder := &recordKeyvalLogger{}
	logger.base = recorder
	logger.SetLevel(debugLevel)
	err = level.Debug(logger).Log("hello", "world")
	require.NoError(t, err)
	require.Equalf(t, 1, recorder.count, "log not found")

	recorder.count = 0
	logger.SetLevel(infoLevel)
	err = level.Debug(logger).Log("hello", "world")
	require.NoError(t, err)
	require.Equalf(t, 0, recorder.count, "log found")
	err = level.Info(logger).Log("hello", "world")
	require.NoError(t, err)
	require.Equalf(t, 1, recorder.count, "log not found")
	err = level.Debug(logger).Log("hello", "world")
	require.NoError(t, err)
	require.Equalf(t, 1, recorder.count, "extra log found")
}
