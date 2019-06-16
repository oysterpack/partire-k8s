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

package fx_test

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/oklog/ulid"
	"github.com/oysterpack/partire-k8s/pkg/app"
	"github.com/oysterpack/partire-k8s/pkg/app/comp"
	"github.com/oysterpack/partire-k8s/pkg/app/err"
	appfx "github.com/oysterpack/partire-k8s/pkg/app/fx"
	"github.com/oysterpack/partire-k8s/pkg/app/fx/option"
	"github.com/oysterpack/partire-k8s/pkg/app/logging"
	"github.com/oysterpack/partire-k8s/pkg/app/metric"
	"github.com/oysterpack/partire-k8s/pkg/app/ulidgen"
	"github.com/oysterpack/partire-k8s/pkg/apptest"
	"github.com/prometheus/client_golang/prometheus"
	io_prometheus_client "github.com/prometheus/client_model/go"
	"github.com/rs/zerolog"
	"go.uber.org/fx"
	"io"
	"log"
	"math/rand"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestMustNewApp(t *testing.T) {
	// reset the std logger when the test is done
	flags := log.Flags()
	defer func() {
		log.SetFlags(flags)
		log.SetOutput(os.Stderr)
	}()

	t.Run("using default settings", testNewAppWithDefaultSettings)

	t.Run("using overidden app start and stop timeouts", testNewAppWithCustomAppTimeouts)

	t.Run("using invalid app start/stop timeouts", testNewAppWithInvalidTimeouts)

	t.Run("using invalid log config", testNewAppWithInvalidLogConfig)

	t.Run("app.Desc env vars not defined", testDescNotDefinedInEnv)
}

func testNewAppWithInvalidLogConfig(t *testing.T) {
	// Given the env is initialized cleanly
	apptest.InitEnv()
	defer apptest.ClearAppEnvSettings()

	// Then the app can be built
	buildApp := func() (*appfx.App, error) {
		return appfx.NewAppBuilder().
			Options(fx.Invoke(func() {})).
			Build()
	}
	_, e := buildApp()
	if e != nil {
		t.Fatalf("*** app failed to build: %v", e)
	}

	// When the global log level env var is mis-configured
	apptest.Setenv(apptest.LogGlobalLevel, "--")
	_, e = buildApp()

	// Then the app should fail to build
	if e == nil {
		t.Fatal("*** creating the app should have failed because the app global log level was misconfigured")
	}
	t.Logf("as expected, the app failed to build: %v", e)
}

func testNewAppWithInvalidTimeouts(t *testing.T) {
	// Given the env is initialized cleanly
	apptest.InitEnv()
	defer apptest.ClearAppEnvSettings()

	// Then the app can be built
	buildApp := func() (*appfx.App, error) {
		return appfx.NewAppBuilder().
			Options(fx.Invoke(func() {})).
			Build()
	}
	_, e := buildApp()
	if e != nil {
		t.Fatalf("*** app failed to build: %v", e)
	}

	// When the start timeout env var is mis-configured
	apptest.Setenv(apptest.StartTimeout, "--")
	_, e = buildApp()
	if e == nil {
		t.Error("***creating the app should have failed because the app start timeout was misconfigured")
	}
	t.Logf("as expected, the app failed to build: %v", e)
}

func testNewAppWithCustomAppTimeouts(t *testing.T) {
	// Given the env is initialized cleanly
	apptest.InitEnv()
	defer apptest.ClearAppEnvSettings()

	// And the app start and stop timeouts env vars are set
	apptest.Setenv(apptest.StartTimeout, "30s")
	apptest.Setenv(apptest.StopTimeout, "60s")

	// When the app is created
	fxapp, e := appfx.NewAppBuilder().
		Options(fx.Invoke(func() {})).
		Build()
	if e != nil {
		t.Fatalf("*** the app failed to build: %v", e)
	}
	// Then app's start and stop timeouts will match what was specified via the env vars
	if fxapp.StartTimeout() != 30*time.Second {
		t.Error("*** StartTimeout did not match the default")
	}
	if fxapp.StopTimeout() != 60*time.Second {
		t.Error("*** StopTimeout did not match the default")
	}
	// And the app will start and stop normally
	if e := fxapp.Start(context.Background()); e != nil {
		panic(e)
	}
	defer func() {
		if e := fxapp.Stop(context.Background()); e != nil {
			t.Errorf("*** fxapp.Stop error: %v", e)
		}
	}()
}

func testNewAppWithDefaultSettings(t *testing.T) {
	// Given the env is initialized
	expectedDesc := apptest.InitEnv()
	defer apptest.ClearAppEnvSettings()

	// When the fx.App is created
	var desc app.Desc
	var instanceID app.InstanceID
	fxapp, e := appfx.NewAppBuilder().
		Options(
			fx.Populate(&desc),
			fx.Populate(&instanceID),
			fx.Invoke(logTestEvents),
		).Build()
	if e != nil {
		t.Fatalf("*** the app failed to build: %v", e)
	}

	// Then the app start and stop timeouts are set to defaults of 15 sec
	if fxapp.StartTimeout() != 15*time.Second {
		t.Error("*** StartTimeout did not match the default")
	}
	if fxapp.StopTimeout() != 15*time.Second {
		t.Error("*** StopTimeout did not match the default")
	}

	// And the app starts and stops with no errors
	if e := fxapp.Start(context.Background()); e != nil {
		t.Fatal(e)
	}
	defer func() {
		if e := fxapp.Stop(context.Background()); e != nil {
			t.Errorf("*** fxapp.Stop error: %v", e)
		}
	}()

	// And app.Desc is provided in the fx.App context
	t.Logf("app desc: %s", &desc)
	apptest.CheckDescsAreEqual(t, desc, expectedDesc)

	// And the app.InstanceID is defined
	t.Logf("app InstanceID: %s", ulid.ULID(instanceID))
	var zeroULID ulid.ULID
	if zeroULID == ulid.ULID(instanceID) {
		t.Error("*** instanceID was not initialized")
	}
}

type empty struct{}

const (
	LogTestEventLogEventName = "LogTestEvents"
	LogTestEventOnStartMsg   = "OnStart"
	LogTestEventOnStopMsg    = "OnStop"
)

func logTestEvents(logger *zerolog.Logger, lc fx.Lifecycle) {
	logger = logging.PackageLogger(logger, app.GetPackage(empty{}))
	foo := logging.MustNewEvent(LogTestEventLogEventName, zerolog.InfoLevel)

	lc.Append(fx.Hook{
		OnStart: func(_ context.Context) error {
			foo.Log(logger).Msg(LogTestEventOnStartMsg)
			return nil
		},
		OnStop: func(_ context.Context) error {
			foo.Log(logger).Msg(LogTestEventOnStopMsg)
			return nil
		},
	})

	log.Printf("logging using go std log")
}

// Feature: The app logs lifecycle events at the appropriate times.
//
// Given that the app log is captured
//
// When the app is started
// Then it logs the Start event as the first lifecycle OnStart hook
// And then after all other OnStart hooks are run, the Running event is logged
//
// When the app is stopped
// Then the Stop event is logged as the first OnStop hook
// Then the Stopped event is logged as the last OnStop hook
func TestAppLifecycleEvents(t *testing.T) {
	checkLifecycleEvents := func(t *testing.T, logFile io.Reader) {
		eventNames := make([]string, 0, 6)
		scanner := bufio.NewScanner(logFile)
		for scanner.Scan() {
			logEventJSON := scanner.Text()
			t.Log(logEventJSON)

			var logEvent apptest.LogEvent
			e := json.Unmarshal([]byte(logEventJSON), &logEvent)
			if e != nil {
				t.Fatal(e)
			}

			switch logEvent.Event {
			case appfx.Start.Name, appfx.Running.Name, appfx.Stop.Name, appfx.Stopped.Name:
				eventNames = append(eventNames, logEvent.Event)
			case LogTestEventLogEventName:
				eventNames = append(eventNames, logEvent.Message)
			}
		}

		expectedEventNamess := []string{
			appfx.Start.Name,
			LogTestEventOnStartMsg,
			appfx.Running.Name,
			appfx.Stop.Name,
			LogTestEventOnStopMsg,
			appfx.Stopped.Name,
		}

		t.Log(eventNames)
		t.Log(expectedEventNamess)

		if len(expectedEventNamess) != len(eventNames) {
			t.Fatalf("*** the expected number of events did not match: %v != %v", len(expectedEventNamess), len(eventNames))
		}
		// the order of events should match
		for i, event := range eventNames {
			if expectedEventNamess[i] != event {
				t.Errorf("*** event did not match: %v != %v", expectedEventNamess[i], event)
			}
		}
	}

	logEventsBuf := new(bytes.Buffer)
	defer checkLifecycleEvents(t, logEventsBuf)

	// Given that the app log is captured
	apptest.InitEnv()
	fxapp, e := appfx.NewAppBuilder().
		LogWriter(logEventsBuf).
		Options(fx.Invoke(logTestEvents)).
		Build()
	if e != nil {
		t.Fatalf("*** app failed to build: %v", e)
	}

	// When the app is started
	if e := fxapp.Start(context.Background()); e != nil {
		t.Fatal(e)
	}
	// Then it logs the Start event as the first lifecycle OnStart hook
	// And then after all other OnStart hooks are run, the Running event is logged

	// When the app is stopped
	if e := fxapp.Stop(context.Background()); e != nil {
		t.Errorf("fxapp.Stop error: %v", e)
	}
	// Then the Stop event is logged as the first OnStop hook
	// Then the Stopped event is logged as the last OnStop hook
}

var (
	TestErr  = err.MustNewDesc("01DCF9FYQMKKM6MA3RAYZWEVTR", "TestError", "test error")
	TestErr1 = err.New(TestErr, "01DC9JRXD98HS9BEXJ1MBXWWM8")
)

// Feature: Errors produced by app functions that are invoked by fx will be logged automatically
//
// Scenario: invoked func return an error of type *err.Instance
//
// Given that the app log is captured
// When the app is started
// Then the app will fail to start because the invoked test function fails
// And the error will be logged
func TestAppInvokeErrorHandling(t *testing.T) {
	checkErrorEvents := func(t *testing.T, logFile io.Reader) {
		scanner := bufio.NewScanner(logFile)
		errorLogged := false
		for scanner.Scan() {
			logEventJSON := scanner.Text()
			t.Log(logEventJSON)

			var logEvent apptest.LogEvent
			e := json.Unmarshal([]byte(logEventJSON), &logEvent)
			if e != nil {
				t.Fatal(e)
			}

			if logEvent.Level == zerolog.ErrorLevel.String() {
				if logEvent.Error.ID == TestErr.ID.String() {
					errorLogged = true
				}
			}
		}

		if !errorLogged {
			t.Error("*** error was not logged")
		}
	}

	logEventsBuf := new(bytes.Buffer)
	defer checkErrorEvents(t, logEventsBuf)

	// When the app is created with an invoke function that fails
	apptest.InitEnv()
	_, e := appfx.NewAppBuilder().
		LogWriter(logEventsBuf).
		Options(fx.Invoke(func() error {
			t.Log("test func has been invoked ...")
			return TestErr1.New()
		})).
		Build()
	if e == nil {
		t.Fatal("app should have failed to be created because the invoked func returned an error")
	}
	t.Log(e)
}

// Feature: Errors produced by app functions that are invoked by fx will be logged automatically
//
// Scenario: invoked func return an error that is a non-standard type, i.e. not of type *err.Instance
//
// Given that the app log is captured
// When the app is started
// Then the app will fail to start because the invoked test function fails
// And the error will be logged
func TestAppInvokeErrorHandlingForNonStandardError(t *testing.T) {
	checkErrorEvents := func(t *testing.T, logFile io.Reader) {
		scanner := bufio.NewScanner(logFile)
		errorLogged := false
		for scanner.Scan() {
			logEventJSON := scanner.Text()
			t.Log(logEventJSON)

			var logEvent apptest.LogEvent
			e := json.Unmarshal([]byte(logEventJSON), &logEvent)
			if e != nil {
				t.Fatal(e)
			}

			if logEvent.Level == zerolog.ErrorLevel.String() {
				if logEvent.Error.ID == appfx.InvokeErrClass.ID.String() {
					errorLogged = true
				}
			}
		}

		if !errorLogged {
			t.Error("*** error was not logged")
		}
	}

	logEventsBuf := new(bytes.Buffer)
	defer checkErrorEvents(t, logEventsBuf)

	// When the app is created with a function that fails and returns an error when invoked
	apptest.InitEnv()
	_, e := appfx.NewAppBuilder().
		LogWriter(logEventsBuf).
		Options(fx.Invoke(func() error {
			t.Log("test func has been invoked ...")
			return errors.New("non standard error")
		})).
		Build()
	if e == nil {
		t.Fatal("*** app should have failed to be created because the invoked func returned an error")
	}
	t.Log(e)
}

// Feature: Errors produced by app functions that are invoked by fx will be logged automatically
//
// Scenario: hook OnStart handler results in an error of type *err.Instance
//
// Given that the app log is captured
// When the app is started
// Then the app will fail to start because a hook OnStart function returns an error
// And the error will be logged
func TestAppHookOnStartErrorHandling(t *testing.T) {
	checkErrorEvents := func(t *testing.T, logFile io.Reader) {
		scanner := bufio.NewScanner(logFile)
		errorLogged := false
		for scanner.Scan() {
			logEventJSON := scanner.Text()
			t.Log(logEventJSON)

			var logEvent apptest.LogEvent
			e := json.Unmarshal([]byte(logEventJSON), &logEvent)
			if e != nil {
				t.Fatal(e)
			}

			if logEvent.Level == zerolog.ErrorLevel.String() {
				if logEvent.Error.ID == appfx.AppStartErrClass.ID.String() {
					errorLogged = true
				}
			}
		}

		if !errorLogged {
			t.Error("error was not logged")
		}
	}

	logEventsBuf := new(bytes.Buffer)
	defer checkErrorEvents(t, logEventsBuf)

	// When the app is created with a function that fails and returns an error when invoked
	apptest.InitEnv()
	fxapp, e := appfx.NewAppBuilder().
		LogWriter(logEventsBuf).
		Options(fx.Invoke(func(lc fx.Lifecycle) error {
			t.Log("test func has been invoked ...")
			lc.Append(fx.Hook{
				OnStart: func(context.Context) error {
					t.Log("OnStart is about to fail ...")
					return TestErr1.New()
				},
			})
			return nil
		})).
		Build()

	if e != nil {
		t.Fatalf("*** app failed to build: %v", e)
	}

	e = fxapp.Run()
	if e == nil {
		t.Fatal("*** expected the app to fail to start up")
	}
	t.Logf("as expected, app failed to start: %v", e)
}

// Feature: Errors produced by app functions that are invoked by fx will be logged automatically
//
// Scenario: hook OnStart handler results in an error of type *err.Instance
//
// Given that the app log is captured
// When the app is signalled to stop
// Then the app will fail to stop cleanly because a hook OnStop function returns an error
// And the error will be logged
func TestAppHookOnStopErrorHandling(t *testing.T) {
	checkErrorEvents := func(t *testing.T, logFile io.Reader) {
		scanner := bufio.NewScanner(logFile)
		errorLogged := false
		for scanner.Scan() {
			logEventJSON := scanner.Text()
			t.Log(logEventJSON)

			var logEvent apptest.LogEvent
			e := json.Unmarshal([]byte(logEventJSON), &logEvent)
			if e != nil {
				t.Fatal(e)
			}

			if logEvent.Level == zerolog.ErrorLevel.String() {
				if logEvent.Error.ID == appfx.AppStopErrClass.ID.String() {
					errorLogged = true
				}
			}
		}

		if !errorLogged {
			t.Error("Error was not logged")
		}
	}

	logEventsBuf := new(bytes.Buffer)
	defer checkErrorEvents(t, logEventsBuf)

	apptest.InitEnv()
	fxapp, e := appfx.NewAppBuilder().
		LogWriter(logEventsBuf).
		Options(
			// When the app is configured with an OnStop hook that will fail
			fx.Invoke(func(lc fx.Lifecycle) error {
				t.Log("test func has been invoked ...")
				lc.Append(fx.Hook{
					OnStop: func(context.Context) error {
						t.Log("OnStop is about to fail ...")
						return TestErr1.New()
					},
				})
				return nil
			}),
			// And the app will stop itself right after it starts
			fx.Invoke(func(lc fx.Lifecycle, shutdowner fx.Shutdowner) {
				lc.Append(fx.Hook{
					OnStart: func(context.Context) error {
						fmt.Println("App will be shutdown ...")
						if e := shutdowner.Shutdown(); e != nil {
							t.Fatalf("shutdowner.Shutdown() failed: %v", e)
						}
						fmt.Println("App has been signalled to shutdown ...")
						return nil
					},
				})

			}),
		).
		Build()

	if e != nil {
		t.Fatalf("*** app failed to build: %v", e)
	}

	errChan := make(chan error)
	go func() {
		e := fxapp.Run()
		if e != nil {
			errChan <- e
		}
		close(errChan)
	}()
	// wait for the app to stop
	e = <-errChan
	if e == nil {
		t.Fatal("*** expected the app to fail to start up")
	}
	t.Logf("as expected, app failed to start: %v", e)
}

// Feature: App will run until it is signalled to shutdown
//
// Scenario: the app will signal itself to shutdown as soon as it starts up
//
// When the app starts, it shuts itself down
// Then the app shuts down cleanly
func TestApp_Run(t *testing.T) {
	apptest.InitEnv()
	fxapp, e := appfx.NewAppBuilder().
		Options(
			// And the app will stop itself right after it starts
			fx.Invoke(func(lc fx.Lifecycle, shutdowner fx.Shutdowner) {
				lc.Append(fx.Hook{
					OnStart: func(context.Context) error {
						if e := shutdowner.Shutdown(); e != nil {
							t.Fatalf("shutdowner.Shutdown() failed: %v", e)
						}
						return nil
					},
				})

			}),
		).
		Build()

	if e != nil {
		t.Fatalf("*** app failed to build: %v", e)
	}

	errChan := make(chan error)
	go func() {
		e := fxapp.Run()
		if e != nil {
			errChan <- e
		}
		close(errChan)
	}()
	// wait for the app to stop
	if e := <-errChan; e != nil {
		t.Errorf("App failed to run: %v", e)
	}
}

func TestErrRegistryIsProvided(t *testing.T) {
	apptest.InitEnv()
	fxapp, e := appfx.NewAppBuilder().
		Options(fx.Invoke(func(errRegistry *err.Registry, logger *zerolog.Logger, shutdowner fx.Shutdowner, lc fx.Lifecycle) {
			logger.Info().Msgf("registered errors: %v", errRegistry.Errs())

			// all of the standard app errors should be registered
			errs := []*err.Err{appfx.InvokeErr, appfx.AppStartErr, appfx.AppStopErr}
			for _, e := range errs {
				if !errRegistry.Registered(e.SrcID) {
					t.Errorf("error is not registered: %v", e)
				}
			}

			// when the app starts, shut it down
			lc.Append(fx.Hook{
				OnStart: func(context.Context) error {
					if e := shutdowner.Shutdown(); e != nil {
						t.Fatalf("shutdowner.Shutdown() failed: %v", e)
					}
					return nil
				},
			})
		})).
		Build()

	if e != nil {
		t.Fatalf("*** app failed to build: %v", e)
	}

	errChan := make(chan error)
	go func() {
		e := fxapp.Run()
		if e != nil {
			errChan <- e
		}
		close(errChan)
	}()
	// wait for the app to stop
	if e := <-errChan; e != nil {
		t.Errorf("App failed to run: %v", e)
	}

}

func TestEventRegistryIsProvided(t *testing.T) {
	apptest.InitEnv()
	fxapp, e := appfx.NewAppBuilder().
		Options(fx.Invoke(func(registry *logging.EventRegistry, logger *zerolog.Logger, shutdowner fx.Shutdowner, lc fx.Lifecycle) {
			logger.Info().Msgf("registered events: %v", registry.Events())

			// all of the standard app events should be registered
			events := []*logging.Event{appfx.Start, appfx.Running, appfx.Stop, appfx.Stopped, appfx.StopSignal, appfx.CompRegistered}
			for _, e := range events {
				if !registry.Registered(e) {
					t.Errorf("event is not registered: %v", e)
				}
			}

			// when the app starts, shut it down
			lc.Append(fx.Hook{
				OnStart: func(context.Context) error {
					if e := shutdowner.Shutdown(); e != nil {
						t.Fatalf("shutdowner.Shutdown() failed: %v", e)
					}
					return nil
				},
			})
		})).
		Build()
	if e != nil {
		t.Fatalf("*** app failed to build: %v", e)
	}

	errChan := make(chan error)
	go func() {
		e := fxapp.Run()
		if e != nil {
			errChan <- e
		}
		close(errChan)
	}()
	// wait for the app to stop
	if e := <-errChan; e != nil {
		t.Errorf("*** app failed to run: %v", e)
	}
}

func testDescNotDefinedInEnv(t *testing.T) {
	apptest.ClearAppEnvSettings()
	_, e := appfx.NewAppBuilder().
		Options(fx.Invoke(func() {})).
		Build()
	if e == nil {
		t.Fatal("*** loading Desc should have failed because required app descriptor fields are missing")
	} else {
		t.Logf("as expected, the app failed to build: %v", e)
	}
}

type RandomNumberGenerator func() int
type ProvideRandomNumberGenerator func() RandomNumberGenerator

type Greeter func() string
type ProvideGreeter func() Greeter

var (
	ProvideRandomNumberGeneratorOption = option.NewDesc(option.Provide, reflect.TypeOf(ProvideRandomNumberGenerator(nil)))
	ProvideGreeterOption               = option.NewDesc(option.Provide, reflect.TypeOf(ProvideGreeter(nil)))

	FooComp = comp.MustNewDesc(
		comp.ID("01DCYBFQBQVXG8PZ758AM9JJCD"),
		comp.Name("foo"),
		comp.Version("0.0.1"),
		app.Package("github.com/oysterpack/partire-k8s/pkg/foo"),
		ProvideRandomNumberGeneratorOption,
	)

	BarComp = comp.MustNewDesc(
		comp.ID("01DCYD1X7FMSRJMVMA8RWK7HMB"),
		comp.Name("bar"),
		comp.Version("0.0.1"),
		app.Package("github.com/oysterpack/partire-k8s/pkg/bar"),
		ProvideGreeterOption,
	)
)

func TestCompRegistryIsProvided(t *testing.T) {
	// reset the std logger when the test is done because the app will configure the std logger to use zerolog
	flags := log.Flags()
	defer func() {
		log.SetFlags(flags)
		log.SetOutput(os.Stderr)
	}()

	t.Run("with 2 comps injectd", testCompRegistryWithCompsRegistered)

	t.Run("with 0 comps injected", testEmptyComponentRegistry)

	// error scenarios
	t.Run("with duplicate comps injected", testComponentRegistryWithDuplicateComps)
	t.Run("with comps that have conflicting errors registered", testCompRegistryWithCompsContainingConflictingErrors)
}

func testComponentRegistryWithDuplicateComps(t *testing.T) {
	// Given 2 components conflict because they have the same ID
	FooComp := comp.MustNewDesc(
		comp.ID("01DCYBFQBQVXG8PZ758AM9JJCD"),
		comp.Name("foo"),
		comp.Version("0.0.1"),
		app.Package("github.com/oysterpack/partire-k8s/pkg/foo"),
		ProvideRandomNumberGeneratorOption,
	)
	BarComp := comp.MustNewDesc(
		comp.ID(FooComp.ID.String()), // dup comp.ID will cause comp registration to fail
		comp.Name("bar"),
		comp.Version("0.0.1"),
		app.Package("github.com/oysterpack/partire-k8s/pkg/bar"),
		ProvideGreeterOption,
	)

	foo := FooComp.MustNewComp(
		ProvideRandomNumberGeneratorOption.NewOption(func() RandomNumberGenerator {
			return rand.Int
		}),
	)
	bar := BarComp.MustNewComp(ProvideGreeterOption.NewOption(func() Greeter {
		return func() string { return "greetings" }
	}))

	apptest.InitEnv()
	_, e := appfx.NewAppBuilder().
		Options(foo.FxOptions(), bar.FxOptions(), fx.Invoke(func(r *comp.Registry, l *zerolog.Logger) {
			// triggers the comp.Registry to be constructed, which should then trigger the error when comps register
			l.Info().Msgf("%v", r.Comps())
		})).
		Build()
	if e == nil {
		t.Fatal("*** app should have failed to be created because the invoked func returned an error")
	}
	t.Log(e)
}

// app components are optional, i.e., in order for an app to run, it requires at least 1 fx.Option
//
// When an app is created with no explicit components, but has options defined
// Then the app starts up fine
func testEmptyComponentRegistry(t *testing.T) {
	apptest.InitEnv()
	// When the app is created with no components
	var compRegistry *comp.Registry
	fxapp, e := appfx.NewAppBuilder().
		Options(fx.Populate(&compRegistry)).
		Build()
	if e != nil {
		t.Fatalf("*** app failed to build: %v", e)
	}
	// Then the app starts up just fine
	if e := fxapp.Start(context.Background()); e != nil {
		t.Errorf("*** failed to start app: %v", e)
	}
	if e := fxapp.Stop(context.Background()); e != nil {
		t.Errorf("*** failed to start app: %v", e)
	}
	t.Logf("registered components: %v", compRegistry.Comps())
}

// When components are registered
// And components expose errors and events
// Then events are logged when the components are registered
// And the component's events are registered with the app event registry
// And the component's errors are registered with the app error registry
func testCompRegistryWithCompsRegistered(t *testing.T) {
	apptest.InitEnv()
	defer apptest.ClearAppEnvSettings()

	event1 := logging.MustNewEvent(ulidgen.MustNew().String(), zerolog.InfoLevel)
	event2 := logging.MustNewEvent(ulidgen.MustNew().String(), zerolog.InfoLevel)

	errDesc1 := err.MustNewDesc(ulidgen.MustNew().String(), ulidgen.MustNew().String(), "errDesc1")
	err1 := err.New(errDesc1, ulidgen.MustNew().String())
	err2 := err.New(errDesc1, ulidgen.MustNew().String())

	// Given 2 components
	foo := FooComp.MustNewComp(
		ProvideRandomNumberGeneratorOption.NewOption(func() RandomNumberGenerator {
			return rand.Int
		}),
	)
	// And the component exposes events
	foo.EventRegistry.Register(event1, event2)
	// And the component exposes errors
	if e := foo.ErrorRegistry.Register(err1, err2); e != nil {
		t.Fatal(e)
	}
	bar := BarComp.MustNewComp(ProvideGreeterOption.NewOption(func() Greeter {
		return func() string { return "greetings" }
	}))

	// When the app is created with the 2 components
	var compRegistry *comp.Registry
	var eventRegistry *logging.EventRegistry
	var errRegistry *err.Registry

	logEventsBuf := new(bytes.Buffer)
	fxapp, e := appfx.NewAppBuilder().
		LogWriter(logEventsBuf).
		Options(
			foo.FxOptions(),
			bar.FxOptions(),
			fx.Populate(&compRegistry),
			fx.Populate(&eventRegistry),
			fx.Populate(&errRegistry),
		).
		Build()
	if e != nil {
		t.Fatalf("*** app failed to build: %v", e)
	}
	if e := fxapp.Start(context.Background()); e != nil {
		t.Errorf("failed to start app: %v", e)
	}

	// Then the components are registered
	for _, c := range []*comp.Comp{foo, bar} {
		if compRegistry.FindByID(c.ID) == nil {
			t.Errorf("*** component was not found in the registry: %v", c)
		}
	}
	// And the component events were registered
	for _, event := range []*logging.Event{event1, event2} {
		if !eventRegistry.Registered(event) {
			t.Errorf("*** event is not registered: %v", event)
		}
	}
	// And the component errors were registered
	for _, e := range []*err.Err{err1, err2} {
		if !errRegistry.Registered(e.SrcID) {
			t.Errorf("*** error is not registered: %v", e)
		}
	}

	if e := fxapp.Stop(context.Background()); e != nil {
		t.Errorf("*** failed to start app: %v", e)
	}

	// check that the component registration events were logged
	compRegisteredEvents := apptest.CollectLogEvents(t, logEventsBuf, func(logEvent *apptest.LogEvent) bool {
		return logEvent.Event == appfx.CompRegistered.Name
	})

	if len(compRegisteredEvents) == 0 {
		t.Errorf("no %q events were logged", appfx.CompRegistered.Name)
	} else {
		t.Logf("len(compRegisteredEvents) = %d", len(compRegisteredEvents))
		checkCompRegistrationEvents(t, []*comp.Comp{foo, bar}, compRegisteredEvents)
	}
}

// checks that all of the expected component information is logged
func checkCompRegistrationEvents(t *testing.T, comps []*comp.Comp, events []*apptest.LogEvent) {
	findEventByCompID := func(events []*apptest.LogEvent, c *comp.Comp) *apptest.LogEvent {
		for _, event := range events {
			if c.ID.String() == event.Comp.ID {
				return event
			}
		}
		return nil
	}

	checkCompOptionsWereLogged := func(c *comp.Comp, event *apptest.LogEvent) {
		for _, opt := range c.Options {
			for _, eventOpt := range event.Comp.Options {
				if !strings.Contains(eventOpt, opt.FuncType.String()) {
					t.Error("option.Desc.FuncType did not match")
				}
				if !strings.Contains(eventOpt, opt.Type.String()) {
					t.Error("option.Desc.Type did not match")
				}
			}
		}
	}

	for _, c := range comps {
		event := findEventByCompID(events, c)
		if event == nil {
			t.Errorf("event was not logged for: %v", c.ID)
			continue
		}
		t.Logf("checking %v against %v", c, event.Comp)
		if event.Comp.Version != c.Version.String() {
			t.Error("comp version did not match")
		}
		if len(c.Options) != len(event.Comp.Options) {
			t.Error("number of options does not match")
			continue
		}
		checkCompOptionsWereLogged(c, event)
	}
}

// When components are registered that conflict
// Then the app construction will fail
func testCompRegistryWithCompsContainingConflictingErrors(t *testing.T) {
	apptest.InitEnv()

	errDesc1 := err.MustNewDesc(ulidgen.MustNew().String(), ulidgen.MustNew().String(), "errDesc1")
	errDesc2 := err.MustNewDesc(ulidgen.MustNew().String(), ulidgen.MustNew().String(), "errDesc2")

	err1 := err.New(errDesc1, ulidgen.MustNew().String())
	err2 := err.New(errDesc2, err1.SrcID.String()) // will fail error registration

	// Given 2 components
	foo := FooComp.MustNewComp(
		ProvideRandomNumberGeneratorOption.NewOption(func() RandomNumberGenerator {
			return rand.Int
		}),
	)
	bar := BarComp.MustNewComp(ProvideGreeterOption.NewOption(func() Greeter {
		return func() string { return "greetings" }
	}))

	// And the component exposes errors, but they conflict
	if e := foo.ErrorRegistry.Register(err1); e != nil {
		t.Fatal(e)
	}
	if e := bar.ErrorRegistry.Register(err2); e != nil {
		t.Fatal(e)
	}

	// When the app is created
	// Then it will fail
	_, e := appfx.NewAppBuilder().
		Comps(foo, bar).
		Build()

	if e == nil {
		t.Fatal("the app should have failed to be created because the comp error registration should have failed")
	}
	t.Log(e)
	errInstance := e.(*err.Instance)
	if errInstance.SrcID != err.RegistryConflictErr.SrcID {
		t.Errorf("unexpected error: %v : %v", errInstance.SrcID, errInstance)
	}

}

func TestFxAppWithStructAndCompatiblyInterfaceInjections(t *testing.T) {

	fxapp := fx.New(
		fx.Provide(func() (*prometheus.Registry, prometheus.Registerer) {
			registry := prometheus.NewRegistry()
			return registry, registry
		}),
		fx.Invoke(func(registry prometheus.Registerer) {
			registry.MustRegister(prometheus.NewCounter(prometheus.CounterOpts{
				Name: "counter2",
			}))
			t.Log("prometheus.Registerer : counter2")
		}),
		fx.Invoke(func(registry *prometheus.Registry, lc fx.Lifecycle) {
			registry.MustRegister(prometheus.NewCounter(prometheus.CounterOpts{
				Name: "counter1",
			}))
			t.Log("*prometheus.Registry : counter1")
			lc.Append(fx.Hook{
				OnStart: func(i context.Context) error {
					metrics, e := registry.Gather()
					if e != nil {
						return e
					}
					t.Logf("registry: %v", metrics)
					return nil
				},
			})
		}),
	)

	e := fxapp.Start(context.Background())
	if e != nil {
		t.Fatalf("*** app failed to start: %v", e)
	}

}

func TestPrometheusRegistryIsProvided(t *testing.T) {
	appDesc := apptest.InitEnv()

	// When the app is created
	var metricRegistry prometheus.Gatherer
	var metricRegisterer prometheus.Registerer
	var instanceID app.InstanceID
	_, e := appfx.NewAppBuilder().
		Options(fx.Populate(&metricRegistry, &metricRegisterer, &instanceID)).
		Build()

	if e != nil {
		t.Fatalf("*** app failed to build: %v", e)
	}

	// Then a metric registry will be provided
	if metricRegistry == nil {
		t.Error("*** *prometheus.Registry was not provided")
	}
	// And a metric registerer will be provided
	if metricRegisterer == nil {
		t.Error("*** prometheus.Registerer was not provided")
	}

	metrics, e := metricRegistry.Gather()
	if e != nil {
		t.Fatalf("metrics failed to be gathereed: %v", e)
	}
	logMetrics := func(metrics []*io_prometheus_client.MetricFamily) {
		for _, mf := range metrics {
			t.Log(mf)
		}
	}
	logMetrics(metrics)

	// And all metrics have the standard app labels
	{
		appMetrics := metric.FindMetricFamilies(metrics, func(mf *io_prometheus_client.MetricFamily) bool {
			for _, m := range mf.Metric {
				labelMatchCount := 0
				for _, labelPair := range m.Label {
					if *labelPair.Name == metric.AppID.String() && *labelPair.Value == appDesc.ID.String() {
						labelMatchCount++
					}
					if *labelPair.Name == metric.AppReleaseID.String() && *labelPair.Value == appDesc.ReleaseID.String() {
						labelMatchCount++
					}
					if *labelPair.Name == metric.AppInstanceID.String() && *labelPair.Value == instanceID.String() {
						labelMatchCount++
					}
					if labelMatchCount == 3 {
						return true
					}
				}
			}
			return false
		})
		if len(appMetrics) != len(metrics) {
			t.Errorf("*** Not all metrics have app labels: %v != %v", len(appMetrics), len(metrics))
		}
	}

	// And go metrics are collected
	{
		metrics := metric.FindMetricFamilies(metrics, func(mf *io_prometheus_client.MetricFamily) bool {
			return strings.HasPrefix(*mf.Name, "go_")
		})
		if len(metrics) == 0 {
			t.Errorf("*** prometheus go collector is not registered")
		} else {
			t.Log("--- go metrics ---")
			logMetrics(metrics)
			t.Log("=== go metrics ===")
		}
	}

	// And process metrics are collected
	{
		metrics := metric.FindMetricFamilies(metrics, func(mf *io_prometheus_client.MetricFamily) bool {
			return strings.HasPrefix(*mf.Name, "process_")
		})
		if len(metrics) == 0 {
			t.Errorf("*** prometheus process metrics collector is not registered")
		} else {
			t.Log("--- process metrics ---")
			logMetrics(metrics)
			t.Log("=== process metrics ===")
		}
	}
}
