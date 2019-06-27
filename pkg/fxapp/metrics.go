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
	"context"
	"fmt"
	"github.com/pkg/errors"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	dto "github.com/prometheus/client_model/go"
	"github.com/rs/zerolog"
	"go.uber.org/fx"
	"net/http"
	"strings"
	"sync"
	"time"
)

// FindMetricFamily returns the first metric family that matches the filter
func FindMetricFamily(mfs []*dto.MetricFamily, accept func(mf *dto.MetricFamily) bool) *dto.MetricFamily {
	for _, mf := range mfs {
		if accept(mf) {
			return mf
		}
	}
	return nil
}

// FindMetricFamilies returns first metric families that match the filter
func FindMetricFamilies(mfs []*dto.MetricFamily, accept func(mf *dto.MetricFamily) bool) []*dto.MetricFamily {
	var result []*dto.MetricFamily
	for _, mf := range mfs {
		if accept(mf) {
			result = append(result, mf)
		}
	}
	return result
}

// MetricDesc is used to describe the metric
type MetricDesc struct {
	Name string
	Help string
	MetricType
	Labels []string
}

// DescsFromMetricFamilies extracts metric descriptors from gathered metrics
func DescsFromMetricFamilies(mfs []*dto.MetricFamily) []*MetricDesc {
	if len(mfs) == 0 {
		return nil
	}

	metrics := make([]*MetricDesc, len(mfs))
	for i, mf := range mfs {
		metrics[i] = NewMetricDesc(mf)
	}

	return metrics
}

// NewMetricDesc extracts the metric descriptor from the gathered metrics
func NewMetricDesc(mf *dto.MetricFamily) *MetricDesc {
	m := &MetricDesc{
		Name:       *mf.Name,
		Help:       *mf.Help,
		MetricType: mapMetricType(*mf.Type),
	}
	if len(mf.Metric) > 0 {
		m.Labels = getLabels(mf.Metric[0])
	}
	return m
}

// MetricType represents a metric type enum
type MetricType uint8

// metric type enum values
const (
	Untyped MetricType = iota
	Counter
	Gauge
	Histogram
	Summary
)

func mapMetricType(t dto.MetricType) MetricType {
	switch t {
	case dto.MetricType_COUNTER:
		return Counter
	case dto.MetricType_GAUGE:
		return Gauge
	case dto.MetricType_HISTOGRAM:
		return Histogram
	case dto.MetricType_SUMMARY:
		return Summary
	default:
		return Untyped
	}
}

func getLabels(m *dto.Metric) []string {
	if len(m.Label) == 0 {
		return nil
	}

	names := make([]string, len(m.Label))
	for i, labelPair := range m.Label {
		names[i] = *labelPair.Name
	}
	return names
}

// PrometheusHTTPServerOpts PrometheusHTTPServer options
type PrometheusHTTPServerOpts struct {
	port          uint
	readTimeout   time.Duration
	writeTimeout  time.Duration
	endpoint      string
	errorHandling promhttp.HandlerErrorHandling
}

// NewPrometheusHTTPServerOpts constructs a new PrometheusHTTPServerOpts with the following settings:
//
//  - Port				5050
//	- ReadTimeout		1 secs
//	- WriteTimeout 		5 sec
//	- Endpoint			"/metrics"
//	- ErrorHandling		promhttp.HTTPErrorOnError
func NewPrometheusHTTPServerOpts() *PrometheusHTTPServerOpts {
	return &PrometheusHTTPServerOpts{
		port:          5050,
		readTimeout:   time.Second,
		writeTimeout:  5 * time.Second,
		endpoint:      "/metrics",
		errorHandling: promhttp.HTTPErrorOnError,
	}
}

// Port to run the http server on - if zero, then it defaults to 5050
func (opts *PrometheusHTTPServerOpts) Port() uint {
	if opts.port == 0 {
		return 5050
	}
	return opts.port
}

// SetPort sets the port
func (opts *PrometheusHTTPServerOpts) SetPort(port uint) *PrometheusHTTPServerOpts {
	opts.port = port
	return opts
}

// ReadTimeout corresponds to http.readTimeout and defaults to 1 sec
func (opts *PrometheusHTTPServerOpts) ReadTimeout() time.Duration {
	if opts.readTimeout == time.Duration(0) {
		return 1 * time.Second
	}
	return opts.readTimeout
}

// SetReadTimeout sets the HTTP request read timeout
func (opts *PrometheusHTTPServerOpts) SetReadTimeout(timeout time.Duration) *PrometheusHTTPServerOpts {
	opts.readTimeout = timeout
	return opts
}

// WriteTimeout corresponds to http.WriteTimeout and defaults to 5 secs
func (opts *PrometheusHTTPServerOpts) WriteTimeout() time.Duration {
	if opts.writeTimeout == time.Duration(0) {
		return 5 * time.Second
	}
	return opts.writeTimeout
}

// SetWriteTimeout set the HTTP write timeout
func (opts *PrometheusHTTPServerOpts) SetWriteTimeout(timeout time.Duration) *PrometheusHTTPServerOpts {
	opts.writeTimeout = timeout
	return opts
}

// Endpoint defaults to /metrics
func (opts *PrometheusHTTPServerOpts) Endpoint() string {
	if opts.endpoint == "" {
		return "/metrics"
	}
	return opts.endpoint
}

// SetEndpoint set the endpoint
func (opts *PrometheusHTTPServerOpts) SetEndpoint(endpoint string) *PrometheusHTTPServerOpts {
	opts.endpoint = strings.TrimSpace(endpoint)
	return opts
}

// ErrorHandling defines how errors are handled - default is promhttp.HTTPErrorOnError
func (opts *PrometheusHTTPServerOpts) ErrorHandling() promhttp.HandlerErrorHandling {
	return opts.errorHandling
}

// SetErrorHandling defines how the prometheus Handler serving metrics will handle errors.
func (opts *PrometheusHTTPServerOpts) SetErrorHandling(errorHandling promhttp.HandlerErrorHandling) *PrometheusHTTPServerOpts {
	opts.errorHandling = errorHandling
	return opts
}

// RunPrometheusHTTPServer runs an HTTP server exposes metrics on the /metrics endpoint
type RunPrometheusHTTPServer func(gatherer prometheus.Gatherer, registerer prometheus.Registerer, logger *zerolog.Logger, lc fx.Lifecycle)

// PrometheusHTTPServerRunner returns a function that will run an HTTP server to expose Prometheus metrics
func PrometheusHTTPServerRunner(httpServerOpts *PrometheusHTTPServerOpts) RunPrometheusHTTPServer {
	return func(gatherer prometheus.Gatherer, registerer prometheus.Registerer, logger *zerolog.Logger, lc fx.Lifecycle) {
		errorLog := prometheusHTTPErrorLog(PrometheusHTTPError.NewLogEventer(logger, zerolog.ErrorLevel))
		opts := promhttp.HandlerOpts{
			ErrorLog:            errorLog,
			ErrorHandling:       httpServerOpts.errorHandling,
			Registry:            registerer,
			MaxRequestsInFlight: 5,
		}
		handler := http.NewServeMux()
		handler.Handle(httpServerOpts.Endpoint(), promhttp.HandlerFor(gatherer, opts))
		server := &http.Server{
			Addr:           fmt.Sprintf(":%d", httpServerOpts.Port()),
			Handler:        handler,
			ReadTimeout:    httpServerOpts.ReadTimeout(),
			WriteTimeout:   httpServerOpts.WriteTimeout(),
			MaxHeaderBytes: 1024,
		}

		lc.Append(fx.Hook{
			OnStart: func(context.Context) error {
				// wait for the HTTP server go routine to start running before returning
				var wg sync.WaitGroup
				wg.Add(1)
				go func() {
					wg.Done()
					err := server.ListenAndServe()
					if err != http.ErrServerClosed {
						errorLog(prometheusHTTPListenAndServerError{err}, "prometheus HTTP server has exited with an error")
					}
				}()
				wg.Wait()
				return nil
			},
			OnStop: func(ctx context.Context) error {
				return server.Shutdown(ctx)
			},
		})
	}
}

// PrometheusHTTPError indicates an error occurred while handling a metrics scrape HTTP request.
const PrometheusHTTPError EventTypeID = "01DEARG17HNQ606ARQNYFY7PG5"

type prometheusHTTPErrorLog LogEventer

func (errLog prometheusHTTPErrorLog) Println(v ...interface{}) {
	errLog(prometheusHTTPError(fmt.Sprint(v...)), "prometheus HTTP handler error")
}

type prometheusHTTPError string

func (err prometheusHTTPError) MarshalZerologObject(e *zerolog.Event) {
	e.Err(errors.New(string(err)))
}

type prometheusHTTPListenAndServerError struct {
	error
}

func (err prometheusHTTPListenAndServerError) MarshalZerologObject(e *zerolog.Event) {
	e.Err(err)
}
