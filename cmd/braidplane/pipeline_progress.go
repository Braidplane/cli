package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net"
	"net/http"
	"os"
	"os/signal"
	"time"

	api "github.com/Braidplane/cli/internal/apiclient"
	"github.com/Braidplane/cli/pkg/xerror"
)

type pipelineRunProgressCommand struct {
	apiPipelineFlags
	RunSlug     string `arg:"" name:"run-slug" required:"" help:"Exact attempt run slug."`
	Environment string `name:"environment" env:"BRAIDPLANE_ENVIRONMENT" help:"Project environment slug."`
	AfterEvent  int64  `name:"after-event" default:"0" help:"Number of events already consumed; zero resynchronizes."`
	Limit       int    `name:"limit" default:"100" help:"Maximum events, between 1 and 100."`
	Output      string `name:"output" default:"json" help:"Output format: json or text."`
}

func (c *pipelineRunProgressCommand) Run(output *cliOutput, runtime *cliRuntime) error {
	if c.AfterEvent < 0 || c.Limit < 1 || c.Limit > 100 {
		return xerror.InvalidInput(xerror.PipelineRun).Detail("after-event must be nonnegative and limit must be between 1 and 100")
	}
	client, project, environment, err := newPipelineEnvironmentAPIClient(c.apiPipelineFlags, c.Environment, runtime)
	if err != nil {
		return xerror.Wrapf("pipeline progress: %w", err)
	}
	progress, err := client.getPipelineRunProgress(context.Background(), project, environment, c.RunSlug, c.AfterEvent, c.Limit)
	if err != nil {
		return xerror.Wrapf("pipeline progress: %w", err)
	}
	return writePipelineRunProgress(output.stdout, progress, c.Output)
}

func (c *cliAPIClient) getPipelineRunProgress(ctx context.Context, project, environment, run string, after int64, limit int) (*api.PipelineRunProgress, error) {
	response, err := c.openapi.GetPipelineRunProgressWithResponse(ctx, c.organization, project, environment, run, &api.GetPipelineRunProgressParams{AfterEvent: &after, Limit: &limit})
	if err != nil {
		return nil, xerror.Wrapf("read pipeline run progress: %w", err)
	}
	if response.StatusCode() != http.StatusOK {
		return nil, &cliAPIStatusError{statusCode: response.StatusCode(), err: decodeAPIError(response.StatusCode(), response.Body)}
	}
	progress := response.JSON200
	if progress == nil {
		return nil, xerror.Unexpected().Detail("missing pipeline run progress")
	}
	if progress.Metadata.RunSlug != run || progress.AfterEvent != after || progress.NextAfterEvent < after || progress.NextAfterEvent > progress.EventCount || progress.NextAfterEvent-after != int64(len(progress.Events)) || len(progress.Events) > limit || (progress.HasMore && progress.NextAfterEvent == after) {
		return nil, xerror.Unexpected().Detail("pipeline run progress identity or cursor is inconsistent")
	}
	return progress, nil
}

func (c *watchPipelineRunCommand) Run(output *cliOutput, runtime *cliRuntime) error {
	if c.PollIntervalSec <= 0 || c.TimeoutSec < 0 {
		return xerror.InvalidInput(xerror.PipelineRun).Detail("poll-interval-seconds must be positive and timeout-seconds must be nonnegative")
	}
	if int64(c.PollIntervalSec) > math.MaxInt64/int64(time.Second) || int64(c.TimeoutSec) > math.MaxInt64/int64(time.Second) {
		return xerror.InvalidInput(xerror.PipelineRun).Detail("poll interval or timeout exceeds supported duration")
	}
	if c.Output != "json" && c.Output != "text" {
		return xerror.InvalidInput(xerror.JSON).Detail("output must be json or text")
	}
	client, project, environment, err := newPipelineEnvironmentAPIClient(c.apiPipelineFlags, c.Environment, runtime)
	if err != nil {
		return xerror.Wrapf("pipeline progress: %w", err)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	if c.TimeoutSec > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, time.Duration(c.TimeoutSec)*time.Second)
		defer cancel()
	}
	return c.watchProgress(ctx, output, client, project, environment, time.Duration(c.PollIntervalSec)*time.Second)
}

func (c *watchPipelineRunCommand) watchProgress(ctx context.Context, output *cliOutput, client *cliAPIClient, project, environment string, interval time.Duration) error {
	after, revision := int64(0), int64(-1)
	lastObservation, lastLiveness, lastStatus := "", "", ""
	failures := 0
	for {
		progress, err := client.getPipelineRunProgress(ctx, project, environment, c.RunSlug, after, 100)
		delay := interval
		if ctx.Err() != nil {
			return watchInterrupted(ctx, c.RunSlug)
		}
		if err != nil {
			var status *cliAPIStatusError
			var network net.Error
			if xerror.As(err, &status) && status.statusCode == http.StatusBadRequest && after > 0 {
				if _, err := fmt.Fprintln(output.stderr, "Progress history changed; resynchronizing from zero."); err != nil {
					return xerror.Wrapf("pipeline progress: %w", err)
				}
				after, revision = 0, -1
				continue
			}
			transient := (xerror.As(err, &status) && (status.statusCode == http.StatusTooManyRequests || status.statusCode >= 500)) || xerror.As(err, &network)
			if !transient {
				return xerror.Wrapf("watch pipeline run %s: %w", c.RunSlug, err)
			}
			failures++
			if failures == 1 {
				if _, err := fmt.Fprintln(output.stderr, "Progress unavailable; reconnecting from the retained cursor."); err != nil {
					return xerror.Wrapf("pipeline progress: %w", err)
				}
			}
			delay = min(min(interval, 30*time.Second)*time.Duration(1<<min(failures-1, 3)), 30*time.Second)
		} else {
			if failures > 0 {
				if _, err := fmt.Fprintln(output.stderr, "Progress reconnected."); err != nil {
					return xerror.Wrapf("pipeline progress: %w", err)
				}
				failures = 0
			}
			if progress.Revision < revision {
				return xerror.Unexpected().Detail("pipeline run progress revision regressed")
			}
			observation := ""
			if progress.WorkerObservedAt != nil {
				observation = progress.WorkerObservedAt.Format(time.RFC3339Nano)
			}
			if progress.Revision != revision || len(progress.Events) > 0 || observation != lastObservation || string(progress.WorkerLiveness) != lastLiveness || string(progress.Metadata.Status) != lastStatus {
				if err := writePipelineRunProgress(output.stdout, progress, c.Output); err != nil {
					return xerror.Wrapf("pipeline progress: %w", err)
				}
			}
			after, revision = progress.NextAfterEvent, progress.Revision
			lastObservation, lastLiveness, lastStatus = observation, string(progress.WorkerLiveness), string(progress.Metadata.Status)
			if progress.HasMore {
				continue
			}
			if pipelineRunTerminalStatus(string(progress.Metadata.Status)) {
				if progress.Metadata.Status == "failed" || progress.Metadata.Status == "canceled" {
					return xerror.ExecutionFailed().Detailf("pipeline run %s %s", c.RunSlug, progress.Metadata.Status)
				}
				return nil
			}
		}
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return watchInterrupted(ctx, c.RunSlug)
		case <-timer.C:
		}
	}
}

func watchInterrupted(ctx context.Context, run string) error {
	reason := "interrupted"
	if xerror.Is(ctx.Err(), context.DeadlineExceeded) {
		reason = "timed out"
	}
	return xerror.ExecutionUnavailable().Detailf("%s watching pipeline run %s; execution was not canceled", reason, run)
}

func writePipelineRunProgress(writer io.Writer, progress *api.PipelineRunProgress, format string) error {
	switch format {
	case "json":
		if err := json.NewEncoder(writer).Encode(progress); err != nil {
			return xerror.Wrapf("encode pipeline progress: %w", err)
		}
		return nil
	case "text":
		observed := "unavailable"
		if progress.ObservedAt != nil {
			observed = progress.ObservedAt.Format(time.RFC3339Nano)
		}
		elapsed := progress.Metadata.DurationMs
		if progress.Metadata.Status == "running" || progress.Metadata.Status == "queued" {
			elapsed = max(int64(0), progress.ServerTime.Sub(progress.Metadata.StartedAt).Milliseconds())
		}
		if _, err := fmt.Fprintf(writer, "Run %s · root %s · attempt %s · %s · last step %s · elapsed %dms · worker %s · observed %s · revision %d\n", progress.Metadata.RunSlug, pipelineRunSourceRootRunText(progress.Metadata.Source), pipelineRunSourceAttemptText(progress.Metadata.Source), progress.Metadata.Status, progress.Metadata.StepSlug, elapsed, progress.WorkerLiveness, observed, progress.Revision); err != nil {
			return xerror.Wrapf("pipeline progress: %w", err)
		}
		if progress.Metadata.StepCount > 0 {
			if _, err := fmt.Fprintf(writer, "  Steps: %d completed · %d failed · %d pending · %d interrupted\n", progress.CompletedSteps, progress.FailedSteps, progress.PendingSteps, progress.InterruptedSteps); err != nil {
				return xerror.Wrapf("pipeline progress: %w", err)
			}
		}
		if progress.WorkerLiveness == "stale" || progress.WorkerLiveness == "unavailable" {
			if _, err := fmt.Fprintln(writer, "  Worker liveness is unknown; a quiet step does not prove worker loss."); err != nil {
				return xerror.Wrapf("pipeline progress: %w", err)
			}
		}
		for i, event := range progress.Events {
			step := ""
			if event.Step != nil {
				step = *event.Step
			}
			if _, err := fmt.Fprintf(writer, "  %d %s %s %s %s\n", progress.AfterEvent+int64(i)+1, event.At.Format(time.RFC3339Nano), event.Type, step, event.Status); err != nil {
				return xerror.Wrapf("pipeline progress: %w", err)
			}
		}
		if progress.Metadata.ErrorSummary != nil {
			if _, err := fmt.Fprintln(writer, *progress.Metadata.ErrorSummary); err != nil {
				return xerror.Wrapf("write pipeline progress error summary: %w", err)
			}
		}
		return nil
	default:
		return xerror.InvalidInput(xerror.JSON).Detail("output must be json or text")
	}
}
