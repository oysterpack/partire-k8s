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
	"github.com/oysterpack/andiamo/pkg/fxapp"
	"github.com/oysterpack/andiamo/pkg/fxapptest"
	"github.com/oysterpack/andiamo/pkg/ulids"
	"github.com/rs/zerolog"
	"go.uber.org/fx"
	"strings"
	"testing"
	"time"
)

func TestAppInitializedEventLogged(t *testing.T) {
	type Foo struct{}

	buf := fxapptest.NewSyncLog()
	_, err := fxapp.NewBuilder(fxapp.ID(ulids.MustNew()), fxapp.ReleaseID(ulids.MustNew())).
		LogWriter(buf).
		SetStopTimeout(time.Minute).
		Provide(func() Foo { return Foo{} }).
		Invoke(func() {}).
		Build()

	switch {
	case err != nil:
		t.Errorf("** app build failed: %v", err)
	default:
		t.Logf("\n%v", buf)

		type Data struct {
			StartTimeout    uint `json:"start_timeout"`
			StopTimeout     uint `json:"stop_timeout"`
			Provides        []string
			Invokes         []string
			DependencyGraph string `json:"dot_graph"`
		}

		type LogEvent struct {
			Name    string `json:"n"`
			Message string `json:"m"`
			Data    Data   `json:"d"`
		}

		var logEvent LogEvent
		for _, line := range strings.Split(buf.String(), "\n") {
			err := json.Unmarshal([]byte(line), &logEvent)
			if err != nil {
				t.Errorf("*** failed to parse log event: %v : %v", err, line)
				continue
			}
			if logEvent.Name == string(fxapp.InitializedEvent) {
				t.Log(line)
				break
			}
		}
		switch {
		case logEvent.Name == string(fxapp.InitializedEvent):
			if logEvent.Message != "app initialized" {
				t.Errorf("*** event message did not match: %v", logEvent.Message)
			}

			if logEvent.Data.StartTimeout*uint(time.Millisecond) != uint(fx.DefaultTimeout) {
				t.Errorf("*** start timeout did not match: %v", logEvent.Data.StartTimeout)
			}

			if logEvent.Data.StopTimeout*uint(time.Millisecond) != uint(time.Minute) {
				t.Errorf("*** stop timeout did not match: %v", logEvent.Data.StartTimeout)
			}
			if len(logEvent.Data.Provides) != 1 {
				t.Errorf("*** provides does not match: %v", logEvent.Data.Provides)
			}
			if len(logEvent.Data.Invokes) != 1 {
				t.Errorf("*** inokes does not match: %v", logEvent.Data.Invokes)
			}
			if logEvent.Data.DependencyGraph == "" {
				t.Error("*** DOT dependency graph was not logged")
			}

		default:
			t.Error("*** app initialization event was not logged")
		}
	}
}

func TestAppStartingEventLogged(t *testing.T) {
	t.Parallel()

	type Foo struct{}

	buf := fxapptest.NewSyncLog()
	app, err := fxapp.NewBuilder(fxapp.ID(ulids.MustNew()), fxapp.ReleaseID(ulids.MustNew())).
		LogWriter(buf).
		SetStopTimeout(time.Minute).
		Provide(func() Foo { return Foo{} }).
		Invoke(func() {}).
		DisableHTTPServer().
		Build()

	switch {
	case err != nil:
		t.Errorf("** app build failed: %v", err)
	default:
		go app.Run()
		<-app.Ready()
		app.Shutdown()
		<-app.Done()

		t.Logf("\n%v", buf)

		type LogEvent struct {
			Name    string `json:"n"`
			Message string `json:"m"`
		}

		var logEvent LogEvent
		for _, line := range strings.Split(buf.String(), "\n") {
			if line == "" {
				break
			}
			err := json.Unmarshal([]byte(line), &logEvent)
			if err != nil {
				t.Errorf("*** failed to parse log event: %v : %v", err, line)
				continue
			}
			if logEvent.Name == string(fxapp.StartingEvent) {
				break
			}
		}
		switch {
		case logEvent.Name == string(fxapp.StartingEvent):
			if logEvent.Message != "app starting" {
				t.Errorf("*** event message did not match: %v", logEvent.Message)
			}
		default:
			t.Error("*** app starting event was not logged")
		}

	}
}

func TestAppStartedEventLogged(t *testing.T) {
	t.Parallel()

	type Foo struct{}

	buf := fxapptest.NewSyncLog()
	app, err := fxapp.NewBuilder(fxapp.ID(ulids.MustNew()), fxapp.ReleaseID(ulids.MustNew())).
		LogWriter(buf).
		SetStopTimeout(time.Minute).
		Provide(func() Foo { return Foo{} }).
		Invoke(func(lc fx.Lifecycle) {
			lc.Append(fx.Hook{
				OnStart: func(context.Context) error {
					// simulate some startup work that consumes some time
					time.Sleep(time.Millisecond)
					return nil
				},
			})
		}).
		DisableHTTPServer().
		Build()

	switch {
	case err != nil:
		t.Errorf("** app build failed: %v", err)
	default:
		go app.Run()
		<-app.Ready()
		app.Shutdown()
		<-app.Done()

		t.Logf("\n%v", buf)

		type Data struct {
			Duration uint
		}

		type LogEvent struct {
			Name    string `json:"n"`
			Message string `json:"m"`
			Data    Data   `json:"d"`
		}

		var logEvent LogEvent
		for _, line := range strings.Split(buf.String(), "\n") {
			if line == "" {
				break
			}
			err := json.Unmarshal([]byte(line), &logEvent)
			if err != nil {
				t.Errorf("*** failed to parse log event: %v : %v", err, line)
				continue
			}
			if logEvent.Name == string(fxapp.StartedEvent) {
				break
			}
		}
		switch {
		case logEvent.Name == string(fxapp.StartedEvent):
			if logEvent.Message != "app started" {
				t.Errorf("*** event message did not match: %v", logEvent.Message)
			}

			if logEvent.Data.Duration == 0 {
				t.Error("*** duration was not logged")
			}
		default:
			t.Error("*** app started event was not logged")
		}

	}
}

func TestAppStoppingEventLogged(t *testing.T) {
	t.Parallel()
	type Foo struct{}

	buf := fxapptest.NewSyncLog()
	app, err := fxapp.NewBuilder(fxapp.ID(ulids.MustNew()), fxapp.ReleaseID(ulids.MustNew())).
		LogWriter(buf).
		SetStopTimeout(time.Minute).
		Provide(func() Foo { return Foo{} }).
		Invoke(func() {}).
		DisableHTTPServer().
		Build()

	switch {
	case err != nil:
		t.Errorf("** app build failed: %v", err)
	default:
		go app.Run()
		<-app.Ready()
		app.Shutdown()
		<-app.Done()

		t.Logf("\n%v", buf)

		type LogEvent struct {
			Name    string `json:"n"`
			Message string `json:"m"`
		}

		var logEvent LogEvent
		for _, line := range strings.Split(buf.String(), "\n") {
			if line == "" {
				break
			}
			err := json.Unmarshal([]byte(line), &logEvent)
			if err != nil {
				t.Errorf("*** failed to parse log event: %v : %v", err, line)
				continue
			}
			if logEvent.Name == string(fxapp.StoppingEvent) {
				break
			}
		}
		switch {
		case logEvent.Name == string(fxapp.StoppingEvent):
			if logEvent.Message != "app stopping" {
				t.Errorf("*** event message did not match: %v", logEvent.Message)
			}
		default:
			t.Error("*** app stopping event was not logged")
		}

	}
}

func TestAppStoppedEventLogged(t *testing.T) {
	t.Parallel()

	type Foo struct{}

	buf := fxapptest.NewSyncLog()
	app, err := fxapp.NewBuilder(fxapp.ID(ulids.MustNew()), fxapp.ReleaseID(ulids.MustNew())).
		LogWriter(buf).
		SetStopTimeout(time.Minute).
		Provide(func() Foo { return Foo{} }).
		Invoke(func(lc fx.Lifecycle) {
			lc.Append(fx.Hook{
				OnStart: func(i context.Context) error {
					return nil
				},
				OnStop: func(context.Context) error {
					// simulate some work that consumes some time
					time.Sleep(time.Millisecond)
					return nil
				},
			})
		}).
		DisableHTTPServer().
		Build()

	switch {
	case err != nil:
		t.Errorf("** app build failed: %v", err)
	default:
		go app.Run()
		<-app.Ready()
		app.Shutdown()
		<-app.Done()

		t.Logf("\n%v", buf)

		type Data struct {
			Duration uint
		}

		type LogEvent struct {
			Name    string `json:"n"`
			Message string `json:"m"`
			Data    Data   `json:"d"`
		}

		var logEvent LogEvent
		for _, line := range strings.Split(buf.String(), "\n") {
			if line == "" {
				break
			}
			err := json.Unmarshal([]byte(line), &logEvent)
			if err != nil {
				t.Errorf("*** failed to parse log event: %v : %v", err, line)
				continue
			}
			if logEvent.Name == string(fxapp.StoppedEvent) {
				break
			}
		}
		switch {
		case logEvent.Name == string(fxapp.StoppedEvent):
			if logEvent.Message != "app stopped" {
				t.Errorf("*** event message did not match: %v", logEvent.Message)
			}

			if logEvent.Data.Duration == 0 {
				t.Error("*** duration was not logged")
			}
		default:
			t.Error("*** app stopped event was not logged")
		}

	}
}

func TestAppInitFailedEventLogged(t *testing.T) {
	t.Parallel()

	type Foo struct{}

	buf := fxapptest.NewSyncLog()
	_, err := fxapp.NewBuilder(fxapp.ID(ulids.MustNew()), fxapp.ReleaseID(ulids.MustNew())).
		LogWriter(buf).
		SetStopTimeout(time.Minute).
		Provide(func() Foo { return Foo{} }).
		Invoke(func() error {
			return errors.New("BOOM!!!")
		}).
		DisableHTTPServer().
		Build()

	switch {
	case err == nil:
		t.Error("*** app should have failed to build")
	default:
		t.Logf("\n%v", buf)

		type Data struct {
			Err string `json:"e"`
		}

		type LogEvent struct {
			Level   string `json:"l"`
			Name    string `json:"n"`
			Message string `json:"m"`
			Data    `json:"d"`
		}

		var logEvent LogEvent
		for _, line := range strings.Split(buf.String(), "\n") {
			logEvent = LogEvent{}
			if line == "" {
				break
			}
			err := json.Unmarshal([]byte(line), &logEvent)
			if err != nil {
				t.Errorf("*** failed to parse log event: %v : %v", err, line)
				continue
			}
			if logEvent.Name == string(fxapp.InitFailedEvent) {
				break
			}
		}
		switch {
		case logEvent.Name == string(fxapp.InitFailedEvent):
			if !strings.Contains(logEvent.Err, "BOOM!!!") {
				t.Errorf("*** event error message did not match: %v", logEvent.Err)
			}

			if logEvent.Level != zerolog.ErrorLevel.String() {
				t.Errorf("*** log level should be error: %v", logEvent.Level)
			}

			if logEvent.Message != "app init failed" {
				t.Errorf("*** message did not match: %v", logEvent.Message)
			}
		default:
			t.Error("*** app event was not logged")
		}

	}
}

func TestAppStartFailedEventLogged(t *testing.T) {
	t.Parallel()

	type Foo struct{}

	buf := fxapptest.NewSyncLog()
	app, err := fxapp.NewBuilder(fxapp.ID(ulids.MustNew()), fxapp.ReleaseID(ulids.MustNew())).
		LogWriter(buf).
		SetStopTimeout(time.Minute).
		Provide(func() Foo { return Foo{} }).
		Invoke(
			func(lc fx.Lifecycle, logger *zerolog.Logger) {
				lc.Append(fx.Hook{
					OnStart: func(context.Context) error {
						logger.Info().Msg("OnStart #1")
						return nil
					},
					// when OnStart #2 fails, the app will rollback, i.e., invoke order app shutdown by stopping all prior
					// services that were started
					OnStop: func(i context.Context) error {
						logger.Info().Msg("OnStop #1")
						return nil
					},
				})
			},
			func(lc fx.Lifecycle, logger *zerolog.Logger) {
				lc.Append(fx.Hook{
					OnStart: func(context.Context) error {
						return errors.New("OnStart #2: BOOM!!!")
					},
					OnStop: func(i context.Context) error {
						logger.Info().Msg("OnStop #2")
						return nil
					},
				})
			},
		).
		DisableHTTPServer().
		Build()

	switch {
	case err != nil:
		t.Errorf("*** app failed to build: %v", err)
	default:
		err := app.Run()
		if err == nil {
			t.Error("*** app should have failed to run")
			break
		}
		t.Logf("app failed to run: %v", err)
		// since app failed to run, then it means it is done
		<-app.Done()

		t.Logf("\n%v", buf)

		type Data struct {
			Err string `json:"e"`
		}

		type LogEvent struct {
			Level   string `json:"l"`
			Name    string `json:"n"`
			Message string `json:"m"`
			Data    `json:"d"`
		}

		var logEvent LogEvent
		for _, line := range strings.Split(buf.String(), "\n") {
			logEvent = LogEvent{}
			if line == "" {
				break
			}
			err := json.Unmarshal([]byte(line), &logEvent)
			if err != nil {
				t.Errorf("*** failed to parse log event: %v : %v", err, line)
				continue
			}
			if logEvent.Name == string(fxapp.StartFailedEvent) {
				break
			}
		}
		switch {
		case logEvent.Name == string(fxapp.StartFailedEvent):
			if !strings.Contains(logEvent.Err, "BOOM!!!") {
				t.Errorf("*** event error message did not match: %v", logEvent.Err)
			}

			if logEvent.Level != zerolog.ErrorLevel.String() {
				t.Errorf("*** log level should be error: %v", logEvent.Level)
			}

			if logEvent.Message != "app start failed" {
				t.Errorf("*** message did not match: %v", logEvent.Message)
			}
		default:
			t.Error("*** app event was not logged")
		}

	}
}

// Given 1 Lifecycle Hook runs successfully, followed by one that fails
// Then the first Lifecycle hook will be rolled back, i.e., it's OnStop hook will be called
// When the first Lifecycle OnStop hook fails
// Then the start and stop errors will be combined into a single mutli-error and logged as an AppStartFailedEvent
func TestAppStartFailedAndStopFailed(t *testing.T) {
	t.Parallel()

	type Foo struct{}

	buf := fxapptest.NewSyncLog()
	app, err := fxapp.NewBuilder(fxapp.ID(ulids.MustNew()), fxapp.ReleaseID(ulids.MustNew())).
		LogWriter(buf).
		SetStopTimeout(time.Minute).
		Provide(func() Foo { return Foo{} }).
		Invoke(
			func(lc fx.Lifecycle, logger *zerolog.Logger) {
				lc.Append(fx.Hook{
					OnStart: func(context.Context) error {
						logger.Info().Msg("OnStart #1")
						return nil
					},
					// when OnStart #2 fails, the app will rollback, i.e., invoke order app shutdown by stopping all prior
					// services that were started
					OnStop: func(i context.Context) error {
						logger.Info().Msg("OnStop #1")
						return errors.New("OnStop #1: BOOM!!!")
					},
				})
			},
			func(lc fx.Lifecycle, logger *zerolog.Logger) {
				lc.Append(fx.Hook{
					OnStart: func(context.Context) error {
						return errors.New("OnStart #2: BOOM!!!")
					},
					OnStop: func(i context.Context) error {
						logger.Info().Msg("OnStop #2")
						return nil
					},
				})
			},
		).
		DisableHTTPServer().
		Build()

	switch {
	case err != nil:
		t.Errorf("*** app failed to build: %v", err)
	default:
		err := app.Run()
		if err == nil {
			t.Error("*** app should have failed to run")
			break
		}
		t.Logf("app failed to run: %v", err)
		// since app failed to run, then it means it is done
		<-app.Done()

		t.Logf("\n%v", buf)

		type Data struct {
			Err string `json:"e"`
		}

		type LogEvent struct {
			Level   string `json:"l"`
			Name    string `json:"n"`
			Message string `json:"m"`
			Data    `json:"d"`
		}

		var logEvent LogEvent
		for _, line := range strings.Split(buf.String(), "\n") {
			logEvent = LogEvent{}
			if line == "" {
				break
			}
			err := json.Unmarshal([]byte(line), &logEvent)
			if err != nil {
				t.Errorf("*** failed to parse log event: %v : %v", err, line)
				continue
			}
			if logEvent.Name == string(fxapp.StartFailedEvent) {
				break
			}
		}
		switch {
		case logEvent.Name == string(fxapp.StartFailedEvent):
			if !strings.Contains(logEvent.Err, "BOOM!!!") {
				t.Errorf("*** event message did not match: %v", logEvent.Err)
			}

			if logEvent.Level != zerolog.ErrorLevel.String() {
				t.Errorf("*** log level should be error: %v", logEvent.Level)
			}
		default:
			t.Error("*** app event was not logged")
		}

		// And the AppStopFailedEvent is not logged
		{
			var logEvent LogEvent
			for _, line := range strings.Split(buf.String(), "\n") {
				if line == "" {
					break
				}
				err := json.Unmarshal([]byte(line), &logEvent)
				if err != nil {
					t.Errorf("*** failed to parse log event: %v : %v", err, line)
					continue
				}
				if logEvent.Name == string(fxapp.StopFailedEvent) {
					break
				}
			}
			if logEvent.Name == string(fxapp.StopFailedEvent) {
				t.Error("*** the app failed to start - thus the AppStopFailedEvent should not have been logged")
			}
		}

	}
}
