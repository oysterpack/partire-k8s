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

package fxapp

import (
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/pkgerrors"
)

// Applies standard zerolog initialization.
//
// The following global settings are applied for performance reasons:
//   - the following standard logger field names are shortened
//     - Timestamp -> t
//     - Level -> l
//	   - Message -> m
//     - Error -> err
//   - Unix time format is used for performance reasons - seconds granularity is sufficient for log events
//
// An error stack marshaller is configured.
func init() {
	zerolog.TimestampFieldName = "t"
	zerolog.LevelFieldName = "l"
	zerolog.MessageFieldName = "m"
	zerolog.ErrorFieldName = "e"

	zerolog.TimeFieldFormat = zerolog.TimeFormatUnix
	zerolog.DurationFieldInteger = true

	zerolog.ErrorStackMarshaler = pkgerrors.MarshalStack
}

// EventLogger returns a new logger with the event name field 'n' set to the specified value.
func EventLogger(logger *zerolog.Logger, id string) *zerolog.Logger {
	l := logger.With().Str("n", id).Logger()
	return &l
}

// ComponentLogger returns a new logger with the component field 'c' set to the specified value.
func ComponentLogger(logger *zerolog.Logger, id string) *zerolog.Logger {
	l := logger.With().Str("c", id).Logger()
	return &l
}
