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

package health

import (
	"fmt"
	"github.com/pkg/errors"
	"go.uber.org/multierr"
	"time"
)

type service struct {
	Opts

	checks []RegisteredCheck

	stop                chan struct{}
	register            chan registerRequest
	getRegisteredChecks chan chan<- []RegisteredCheck
	getCheckResults     chan checkResultsRequest
	getOverallHealth    chan chan<- Status

	subscribeForRegisteredChecks     chan subscribeForRegisteredChecksRequest
	subscriptionsForRegisteredChecks map[chan<- RegisteredCheck]struct{}

	subscribeForCheckResults     chan subscribeForCheckResults
	subscriptionsForCheckResults map[chan<- Result]func(result Result) bool

	subscribeForOverallHealthChanges     chan chan (chan Status)
	subscriptionsForOverallHealthChanges map[chan<- Status]struct{}
	overallHealth                        Status

	// to protect the application and system from the health checks themselves we want to limit the number of health checks
	// that are allowed to run concurrently
	runSemaphore chan struct{}
	results      chan Result
	runResults   map[string]Result
}

func newService(opts Opts) *service {
	runSemaphore := make(chan struct{}, opts.MaxCheckParallelism)
	var i uint8
	for ; i < opts.MaxCheckParallelism; i++ {
		runSemaphore <- struct{}{}
	}

	return &service{
		stop:                make(chan struct{}),
		register:            make(chan registerRequest),
		getRegisteredChecks: make(chan chan<- []RegisteredCheck),
		getCheckResults:     make(chan checkResultsRequest),
		getOverallHealth:    make(chan chan<- Status),

		subscribeForRegisteredChecks:     make(chan subscribeForRegisteredChecksRequest),
		subscriptionsForRegisteredChecks: make(map[chan<- RegisteredCheck]struct{}),

		subscribeForCheckResults:     make(chan subscribeForCheckResults),
		subscriptionsForCheckResults: make(map[chan<- Result]func(result Result) bool),

		subscribeForOverallHealthChanges:     make(chan chan (chan Status)),
		subscriptionsForOverallHealthChanges: make(map[chan<- Status]struct{}),

		runSemaphore: runSemaphore,
		results:      make(chan Result),
		runResults:   make(map[string]Result),

		Opts: opts,
	}
}

func (s *service) run() {
	for {
		select {
		case <-s.stop:
			return
		case req := <-s.register:
			err := s.Register(req)
			s.sendError(req.reply, err)
		case result := <-s.results:
			s.runResults[result.ID] = result
			s.updateOverallHealth()
			s.publishResult(result)
		case replyChan := <-s.getRegisteredChecks:
			s.SendRegisteredChecks(replyChan)
		case replyChan := <-s.getCheckResults:
			s.SendCheckResults(replyChan)
		case req := <-s.subscribeForRegisteredChecks:
			s.SubscribeForRegisteredChecks(req)
		case req := <-s.subscribeForCheckResults:
			s.SubscribeForCheckResults(req)
		case reply := <-s.getOverallHealth:
			reply <- s.overallHealth
		case reply := <-s.subscribeForOverallHealthChanges:
			s.SubscribeForOverallHealthChanges(reply)
		}
	}
}

func (s *service) sendError(ch chan<- error, err error) {
	defer close(ch)

	if err == nil {
		return
	}

	select {
	case <-s.stop:
	case ch <- err:
	}
}

func (s *service) publishResult(result Result) {
	for ch, filter := range s.subscriptionsForCheckResults {
		if filter(result) {
			go func(ch chan<- Result) {
				select {
				case <-s.stop:
				case ch <- result:
				}
			}(ch)
		}
	}
}

// - compute the current overall health
// - if the overall health status has changed, then notify monitors
func (s *service) updateOverallHealth() {
	previous := s.overallHealth
	s.overallHealth = s.OverallHealth()
	if previous == s.overallHealth {
		return
	}
	for ch := range s.subscriptionsForOverallHealthChanges {
		go func(ch chan<- Status, status Status) {
			select {
			case <-s.stop:
			case ch <- status:
			}
		}(ch, s.overallHealth)
	}
}

func (s *service) TriggerShutdown() {
	select {
	case <-s.stop:
	default:
		close(s.stop)
	}
}

type registerRequest struct {
	check   Check
	opts    CheckerOpts
	checker func() (Status, error)

	reply chan<- error
}

func (s *service) Register(req registerRequest) error {
	WithTimeout := func(id string, check func() (Status, error), timeout time.Duration) Checker {
		healthCheckFailure := func(status Status, err error) error {
			if status == Green {
				return nil
			}

			return multierr.Append(
				fmt.Errorf("health check failed: %s : %s", id, status),
				err,
			)
		}

		return func() Result {
			reply := make(chan Result, 1)
			timer := time.After(timeout)
			// run the check
			go func() {
				start := time.Now()
				status, err := check()
				duration := time.Since(start)
				reply <- Result{
					ID: id,

					Status: status,
					Err:    healthCheckFailure(status, err),

					Time:     start,
					Duration: duration,
				}
			}()

			// wait for the check result with a timeout
			result := func() Result {
				select {
				case <-timer: // health check timed out
					return Result{
						ID: id,

						Status: Red,
						Err:    healthCheckFailure(Red, ErrTimeout),

						Time:     time.Now().Add(timeout * -1),
						Duration: timeout,
					}
				case result := <-reply:
					return result
				}
			}()

			// report the health check result
			go func() {
				select {
				case <-s.stop:
				case s.results <- result:
				}
			}()

			return result
		}
	}

	Schedule := func(id string, check Checker, interval time.Duration) {
		run := func() {
			<-s.runSemaphore
			defer func() {
				s.runSemaphore <- struct{}{}
			}()
			check()
		}

		// run the health check immediately
		run()

		// then run it on its specified interval
		for {
			timer := time.After(interval)
			select {
			case <-s.stop:
				return
			case <-timer:
				run()
			}
		}
	}

	ApplyDefaultOpts := func(opts CheckerOpts) CheckerOpts {
		if opts.Timeout == time.Duration(0) {
			opts.Timeout = s.DefaultTimeout
		}
		if opts.RunInterval == time.Duration(0) {
			opts.RunInterval = s.DefaultRunInterval
		}

		return opts
	}

	ValidateOpts := func(opts CheckerOpts) error {
		var err error
		if opts.RunInterval < s.MinRunInterval {
			err = ErrRunIntervalTooFrequent
		}
		if opts.Timeout > s.MaxTimeout {
			err = multierr.Append(err, ErrRunTimeoutTooHigh)
		}
		return err
	}

	SendRegisteredCheckToSubscribers := func(check RegisteredCheck) {
		for ch := range s.subscriptionsForRegisteredChecks {
			go func(ch chan<- RegisteredCheck) {
				select {
				case <-s.stop:
				case ch <- check:
				}
			}(ch)
		}
	}

	check := req.check

	if req.checker == nil {
		return multierr.Append(errors.New(check.ID), ErrNilChecker)
	}

	opts := ApplyDefaultOpts(req.opts)
	if err := ValidateOpts(opts); err != nil {
		return multierr.Append(fmt.Errorf("invalid health checker opts: %s : %#v", check.ID, opts), err)
	}

	if s.RegisteredCheck(check.ID) != nil {
		return fmt.Errorf("health check is already registered: %s", check.ID)
	}

	registeredCheck := RegisteredCheck{
		Check:       check,
		CheckerOpts: opts,
		Checker:     WithTimeout(check.ID, req.checker, opts.Timeout),
	}
	s.checks = append(s.checks, registeredCheck)
	go Schedule(registeredCheck.ID, registeredCheck.Checker, registeredCheck.RunInterval)
	SendRegisteredCheckToSubscribers(registeredCheck)

	return nil
}

func (s *service) RegisteredCheck(id string) *RegisteredCheck {
	for _, c := range s.checks {
		if c.ID == id {
			return &c
		}
	}
	return nil
}

type checkResultsRequest struct {
	reply  chan []Result
	filter func(result Result) bool
}

func (s *service) SendCheckResults(req checkResultsRequest) {
	var results []Result
	if req.filter == nil {
		results = make([]Result, 0, len(s.runResults))
		for _, result := range s.runResults {
			results = append(results, result)
		}
	} else {
		for _, result := range s.runResults {
			if req.filter(result) {
				results = append(results, result)
			}
		}
	}

	defer close(req.reply)
	req.reply <- results
}

func (s *service) SendRegisteredChecks(reply chan<- []RegisteredCheck) {
	checks := make([]RegisteredCheck, len(s.checks))
	copy(checks, s.checks)

	defer close(reply)
	reply <- checks
}

type subscribeForRegisteredChecksRequest struct {
	reply chan chan RegisteredCheck
}

func (s *service) SubscribeForRegisteredChecks(req subscribeForRegisteredChecksRequest) {
	ch := make(chan RegisteredCheck)
	s.subscriptionsForRegisteredChecks[ch] = struct{}{}

	defer close(req.reply)
	req.reply <- ch
}

type subscribeForCheckResults struct {
	reply  chan chan Result
	filter func(result Result) bool
}

func (s *service) SubscribeForCheckResults(req subscribeForCheckResults) {
	ch := make(chan Result)
	if req.filter != nil {
		s.subscriptionsForCheckResults[ch] = req.filter
	} else {
		s.subscriptionsForCheckResults[ch] = func(Result) bool { return true }
	}

	defer close(req.reply)
	req.reply <- ch
}

func (s *service) OverallHealth() Status {
	var status Status
	for _, result := range s.runResults {
		switch result.Status {
		case Yellow:
			status = result.Status
		case Red:
			return Red
		}
	}

	return status
}

func (s *service) SubscribeForOverallHealthChanges(reply chan (chan Status)) {
	ch := make(chan Status, 1)
	ch <- s.overallHealth
	s.subscriptionsForOverallHealthChanges[ch] = struct{}{}
	select {
	case <-s.stop:
	case reply <- ch:
	}
}
