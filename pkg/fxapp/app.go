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
	"errors"
	"go.uber.org/fx"
	"go.uber.org/multierr"
	"time"
)

type App interface {
	Desc() Desc

	StartTimeout() time.Duration
	StopTimeout() time.Duration
}

type AppBuilder interface {
	Build() (App, error)

	SetStartTimeout(timeout time.Duration) AppBuilder
	SetStopTimeout(timeout time.Duration) AppBuilder

	Constructors(constructors ...interface{}) AppBuilder
	Funcs(funcs ...interface{}) AppBuilder
}

func NewAppBuilder(desc Desc) AppBuilder {
	return &app{
		desc:         desc,
		startTimeout: 15 * time.Second,
		stopTimeout:  15 * time.Second,
	}
}

type app struct {
	desc Desc

	startTimeout time.Duration
	stopTimeout  time.Duration

	constructors []interface{}
	funcs        []interface{}

	*fx.App
}

func (a *app) Desc() Desc {
	return a.desc
}

func (a *app) Build() (App, error) {
	var err error
	if a.desc == nil {
		err = multierr.Append(err, a.desc.Validate())
	}
	if len(a.constructors) == 0 && len(a.funcs) == 0 {
		err = multierr.Append(err, errors.New("at least 1 functional option is required"))
	}

	compOptions := make([]fx.Option, 0, len(a.constructors)+len(a.funcs))
	for _, f := range a.constructors {
		compOptions = append(compOptions, fx.Provide(f))
	}
	for _, f := range a.funcs {
		compOptions = append(compOptions, fx.Invoke(f))
	}

	a.App = fx.New(
		fx.StartTimeout(a.startTimeout),
		fx.StopTimeout(a.stopTimeout),
		fx.Options(compOptions...),
	)
	err = multierr.Append(err, a.App.Err())

	if err != nil {
		return nil, err
	}

	return a, nil
}

func (a *app) SetStartTimeout(timeout time.Duration) AppBuilder {
	a.startTimeout = timeout
	return a
}

func (a *app) SetStopTimeout(timeout time.Duration) AppBuilder {
	a.stopTimeout = timeout
	return a
}

func (a *app) Constructors(constructors ...interface{}) AppBuilder {
	a.constructors = append(a.constructors, constructors...)
	return a
}

func (a *app) Funcs(funcs ...interface{}) AppBuilder {
	a.funcs = append(a.funcs, funcs...)
	return a
}
