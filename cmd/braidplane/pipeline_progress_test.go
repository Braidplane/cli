package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	api "github.com/Braidplane/cli/internal/apiclient"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWatchProgressStreamsBoundariesAndReconnectsWithoutRepeatingEvents(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		assert.Equal(t, http.MethodGet, r.Method)
		assert.Equal(t, "/organizations/acme/projects/ops/environments/staging/runs/attempt-2/progress", r.URL.Path)
		assert.Equal(t, "Bearer synthetic-api-key", r.Header.Get("Authorization"))
		after := r.URL.Query().Get("after_event")
		if requests == 1 {
			assert.Equal(t, "0", after)
		} else {
			assert.Equal(t, "1", after)
		}
		w.Header().Set("Content-Type", "application/json")
		if requests == 2 {
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = w.Write([]byte(`{"error":{"code":"UNAVAILABLE","message":"unavailable"}}`))
			return
		}
		event := api.PipelineRunTimelineEvent{At: time.Now(), Type: "step_started", Status: "running"}
		progress := api.PipelineRunProgress{Metadata: api.PipelineRunMetadata{RunSlug: "attempt-2", PipelineVersionSpecHash: "recorded-hash", Status: "running"}, Revision: 2, Events: []api.PipelineRunTimelineEvent{event}, EventCount: 1, NextAfterEvent: 1, WorkerLiveness: "recent"}
		if requests == 3 {
			progress.Metadata.Status = "failed"
			progress.Revision = 3
			progress.AfterEvent = 1
			progress.NextAfterEvent = 2
			progress.EventCount = 2
			progress.Events[0].Type = "step_failed"
			progress.Events[0].Status = "failed"
			progress.WorkerLiveness = "terminal"
		}
		assert.NoError(t, json.NewEncoder(w).Encode(progress))
	}))
	defer server.Close()
	client, err := newCLIAPIClient(cliAuthConfig{ServerURL: server.URL, Organization: "acme", APIKey: "synthetic-api-key"})
	require.NoError(t, err)
	var stdout, stderr bytes.Buffer
	command := watchPipelineRunCommand{RunSlug: "attempt-2", Output: "json"}
	err = command.watchProgress(t.Context(), &cliOutput{stdout: &stdout, stderr: &stderr}, client, "ops", "staging", time.Millisecond)
	require.ErrorContains(t, err, "attempt-2 failed")
	require.Equal(t, 3, requests)
	require.Contains(t, stderr.String(), "reconnecting")
	require.Contains(t, stderr.String(), "reconnected")
	decoder := json.NewDecoder(&stdout)
	for i, status := range []api.PipelineRunMetadataStatus{"running", "failed"} {
		var progress api.PipelineRunProgress
		require.NoError(t, decoder.Decode(&progress))
		require.Equal(t, status, progress.Metadata.Status)
		require.Equal(t, "recorded-hash", progress.Metadata.PipelineVersionSpecHash)
		require.EqualValues(t, i, progress.AfterEvent)
		require.Len(t, progress.Events, 1)
	}
	var extra any
	require.ErrorIs(t, decoder.Decode(&extra), io.EOF)
}

func TestWatchProgressDenialFailsWithoutRetryAndInterruptionDoesNotCancelRun(t *testing.T) {
	for _, denied := range []bool{true, false} {
		t.Run(map[bool]string{true: "denied", false: "interrupted"}[denied], func(t *testing.T) {
			requests := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests++
				assert.Equal(t, http.MethodGet, r.Method)
				w.Header().Set("Content-Type", "application/json")
				if denied {
					w.WriteHeader(http.StatusForbidden)
					_, _ = w.Write([]byte(`{"error":{"code":"FORBIDDEN","message":"forbidden"}}`))
					return
				}
				assert.NoError(t, json.NewEncoder(w).Encode(api.PipelineRunProgress{Metadata: api.PipelineRunMetadata{RunSlug: "quiet", Status: "running"}, Events: []api.PipelineRunTimelineEvent{}, WorkerLiveness: "stale"}))
			}))
			defer server.Close()
			client, err := newCLIAPIClient(cliAuthConfig{ServerURL: server.URL, Organization: "acme", APIKey: "synthetic-api-key"})
			require.NoError(t, err)
			ctx, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
			defer cancel()
			var stdout, stderr bytes.Buffer
			command := watchPipelineRunCommand{RunSlug: "quiet", Output: "json"}
			err = command.watchProgress(ctx, &cliOutput{stdout: &stdout, stderr: &stderr}, client, "ops", "staging", time.Second)
			require.Error(t, err)
			require.Equal(t, 1, requests)
			require.Empty(t, stderr.String())
			if denied {
				require.Contains(t, err.Error(), "403")
				require.Empty(t, stdout.String())
			} else {
				require.Contains(t, err.Error(), "execution was not canceled")
				require.Contains(t, stdout.String(), `"worker_liveness":"stale"`)
			}
		})
	}
}

func TestWatchProgressRejectsOverflowingDurations(t *testing.T) {
	command := watchPipelineRunCommand{PollIntervalSec: int(^uint(0) >> 1), Output: "json"}
	require.ErrorContains(t, command.Run(&cliOutput{}, &cliRuntime{}), "exceeds supported duration")
	command.PollIntervalSec = 2
	command.TimeoutSec = int(^uint(0) >> 1)
	require.ErrorContains(t, command.Run(&cliOutput{}, &cliRuntime{}), "exceeds supported duration")
}
