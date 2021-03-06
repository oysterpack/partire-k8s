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
	"github.com/oysterpack/andiamo/pkg/fx/health"
	"github.com/rs/zerolog"
	"go.uber.org/fx"
	"reflect"
	"time"
)

// app lifecycle event IDs
const (
	// 	type Data struct {
	//		StartTimeout 	uint `json:"start_timeout"`
	//		StopTimeout  	uint `json:"stop_timeout"`
	//		Provides     	[]string
	//		Invokes      	[]string
	//		DependencyGraph string `json:"dot_graph"` // DOT language visualization of the app dependency graph
	//	}
	InitializedEvent = "01DE4STZ0S24RG7R08PAY1RQX3"
	// 	type Data struct {
	//		Err string `json:"e"`
	//	}
	InitFailedEvent = "01DE4SWMZXD1ZB40QRT7RGQVPN"

	StartingEvent = "01DE4SXMG8W3KSPZ9FNZ8Z17F8"
	// 	type Data struct {
	//		Err string `json:"e"`
	//	}
	StartFailedEvent = "01DE4SY6RYCD0356KYJV7G7THW"

	// 	type Data struct {
	//		Duration uint
	//	}
	StartedEvent = "01DE4X10QCV1M8TKRNXDK6AK7C"

	ReadyEvent = "01DEJ5RA8XRZVECJDJFAA2PWJF"

	StoppingEvent = "01DE4SZ1KY60JQTF7XP4DQ8WGC"
	// 	type Data struct {
	//		Err string `json:"e"`
	//	}
	StopFailedEvent = "01DE4T0W35RPD6QMDS42WQXR48"

	// 	type Data struct {
	//		Duration uint
	//	}
	StoppedEvent = "01DE4T1V9N50BB67V424S6MG5C"
)

type appInfo struct {
	App
	fx.DotGraph
}

// MarshalZerologObject implements zerolog.LogObjectMarshaler interface
func (event appInfo) MarshalZerologObject(e *zerolog.Event) {
	e.Dur("start_timeout", event.StartTimeout())
	e.Dur("stop_timeout", event.StopTimeout())

	typeNames := func(types []reflect.Type) []string {
		var names []string
		for _, t := range types {
			names = append(names, t.String())
		}
		return names
	}

	e.Strs("provides", typeNames(event.App.ConstructorTypes()))
	e.Strs("invokes", typeNames(event.App.FuncTypes()))
	e.Str("dot_graph", string(event.DotGraph))
}

type duration time.Duration

// MarshalZerologObject implements zerolog.LogObjectMarshaler interface
func (d duration) MarshalZerologObject(e *zerolog.Event) {
	e.Dur("duration", time.Duration(d))
}

// health check related events
const (
	//  sample event data:
	//  {
	//    "id": "01DF3MNDKPB69AJR7ZGDNB3KA1",
	//    "desc_id": "01DF3MNDKP8DS3B04E2TKFHXD9",
	//    "description": "Foo",
	//    "red_impact": "app is unavailable",
	//    "yellow_impact": "app response times are slow",
	//    "timeout": 5000,
	//    "run_interval": 15000
	//  }
	HealthCheckRegisteredEvent = "01DF3FV60A2J1WKX5NQHP47H61"

	//  sample event data:
	//  {
	//    "id": "01DF3MNDKPB69AJR7ZGDNB3KA1",
	//	  "t": 155454546546,
	//	  "d": 9
	//  }
	HealthCheckResultEvent = "01DF3X60Z7XFYVVXGE9TFFQ7Z1"

	HealthCheckGaugeRegistrationErrorEvent = "01DF6M0T7K3DNSFMFQ26TM7XX4"
)

type healthCheck struct {
	health.RegisteredCheck
	error
}

func (h *healthCheck) MarshalZerologObject(e *zerolog.Event) {
	e.Str("id", h.ID)
	e.Str("description", h.Description)
	e.Str("red_impact", h.RedImpact)
	if h.YellowImpact != "" {
		e.Str("yellow_impact", h.YellowImpact)
	}
	e.Dur("timeout", h.Timeout)
	e.Dur("run_interval", h.RunInterval)
	if h.error != nil {
		e.Err(h.error)
	}
}

type healthCheckResult struct {
	health.Result
}

func (h *healthCheckResult) MarshalZerologObject(e *zerolog.Event) {
	e.Str("id", h.ID)
	e.Uint8("status", uint8(h.Status))
	e.Time("start", h.Time)
	e.Dur("dur", h.Duration)
	if h.Err != nil {
		e.Err(h.Err)
	}
}

// probe related events
const (
	// 	type Data struct {
	//		Duration uint
	//	}
	LivenessProbeEvent = "01DF91XTSXWVDJQ4XJ432KQFXY"
)
