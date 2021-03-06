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
	"time"
)

// Result represents a health check Result
type Result struct {
	// ID is the health check ID
	ID string

	Status Status
	// error should be nil if the status is `Green`
	Err error

	// Time is when the health check was run
	time.Time
	// Duration is how long it took for the health check to run
	time.Duration
}

func (r *Result) String() string {
	return fmt.Sprintf("Result{ID: %q, Status: %s, Time: %s, Duration: %s", r.ID, r.Status, r.Time, r.Duration)
}
