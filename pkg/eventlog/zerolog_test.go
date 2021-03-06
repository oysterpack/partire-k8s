/*
 * Copyright (c) 2019 OysterPack, Inc.
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 * http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

package eventlog_test

import (
	"bytes"
	"encoding/json"
	"github.com/oysterpack/andiamo/pkg/eventlog"
	"github.com/pkg/errors"
	"github.com/rs/xid"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"testing"
	"time"
)

func TestZerologFieldNames(t *testing.T) {
	t.Parallel()

	type LogEvent struct {
		Timestamp uint   `json:"t"`
		Message   string `json:"m"`
		Level     string `json:"l"`
		Name      string `json:"n"`
		Component string `json:"c"`
		Error     string `json:"e"`
	}

	buf := new(bytes.Buffer)
	logger := zerolog.New(buf).With().Timestamp().Logger()
	compLogger := eventlog.ForComponent(&logger, "foo")
	eventLogger := eventlog.ForEvent(compLogger, "bar")

	eventLogger.Error().
		Err(errors.New("BOOM!!!")).
		Msg("foobar")

	t.Log(buf.String())

	var logEvent LogEvent
	err := json.Unmarshal(buf.Bytes(), &logEvent)
	switch {
	case err != nil:
		t.Errorf("*** failed to parse log event as JSON: %v : %v", err, buf.String())
	default:
		t.Logf("%#v", logEvent)
		if logEvent.Timestamp == 0 {
			t.Errorf("*** timestamp field did not match: %#v", logEvent)
		}
		if logEvent.Message != "foobar" {
			t.Errorf("*** message field did not match: %#v", logEvent)
		}
		if logEvent.Level != "error" {
			t.Errorf("*** level field did not match: %#v", logEvent)
		}
		if logEvent.Component != "foo" {
			t.Errorf("*** component field did not match: %#v", logEvent)
		}
		if logEvent.Name != "bar" {
			t.Errorf("*** name field did not match: %#v", logEvent)
		}
		if logEvent.Error != "BOOM!!!" {
			t.Errorf("*** error field did not match: %#v", logEvent)
		}
	}
}

func TestForComponent(t *testing.T) {
	t.Parallel()
	buf := new(bytes.Buffer)
	logger := zerolog.New(buf)
	componentLogger := eventlog.ForComponent(&logger, "foo")
	componentLogger.Log().Msg("")

	type LogEvent struct {
		Component string `json:"c"`
	}

	var logEvent LogEvent
	err := json.Unmarshal(buf.Bytes(), &logEvent)
	switch {
	case err != nil:
		t.Errorf("*** failed to parse log event as JSON: %v : %v", err, buf.String())
	default:
		t.Log(logEvent)
		if logEvent.Component != "foo" {
			t.Errorf("*** component field did not match: %#v", logEvent)
		}
	}
}

func TestForEvent(t *testing.T) {
	t.Parallel()
	buf := new(bytes.Buffer)
	logger := zerolog.New(buf)
	eventLogger := eventlog.ForEvent(&logger, "foo")
	eventLogger.Log().Msg("")

	type LogEvent struct {
		Name string `json:"n"`
	}

	var logEvent LogEvent
	err := json.Unmarshal(buf.Bytes(), &logEvent)
	switch {
	case err != nil:
		t.Errorf("*** failed to parse log event as JSON: %v : %v", err, buf.String())
	default:
		t.Log(logEvent)
		if logEvent.Name != "foo" {
			t.Errorf("*** event name field did not match: %#v", logEvent)
		}
	}
}

func TestWithEventULID(t *testing.T) {
	t.Parallel()
	buf := new(bytes.Buffer)
	eventLogger := eventlog.WithEventXID(zerolog.New(buf))
	eventLogger.Log().Msg("")

	type LogEvent struct {
		XID string `json:"x"`
	}

	var logEvent LogEvent
	err := json.Unmarshal(buf.Bytes(), &logEvent)
	switch {
	case err != nil:
		t.Errorf("*** failed to parse log event as JSON: %v : %v", err, buf.String())
	default:
		t.Log(logEvent)
		_, err := xid.FromString(logEvent.XID)
		assert.NoError(t, err)
	}
}

func TestNewZeroLogger(t *testing.T) {
	t.Parallel()
	buf := new(bytes.Buffer)
	logger := eventlog.NewZeroLogger(buf)

	ts := time.Now()
	logger.Log().Msg("Hello World")
	t.Log(buf.String())

	type LogEvent struct {
		XID       string `json:"x"`
		Timestamp int64  `json:"t"`
	}

	var logEvent LogEvent
	err := json.Unmarshal(buf.Bytes(), &logEvent)
	switch {
	case err != nil:
		t.Errorf("*** failed to parse log event as JSON: %v : %v", err, buf.String())
	default:
		t.Log(logEvent)
		_, err := xid.FromString(logEvent.XID)
		assert.NoError(t, err)

		if logEvent.Timestamp == 0 {
			t.Error("*** timestamp was not set")
		}
		if logEvent.Timestamp < ts.Unix() {
			t.Errorf("*** timestamp is not correct: %v < %v", logEvent.Timestamp, ts.Unix())
		}
	}

}
