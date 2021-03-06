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

package fxapp_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/hashicorp/go-retryablehttp"
	"github.com/oysterpack/andiamo/pkg/fx/health"
	"github.com/oysterpack/andiamo/pkg/fxapp"
	"github.com/oysterpack/andiamo/pkg/fxapptest"
	"github.com/oysterpack/andiamo/pkg/ulids"
	"github.com/prometheus/client_golang/prometheus"
	io_prometheus_client "github.com/prometheus/client_model/go"
	"go.uber.org/fx"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// Ready is an app lifecycle state. To be ready means the app is ready to serve requests.
// When the app is ready, it logs an event.
func TestReadinessProbe(t *testing.T) {
	buf := fxapptest.NewSyncLog()
	var readinessProbe fxapp.ReadinessWaitGroup
	app, err := fxapp.NewBuilder(fxapp.ID(ulids.MustNew()), fxapp.ReleaseID(ulids.MustNew())).
		Invoke(func(lc fx.Lifecycle, readinessProbe fxapp.ReadinessWaitGroup) {
			readinessProbe.Add(1)
			lc.Append(fx.Hook{
				OnStart: func(context.Context) error {
					readinessProbe.Done()
					return nil
				},
			})
		}).
		Populate(&readinessProbe).
		LogWriter(buf).
		Build()

	switch {
	case err != nil:
		t.Errorf("*** app build failed : %v", err)
	default:
		if readinessProbe.Count() < 2 {
			t.Errorf("*** the readiness probe count should be at least 2: %d", readinessProbe.Count())
		}

		go app.Run()
		defer func() {
			app.Shutdown()
			<-app.Done()
		}()
		<-app.Started()

		// wait for the app to be ready
		<-readinessProbe.Ready()
		// the app uses the same underying readiness probe
		<-app.Ready()

		// Then the app's readiness HTTP endpoint should pass
		checkHTTPGetResponseStatusOK(t, fmt.Sprintf("http://:8008/%s", fxapp.ReadyEvent))

		app.Shutdown()
		<-app.Done()

		// And the app Ready lifecycle event is logged
		type LogEvent struct {
			Name    string `json:"n"`
			Message string `json:"m"`
		}

		var logEvent LogEvent
		for _, line := range strings.Split(buf.String(), "\n") {
			t.Log(line)
			err := json.Unmarshal([]byte(line), &logEvent)
			if err != nil {
				t.Errorf("*** failed to parse log event: %v", err)
				break
			}

			if logEvent.Name == string(fxapp.ReadyEvent) {
				break
			}
		}

		if logEvent.Name != string(fxapp.ReadyEvent) {
			t.Error("*** app readiness log event was not logged")
		}
	}
}

func TestReadinessProbeNotReady(t *testing.T) {
	buf := fxapptest.NewSyncLog()
	wg := &sync.WaitGroup{}
	wg.Add(1)
	var readinessProbe fxapp.ReadinessWaitGroup
	app, err := fxapp.NewBuilder(fxapp.ID(ulids.MustNew()), fxapp.ReleaseID(ulids.MustNew())).
		Invoke(func(lc fx.Lifecycle, readinessProbe fxapp.ReadinessWaitGroup) {
			readinessProbe.Add(1)
			lc.Append(fx.Hook{
				OnStart: func(context.Context) error {
					go func() {
						wg.Wait()
						readinessProbe.Done()
					}()
					return nil
				},
			})
		}).
		Populate(&readinessProbe).
		LogWriter(buf).
		Build()

	switch {
	case err != nil:
		t.Errorf("*** app build failed : %v", err)
	default:
		go app.Run()
		defer func() {
			app.Shutdown()
			<-app.Done()
		}()
		<-app.Started()

		checkHTTPGetResponseStatus(t, fmt.Sprintf("http://:8008/%s", fxapp.ReadyEvent), http.StatusServiceUnavailable)
		checkHTTPGetResponse(t, fmt.Sprintf("http://:8008/%s", fxapp.ReadyEvent), func(response *http.Response) {
			t.Log("status ", response.StatusCode)
			if count, err := strconv.ParseUint(response.Header.Get("x-readiness-wait-group-count"), 10, 64); err != nil {
				t.Errorf("*** failed to parse `x-readiness-wait-group-count` header into num: %v", err)
			} else if count == 0 {
				t.Errorf("*** expected count to be > 0: %d", count)
			}

		})
		wg.Done()
		// the app uses the same underying readiness probe
		<-app.Ready()

		// Then the app's readiness HTTP endpoint should pass
		checkHTTPGetResponseStatusOK(t, fmt.Sprintf("http://:8008/%s", fxapp.ReadyEvent))
	}
}

func TestNewReadinessWaitgroup(t *testing.T) {
	readinessGroup := fxapp.NewReadinessWaitgroup(10)
	if readinessGroup.Count() != 10 {
		t.Errorf("*** count should be 10: %d", readinessGroup.Count())
	}
	readinessGroup.Done()
	if readinessGroup.Count() != 9 {
		t.Errorf("*** count should be 9: %d", readinessGroup.Count())
	}

	i := 0
	for ; readinessGroup.Count() > 0; i++ {
		readinessGroup.Done()
	}
	if readinessGroup.Count() != 0 {
		t.Errorf("*** count should be 0: %d", readinessGroup.Count())
	}
	if i != 9 {
		t.Errorf("*** counter should have decremented 9 times: %d", i)
	}
}

func TestNewReadinessWaitgroup_Async(t *testing.T) {
	readinessGroup := fxapp.NewReadinessWaitgroup(10)
	for i := 0; i < 10; i++ {
		go readinessGroup.Done()
	}
	<-readinessGroup.Ready()
	if readinessGroup.Count() != 0 {
		t.Errorf("*** count should be 0: %d", readinessGroup.Count())
	}
}

func TestLivenessProbe(t *testing.T) {
	t.Parallel()
	Foo := health.Check{
		ID:           ulids.MustNew().String(),
		Description:  "Foo",
		RedImpact:    "Red",
		YellowImpact: "Yellow",
	}

	checkProbe := func(t *testing.T, status health.Status, test func(t *testing.T, probe fxapp.LivenessProbe)) {
		Checker := func() (health.Status, error) {
			switch status {
			case health.Green:
				return health.Green, nil
			case health.Yellow:
				return health.Yellow, errors.New("YELLOW")
			default:
				return health.Red, errors.New("RED")
			}
		}

		var probe fxapp.LivenessProbe
		var register health.Register
		var subscribeForCheckResults health.SubscribeForCheckResults
		var gatherer prometheus.Gatherer
		app, err := fxapp.NewBuilder(fxapp.ID(ulids.MustNew()), fxapp.ReleaseID(ulids.MustNew())).
			Invoke(func() {}).
			Populate(&probe, &register, &gatherer, &subscribeForCheckResults).
			Build()

		if err != nil {
			t.Errorf("*** app failed to build: %v", err)
		}

		go app.Run()
		defer func() {
			app.Shutdown()
			<-app.Done()
		}()
		<-app.Ready()

		if err := probe(); err != nil {
			t.Errorf("*** probe should succeed, but instead failed: %v", err)
		}

		// register a health check
		healthCheckResultChan := subscribeForCheckResults(func(result health.Result) bool {
			return result.ID == Foo.ID
		})
		register(Foo, health.CheckerOpts{}, Checker)
		select {
		case <-time.After(time.Millisecond):
		case <-healthCheckResultChan.Chan():
		}

		for {
			mfs, err := gatherer.Gather()
			if err != nil {
				t.Errorf("*** failed to gather metrics: %v", err)
				return
			}
			healthCheckMetricFamily := fxapp.FindMetricFamily(mfs, func(mf *io_prometheus_client.MetricFamily) bool {
				if mf.GetName() == fxapp.HealthCheckMetricID {
					for _, metric := range mf.Metric {
						for _, labelPair := range metric.Label {
							if labelPair.GetValue() == Foo.ID {
								return true
							}
						}
					}
				}
				return false
			})

			if healthCheckMetricFamily != nil {
				break
			}
			time.Sleep(time.Millisecond)
		}

		test(t, probe)
	}

	t.Run("no health checks registered", func(t *testing.T) {
		t.Parallel()
		var probe fxapp.LivenessProbe
		_, err := fxapp.NewBuilder(fxapp.ID(ulids.MustNew()), fxapp.ReleaseID(ulids.MustNew())).
			Invoke(func() {}).
			Populate(&probe).
			DisableHTTPServer().
			Build()

		if err != nil {
			t.Errorf("*** app failed to build: %v", err)
		}

		if err := probe(); err != nil {
			t.Errorf("*** probe should succeed, but instead failed: %v", err)
		}
	})

	t.Run("green health checks registered", func(t *testing.T) {
		t.Parallel()
		checkProbe(t, health.Green, func(t *testing.T, probe fxapp.LivenessProbe) {
			if err := probe(); err != nil {
				t.Errorf("*** probe should succeed, but instead failed: %v", err)
			}
		})
	})

	// liveness probe should succeed if health checks are yellow
	t.Run("yellow health checks registered", func(t *testing.T) {
		t.Parallel()
		checkProbe(t, health.Yellow, func(t *testing.T, probe fxapp.LivenessProbe) {
			if err := probe(); err != nil {
				t.Errorf("*** probe should succeed, but instead failed: %v", err)
			}
		})
	})

	t.Run("red health checks registered", func(t *testing.T) {
		t.Parallel()
		checkProbe(t, health.Red, func(t *testing.T, probe fxapp.LivenessProbe) {
			if err := probe(); err == nil {
				t.Error("*** probe should have failed")
			} else {
				t.Log(err)
			}
		})
	})

}

func TestLivenessProbHTTPEndpoint(t *testing.T) {
	Foo := health.Check{
		ID:           ulids.MustNew().String(),
		Description:  "Foo",
		RedImpact:    "Red",
		YellowImpact: "Yellow",
	}

	livenessProbeEndpoint := fmt.Sprintf("http://:8008/%s", fxapp.LivenessProbeEvent)

	checkProbe := func(t *testing.T, status health.Status) {
		Checker := func() (health.Status, error) {
			switch status {
			case health.Green:
				return health.Green, nil
			case health.Yellow:
				return health.Yellow, errors.New("YELLOW")
			default:
				return health.Red, errors.New("RED")
			}
		}

		var probe fxapp.LivenessProbe
		var register health.Register
		var subscribeForCheckResults health.SubscribeForCheckResults
		var gatherer prometheus.Gatherer
		app, err := fxapp.NewBuilder(fxapp.ID(ulids.MustNew()), fxapp.ReleaseID(ulids.MustNew())).
			Invoke(func() {}).
			Populate(&probe, &register, &gatherer, &subscribeForCheckResults).
			Build()

		if err != nil {
			t.Errorf("*** app failed to build: %v", err)
		}

		go app.Run()
		defer func() {
			app.Shutdown()
			<-app.Done()
		}()
		<-app.Ready()

		if err := probe(); err != nil {
			t.Errorf("*** probe should succeed, but instead failed: %v", err)
		}

		// register a health check
		healthCheckResultChan := subscribeForCheckResults(func(result health.Result) bool {
			return result.ID == Foo.ID
		})
		register(Foo, health.CheckerOpts{}, Checker)
		select {
		case <-time.After(time.Millisecond):
		case <-healthCheckResultChan.Chan():
		}

		for {
			mfs, err := gatherer.Gather()
			if err != nil {
				t.Errorf("*** failed to gather metrics: %v", err)
				return
			}
			healthCheckMetricFamily := fxapp.FindMetricFamily(mfs, func(mf *io_prometheus_client.MetricFamily) bool {
				if mf.GetName() == fxapp.HealthCheckMetricID {
					for _, metric := range mf.Metric {
						for _, labelPair := range metric.Label {
							if labelPair.GetValue() == Foo.ID {
								return true
							}
						}
					}
				}
				return false
			})

			if healthCheckMetricFamily != nil {
				break
			}
			time.Sleep(time.Millisecond)
		}

		// ensure that the HTTP server is running
		retryablehttp.Get(fmt.Sprintf("http://:8008/%s", fxapp.MetricsEndpoint))

		httpResponse, err := http.Get(livenessProbeEndpoint)
		if err != nil {
			t.Errorf("*** liveness probe HTTP GET failed: %v", err)
		}
		err = probe()
		if err == nil {
			if httpResponse.StatusCode != http.StatusOK {
				t.Errorf("*** liveness probe should have returned HTTP 200: %v", httpResponse.StatusCode)
			}
		} else {
			if httpResponse.StatusCode != http.StatusServiceUnavailable {
				t.Errorf("*** liveness probe should have returned HTTP 503: %v", httpResponse.StatusCode)
			}
		}
	}

	t.Run("no health checks registered", func(t *testing.T) {
		t.Parallel()
		var probe fxapp.LivenessProbe
		_, err := fxapp.NewBuilder(fxapp.ID(ulids.MustNew()), fxapp.ReleaseID(ulids.MustNew())).
			Invoke(func() {}).
			Populate(&probe).
			Build()

		if err != nil {
			t.Errorf("*** app failed to build: %v", err)
		}

		if err := probe(); err != nil {
			t.Errorf("*** probe should succeed, but instead failed: %v", err)
		}
	})

	t.Run("green health checks registered", func(t *testing.T) {
		checkProbe(t, health.Green)
	})

	// liveness probe should succeed if health checks are yellow
	t.Run("yellow health checks registered", func(t *testing.T) {
		checkProbe(t, health.Yellow)
	})

	t.Run("red health checks registered", func(t *testing.T) {
		checkProbe(t, health.Red)
	})
}
