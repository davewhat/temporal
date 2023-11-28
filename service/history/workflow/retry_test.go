// The MIT License
//
// Copyright (c) 2020 Temporal Technologies Inc.  All rights reserved.
//
// Copyright (c) 2020 Uber Technologies, Inc.
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

package workflow

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	enumspb "go.temporal.io/api/enums/v1"
	failurepb "go.temporal.io/api/failure/v1"
	"google.golang.org/protobuf/types/known/durationpb"
	"google.golang.org/protobuf/types/known/timestamppb"

	persistencespb "go.temporal.io/server/api/persistence/v1"
	"go.temporal.io/server/common"
	"go.temporal.io/server/common/backoff"
	"go.temporal.io/server/common/clock"
	"go.temporal.io/server/common/failure"
	"go.temporal.io/server/common/primitives/timestamp"
)

func Test_IsRetryable(t *testing.T) {
	a := assert.New(t)

	f := &failurepb.Failure{
		FailureInfo: &failurepb.Failure_TerminatedFailureInfo{TerminatedFailureInfo: &failurepb.TerminatedFailureInfo{}},
	}
	a.False(isRetryable(f, nil))

	f = &failurepb.Failure{
		FailureInfo: &failurepb.Failure_CanceledFailureInfo{CanceledFailureInfo: &failurepb.CanceledFailureInfo{}},
	}
	a.False(isRetryable(f, nil))

	f = &failurepb.Failure{
		FailureInfo: &failurepb.Failure_TimeoutFailureInfo{TimeoutFailureInfo: &failurepb.TimeoutFailureInfo{
			TimeoutType: enumspb.TIMEOUT_TYPE_UNSPECIFIED,
		}},
	}
	a.False(isRetryable(f, nil))

	f = &failurepb.Failure{
		FailureInfo: &failurepb.Failure_TimeoutFailureInfo{TimeoutFailureInfo: &failurepb.TimeoutFailureInfo{
			TimeoutType: enumspb.TIMEOUT_TYPE_START_TO_CLOSE,
		}},
	}
	a.True(isRetryable(f, nil))
	a.False(isRetryable(f, []string{common.TimeoutFailureTypePrefix + enumspb.TIMEOUT_TYPE_START_TO_CLOSE.String()}))

	f = &failurepb.Failure{
		FailureInfo: &failurepb.Failure_TimeoutFailureInfo{TimeoutFailureInfo: &failurepb.TimeoutFailureInfo{
			TimeoutType: enumspb.TIMEOUT_TYPE_SCHEDULE_TO_START,
		}},
	}
	a.False(isRetryable(f, nil))
	a.False(isRetryable(f, []string{common.TimeoutFailureTypePrefix + enumspb.TIMEOUT_TYPE_SCHEDULE_TO_START.String()}))

	f = &failurepb.Failure{
		FailureInfo: &failurepb.Failure_TimeoutFailureInfo{TimeoutFailureInfo: &failurepb.TimeoutFailureInfo{
			TimeoutType: enumspb.TIMEOUT_TYPE_SCHEDULE_TO_CLOSE,
		}},
	}
	a.False(isRetryable(f, nil))
	a.False(isRetryable(f, []string{common.TimeoutFailureTypePrefix + enumspb.TIMEOUT_TYPE_SCHEDULE_TO_CLOSE.String()}))

	f = &failurepb.Failure{
		FailureInfo: &failurepb.Failure_TimeoutFailureInfo{TimeoutFailureInfo: &failurepb.TimeoutFailureInfo{
			TimeoutType: enumspb.TIMEOUT_TYPE_HEARTBEAT,
		}},
	}
	a.True(isRetryable(f, nil))
	a.False(isRetryable(f, []string{common.TimeoutFailureTypePrefix + enumspb.TIMEOUT_TYPE_HEARTBEAT.String()}))
	a.True(isRetryable(f, []string{common.TimeoutFailureTypePrefix + enumspb.TIMEOUT_TYPE_START_TO_CLOSE.String()}))
	a.True(isRetryable(f, []string{common.TimeoutFailureTypePrefix + "unknown timeout type string"}))

	f = &failurepb.Failure{
		FailureInfo: &failurepb.Failure_ServerFailureInfo{ServerFailureInfo: &failurepb.ServerFailureInfo{
			NonRetryable: false,
		}},
	}
	a.True(isRetryable(f, nil))

	f = &failurepb.Failure{
		FailureInfo: &failurepb.Failure_ServerFailureInfo{ServerFailureInfo: &failurepb.ServerFailureInfo{
			NonRetryable: true,
		}},
	}
	a.False(isRetryable(f, nil))

	f = &failurepb.Failure{
		FailureInfo: &failurepb.Failure_ApplicationFailureInfo{ApplicationFailureInfo: &failurepb.ApplicationFailureInfo{
			NonRetryable: true,
		}},
	}
	a.False(isRetryable(f, nil))

	f = &failurepb.Failure{
		FailureInfo: &failurepb.Failure_ApplicationFailureInfo{ApplicationFailureInfo: &failurepb.ApplicationFailureInfo{
			NonRetryable: false,
			Type:         "type",
		}},
	}
	a.True(isRetryable(f, nil))
	a.True(isRetryable(f, []string{"otherType"}))
	a.False(isRetryable(f, []string{"otherType", "type"}))
	a.False(isRetryable(f, []string{"type"}))

	// When any failure is inside ChildWorkflowExecutionFailure, it is always retryable because ChildWorkflow is always retryable.
	f = &failurepb.Failure{
		FailureInfo: &failurepb.Failure_ChildWorkflowExecutionFailureInfo{ChildWorkflowExecutionFailureInfo: &failurepb.ChildWorkflowExecutionFailureInfo{}},
		Cause: &failurepb.Failure{
			FailureInfo: &failurepb.Failure_ApplicationFailureInfo{ApplicationFailureInfo: &failurepb.ApplicationFailureInfo{
				NonRetryable: true,
			}},
		},
	}
	a.True(isRetryable(f, nil))

	f = &failurepb.Failure{
		FailureInfo: &failurepb.Failure_ChildWorkflowExecutionFailureInfo{ChildWorkflowExecutionFailureInfo: &failurepb.ChildWorkflowExecutionFailureInfo{}},
		Cause: &failurepb.Failure{
			FailureInfo: &failurepb.Failure_ActivityFailureInfo{ActivityFailureInfo: &failurepb.ActivityFailureInfo{}},
			Cause: &failurepb.Failure{
				FailureInfo: &failurepb.Failure_ApplicationFailureInfo{ApplicationFailureInfo: &failurepb.ApplicationFailureInfo{
					NonRetryable: true,
				}},
			},
		},
	}
	a.True(isRetryable(f, nil))
}

func Test_NextRetry(t *testing.T) {
	a := assert.New(t)
	now, _ := time.Parse(time.RFC3339, "2018-04-13T16:08:08+00:00")
	serverFailure := failure.NewServerFailure("some retryable server failure", false)
	identity := "some-worker-identity"

	ai := &persistencespb.ActivityInfo{
		ScheduleToStartTimeout:      timestamp.DurationFromSeconds(5),
		ScheduleToCloseTimeout:      timestamp.DurationFromSeconds(30),
		StartToCloseTimeout:         timestamp.DurationFromSeconds(25),
		HasRetryPolicy:              false,
		RetryNonRetryableErrorTypes: []string{},
		StartedIdentity:             identity,
		Attempt:                     1,
		RetryMaximumAttempts:        2,
		RetryExpirationTime:         timestamppb.New(now.Add(100 * time.Second)),
		RetryInitialInterval:        durationpb.New(time.Duration(0)),
		RetryMaximumInterval:        durationpb.New(time.Duration(0)),
		RetryBackoffCoefficient:     2,
	}

	t.Run("should not retry if there is no retry policy", func(t *testing.T) {
		// no retry without retry policy
		// retry if both MaximumAttempts and WorkflowExpirationTime are not set
		//  above means no limitation on attempts and expiration
		ai.RetryMaximumAttempts = 0
		ai.RetryExpirationTime = nil
		ai.RetryInitialInterval = timestamp.DurationFromSeconds(1)
		ai.CancelRequested = false
		interval, retryState := getBackoffInterval(
			clock.NewRealTimeSource().Now(),
			ai.Attempt,
			ai.RetryMaximumAttempts,
			ai.RetryInitialInterval,
			ai.RetryMaximumInterval,
			ai.RetryExpirationTime,
			ai.RetryBackoffCoefficient,
			serverFailure,
			ai.RetryNonRetryableErrorTypes,
		)
		a.Equal(time.Second, interval)
		a.Equal(enumspb.RETRY_STATE_IN_PROGRESS, retryState)
	})

	t.Run("no retry if MaximumAttempts is 1 (for initial Attempt)", func(t *testing.T) {
		ai.RetryMaximumAttempts = 1
		ai.RetryExpirationTime = nil
		ai.RetryInitialInterval = timestamp.DurationFromSeconds(1)
		interval, retryState := getBackoffInterval(
			clock.NewRealTimeSource().Now(),
			ai.Attempt,
			ai.RetryMaximumAttempts,
			ai.RetryInitialInterval,
			ai.RetryMaximumInterval,
			ai.RetryExpirationTime,
			ai.RetryBackoffCoefficient,
			serverFailure,
			ai.RetryNonRetryableErrorTypes,
		)
		a.Equal(backoff.NoBackoff, interval)
		a.Equal(enumspb.RETRY_STATE_MAXIMUM_ATTEMPTS_REACHED, retryState)
	})

	t.Run("backoff retry, intervals: 1ms, 2ms, 4ms, 8ms.", func(t *testing.T) {
		ai.RetryMaximumAttempts = 5
		ai.RetryBackoffCoefficient = 2
		ai.RetryExpirationTime = nil
		ai.RetryInitialInterval = durationpb.New(1 * time.Millisecond)
		interval, retryState := getBackoffInterval(
			now,
			ai.Attempt,
			ai.RetryMaximumAttempts,
			ai.RetryInitialInterval,
			ai.RetryMaximumInterval,
			ai.RetryExpirationTime,
			ai.RetryBackoffCoefficient,
			serverFailure,
			ai.RetryNonRetryableErrorTypes,
		)
		a.Equal(time.Millisecond, interval)
		a.Equal(enumspb.RETRY_STATE_IN_PROGRESS, retryState)
		ai.Attempt = 2

		interval, retryState = getBackoffInterval(
			now,
			ai.Attempt,
			ai.RetryMaximumAttempts,
			ai.RetryInitialInterval,
			ai.RetryMaximumInterval,
			ai.RetryExpirationTime,
			ai.RetryBackoffCoefficient,
			serverFailure,
			ai.RetryNonRetryableErrorTypes,
		)
		a.Equal(time.Millisecond*2, interval)
		a.Equal(enumspb.RETRY_STATE_IN_PROGRESS, retryState)
		ai.Attempt = 3

		interval, retryState = getBackoffInterval(
			now,
			ai.Attempt,
			ai.RetryMaximumAttempts,
			ai.RetryInitialInterval,
			ai.RetryMaximumInterval,
			ai.RetryExpirationTime,
			ai.RetryBackoffCoefficient,
			serverFailure,
			ai.RetryNonRetryableErrorTypes,
		)
		a.Equal(time.Millisecond*4, interval)
		a.Equal(enumspb.RETRY_STATE_IN_PROGRESS, retryState)
	})

	t.Run("test non-retryable error", func(t *testing.T) {
		ai.Attempt = 4
		serverFailure = failure.NewServerFailure("some non-retryable server failure", true)
		interval, retryState := getBackoffInterval(
			now,
			ai.Attempt,
			ai.RetryMaximumAttempts,
			ai.RetryInitialInterval,
			ai.RetryMaximumInterval,
			ai.RetryExpirationTime,
			ai.RetryBackoffCoefficient,
			serverFailure,
			ai.RetryNonRetryableErrorTypes,
		)
		a.Equal(backoff.NoBackoff, interval)
		a.Equal(enumspb.RETRY_STATE_NON_RETRYABLE_FAILURE, retryState)

		serverFailure = failure.NewServerFailure("good-reason", false)

		interval, retryState = getBackoffInterval(
			now,
			ai.Attempt,
			ai.RetryMaximumAttempts,
			ai.RetryInitialInterval,
			ai.RetryMaximumInterval,
			ai.RetryExpirationTime,
			ai.RetryBackoffCoefficient,
			serverFailure,
			ai.RetryNonRetryableErrorTypes,
		)
		a.Equal(time.Millisecond*8, interval)
		a.Equal(enumspb.RETRY_STATE_IN_PROGRESS, retryState)
	})

	t.Run("test non-retryable error", func(t *testing.T) {
		ai.Attempt = 5
		ai.RetryMaximumAttempts = 5
		interval, retryState := getBackoffInterval(
			now,
			ai.Attempt,
			ai.RetryMaximumAttempts,
			ai.RetryInitialInterval,
			ai.RetryMaximumInterval,
			ai.RetryExpirationTime,
			ai.RetryBackoffCoefficient,
			serverFailure,
			ai.RetryNonRetryableErrorTypes,
		)
		a.Equal(backoff.NoBackoff, interval)
		a.Equal(enumspb.RETRY_STATE_MAXIMUM_ATTEMPTS_REACHED, retryState)
	})

	t.Run("increase max attempts, with max interval cap at 10s", func(t *testing.T) {
		ai.Attempt = 5
		ai.RetryMaximumAttempts = 6
		ai.RetryMaximumInterval = durationpb.New(10 * time.Millisecond)
		interval, retryState := getBackoffInterval(
			now,
			ai.Attempt,
			ai.RetryMaximumAttempts,
			ai.RetryInitialInterval,
			ai.RetryMaximumInterval,
			ai.RetryExpirationTime,
			ai.RetryBackoffCoefficient,
			serverFailure,
			ai.RetryNonRetryableErrorTypes,
		)
		a.Equal(time.Millisecond*10, interval)
		a.Equal(enumspb.RETRY_STATE_IN_PROGRESS, retryState)
	})

	t.Run("no retry because expiration time before next interval", func(t *testing.T) {
		ai.Attempt = 6
		ai.RetryMaximumAttempts = 8
		ai.RetryExpirationTime = timestamppb.New(now.Add(time.Millisecond * 5))
		interval, retryState := getBackoffInterval(
			now,
			ai.Attempt,
			ai.RetryMaximumAttempts,
			ai.RetryInitialInterval,
			ai.RetryMaximumInterval,
			ai.RetryExpirationTime,
			ai.RetryBackoffCoefficient,
			serverFailure,
			ai.RetryNonRetryableErrorTypes,
		)
		a.Equal(backoff.NoBackoff, interval)
		a.Equal(enumspb.RETRY_STATE_TIMEOUT, retryState)
	})

	t.Run("extend expiration, next interval should be 10s", func(t *testing.T) {
		ai.RetryExpirationTime = timestamppb.New(now.Add(time.Minute))
		interval, retryState := getBackoffInterval(
			now,
			ai.Attempt,
			ai.RetryMaximumAttempts,
			ai.RetryInitialInterval,
			ai.RetryMaximumInterval,
			ai.RetryExpirationTime,
			ai.RetryBackoffCoefficient,
			serverFailure,
			ai.RetryNonRetryableErrorTypes,
		)
		a.Equal(time.Millisecond*10, interval)
		a.Equal(enumspb.RETRY_STATE_IN_PROGRESS, retryState)
	})

	t.Run("with big max retry, math.Pow() could overflow, verify that it uses the MaxInterval", func(t *testing.T) {
		ai.Attempt = 64
		ai.RetryMaximumAttempts = 100
		interval, retryState := getBackoffInterval(
			now,
			ai.Attempt,
			ai.RetryMaximumAttempts,
			ai.RetryInitialInterval,
			ai.RetryMaximumInterval,
			ai.RetryExpirationTime,
			ai.RetryBackoffCoefficient,
			serverFailure,
			ai.RetryNonRetryableErrorTypes,
		)
		a.Equal(time.Millisecond*10, interval)
		a.Equal(enumspb.RETRY_STATE_IN_PROGRESS, retryState)
	})
}
