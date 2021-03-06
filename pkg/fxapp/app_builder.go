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
	"bytes"
	"context"
	"errors"
	"fmt"
	"github.com/oklog/ulid"
	"github.com/oysterpack/andiamo/pkg/eventlog"
	"github.com/oysterpack/andiamo/pkg/fx/health"
	"github.com/oysterpack/andiamo/pkg/ulids"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/rs/zerolog"
	"go.uber.org/fx"
	"go.uber.org/multierr"
	"io"
	"log"
	"os"
	"reflect"
	"time"
)

// Builder is used to construct a new App instance.
//
//
type Builder interface {
	// Provide is used to provide dependency injection
	Provide(constructors ...interface{}) Builder
	// Invoke is used to register application functions, which will be invoked to to initialize the app.
	// The functions are invoked in the order that they are registered.
	Invoke(funcs ...interface{}) Builder

	SetStartTimeout(timeout time.Duration) Builder
	SetStopTimeout(timeout time.Duration) Builder

	// LogWriter is used as the zerolog writer.
	//
	// By default, stderr is used.
	LogWriter(w io.Writer) Builder
	LogLevel(level LogLevel) Builder

	// Error handlers
	HandleInvokeError(errorHandlers ...func(error)) Builder
	HandleStartupError(errorHandlers ...func(error)) Builder
	HandleShutdownError(errorHandlers ...func(error)) Builder
	// HandleError will handle any app error, i.e., app function invoke errors, app startup errors, and app shutdown errors.
	HandleError(errorHandlers ...func(error)) Builder

	// Populate sets targets with values from the dependency injection container during application initialization.
	// All targets must be pointers to the values that must be populated.
	// Pointers to structs that embed fx.In are supported, which can be used to populate multiple values in a struct.
	//
	// NOTE: this is useful for unit testing
	Populate(targets ...interface{}) Builder

	// DisableHTTPServer disables the HTTP server
	//
	// Uses cases for disabling the HTTP server:
	//  - when using the App for running tests the HTTP server can be disabled to reduce overhead. It also enables tests
	//    to be run in parallel
	//  - for CLI based apps
	DisableHTTPServer() Builder

	Build() (App, error)
}

// NewBuilder constructs a new Builder
func NewBuilder(id ID, releaseID ReleaseID) Builder {
	return &builder{
		instanceID: InstanceID(ulids.MustNew()),
		id:         id,
		releaseID:  releaseID,

		startTimeout: fx.DefaultTimeout,
		stopTimeout:  fx.DefaultTimeout,

		globalLogLevel: zerolog.InfoLevel,
		logWriter:      os.Stderr,
	}
}

type builder struct {
	instanceID InstanceID
	id         ID
	releaseID  ReleaseID

	startTimeout time.Duration
	stopTimeout  time.Duration

	constructors    []interface{}
	funcs           []interface{}
	populateTargets []interface{}

	logWriter      io.Writer
	globalLogLevel zerolog.Level

	invokeErrorHandlers, startErrorHandlers, stopErrorHandlers []func(error)

	disableHTTPServer bool
}

func (b *builder) String() string {
	types := func(objs []interface{}) string {
		if len(objs) == 0 {
			return "[]"
		}
		s := new(bytes.Buffer)
		s.WriteString("[")
		s.WriteString(reflect.TypeOf(objs[0]).String())
		for i := 1; i < len(objs); i++ {
			s.WriteString("|")
			s.WriteString(reflect.TypeOf(objs[i]).String())
		}

		s.WriteString("]")
		return s.String()
	}

	return fmt.Sprintf("Builder{ID: %v, ReleaseID: %v, StartTimeout: %s, StopTimeout: %s, Provide: %s, Invoke: %s, Populate: %s, InvokeErrHandlerCount: %d, StartErrHandlerCount: %d}",
		ulid.ULID(b.id),
		ulid.ULID(b.releaseID),
		b.startTimeout,
		b.startTimeout,
		types(b.constructors),
		types(b.funcs),
		types(b.populateTargets),
		len(b.invokeErrorHandlers),
		len(b.startErrorHandlers),
	)
}

// New tries to construct and initialize a new App instance.
// All of the app's functions are run as part of the app initialization phase.
func (b *builder) Build() (App, error) {
	if err := b.validate(); err != nil {
		return nil, err
	}

	var shutdowner fx.Shutdowner
	var logger *zerolog.Logger
	var readinessWaitGroup ReadinessWaitGroup
	var dotGraph fx.DotGraph
	b.populateTargets = append(b.populateTargets, &shutdowner, &logger, &readinessWaitGroup, &dotGraph)
	app := &app{
		instanceID:   b.instanceID,
		id:           b.id,
		releaseID:    b.releaseID,
		constructors: b.constructors,
		funcs:        b.funcs,

		startErrorHandlers: b.startErrorHandlers,
		stopErrorHandlers:  b.stopErrorHandlers,

		starting: make(chan struct{}),
		started:  make(chan struct{}),
		stopping: make(chan os.Signal, 1),
		stopped:  make(chan os.Signal, 1),

		App: fx.New(
			fx.StartTimeout(b.startTimeout),
			fx.StopTimeout(b.stopTimeout),
			fx.Options(b.options()...),
		),

		Shutdowner: shutdowner,
	}
	app.startErrorHandlers = append(app.startErrorHandlers, func(e error) {
		logEvent := eventlog.NewLogger(StartFailedEvent, logger, zerolog.ErrorLevel)
		logEvent(eventlog.NewError(e), "app start failed")
	})
	app.stopErrorHandlers = append(app.stopErrorHandlers, func(e error) {
		logEvent := eventlog.NewLogger(StopFailedEvent, logger, zerolog.ErrorLevel)
		logEvent(eventlog.NewError(e), "app stop failed")
	})

	if err := app.Err(); err != nil {
		return nil, err
	}
	app.logger = logger
	app.readiness = readinessWaitGroup
	app.logAppInitialized(dotGraph)
	return app, nil
}

func (b *builder) validate() error {
	if len(b.funcs) == 0 {
		return errors.New("at least 1 functional option is required")
	}
	return nil
}

// This is the key method used to compose the application options
func (b *builder) options() []fx.Option {
	logger := b.initZerolog()

	compOptions := make([]fx.Option, 0, len(b.invokeErrorHandlers)+9)
	compOptions = append(compOptions, fx.Provide(
		func() (ID, ReleaseID, InstanceID, *zerolog.Logger) { return b.id, b.releaseID, b.instanceID, logger },

		providePrometheusMetricsSupport,
		newPrometheusHTTPHandler,

		func() ReadinessWaitGroup { return NewReadinessWaitgroup(1) },
		readinessProbeHTTPHandler,

		livenessProbe,
		livenessProbeHTTPHandler,
	))
	compOptions = append(compOptions, health.Module(health.DefaultOpts()))
	compOptions = append(compOptions, fx.Provide(b.constructors...))
	compOptions = append(compOptions, fx.Invoke(
		handleHealthCheckRegistrations,
		logHealthCheckResults,
	))
	compOptions = append(compOptions, fx.Invoke(b.funcs...))
	compOptions = append(compOptions, fx.Invoke(healthCheckReadiness))

	if !b.disableHTTPServer {
		compOptions = append(compOptions, fx.Invoke(runHTTPServer))
	}
	compOptions = append(compOptions, fx.Populate(b.populateTargets...))
	// configure fx logger
	compOptions = append(compOptions, fx.Logger(newFxLogger(logger)))
	// register error handlers
	{
		for _, f := range b.invokeErrorHandlers {
			compOptions = append(compOptions, fx.ErrorHook(errorHandler(f)))
		}
		compOptions = append(compOptions, fx.ErrorHook(errorHandler(func(err error) {
			logEvent := eventlog.NewLogger(InitFailedEvent, logger, zerolog.ErrorLevel)
			logEvent(eventlog.NewError(err), "app init failed")
		})))
	}

	return compOptions
}

// used to implement the fx.ErrorHandler interface
type errorHandler func(err error)

func (f errorHandler) HandleError(err error) {
	f(err)
}

func providePrometheusMetricsSupport(id ID, releaseID ReleaseID, instanceID InstanceID) (prometheus.Gatherer, prometheus.Registerer) {
	registry := prometheus.NewRegistry()
	regsisterer := prometheus.WrapRegistererWith(
		prometheus.Labels{
			AppIDLabel:         ulid.ULID(id).String(),
			AppReleaseIDLabel:  ulid.ULID(releaseID).String(),
			AppInstanceIDLabel: ulid.ULID(instanceID).String(),
		},
		registry,
	)
	regsisterer.MustRegister(prometheus.NewGoCollector())

	return registry, regsisterer
}

// - registers a lifecycle hook that waits until all health checks are run on app start up
//   - the app is not ready to service requests until all health checks have been run and passed with a Green status
//   - if any health checks fail to run on start up then the app will fail to start up
func healthCheckReadiness(registeredChecks health.RegisteredChecks, checkResults health.CheckResults, wg ReadinessWaitGroup, lc fx.Lifecycle) {
	wg.Add(1)
	lc.Append(fx.Hook{
		OnStart: func(ctx context.Context) error {
			defer wg.Done()

			var err error
			for _, check := range <-registeredChecks() {
				if result := check.Checker(); result.Status != health.Green {
					err = multierr.Combine(err, fmt.Errorf("health check failed: %s", check.ID), result.Err)
				}
			}
			if err != nil {
				return err
			}

			return nil
		},
	})
}

// - log health checks as they are registered
// - register health check gauge
func handleHealthCheckRegistrations(subscribeForRegisteredChecks health.SubscribeForRegisteredChecks, subscribeForCheckResults health.SubscribeForCheckResults, checkResults health.CheckResults, metricRegisterer prometheus.Registerer, lc fx.Lifecycle, logger *zerolog.Logger) {
	done := make(chan struct{})
	logHealthCheckRegistered := eventlog.NewLogger(HealthCheckRegisteredEvent, logger, zerolog.NoLevel)
	logHealthCheckGaugeRegistrationError := eventlog.NewLogger(HealthCheckGaugeRegistrationErrorEvent, logger, zerolog.ErrorLevel)
	healthCheckRegistered := subscribeForRegisteredChecks()
	go func() {
		for {
			select {
			case <-done:
				return
			case registeredCheck, ok := <-healthCheckRegistered.Chan():
				if ok {
					logHealthCheckRegistered(&healthCheck{registeredCheck, nil}, "health check registered")
					if err := registerHealthCheckGauge(done, registeredCheck, subscribeForCheckResults, checkResults, metricRegisterer); err != nil {
						// this should never happen
						logHealthCheckGaugeRegistrationError(&healthCheck{registeredCheck, err}, "health check failed to register")
					}
				}
			}
		}
	}()
	lc.Append(fx.Hook{
		OnStop: func(context.Context) error {
			close(done)
			return nil
		},
	})
}

func logHealthCheckResults(subscribe health.SubscribeForCheckResults, logger *zerolog.Logger, lc fx.Lifecycle) {
	done := make(chan struct{})
	startHealthCheckLogger := startHealthCheckLoggerFunc(subscribe(nil), logger, done)
	go startHealthCheckLogger()
	lc.Append(fx.Hook{
		OnStop: func(context.Context) error {
			close(done)
			return nil
		},
	})
}

// Creates a function that starts up a listener on the  healthCheckResults channel. The listener stops when a signal
// is received on the done channel. When a health check result message is received it logs it.
//
// NOTE: this is extracted out in order to make it testable
func startHealthCheckLoggerFunc(healthCheckResults health.CheckResultsSubscription, logger *zerolog.Logger, done <-chan struct{}) func() {
	logGreenHealthCheck := eventlog.NewLogger(HealthCheckResultEvent, logger, zerolog.NoLevel)
	logYellowHealthCheck := eventlog.NewLogger(HealthCheckResultEvent, logger, zerolog.WarnLevel)
	logRedHealthCheck := eventlog.NewLogger(HealthCheckResultEvent, logger, zerolog.ErrorLevel)
	return func() {
		for {
			select {
			case <-done:
				return
			case result := <-healthCheckResults.Chan():
				switch result.Status {
				case health.Green:
					logGreenHealthCheck(&healthCheckResult{result}, "health check is Green")
				case health.Yellow:
					logYellowHealthCheck(&healthCheckResult{result}, "health check is Yellow")
				default:
					logRedHealthCheck(&healthCheckResult{result}, "health check is Red")
				}
			}
		}
	}
}

type fxlogger struct {
	*zerolog.Logger
}

func newFxLogger(logger *zerolog.Logger) fxlogger {
	return fxlogger{eventlog.ForComponent(logger, "fx")}
}

func (l fxlogger) Printf(msg string, params ...interface{}) {
	l.Log().Msgf(msg, params...)
}

func (b *builder) initZerolog() *zerolog.Logger {
	zerolog.SetGlobalLevel(b.globalLogLevel)

	logger := eventlog.NewZeroLogger(b.logWriter).
		With().
		Str(AppIDLabel, ulid.ULID(b.id).String()).
		Str(AppReleaseIDLabel, ulid.ULID(b.releaseID).String()).
		Str(AppInstanceIDLabel, ulid.ULID(b.instanceID).String()).
		Logger()

	// use the logger as the go standard log output
	log.SetFlags(0)
	log.SetOutput(eventlog.ForComponent(&logger, "log"))

	return &logger
}

func (b *builder) SetStartTimeout(timeout time.Duration) Builder {
	b.startTimeout = timeout
	return b
}

func (b *builder) SetStopTimeout(timeout time.Duration) Builder {
	b.stopTimeout = timeout
	return b
}

func (b *builder) Provide(constructors ...interface{}) Builder {
	b.constructors = append(b.constructors, constructors...)
	return b
}

func (b *builder) Invoke(funcs ...interface{}) Builder {
	b.funcs = append(b.funcs, funcs...)
	return b
}

func (b *builder) Populate(targets ...interface{}) Builder {
	b.populateTargets = append(b.populateTargets, targets...)
	return b
}

func (b *builder) HandleInvokeError(errorHandlers ...func(error)) Builder {
	b.invokeErrorHandlers = append(b.invokeErrorHandlers, errorHandlers...)
	return b
}

func (b *builder) HandleStartupError(errorHandlers ...func(error)) Builder {
	b.startErrorHandlers = append(b.startErrorHandlers, errorHandlers...)
	return b
}

func (b *builder) HandleShutdownError(errorHandlers ...func(error)) Builder {
	b.stopErrorHandlers = append(b.stopErrorHandlers, errorHandlers...)
	return b
}

func (b *builder) HandleError(errorHandlers ...func(error)) Builder {
	b.HandleInvokeError(errorHandlers...)
	b.HandleStartupError(errorHandlers...)
	b.HandleShutdownError(errorHandlers...)
	return b
}

func (b *builder) LogWriter(w io.Writer) Builder {
	b.logWriter = w
	return b
}

func (b *builder) LogLevel(level LogLevel) Builder {
	b.globalLogLevel = level.ZerologLevel()
	return b
}

func (b *builder) DisableHTTPServer() Builder {
	b.disableHTTPServer = true
	return b
}
