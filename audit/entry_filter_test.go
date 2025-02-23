// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: BUSL-1.1

package audit

import (
	"context"
	"testing"
	"time"

	"github.com/hashicorp/eventlogger"
	"github.com/hashicorp/vault/helper/namespace"
	"github.com/hashicorp/vault/internal/observability/event"
	"github.com/hashicorp/vault/sdk/logical"
	"github.com/stretchr/testify/require"
)

// TestEntryFilter_NewEntryFilter tests that we can create EntryFilter types correctly.
func TestEntryFilter_NewEntryFilter(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		Filter               string
		IsErrorExpected      bool
		ExpectedErrorMessage string
	}{
		"empty-filter": {
			Filter:               "",
			IsErrorExpected:      true,
			ExpectedErrorMessage: "audit.NewEntryFilter: cannot create new audit filter with empty filter expression: invalid parameter",
		},
		"spacey-filter": {
			Filter:               "    ",
			IsErrorExpected:      true,
			ExpectedErrorMessage: "audit.NewEntryFilter: cannot create new audit filter with empty filter expression: invalid parameter",
		},
		"bad-filter": {
			Filter:               "____",
			IsErrorExpected:      true,
			ExpectedErrorMessage: "audit.NewEntryFilter: cannot create new audit filter",
		},
		"good-filter": {
			Filter:          "foo == bar",
			IsErrorExpected: false,
		},
	}

	for name, tc := range tests {
		name := name
		tc := tc
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			f, err := NewEntryFilter(tc.Filter)
			switch {
			case tc.IsErrorExpected:
				require.Error(t, err)
				require.ErrorContains(t, err, tc.ExpectedErrorMessage)
				require.Nil(t, f)
			default:
				require.NoError(t, err)
				require.NotNil(t, f)
			}
		})
	}
}

// TestEntryFilter_Reopen ensures we can reopen the filter node.
func TestEntryFilter_Reopen(t *testing.T) {
	t.Parallel()

	f := &EntryFilter{}
	res := f.Reopen()
	require.Nil(t, res)
}

// TestEntryFilter_Type ensures we always return the right type for this node.
func TestEntryFilter_Type(t *testing.T) {
	t.Parallel()

	f := &EntryFilter{}
	require.Equal(t, eventlogger.NodeTypeFilter, f.Type())
}

// TestEntryFilter_Process_ContextDone ensures that we stop processing the event
// if the context was cancelled.
func TestEntryFilter_Process_ContextDone(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())

	// Explicitly cancel the context
	cancel()

	l, err := NewEntryFilter("foo == bar")
	require.NoError(t, err)

	// Fake audit event
	a, err := NewEvent(RequestType)
	require.NoError(t, err)

	// Fake event logger event
	e := &eventlogger.Event{
		Type:      eventlogger.EventType(event.AuditType.String()),
		CreatedAt: time.Now(),
		Formatted: make(map[string][]byte),
		Payload:   a,
	}

	e2, err := l.Process(ctx, e)

	require.Error(t, err)
	require.ErrorContains(t, err, "context canceled")

	// Ensure that the pipeline won't continue.
	require.Nil(t, e2)
}

// TestEntryFilter_Process_NilEvent ensures we receive the right error when the
// event we are trying to process is nil.
func TestEntryFilter_Process_NilEvent(t *testing.T) {
	t.Parallel()

	l, err := NewEntryFilter("foo == bar")
	require.NoError(t, err)
	e, err := l.Process(context.Background(), nil)
	require.Error(t, err)
	require.EqualError(t, err, "audit.(EntryFilter).Process: event is nil: invalid parameter")

	// Ensure that the pipeline won't continue.
	require.Nil(t, e)
}

// TestEntryFilter_Process_BadPayload ensures we receive the correct error when
// attempting to process an event with a payload that cannot be parsed back to
// an audit event.
func TestEntryFilter_Process_BadPayload(t *testing.T) {
	t.Parallel()

	l, err := NewEntryFilter("foo == bar")
	require.NoError(t, err)

	e := &eventlogger.Event{
		Type:      eventlogger.EventType(event.AuditType.String()),
		CreatedAt: time.Now(),
		Formatted: make(map[string][]byte),
		Payload:   nil,
	}

	e2, err := l.Process(context.Background(), e)
	require.Error(t, err)
	require.EqualError(t, err, "audit.(EntryFilter).Process: cannot parse event payload: invalid parameter")

	// Ensure that the pipeline won't continue.
	require.Nil(t, e2)
}

// TestEntryFilter_Process_NoAuditDataInPayload ensure we stop processing a pipeline
// when the data in the audit event is nil.
func TestEntryFilter_Process_NoAuditDataInPayload(t *testing.T) {
	t.Parallel()

	l, err := NewEntryFilter("foo == bar")
	require.NoError(t, err)

	a, err := NewEvent(RequestType)
	require.NoError(t, err)

	// Ensure audit data is nil
	a.Data = nil

	e := &eventlogger.Event{
		Type:      eventlogger.EventType(event.AuditType.String()),
		CreatedAt: time.Now(),
		Formatted: make(map[string][]byte),
		Payload:   a,
	}

	e2, err := l.Process(context.Background(), e)

	// Make sure we get the 'nil, nil' response to stop processing this pipeline.
	require.NoError(t, err)
	require.Nil(t, e2)
}

// TestEntryFilter_Process_FilterSuccess tests that when a filter matches we
// receive no error and the event is not nil so it continues in the pipeline.
func TestEntryFilter_Process_FilterSuccess(t *testing.T) {
	t.Parallel()

	l, err := NewEntryFilter("mount_type == juan")
	require.NoError(t, err)

	a, err := NewEvent(RequestType)
	require.NoError(t, err)

	a.Data = &logical.LogInput{
		Request: &logical.Request{
			Operation: logical.CreateOperation,
			MountType: "juan",
		},
	}

	e := &eventlogger.Event{
		Type:      eventlogger.EventType(event.AuditType.String()),
		CreatedAt: time.Now(),
		Formatted: make(map[string][]byte),
		Payload:   a,
	}

	ctx := namespace.ContextWithNamespace(context.Background(), namespace.RootNamespace)

	e2, err := l.Process(ctx, e)

	require.NoError(t, err)
	require.NotNil(t, e2)
}

// TestEntryFilter_Process_FilterFail tests that when a filter fails to match we
// receive no error, but also the event is nil so that the pipeline completes.
func TestEntryFilter_Process_FilterFail(t *testing.T) {
	t.Parallel()

	l, err := NewEntryFilter("mount_type == john and operation == create and namespace == root")
	require.NoError(t, err)

	a, err := NewEvent(RequestType)
	require.NoError(t, err)

	a.Data = &logical.LogInput{
		Request: &logical.Request{
			Operation: logical.CreateOperation,
			MountType: "juan",
		},
	}

	e := &eventlogger.Event{
		Type:      eventlogger.EventType(event.AuditType.String()),
		CreatedAt: time.Now(),
		Formatted: make(map[string][]byte),
		Payload:   a,
	}

	ctx := namespace.ContextWithNamespace(context.Background(), namespace.RootNamespace)

	e2, err := l.Process(ctx, e)

	require.NoError(t, err)
	require.Nil(t, e2)
}
