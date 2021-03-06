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
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/uber/jaeger/pkg/testutils"
	"github.com/uber/jaeger/thrift-gen/zipkincore"
)

var (
	negativeDuration = int64(-1)
	positiveDuration = int64(1)
)

func TestChainedSanitizer(t *testing.T) {
	sanitizer := NewChainedSanitizer(NewSpanDurationSanitizer(zap.NewNop()))

	span := &zipkincore.Span{Duration: &negativeDuration}
	actual := sanitizer.Sanitize(span)
	assert.Equal(t, positiveDuration, *actual.Duration)
}

func TestSpanDurationSanitizer(t *testing.T) {
	logger, _ := testutils.NewLogger()

	sanitizer := NewSpanDurationSanitizer(logger)

	span := &zipkincore.Span{Duration: &negativeDuration}
	actual := sanitizer.Sanitize(span)
	assert.Equal(t, positiveDuration, *actual.Duration)
	assert.Len(t, actual.BinaryAnnotations, 1)
	assert.Equal(t, "-1", string(actual.BinaryAnnotations[0].Value))

	logger, _ = testutils.NewLogger()
	sanitizer = NewSpanDurationSanitizer(logger)
	span = &zipkincore.Span{Duration: &positiveDuration}
	actual = sanitizer.Sanitize(span)
	assert.Equal(t, positiveDuration, *actual.Duration)
	assert.Len(t, actual.BinaryAnnotations, 0)

	logger, _ = testutils.NewLogger()
	sanitizer = NewSpanDurationSanitizer(logger)
	nilDurationSpan := &zipkincore.Span{}
	actual = sanitizer.Sanitize(nilDurationSpan)
	assert.Equal(t, int64(1), *actual.Duration)
}

func TestSpanParentIDSanitizer(t *testing.T) {
	var (
		zero = int64(0)
		four = int64(4)
	)
	tests := []struct {
		parentID *int64
		expected *int64
		tag      bool
		descr    string
	}{
		{&zero, nil, true, "zero"},
		{&four, &four, false, "four"},
		{nil, nil, false, "nil"},
	}
	for _, test := range tests {
		span := &zipkincore.Span{
			ParentID: test.parentID,
		}
		logger, log := testutils.NewLogger()
		sanitizer := NewParentIDSanitizer(logger)
		actual := sanitizer.Sanitize(span)
		assert.Equal(t, test.expected, actual.ParentID)
		if test.tag {
			if assert.Len(t, actual.BinaryAnnotations, 1) {
				assert.Equal(t, "0", string(actual.BinaryAnnotations[0].Value))
				assert.Equal(t, zeroParentIDTag, string(actual.BinaryAnnotations[0].Key))
			}
		} else {
			assert.Len(t, actual.BinaryAnnotations, 0)
		}
		assert.Empty(t, log.Bytes())
	}
}

func TestSpanErrorSanitizer(t *testing.T) {
	sanitizer := NewErrorTagSanitizer()

	tests := []struct {
		binAnn        *zipkincore.BinaryAnnotation
		isErrorTag    bool
		isError       bool
		addErrMsgAnno bool
	}{
		// value is string
		{&zipkincore.BinaryAnnotation{Key: "error", AnnotationType: zipkincore.AnnotationType_STRING},
			true, true, false,
		},
		{&zipkincore.BinaryAnnotation{Key: "error", Value: []byte("true"), AnnotationType: zipkincore.AnnotationType_STRING},
			true, true, false,
		},
		{&zipkincore.BinaryAnnotation{Key: "error", Value: []byte("message"), AnnotationType: zipkincore.AnnotationType_STRING},
			true, true, true,
		},
		{&zipkincore.BinaryAnnotation{Key: "error", Value: []byte("false"), AnnotationType: zipkincore.AnnotationType_STRING},
			true, false, false,
		},
	}

	for _, test := range tests {
		span := &zipkincore.Span{
			BinaryAnnotations: []*zipkincore.BinaryAnnotation{test.binAnn},
		}

		sanitized := sanitizer.Sanitize(span)
		if test.isErrorTag {
			var expectedVal = []byte{0}
			if test.isError {
				expectedVal = []byte{1}
			}

			assert.Equal(t, expectedVal, sanitized.BinaryAnnotations[0].Value, test.binAnn.Key)
			assert.Equal(t, zipkincore.AnnotationType_BOOL, sanitized.BinaryAnnotations[0].AnnotationType)

			if test.addErrMsgAnno {
				assert.Equal(t, 2, len(sanitized.BinaryAnnotations))
				assert.Equal(t, "error.message", sanitized.BinaryAnnotations[1].Key)
			} else {
				assert.Equal(t, 1, len(sanitized.BinaryAnnotations))
			}
		}
	}
}

func TestSpanLogger(t *testing.T) {
	logger, log := testutils.NewLogger()
	span := &zipkincore.Span{
		TraceID: 123,
		ID:      567,
	}
	spLogger := spanLogger{logger}
	spLogger.ForSpan(span).Warn("oh my")

	data := make(map[string]string)
	require.NoError(t, json.Unmarshal(log.Bytes(), &data))
	delete(data, "time")
	assert.Equal(t, map[string]string{
		"level":   "warn",
		"msg":     "oh my",
		"spanID":  "237",
		"traceID": "7b",
	}, data)
}
