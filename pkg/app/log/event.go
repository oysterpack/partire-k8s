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

package log

import "github.com/rs/zerolog"

// Event is used to define application log events.
// This enables application log events to be defined as code and documented.
type Event struct {
	Name string
	zerolog.Level
}

// Log starts a new log message.
// - Event.Level is used as the message log level
// - Event.Name is used for the `EVENT` log field value
//
// NOTE: You must call Msg on the returned event in order to send the event.
func (l *Event) Log(logger *zerolog.Logger) *zerolog.Event {
	return logger.WithLevel(l.Level).Str(string(EVENT), l.Name)
}

// standard common events
// - NOTE: they are logged with no level to ensure they are always logged, i.e., regardless of the global log level
var (
	// Start signals that something is being started.
	Start = Event{
		Name:  "start",
		Level: zerolog.NoLevel,
	}

	// Running signals that something is running.
	Running = Event{
		Name:  "running",
		Level: zerolog.NoLevel,
	}

	// Stop signals that something is being stopped.
	Stop = Event{
		Name:  "stop",
		Level: zerolog.NoLevel,
	}

	// Stop signals that something has stopped.
	Stopped = Event{
		Name:  "stopped",
		Level: zerolog.NoLevel,
	}
)