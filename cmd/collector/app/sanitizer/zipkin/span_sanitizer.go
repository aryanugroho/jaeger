// Copyright (c) 2017 Uber Technologies, Inc.
//
// Permission is hereby granted, free of charge, to any person obtaining a copy
// of this software and associated documentation files (the "Software"), to deal
// in the Software without restriction, including without limitation the rights
// to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
// copies of the Software, and to permit persons to whom the Software is
// furnished to do so, subject to the following conditions:
//
// The above copyright notice and this permission notice shall be included in
// all copies or substantial portions of the Software.
//
// THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
// IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
// FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
// AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
// LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
// OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN
// THE SOFTWARE.

package zipkin

import (
	"strconv"
	"strings"

	"go.uber.org/zap"

	zc "github.com/uber/jaeger/thrift-gen/zipkincore"
)

const (
	negativeDurationTag = "errNegativeDuration"
	zeroParentIDTag     = "errZeroParentID"
)

var (
	defaultDuration = int64(1)
)

// Sanitizer interface for sanitizing spans. Any business logic that needs to be applied to normalize the contents of a
// span should implement this interface.
// TODO - just make this a function
type Sanitizer interface {
	Sanitize(span *zc.Span) *zc.Span
}

// ChainedSanitizer applies multiple sanitizers in serial fashion
type ChainedSanitizer []Sanitizer

// NewChainedSanitizer creates a Sanitizer from the variadic list of passed Sanitizers
func NewChainedSanitizer(sanitizers ...Sanitizer) ChainedSanitizer {
	return sanitizers
}

// Sanitize calls each Sanitize, returning the first error
func (cs ChainedSanitizer) Sanitize(span *zc.Span) *zc.Span {
	for _, s := range cs {
		span = s.Sanitize(span)
	}
	return span
}

type spanLogger struct {
	logger *zap.Logger
}

func (s spanLogger) ForSpan(span *zc.Span) *zap.Logger {
	return s.logger.
		With(zap.String("traceID", strconv.FormatUint(uint64(span.TraceID), 16))).
		With(zap.String("spanID", strconv.FormatUint(uint64(span.ID), 16)))
}

// NewSpanDurationSanitizer returns a sanitizer that deals with nil or 0 span duration.
func NewSpanDurationSanitizer(logger *zap.Logger) Sanitizer {
	return &spanDurationSanitizer{log: spanLogger{logger}}
}

type spanDurationSanitizer struct {
	log spanLogger
}

func (s *spanDurationSanitizer) Sanitize(span *zc.Span) *zc.Span {
	if span.Duration == nil {
		span.Duration = &defaultDuration
		return span
	}
	duration := *span.Duration
	if duration >= 0 {
		return span
	}
	span.Duration = &defaultDuration
	annotation := zc.BinaryAnnotation{
		Key:            negativeDurationTag,
		Value:          []byte(strconv.FormatInt(duration, 10)),
		AnnotationType: zc.AnnotationType_STRING,
	}
	span.BinaryAnnotations = append(span.BinaryAnnotations, &annotation)
	return span
}

// NewParentIDSanitizer returns a sanitizer that deals parentID == 0
// by replacing with nil, per Zipkin convention.
func NewParentIDSanitizer(logger *zap.Logger) Sanitizer {
	return &parentIDSanitizer{log: spanLogger{logger}}
}

type parentIDSanitizer struct {
	log spanLogger
}

func (s *parentIDSanitizer) Sanitize(span *zc.Span) *zc.Span {
	if span.ParentID == nil || *span.ParentID != 0 {
		return span
	}
	annotation := zc.BinaryAnnotation{
		Key:            zeroParentIDTag,
		Value:          []byte("0"),
		AnnotationType: zc.AnnotationType_STRING,
	}
	span.BinaryAnnotations = append(span.BinaryAnnotations, &annotation)
	span.ParentID = nil
	return span
}

// NewErrorTagSanitizer returns a sanitizer that changes error binary annotations to boolean type
// and sets appropriate value, in case value was a string message it adds a 'error.message' binary annotation with
// this message.
func NewErrorTagSanitizer() Sanitizer {
	return &errorTagSanitizer{}
}

type errorTagSanitizer struct {
}

func (s *errorTagSanitizer) Sanitize(span *zc.Span) *zc.Span {
	for _, binAnno := range span.BinaryAnnotations {
		if binAnno.AnnotationType != zc.AnnotationType_BOOL && strings.EqualFold("error", binAnno.Key) {
			binAnno.AnnotationType = zc.AnnotationType_BOOL

			if strings.EqualFold("true", string(binAnno.Value)) || len(binAnno.Value) == 0 {
				binAnno.Value = []byte{1}
			} else if strings.EqualFold("false", string(binAnno.Value)) {
				binAnno.Value = []byte{0}
			} else {
				// value is different to true/false, create another bin annotation with error message
				annoErrorMsg := &zc.BinaryAnnotation{
					Key:   "error.message",
					Value: binAnno.Value,
				}
				span.BinaryAnnotations = append(span.BinaryAnnotations, annoErrorMsg)
				binAnno.Value = []byte{1}
			}
		}
	}

	return span
}
