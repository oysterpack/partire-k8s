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

package fx

import (
	"github.com/oysterpack/partire-k8s/pkg/app/logging"
	"github.com/rs/zerolog"
	"runtime/debug"
)

// AppTag is the tag used to indicate that the events were generated by the app container.
const AppTag logging.Tag = "app"

var (
	// New indicates a new app has been created, and is ready to be initialized
	New = logging.MustNewEvent("new", zerolog.NoLevel, AppTag)

	// Initialized indicates the app has been initialized and is ready to be started.
	Initialized = logging.MustNewEvent("initialized", zerolog.NoLevel, AppTag)

	// Start signals that the app is starting.
	//
	// If debug build info is available, then it will be included in the event data with following structure:
	//
	//   type LogEvent struct {
	//	    Build struct {
	//		  Path string
	//		  Main struct {
	//		  	Path     string
	//			Version  string
	//			Checksum string
	//		  }
	//		  Deps []struct {
	//			Path     string
	//			Version  string
	//			Checksum string
	//		  }
	//	    }
	//   }
	Start = logging.MustNewEvent("start", zerolog.NoLevel, AppTag)

	// Running signals that the app is running.
	Running = logging.MustNewEvent("running", zerolog.NoLevel, AppTag)

	// StopSignal indicates that the stop signalled has been received
	StopSignal = logging.MustNewEvent("stop_signal", zerolog.NoLevel, AppTag)

	// Stop signals that the app is stopping.
	Stop = logging.MustNewEvent("stop", zerolog.NoLevel, AppTag)

	// Stopped signals that the app has stopped.
	Stopped = logging.MustNewEvent("stopped", zerolog.NoLevel, AppTag)

	// CompRegistered indicates that a component has been registered
	CompRegistered = logging.MustNewEvent("comp_registered", zerolog.NoLevel, AppTag)
)

func logStartEvent(logger *zerolog.Logger) {
	logEvent := Start.Log(logger)

	if buildInfo, ok := debug.ReadBuildInfo(); ok {
		appendBuildInfo(logEvent, newBuildInfo(buildInfo))
	}

	logEvent.Msg("")
}

func appendBuildInfo(logEvent *zerolog.Event, b *buildInfo) {
	logEvent.Dict("build", zerolog.Dict().
		Str("path", b.path).
		Dict("main", zerolog.Dict().
			Str("path", b.main.path).
			Str("version", b.main.version).
			Str("checksum", b.main.checksum)).
		Array("deps", b.depArr()),
	)
}

type buildInfo struct {
	path string    // The main package path
	main module    // The main module information
	deps []*module // Module dependencies
}

func (b *buildInfo) depArr() *zerolog.Array {
	arr := zerolog.Arr()
	for _, d := range b.deps {
		arr.Object(&module{d.path, d.version, d.checksum})
	}
	return arr
}

func newBuildInfo(b *debug.BuildInfo) *buildInfo {
	var deps []*module
	for _, dep := range b.Deps {
		deps = append(deps, newModule(dep))
	}
	return &buildInfo{
		b.Path,
		module{b.Main.Path, b.Main.Version, b.Main.Sum},
		deps,
	}
}

type module struct {
	path     string
	version  string
	checksum string
}

func newModule(m *debug.Module) *module {
	d := m
	if m.Replace != nil {
		d = m.Replace
	}
	return &module{d.Path, d.Version, d.Sum}
}

func (m *module) MarshalZerologObject(e *zerolog.Event) {
	e.Str("path", m.path)
	e.Str("version", m.version)
	e.Str("checksum", m.checksum)
}
