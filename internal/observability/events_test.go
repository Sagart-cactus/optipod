/*
Copyright 2025.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package observability

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/leanovate/gopter"
	"github.com/leanovate/gopter/gen"
	"github.com/leanovate/gopter/prop"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/record"
)

// mockEventRecorder captures events for testing
type mockEventRecorder struct {
	events []mockEvent
}

type mockEvent struct {
	eventType string
	reason    string
	message   string
}

func (m *mockEventRecorder) Event(object runtime.Object, eventtype, reason, message string) {
	m.events = append(m.events, mockEvent{
		eventType: eventtype,
		reason:    reason,
		message:   message,
	})
}

func (m *mockEventRecorder) Eventf(object runtime.Object, eventtype, reason, messageFmt string, args ...interface{}) {
	m.Event(object, eventtype, reason, fmt.Sprintf(messageFmt, args...))
}

func (m *mockEventRecorder) AnnotatedEventf(object runtime.Object, annotations map[string]string, eventtype, reason, messageFmt string, args ...interface{}) {
	m.Event(object, eventtype, reason, fmt.Sprintf(messageFmt, args...))
}

// Feature: k8s-workload-rightsizing, Property 26: Failure event creation
// Validates: Requirements 11.1, 11.2, 11.3, 11.4
// For any resource update failure, metrics collection error, or policy validation error,
// the system should create a Kubernetes Event with a clear reason and actionable suggestions.
func TestProperty_FailureEventCreation(t *testing.T) {
	parameters := gopter.DefaultTestParameters()
	parameters.MinSuccessfulTests = 100
	properties := gopter.NewProperties(parameters)

	properties.Property("workload update failures create events with actionable suggestions", prop.ForAll(
		func(workloadName, namespace, errorMsg string) bool {
			mockRecorder := &mockEventRecorder{}
			eventRecorder := NewEventRecorder(mockRecorder)

			// Create a test object
			testObj := &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      workloadName,
					Namespace: namespace,
				},
			}

			// Record a workload update failure
			err := errors.New(errorMsg)
			eventRecorder.RecordWorkloadUpdateFailure(testObj, workloadName, namespace, err)

			// Verify event was created
			if len(mockRecorder.events) != 1 {
				return false
			}

			event := mockRecorder.events[0]

			// Verify event type is Warning
			if event.eventType != corev1.EventTypeWarning {
				return false
			}

			// Verify reason is set
			if event.reason != "UpdateFailed" {
				return false
			}

			// Verify message contains workload name, namespace, and error
			if !strings.Contains(event.message, workloadName) {
				return false
			}
			if !strings.Contains(event.message, namespace) {
				return false
			}
			if !strings.Contains(event.message, errorMsg) {
				return false
			}

			// Verify message contains actionable suggestion
			if !strings.Contains(event.message, "Suggestion:") {
				return false
			}

			return true
		},
		gen.Identifier().SuchThat(func(v string) bool { return len(v) > 0 }),
		gen.Identifier().SuchThat(func(v string) bool { return len(v) > 0 }),
		gen.AlphaString().SuchThat(func(v string) bool { return len(v) > 0 }),
	))

	properties.Property("policy validation errors create events with actionable suggestions", prop.ForAll(
		func(policyName, errorMsg string) bool {
			mockRecorder := &mockEventRecorder{}
			eventRecorder := NewEventRecorder(mockRecorder)

			// Create a test object
			testObj := &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name: policyName,
				},
			}

			// Record a policy validation error
			err := errors.New(errorMsg)
			eventRecorder.RecordPolicyValidationError(testObj, policyName, err)

			// Verify event was created
			if len(mockRecorder.events) != 1 {
				return false
			}

			event := mockRecorder.events[0]

			// Verify event type is Warning
			if event.eventType != corev1.EventTypeWarning {
				return false
			}

			// Verify reason is set
			if event.reason != "ValidationFailed" {
				return false
			}

			// Verify message contains policy name and error
			if !strings.Contains(event.message, policyName) {
				return false
			}
			if !strings.Contains(event.message, errorMsg) {
				return false
			}

			// Verify message contains actionable suggestion
			if !strings.Contains(event.message, "Suggestion:") {
				return false
			}

			return true
		},
		gen.Identifier().SuchThat(func(v string) bool { return len(v) > 0 }),
		gen.AlphaString().SuchThat(func(v string) bool { return len(v) > 0 }),
	))

	properties.Property("metrics collection errors create events with actionable suggestions", prop.ForAll(
		func(workloadName, namespace, provider, errorMsg string) bool {
			mockRecorder := &mockEventRecorder{}
			eventRecorder := NewEventRecorder(mockRecorder)

			// Create a test object
			testObj := &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      workloadName,
					Namespace: namespace,
				},
			}

			// Record a metrics collection error
			err := errors.New(errorMsg)
			eventRecorder.RecordMetricsCollectionError(testObj, workloadName, namespace, provider, err)

			// Verify event was created
			if len(mockRecorder.events) != 1 {
				return false
			}

			event := mockRecorder.events[0]

			// Verify event type is Warning
			if event.eventType != corev1.EventTypeWarning {
				return false
			}

			// Verify reason is set
			if event.reason != "MetricsCollectionFailed" {
				return false
			}

			// Verify message contains workload name, namespace, provider, and error
			if !strings.Contains(event.message, workloadName) {
				return false
			}
			if !strings.Contains(event.message, namespace) {
				return false
			}
			if !strings.Contains(event.message, provider) {
				return false
			}
			if !strings.Contains(event.message, errorMsg) {
				return false
			}

			// Verify message contains actionable suggestion
			if !strings.Contains(event.message, "Suggestion:") {
				return false
			}

			return true
		},
		gen.Identifier().SuchThat(func(v string) bool { return len(v) > 0 }),
		gen.Identifier().SuchThat(func(v string) bool { return len(v) > 0 }),
		gen.Identifier().SuchThat(func(v string) bool { return len(v) > 0 }),
		gen.AlphaString().SuchThat(func(v string) bool { return len(v) > 0 }),
	))

	properties.Property("RBAC errors create events with actionable suggestions", prop.ForAll(
		func(workloadName, namespace, operation string) bool {
			mockRecorder := &mockEventRecorder{}
			eventRecorder := NewEventRecorder(mockRecorder)

			// Create a test object
			testObj := &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      workloadName,
					Namespace: namespace,
				},
			}

			// Record an RBAC error
			eventRecorder.RecordRBACError(testObj, workloadName, namespace, operation)

			// Verify event was created
			if len(mockRecorder.events) != 1 {
				return false
			}

			event := mockRecorder.events[0]

			// Verify event type is Warning
			if event.eventType != corev1.EventTypeWarning {
				return false
			}

			// Verify reason is set
			if event.reason != "RBACError" {
				return false
			}

			// Verify message contains workload name, namespace, and operation
			if !strings.Contains(event.message, workloadName) {
				return false
			}
			if !strings.Contains(event.message, namespace) {
				return false
			}
			if !strings.Contains(event.message, operation) {
				return false
			}

			// Verify message contains actionable suggestion
			if !strings.Contains(event.message, "Suggestion:") {
				return false
			}

			return true
		},
		gen.Identifier().SuchThat(func(v string) bool { return len(v) > 0 }),
		gen.Identifier().SuchThat(func(v string) bool { return len(v) > 0 }),
		gen.Identifier().SuchThat(func(v string) bool { return len(v) > 0 }),
	))

	properties.TestingRun(t)
}

// TestSSAOwnershipTakenEvent tests the RecordSSAOwnershipTaken event creation
func TestSSAOwnershipTakenEvent(t *testing.T) {
	mockRecorder := &mockEventRecorder{}
	eventRecorder := NewEventRecorder(mockRecorder)

	testObj := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-pod",
			Namespace: "test-namespace",
		},
	}

	t.Run("with previous owner", func(t *testing.T) {
		mockRecorder.events = nil // Reset events
		eventRecorder.RecordSSAOwnershipTaken(testObj, "test-workload", "test-ns", "argocd")

		if len(mockRecorder.events) != 1 {
			t.Fatalf("expected 1 event, got %d", len(mockRecorder.events))
		}

		event := mockRecorder.events[0]

		if event.eventType != corev1.EventTypeNormal {
			t.Errorf("expected event type Normal, got %s", event.eventType)
		}

		if event.reason != EventReasonSSAOwnershipTaken {
			t.Errorf("expected reason %s, got %s", EventReasonSSAOwnershipTaken, event.reason)
		}

		if !strings.Contains(event.message, "test-workload") {
			t.Errorf("message should contain workload name")
		}

		if !strings.Contains(event.message, "test-ns") {
			t.Errorf("message should contain namespace")
		}

		if !strings.Contains(event.message, "argocd") {
			t.Errorf("message should contain previous owner")
		}

		if !strings.Contains(event.message, "Server-Side Apply") {
			t.Errorf("message should mention Server-Side Apply")
		}
	})

	t.Run("without previous owner", func(t *testing.T) {
		mockRecorder.events = nil // Reset events
		eventRecorder.RecordSSAOwnershipTaken(testObj, "test-workload", "test-ns", "")

		if len(mockRecorder.events) != 1 {
			t.Fatalf("expected 1 event, got %d", len(mockRecorder.events))
		}

		event := mockRecorder.events[0]

		if event.eventType != corev1.EventTypeNormal {
			t.Errorf("expected event type Normal, got %s", event.eventType)
		}

		if event.reason != EventReasonSSAOwnershipTaken {
			t.Errorf("expected reason %s, got %s", EventReasonSSAOwnershipTaken, event.reason)
		}

		if !strings.Contains(event.message, "test-workload") {
			t.Errorf("message should contain workload name")
		}

		if !strings.Contains(event.message, "test-ns") {
			t.Errorf("message should contain namespace")
		}

		if strings.Contains(event.message, "previous owner") {
			t.Errorf("message should not mention previous owner when empty")
		}
	})
}

// TestSSAConflictEvent tests the RecordSSAConflict event creation
func TestSSAConflictEvent(t *testing.T) {
	mockRecorder := &mockEventRecorder{}
	eventRecorder := NewEventRecorder(mockRecorder)

	testObj := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-pod",
			Namespace: "test-namespace",
		},
	}

	err := errors.New("field ownership conflict")
	eventRecorder.RecordSSAConflict(testObj, "test-workload", "test-ns", "kubectl", err)

	if len(mockRecorder.events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(mockRecorder.events))
	}

	event := mockRecorder.events[0]

	if event.eventType != corev1.EventTypeWarning {
		t.Errorf("expected event type Warning, got %s", event.eventType)
	}

	if event.reason != EventReasonSSAConflict {
		t.Errorf("expected reason %s, got %s", EventReasonSSAConflict, event.reason)
	}

	if !strings.Contains(event.message, "test-workload") {
		t.Errorf("message should contain workload name")
	}

	if !strings.Contains(event.message, "test-ns") {
		t.Errorf("message should contain namespace")
	}

	if !strings.Contains(event.message, "kubectl") {
		t.Errorf("message should contain conflicting manager")
	}

	if !strings.Contains(event.message, "field ownership conflict") {
		t.Errorf("message should contain error message")
	}

	if !strings.Contains(event.message, "Suggestion:") {
		t.Errorf("message should contain actionable suggestion")
	}

	if !strings.Contains(event.message, "Server-Side Apply") {
		t.Errorf("message should mention Server-Side Apply")
	}
}

// Ensure mockEventRecorder implements record.EventRecorder interface
var _ record.EventRecorder = (*mockEventRecorder)(nil)
