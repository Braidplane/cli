package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	api "github.com/Braidplane/cli/internal/apiclient"
	pipelinediff "github.com/Braidplane/cli/pkg/pipeline/diff"
	pipelineruntimevars "github.com/Braidplane/cli/pkg/pipeline/runtime"
	pipelinespec "github.com/Braidplane/cli/pkg/pipeline/spec"
	pipelinevalidate "github.com/Braidplane/cli/pkg/pipeline/validate"
	"github.com/Braidplane/cli/pkg/xerror"
)

const unavailablePipelineRunSourceLabel = "unavailable"

type pipelineCommand struct {
	List       listPipelinesCommand             `cmd:"" help:"List pipelines from the Braidplane API."`
	Get        getPipelineCommand               `cmd:"" help:"Get a pipeline from the Braidplane API."`
	Update     updatePipelineCommand            `cmd:"" help:"Propose a pipeline update through the Braidplane API."`
	Draft      draftPipelineCommand             `cmd:"" help:"Manage pipeline drafts through the Braidplane API."`
	Run        runPipelineCommand               `cmd:"" help:"Inspect persisted pipeline runs through the Braidplane API."`
	Trigger    triggerPipelineCommand           `cmd:"" help:"Manage environment-scoped pipeline triggers through the Braidplane API."`
	Version    versionPipelineCommand           `cmd:"" help:"Inspect immutable pipeline versions through the Braidplane API."`
	Apply      applyPipelineCommand             `cmd:"" help:"Materialize Git-authored pipelines from the project backend into Braidplane."`
	Deploy     deployPipelineCommand            `cmd:"" help:"Deploy an immutable pipeline version to an environment."`
	Deployment pipelineDeploymentCommand        `cmd:"" help:"Inspect environment deployment state and history."`
	Validation pipelineValidationHistoryCommand `cmd:"" help:"Inspect and refresh persisted validation history."`
	Drift      pipelineDriftCommand             `cmd:"" help:"Inspect drift and impact reports."`
	Validate   validateCommand                  `cmd:"" help:"Validate pipeline specs without running a server."`
	Format     formatPipelineCommand            `cmd:"" help:"Format a pipeline spec through the Braidplane API."`
	Diff       diffPipelineCommand              `cmd:"" help:"Compare a local pipeline spec with the upstream Braidplane API pipeline."`
}

type draftPipelineCommand struct {
	Get     getPipelineDraftCommand     `cmd:"" help:"Get a pipeline draft from the Braidplane API."`
	Save    savePipelineDraftCommand    `cmd:"" help:"Save a pipeline draft through the Braidplane API."`
	Publish publishPipelineDraftCommand `cmd:"" help:"Propose a pipeline draft as Git branch work through the Braidplane API."`
	Discard discardPipelineDraftCommand `cmd:"" help:"Discard a pipeline draft through the Braidplane API."`
}

type versionPipelineCommand struct {
	List listPipelineVersionsCommand `cmd:"" help:"List immutable versions for a pipeline from the Braidplane API."`
	Get  getPipelineVersionCommand   `cmd:"" help:"Get an immutable pipeline version from the Braidplane API."`
}

type runPipelineCommand struct {
	List          listPipelineRunsCommand             `cmd:"" help:"List persisted runs for a pipeline from the Braidplane API."`
	Get           getPipelineRunCommand               `cmd:"" help:"Get persisted pipeline run details from the Braidplane API."`
	Watch         watchPipelineRunCommand             `cmd:"" help:"Poll a pipeline run until it reaches a terminal status."`
	Start         startPipelineRunCommand             `cmd:"" help:"Start a pipeline run through the Braidplane API."`
	RetryAttempts listPipelineRunRetryAttemptsCommand `cmd:"" name:"retry-attempts" help:"List trigger retry attempts for a pipeline run retry group."`
}

func newPipelineAPIClient(
	flags apiPipelineFlags,
	runtime *cliRuntime,
) (*cliAPIClient, string, error) {
	projectSlug, err := flags.resolveProjectSlug()
	if err != nil {
		return nil, "", err
	}

	cfg, err := flags.resolveAuthConfig(runtime)
	if err != nil {
		return nil, "", err
	}

	client, err := newCLIAPIClient(cfg)
	if err != nil {
		return nil, "", err
	}

	return client, projectSlug, nil
}

// newPipelineEnvironmentAPIClient resolves the shared auth, project, and environment scope.
func newPipelineEnvironmentAPIClient(
	flags apiPipelineFlags,
	environment string,
	runtime *cliRuntime,
) (*cliAPIClient, string, string, error) {
	client, projectSlug, err := newPipelineAPIClient(flags, runtime)
	if err != nil {
		return nil, "", "", err
	}
	environmentSlug, err := runtime.resolveEnvironmentSlug(environment)
	if err != nil {
		return nil, "", "", err
	}

	return client, projectSlug, environmentSlug, nil
}

// newPipelineEnvironmentPATAPIClient resolves user-scoped auth for pipeline
// operations whose HTTP contract rejects organization API-key principals.
func newPipelineEnvironmentPATAPIClient(
	flags apiPipelineFlags,
	environment string,
	runtime *cliRuntime,
) (*cliAPIClient, string, string, error) {
	client, projectSlug, err := newPipelinePATAPIClient(flags, runtime)
	if err != nil {
		return nil, "", "", err
	}
	environmentSlug, err := runtime.resolveEnvironmentSlug(environment)
	if err != nil {
		return nil, "", "", err
	}

	return client, projectSlug, environmentSlug, nil
}

func newPipelinePATAPIClient(
	flags apiPipelineFlags,
	runtime *cliRuntime,
) (*cliAPIClient, string, error) {
	projectSlug, err := flags.resolveProjectSlug()
	if err != nil {
		return nil, "", err
	}

	cfg, err := flags.resolvePATAuthConfig(runtime)
	if err != nil {
		return nil, "", err
	}

	client, err := newCLIAPIClient(cfg)
	if err != nil {
		return nil, "", err
	}

	return client, projectSlug, nil
}

func newAPIClient(
	flags apiAuthFlags,
	runtime *cliRuntime,
) (*cliAPIClient, error) {
	cfg, err := flags.resolveAuthConfig(runtime)
	if err != nil {
		return nil, err
	}

	client, err := newCLIAPIClient(cfg)
	if err != nil {
		return nil, err
	}

	return client, nil
}

type listPipelinesCommand struct {
	apiPipelineFlags

	Limit     int32  `help:"Maximum number of pipelines to return." name:"limit"`
	Cursor    string `help:"Pagination cursor from a previous list response." name:"cursor"`
	Direction string `help:"Pagination direction." name:"direction"`
}

func (c *listPipelinesCommand) Run(output *cliOutput, runtime *cliRuntime) error {
	client, projectSlug, err := newPipelineAPIClient(c.apiPipelineFlags, runtime)
	if err != nil {
		return err
	}

	params := api.ListPipelinesParams{}
	if c.Limit < 0 {
		return xerror.Unexpected().Detail("limit must be zero or greater")
	}
	if c.Limit > 0 {
		limit := c.Limit
		params.Limit = &limit
	}
	if c.Cursor != "" {
		cursor := c.Cursor
		params.Cursor = &cursor
	}
	if c.Direction != "" {
		if c.Direction != string(api.ListPipelinesParamsDirectionNext) &&
			c.Direction != string(api.ListPipelinesParamsDirectionPrev) {
			return xerror.Unexpected().Detail("direction must be next or prev")
		}
		direction := api.ListPipelinesParamsDirection(c.Direction)
		params.Direction = &direction
	}

	response, err := client.listPipelines(context.Background(), projectSlug, &params)
	if err != nil {
		return err
	}

	return writeCLIJSON(output.stdout, response)
}

type getPipelineCommand struct {
	apiPipelineFlags

	PipelineSlug string `arg:"" name:"pipeline-slug" help:"Pipeline slug." required:""`
}

func (c *getPipelineCommand) Run(output *cliOutput, runtime *cliRuntime) error {
	client, projectSlug, err := newPipelineAPIClient(c.apiPipelineFlags, runtime)
	if err != nil {
		return err
	}

	pipeline, err := client.getPipeline(context.Background(), projectSlug, c.PipelineSlug)
	if err != nil {
		return err
	}

	return writeCLIJSON(output.stdout, pipeline)
}

type updatePipelineCommand struct {
	apiPipelineFlags

	PipelineSlug string `arg:"" name:"pipeline-slug" help:"Pipeline slug." required:""`
	File         string `help:"Path to a pipeline spec file." required:"" type:"path"`
}

func (c *updatePipelineCommand) Run(output *cliOutput, runtime *cliRuntime) error {
	_, spec, err := loadUpdatePipelineSpecs(c.File, c.PipelineSlug)
	if err != nil {
		return err
	}

	client, projectSlug, err := newPipelineAPIClient(c.apiPipelineFlags, runtime)
	if err != nil {
		return err
	}

	pipeline, err := client.updatePipeline(
		context.Background(),
		projectSlug,
		c.PipelineSlug,
		api.UpdatePipeline{Spec: spec},
	)
	if err != nil {
		return err
	}

	return writeCLIJSON(output.stdout, pipeline)
}

type listPipelineVersionsCommand struct {
	apiPipelineFlags

	PipelineSlug string `arg:"" name:"pipeline-slug" help:"Pipeline slug." required:""`
}

func (c *listPipelineVersionsCommand) Run(output *cliOutput, runtime *cliRuntime) error {
	client, projectSlug, err := newPipelineAPIClient(c.apiPipelineFlags, runtime)
	if err != nil {
		return err
	}

	versions, err := client.listPipelineVersions(context.Background(), projectSlug, c.PipelineSlug)
	if err != nil {
		return err
	}

	return writeCLIJSON(output.stdout, versions)
}

type listPipelineRunsCommand struct {
	apiPipelineFlags

	PipelineSlug       string `arg:"" name:"pipeline-slug" help:"Pipeline slug." required:""`
	Environment        string `help:"Project environment slug." name:"environment" env:"BRAIDPLANE_ENVIRONMENT"`
	Limit              int32  `help:"Maximum number of runs to return." name:"limit"`
	Cursor             string `help:"Pagination cursor from a previous list response." name:"cursor"`
	Direction          string `help:"Pagination direction." name:"direction"`
	Status             string `help:"Filter by run status: queued, running, succeeded, failed, or canceled." name:"status"`
	Mode               string `help:"Filter by run execution mode: full, incremental, or backfill." name:"mode"`
	BackfillSeriesSlug string `help:"Filter by backfill series slug." name:"backfill-series-slug"`
	SortBy             string `help:"Sort field: run_created_at." name:"sort-by"`
	Order              string `help:"Sort order: asc or desc." name:"order"`
}

func (c *listPipelineRunsCommand) Run(output *cliOutput, runtime *cliRuntime) error {
	client, projectSlug, environmentSlug, err := newPipelineEnvironmentAPIClient(
		c.apiPipelineFlags,
		c.Environment,
		runtime,
	)
	if err != nil {
		return err
	}

	params := api.ListPipelineRunsParams{}
	if c.Limit < 0 {
		return xerror.Unexpected().Detail("limit must be zero or greater")
	}
	if c.Limit > 0 {
		limit := c.Limit
		params.Limit = &limit
	}
	if c.Cursor != "" {
		cursor := c.Cursor
		params.Cursor = &cursor
	}
	if c.Direction != "" {
		if c.Direction != string(api.ListPipelineRunsParamsDirectionNext) &&
			c.Direction != string(api.ListPipelineRunsParamsDirectionPrev) {
			return xerror.Unexpected().Detail("direction must be next or prev")
		}
		direction := api.ListPipelineRunsParamsDirection(c.Direction)
		params.Direction = &direction
	}
	if c.Status != "" {
		status := api.ListPipelineRunsParamsStatus(c.Status)
		if !status.Valid() {
			return xerror.Unexpected().Detail("status must be queued, running, succeeded, failed, or canceled")
		}
		params.Status = &status
	}
	if c.Mode != "" {
		mode := api.PipelineRunMode(c.Mode)
		if !mode.Valid() {
			return xerror.Unexpected().Detail("mode must be full, incremental, or backfill")
		}
		params.Mode = &mode
	}
	if c.BackfillSeriesSlug != "" {
		backfillSeriesSlug := c.BackfillSeriesSlug
		params.BackfillSeriesSlug = &backfillSeriesSlug
	}
	if c.SortBy != "" {
		sortBy := api.ListPipelineRunsParamsSortBy(c.SortBy)
		if !sortBy.Valid() {
			return xerror.Unexpected().Detail("sort-by must be run_created_at")
		}
		params.SortBy = &sortBy
	}
	if c.Order != "" {
		order := api.ListPipelineRunsParamsOrder(c.Order)
		if !order.Valid() {
			return xerror.Unexpected().Detail("order must be asc or desc")
		}
		params.Order = &order
	}
	pipelineSlug := c.PipelineSlug
	params.PipelineSlug = &pipelineSlug

	runs, err := client.listPipelineRuns(context.Background(), projectSlug, environmentSlug, &params)
	if err != nil {
		if c.Cursor != "" {
			return xerror.Wrapf("list pipeline runs for %s with cursor %q: %w", c.PipelineSlug, c.Cursor, err)
		}
		return xerror.Wrapf("list pipeline runs for %s: %w", c.PipelineSlug, err)
	}

	return writeCLIJSON(output.stdout, runs)
}

type getPipelineRunCommand struct {
	apiPipelineFlags

	RunSlug     string `arg:"" name:"run-slug" help:"Run slug." required:""`
	Environment string `help:"Project environment slug." name:"environment" env:"BRAIDPLANE_ENVIRONMENT"`
	Output      string `help:"Output format: json or text." name:"output" default:"json"`
}

func (c *getPipelineRunCommand) Run(output *cliOutput, runtime *cliRuntime) error {
	client, projectSlug, environmentSlug, err := newPipelineEnvironmentAPIClient(
		c.apiPipelineFlags,
		c.Environment,
		runtime,
	)
	if err != nil {
		return err
	}

	run, err := client.getPipelineRun(context.Background(), projectSlug, environmentSlug, c.RunSlug)
	if err != nil {
		return xerror.Wrapf("get pipeline run %s: %w", c.RunSlug, err)
	}

	return writePipelineRunOutput(output.stdout, run, c.Output)
}

type watchPipelineRunCommand struct {
	apiPipelineFlags

	RunSlug         string `arg:"" name:"run-slug" help:"Run slug." required:""`
	Environment     string `help:"Project environment slug." name:"environment" env:"BRAIDPLANE_ENVIRONMENT"`
	PollIntervalSec int    `help:"Polling interval in seconds." name:"poll-interval-seconds" default:"2"`
	TimeoutSec      int    `help:"Optional timeout in seconds; zero waits indefinitely." name:"timeout-seconds" default:"0"`
	Output          string `help:"Output format: json or text." name:"output" default:"json"`
}

func (c *watchPipelineRunCommand) Run(output *cliOutput, runtime *cliRuntime) error {
	client, projectSlug, environmentSlug, err := newPipelineEnvironmentAPIClient(
		c.apiPipelineFlags,
		c.Environment,
		runtime,
	)
	if err != nil {
		return err
	}
	if c.PollIntervalSec <= 0 {
		return xerror.Unexpected().Detail("poll-interval-seconds must be greater than zero")
	}
	if c.TimeoutSec < 0 {
		return xerror.Unexpected().Detail("timeout-seconds must be zero or greater")
	}

	ctx := context.Background()
	if c.TimeoutSec > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, time.Duration(c.TimeoutSec)*time.Second)
		defer cancel()
	}

	ticker := time.NewTicker(time.Duration(c.PollIntervalSec) * time.Second)
	defer ticker.Stop()

	for {
		run, err := client.getPipelineRun(ctx, projectSlug, environmentSlug, c.RunSlug)
		if err != nil {
			return xerror.Wrapf("watch pipeline run %s: %w", c.RunSlug, err)
		}
		if pipelineRunTerminalStatus(string(run.Metadata.Status)) {
			if writeErr := writePipelineRunOutput(output.stdout, run, c.Output); writeErr != nil {
				return writeErr
			}
			if string(run.Metadata.Status) == "failed" || string(run.Metadata.Status) == "canceled" {
				return xerror.ExecutionFailed().Detailf("pipeline run %s %s", c.RunSlug, run.Metadata.Status)
			}
			return nil
		}

		select {
		case <-ctx.Done():
			return xerror.ExecutionUnavailable().Detailf("timed out waiting for pipeline run %s", c.RunSlug)
		case <-ticker.C:
		}
	}
}

func pipelineRunTerminalStatus(status string) bool {
	switch strings.TrimSpace(status) {
	case "succeeded", "failed", "canceled":
		return true
	default:
		return false
	}
}

type listPipelineRunRetryAttemptsCommand struct {
	apiPipelineFlags

	RunSlug     string `arg:"" name:"run-slug" help:"Run slug from the retry group." required:""`
	Environment string `help:"Project environment slug." name:"environment" env:"BRAIDPLANE_ENVIRONMENT"`
}

// Run lists trigger retry-attempt diagnostics for the selected run retry group.
func (c *listPipelineRunRetryAttemptsCommand) Run(output *cliOutput, runtime *cliRuntime) error {
	client, projectSlug, environmentSlug, err := newPipelineEnvironmentAPIClient(
		c.apiPipelineFlags,
		c.Environment,
		runtime,
	)
	if err != nil {
		return err
	}

	attempts, err := client.listPipelineRunTriggerRetryAttempts(
		context.Background(),
		projectSlug,
		environmentSlug,
		c.RunSlug,
	)
	if err != nil {
		return xerror.Wrapf("list pipeline run retry attempts for %s: %w", c.RunSlug, err)
	}

	return writeCLIJSON(output.stdout, attempts)
}

type startPipelineRunCommand struct {
	apiPipelineFlags

	PipelineSlug string `arg:"" name:"pipeline-slug" help:"Pipeline slug." required:""`
	Environment  string `help:"Project environment slug." name:"environment" env:"BRAIDPLANE_ENVIRONMENT"`
	RunSlug      string `help:"Optional run slug override." name:"run-slug"`
	Confirm      string `help:"Exact environment slug required when running in a protected environment." name:"confirm-environment"`
	InputJSON    string `help:"Non-secret runtime input JSON object available as {{runtime.input.key}}." name:"input-json"`
	Output       string `help:"Output format: json or text." name:"output" default:"json"`
}

func (c *startPipelineRunCommand) Run(output *cliOutput, runtime *cliRuntime) error {
	client, projectSlug, environmentSlug, err := newPipelineEnvironmentAPIClient(
		c.apiPipelineFlags,
		c.Environment,
		runtime,
	)
	if err != nil {
		return err
	}

	body := api.ExecutePipelineRequest{}
	if c.RunSlug != "" {
		runSlug := c.RunSlug
		body.RunSlug = &runSlug
	}
	if c.InputJSON != "" {
		input, err := parseRuntimeInputJSON(c.InputJSON)
		if err != nil {
			return err
		}
		body.Input = &input
	}
	body.ProtectedEnvironmentConfirmation = optionalAPISlug(c.Confirm)

	run, err := client.executePipeline(context.Background(), projectSlug, environmentSlug, c.PipelineSlug, body)
	if err != nil {
		return xerror.Wrapf("start pipeline run for %s: %w", c.PipelineSlug, err)
	}

	return writePipelineRunOutput(output.stdout, run, c.Output)
}

func parseRuntimeInputJSON(raw string) (map[string]interface{}, error) {
	var input map[string]interface{}
	if err := json.Unmarshal([]byte(raw), &input); err != nil {
		return nil, xerror.InvalidInput(xerror.JSON).Detailf("input-json must be a JSON object: %v", err)
	}
	if input == nil {
		return nil, xerror.InvalidInput(xerror.JSON).Detail("input-json must be a JSON object")
	}
	if err := pipelineruntimevars.ValidateInput(input); err != nil {
		return nil, err
	}
	return input, nil
}

type getPipelineVersionCommand struct {
	apiPipelineFlags

	PipelineSlug string `arg:"" name:"pipeline-slug" help:"Pipeline slug." required:""`
	SpecHash     string `arg:"" name:"spec-hash" help:"Full immutable pipeline spec hash or unique short hash prefix." required:""`
}

func (c *getPipelineVersionCommand) Run(output *cliOutput, runtime *cliRuntime) error {
	client, projectSlug, err := newPipelineAPIClient(c.apiPipelineFlags, runtime)
	if err != nil {
		return err
	}

	version, err := client.getPipelineVersion(context.Background(), projectSlug, c.PipelineSlug, c.SpecHash)
	if err != nil {
		return err
	}

	return writeCLIJSON(output.stdout, version)
}

type getPipelineDraftCommand struct {
	apiPipelineFlags

	PipelineSlug string `arg:"" name:"pipeline-slug" help:"Pipeline slug." required:""`
}

func (c *getPipelineDraftCommand) Run(output *cliOutput, runtime *cliRuntime) error {
	client, projectSlug, err := newPipelinePATAPIClient(c.apiPipelineFlags, runtime)
	if err != nil {
		return err
	}

	draft, err := client.getPipelineDraft(context.Background(), projectSlug, c.PipelineSlug)
	if err != nil {
		return err
	}

	return writeCLIJSON(output.stdout, draft)
}

type discardPipelineDraftCommand struct {
	apiPipelineFlags

	PipelineSlug string `arg:"" name:"pipeline-slug" help:"Pipeline slug." required:""`
}

func (c *discardPipelineDraftCommand) Run(_ *cliOutput, runtime *cliRuntime) error {
	client, projectSlug, err := newPipelinePATAPIClient(c.apiPipelineFlags, runtime)
	if err != nil {
		return err
	}

	return client.deletePipelineDraft(context.Background(), projectSlug, c.PipelineSlug)
}

type savePipelineDraftCommand struct {
	apiPipelineFlags

	PipelineSlug     string  `arg:"" name:"pipeline-slug" help:"Pipeline slug." required:""`
	File             string  `help:"Path to a pipeline spec file." required:"" type:"path"`
	ExpectedSpecHash *string `help:"Canonical pipeline spec hash the editor was opened from." name:"expected-spec-hash"`
}

func (c *savePipelineDraftCommand) Run(output *cliOutput, runtime *cliRuntime) error {
	spec, err := loadDraftSavePipelineSpec(c.File, c.PipelineSlug)
	if err != nil {
		return err
	}

	client, projectSlug, err := newPipelinePATAPIClient(c.apiPipelineFlags, runtime)
	if err != nil {
		return err
	}

	draft, err := client.savePipelineDraft(context.Background(), projectSlug, c.PipelineSlug, api.SavePipelineDraftRequest{
		Spec:             spec,
		ExpectedSpecHash: c.ExpectedSpecHash,
	})
	if err != nil {
		return err
	}

	return writeCLIJSON(output.stdout, draft)
}

type publishPipelineDraftCommand struct {
	apiPipelineFlags

	PipelineSlug     string  `arg:"" name:"pipeline-slug" help:"Pipeline slug." required:""`
	ExpectedSpecHash *string `help:"Canonical pipeline spec hash the editor believes is current." name:"expected-spec-hash"`
	Force            *bool   `help:"Queue the Git proposal even when the expected materialized version is stale." name:"force"`
}

func (c *publishPipelineDraftCommand) Run(output *cliOutput, runtime *cliRuntime) error {
	client, projectSlug, err := newPipelinePATAPIClient(c.apiPipelineFlags, runtime)
	if err != nil {
		return err
	}

	pipeline, err := client.publishPipelineDraft(context.Background(), projectSlug, c.PipelineSlug, api.PublishPipelineDraftRequest{
		ExpectedSpecHash: c.ExpectedSpecHash,
		Force:            c.Force,
	})
	if err != nil {
		return err
	}

	return writeCLIJSON(output.stdout, pipeline)
}

type applyPipelineCommand struct {
	apiPipelineFlags

	DryRun      bool     `help:"Compute the deploy-from-git report without mutating DB state." name:"dry-run"`
	Dir         string   `help:"Directory inside the git repository to scan for pipeline specs." name:"dir"`
	Pipelines   []string `help:"Pipeline slug filter. Repeat to apply multiple pipelines." name:"pipeline"`
	Environment string   `help:"Project environment slug for this reconcile/deploy workflow." name:"environment" env:"BRAIDPLANE_ENVIRONMENT"`
}

func (c *applyPipelineCommand) Run(output *cliOutput, runtime *cliRuntime) error {
	client, projectSlug, err := newPipelineAPIClient(c.apiPipelineFlags, runtime)
	if err != nil {
		return err
	}
	environmentSlug, err := runtime.resolveEnvironmentSlug(c.Environment)
	if err != nil {
		return err
	}

	response, err := client.applyPipelines(
		context.Background(),
		projectSlug,
		c.applyRequest(),
	)
	if err != nil {
		return err
	}
	if !response.DryRun {
		if err := c.deployAppliedPipelines(context.Background(), client, projectSlug, environmentSlug, response); err != nil {
			return err
		}
	}

	return writeCLIJSON(output.stdout, response)
}

func (c *applyPipelineCommand) deployAppliedPipelines(
	ctx context.Context,
	client *cliAPIClient,
	projectSlug string,
	environmentSlug string,
	response *api.PipelineReconcileResponse,
) error {
	if response == nil {
		return nil
	}
	for _, synced := range response.Synced {
		_, err := client.deployPipeline(
			ctx,
			projectSlug,
			environmentSlug,
			synced.PipelineSlug,
			api.PipelineDeploymentRequest{
				SpecHash: synced.SpecHash,
			},
		)
		if err != nil {
			return err
		}
	}
	return nil
}

func (c *applyPipelineCommand) applyRequest() api.PipelineReconcileRequest {
	req := api.PipelineReconcileRequest{}
	if c.DryRun {
		dryRun := true
		req.DryRun = &dryRun
	}
	if c.Dir != "" {
		req.PipelineDir = &c.Dir
	}
	if len(c.Pipelines) > 0 {
		pipelineSlugs := make([]api.Slug, len(c.Pipelines))
		copy(pipelineSlugs, c.Pipelines)
		req.PipelineSlugs = &pipelineSlugs
	}

	return req
}

type deployPipelineCommand struct {
	apiPipelineFlags

	PipelineSlug            string  `arg:"" name:"pipeline-slug" help:"Pipeline slug." required:""`
	Environment             string  `help:"Project environment slug to deploy into." name:"environment" env:"BRAIDPLANE_ENVIRONMENT"`
	SpecHash                string  `help:"Immutable pipeline version hash or unique short hash prefix to deploy." name:"spec-hash" required:""`
	ExpectedCurrentSpecHash *string `help:"Optional optimistic guard for the currently deployed version." name:"expected-current-spec-hash"`
	SourceEnvironment       string  `help:"Optional source environment slug for promotion metadata." name:"source-environment"`
	Confirm                 string  `help:"Exact environment slug required when deploying to a protected environment." name:"confirm-environment"`
	ProofReceipt            string  `help:"Optional exact Proof receipt slug to link to this deployment." name:"proof-receipt"`
}

func (c *deployPipelineCommand) Run(output *cliOutput, runtime *cliRuntime) error {
	client, projectSlug, err := newPipelineAPIClient(c.apiPipelineFlags, runtime)
	if err != nil {
		return err
	}
	environmentSlug, err := runtime.resolveEnvironmentSlug(c.Environment)
	if err != nil {
		return err
	}

	body := api.PipelineDeploymentRequest{
		SpecHash:                         c.SpecHash,
		ExpectedCurrentSpecHash:          c.ExpectedCurrentSpecHash,
		ProtectedEnvironmentConfirmation: optionalAPISlug(c.Confirm),
		ProofReceiptSlug:                 optionalAPISlug(c.ProofReceipt),
	}
	if strings.TrimSpace(c.SourceEnvironment) != "" {
		sourceEnvironmentSlug := strings.TrimSpace(c.SourceEnvironment)
		body.SourceEnvironmentSlug = &sourceEnvironmentSlug
	}

	deployment, err := client.deployPipeline(
		context.Background(),
		projectSlug,
		environmentSlug,
		c.PipelineSlug,
		body,
	)
	if err != nil {
		return err
	}

	return writeCLIJSON(output.stdout, deployment)
}

type validateCommand struct {
	apiPipelineFlags

	File        string `help:"Path to a pipeline spec file." type:"path"`
	Dir         string `help:"Path to a directory tree containing pipeline spec files." type:"path"`
	Local       bool   `help:"Validate locally even when an API server target is configured."`
	Environment string `help:"Project environment slug for server-side ref-aware validation." name:"environment" env:"BRAIDPLANE_ENVIRONMENT"`
}

func (c *validateCommand) Validate() error {
	if c.File == "" && c.Dir == "" {
		return xerror.InvalidInput(xerror.Pipeline).Detail("one of --file or --dir is required")
	}
	if c.File != "" && c.Dir != "" {
		return xerror.InvalidInput(xerror.Pipeline).Detail("only one of --file or --dir may be set")
	}

	return nil
}

func (c *validateCommand) Run(output *cliOutput, runtime *cliRuntime) error {
	if c.Dir != "" {
		count, err := pipelinevalidate.ValidateDirectory(c.Dir)
		if err != nil {
			return xerror.Wrapf("validate pipelines: %w", err)
		}

		_, err = fmt.Fprintf(
			output.stdout,
			"pipeline validation succeeded for %d pipeline spec(s) in %s\n",
			count,
			c.Dir,
		)
		if err != nil {
			return xerror.Unexpected().With(err)
		}

		return nil
	}

	if c.shouldUseServerValidation() {
		return c.runServerValidation(output, runtime)
	}

	_, err := pipelinevalidate.LoadFile(c.File)
	if err != nil {
		return xerror.Wrapf("validate pipeline: %w", err)
	}
	_, err = fmt.Fprintf(output.stdout, "pipeline validation succeeded for %s\n", c.File)
	if err != nil {
		return xerror.Unexpected().With(err)
	}

	return nil
}

func (c *validateCommand) shouldUseServerValidation() bool {
	return !c.Local && strings.TrimSpace(c.Project) != ""
}

func (c *validateCommand) runServerValidation(output *cliOutput, runtime *cliRuntime) error {
	spec, err := loadPipelineValidationSpec(c.File)
	if err != nil {
		return err
	}

	client, projectSlug, err := newPipelineAPIClient(c.apiPipelineFlags, runtime)
	if err != nil {
		return err
	}
	environmentSlug, err := runtime.resolveEnvironmentSlug(c.Environment)
	if err != nil {
		return err
	}

	result, err := client.validatePipeline(
		context.Background(),
		projectSlug,
		environmentSlug,
		api.PipelineValidationRequest{Spec: spec},
	)
	if err != nil {
		return err
	}

	if err := writePipelineValidationResult(output.stdout, result); err != nil {
		return err
	}

	if !result.Valid {
		return xerror.InvalidInput(xerror.Pipeline).Detail("pipeline validation failed")
	}

	_, err = fmt.Fprintf(
		output.stdout,
		"pipeline validation succeeded for %s via Braidplane API\n",
		c.File,
	)
	if err != nil {
		return xerror.Unexpected().With(err)
	}

	return nil
}

func resolvePipelineEnvironmentSlug(value string) (string, error) {
	environment := strings.TrimSpace(value)
	if environment == "" {
		return "", xerror.InvalidRequired(xerror.ProjectEnvironment).
			Detail("set --environment or BRAIDPLANE_ENVIRONMENT, or save environment in CLI config")
	}
	return environment, nil
}

func writePipelineValidationResult(
	output io.Writer,
	result *api.PipelineValidationResponse,
) error {
	if result == nil {
		return nil
	}
	if err := writePipelineDataSourceRefSync(output, result.DataSourceRefSync); err != nil {
		return err
	}
	for _, diagnostic := range result.Diagnostics {
		stepSlug := ""
		if diagnostic.StepSlug != nil {
			stepSlug = *diagnostic.StepSlug
		}
		_, writeErr := fmt.Fprintf(
			output,
			"%s %s %s %s: %s\n",
			strings.ToUpper(string(diagnostic.Severity)),
			diagnostic.Field,
			stepSlug,
			diagnostic.Code,
			diagnostic.Message+pipelineValidationDiagnosticDetails(diagnostic),
		)
		if writeErr != nil {
			return xerror.Unexpected().With(writeErr)
		}
	}
	if err := writePipelineValidationImpactReport(output, result.ImpactReport); err != nil {
		return err
	}

	return nil
}

// writePipelineDataSourceRefSync prints deploy-preflight logical ref sync results.
func writePipelineDataSourceRefSync(
	output io.Writer,
	result api.PipelineDataSourceRefSyncResult,
) error {
	for _, ref := range result.DataSourceRefs {
		_, writeErr := fmt.Fprintf(
			output,
			"SYNC data_source_ref %s %s data_source_type=%s capabilities=%s\n",
			ref.DataSourceRefSlug,
			ref.Status,
			pipelineDataSourceRefType(ref.DataSourceType),
			pipelineDataSourceRefCapabilities(ref.RequiredCapabilities),
		)
		if writeErr != nil {
			return xerror.Unexpected().With(writeErr)
		}
	}
	return nil
}

// pipelineDataSourceRefType formats optional sync type metadata for CLI output.
func pipelineDataSourceRefType(dsType *api.DataSourceType) string {
	if dsType == nil {
		return "untyped"
	}
	return string(*dsType)
}

// pipelineDataSourceRefCapabilities formats capability lists for CLI sync output.
func pipelineDataSourceRefCapabilities(capabilities []api.DataSourceRefCapability) string {
	values := make([]string, len(capabilities))
	for i, capability := range capabilities {
		values[i] = string(capability)
	}
	return strings.Join(values, ",")
}

// pipelineValidationDiagnosticDetails appends structured ref details to diagnostics.
func pipelineValidationDiagnosticDetails(diagnostic api.PipelineValidationDiagnostic) string {
	details := make([]string, 0, 4)
	if diagnostic.DataSourceRefSlug != nil {
		details = append(details, "data_source_ref="+*diagnostic.DataSourceRefSlug)
	} else if diagnostic.DataSourceRef != nil {
		details = append(details, "data_source_ref="+*diagnostic.DataSourceRef)
	}
	if diagnostic.EnvironmentSlug != nil {
		details = append(details, "environment="+*diagnostic.EnvironmentSlug)
	}
	if diagnostic.RequiredCapability != nil {
		details = append(details, "required_capability="+string(*diagnostic.RequiredCapability))
	}
	if diagnostic.ExpectedDataSourceType != nil {
		details = append(details, "expected_data_source_type="+string(*diagnostic.ExpectedDataSourceType))
	}
	if len(details) == 0 {
		return ""
	}
	return " (" + strings.Join(details, " ") + ")"
}

func writePipelineValidationImpactReport(output io.Writer, report *api.DriftImpactReportPreview) error {
	if report == nil {
		return nil
	}
	_, err := fmt.Fprintf(
		output,
		"%s impact_report: %s\n",
		strings.ToUpper(string(report.Severity)),
		report.Summary,
	)
	if err != nil {
		return xerror.Unexpected().With(err)
	}
	for _, change := range report.Changes {
		_, err := fmt.Fprintf(
			output,
			"CHANGE %s %s %s %s -> %s reason=%s\n",
			strings.ToUpper(string(change.Severity)),
			change.Kind,
			change.Path,
			formatDriftImpactValue(change.Before),
			formatDriftImpactValue(change.After),
			change.ReasonCode,
		)
		if err != nil {
			return xerror.Unexpected().With(err)
		}
	}
	for _, impact := range report.Impacts {
		target := pipelineValidationImpactTarget(impact)
		_, err := fmt.Fprintf(
			output,
			"IMPACT %s %s %s: %s\n",
			strings.ToUpper(string(impact.Severity)),
			impact.Kind,
			target,
			impact.Message,
		)
		if err != nil {
			return xerror.Unexpected().With(err)
		}
	}
	return nil
}

func formatDriftImpactValue(value *api.DriftImpactValue) string {
	if value == nil {
		return "null"
	}
	var data bytes.Buffer
	encoder := json.NewEncoder(&data)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(value); err != nil {
		return "<invalid>"
	}
	return strings.TrimSuffix(data.String(), "\n")
}

func pipelineValidationImpactTarget(impact api.DriftImpactImpact) string {
	var parts []string
	if impact.PipelineSlug != nil {
		parts = append(parts, "pipeline="+*impact.PipelineSlug)
	}
	if impact.StepSlug != nil {
		parts = append(parts, "step="+*impact.StepSlug)
	}
	if impact.EdgeSlug != nil {
		parts = append(parts, "edge="+*impact.EdgeSlug)
	}
	if len(parts) == 0 {
		return "target=unknown"
	}
	return "target=" + strings.Join(parts, " ")
}

type formatPipelineCommand struct {
	apiAuthFlags

	File   string  `help:"Path to a pipeline spec file." required:"" type:"path"`
	Format *string `help:"Pipeline spec serialization format. Defaults to the file extension." enum:"json,yaml" name:"format"`
}

func (c *formatPipelineCommand) Run(output *cliOutput, runtime *cliRuntime) error {
	format, err := c.resolveFormat()
	if err != nil {
		return err
	}

	data, err := os.ReadFile(c.File)
	if err != nil {
		return xerror.Wrapf("read pipeline: %w", err)
	}

	client, err := newAPIClient(c.apiAuthFlags, runtime)
	if err != nil {
		return err
	}

	result, err := client.formatPipelineSpec(
		context.Background(),
		api.PipelineSpecFormatRequest{
			Format: api.PipelineSpecFormatRequestFormat(format),
			Data:   string(data),
		},
	)
	if err != nil {
		return err
	}
	if string(result.Format) != format {
		return xerror.Unexpected().Detailf(
			"unexpected API response: formatted pipeline format %q does not match requested format %q",
			result.Format,
			format,
		)
	}

	if err := printPipelineFormatDiagnostics(output.stderr, result.Diagnostics); err != nil {
		return err
	}
	if _, err := io.WriteString(output.stdout, result.Data); err != nil {
		return xerror.Unexpected().With(err)
	}
	if !strings.HasSuffix(result.Data, "\n") {
		if _, err := io.WriteString(output.stdout, "\n"); err != nil {
			return xerror.Unexpected().With(err)
		}
	}

	return nil
}

func (c *formatPipelineCommand) resolveFormat() (string, error) {
	if c.Format != nil {
		return strings.ToLower(strings.TrimSpace(*c.Format)), nil
	}

	switch strings.ToLower(filepath.Ext(c.File)) {
	case ".json":
		return string(api.PipelineSpecFormatRequestFormatJson), nil
	case ".yaml", ".yml":
		return string(api.PipelineSpecFormatRequestFormatYaml), nil
	default:
		return "", xerror.InvalidInput(xerror.Pipeline).Detail(
			"pipeline format could not be inferred from file extension; set --format json or --format yaml",
		)
	}
}

func printPipelineFormatDiagnostics(
	stderr io.Writer,
	diagnostics []api.PipelineSpecFormatDiagnostic,
) error {
	for _, diagnostic := range diagnostics {
		_, err := fmt.Fprintf(
			stderr,
			"WARNING %s %s %s: %s\n",
			diagnostic.StepSlug,
			diagnostic.ReferencedStepSlug,
			diagnostic.Code,
			diagnostic.Message,
		)
		if err != nil {
			return xerror.Unexpected().With(err)
		}
	}

	return nil
}

type diffPipelineCommand struct {
	apiPipelineFlags

	PipelineSlug string `arg:"" name:"pipeline-slug" help:"Pipeline slug." required:""`
	File         string `help:"Path to the local candidate pipeline spec file." required:"" type:"path"`
}

func (c *diffPipelineCommand) Run(output *cliOutput, runtime *cliRuntime) error {
	local, err := pipelinevalidate.LoadFile(c.File)
	if err != nil {
		return xerror.Wrapf("load local pipeline: %w", err)
	}
	if string(local.Metadata.Slug) != c.PipelineSlug {
		return xerror.InvalidInput(xerror.Pipeline).Detailf(
			"pipeline slug mismatch: CLI slug %q does not match file slug %q",
			c.PipelineSlug,
			local.Metadata.Slug,
		)
	}

	client, projectSlug, err := newPipelineAPIClient(c.apiPipelineFlags, runtime)
	if err != nil {
		return err
	}

	upstream, err := client.getPipeline(context.Background(), projectSlug, c.PipelineSlug)
	if err != nil {
		return xerror.Wrapf("get upstream pipeline %s: %w", c.PipelineSlug, err)
	}

	upstreamSpec, err := loadPipelineSpecFromAPI(upstream.Spec)
	if err != nil {
		return xerror.Wrapf("load upstream pipeline %s: %w", c.PipelineSlug, err)
	}

	result := pipelinediff.Check(pipelinediff.Diff(upstreamSpec, local))
	_, err = fmt.Fprintln(output.stdout, result.Format())
	if err != nil {
		return xerror.Unexpected().With(err)
	}
	if pipelinediff.ShouldBlockDiff(local, result) {
		return xerror.InvalidInput(xerror.Pipeline).Detail("breaking pipeline changes detected")
	}

	return nil
}

func loadPipelineSpecFromAPI(spec api.PipelineSpec) (*pipelinespec.Pipeline, error) {
	data, err := json.Marshal(spec)
	if err != nil {
		return nil, xerror.InvalidInput(xerror.JSON).With(err)
	}

	pipeline, err := pipelinevalidate.Load("upstream.pipeline.json", data)
	if err != nil {
		return nil, err
	}

	return pipeline, nil
}

func loadPipelineSpecForProposal(path string) (api.PipelineSpec, error) {
	pipeline, err := pipelinevalidate.LoadFile(path)
	if err != nil {
		return api.PipelineSpec{}, xerror.Wrapf("validate pipeline: %w", err)
	}

	data, err := json.Marshal(pipeline)
	if err != nil {
		return api.PipelineSpec{}, xerror.InvalidInput(xerror.JSON).With(err)
	}

	var spec api.PipelineSpec
	if err := json.Unmarshal(data, &spec); err != nil {
		return api.PipelineSpec{}, xerror.InvalidInput(xerror.JSON).With(err)
	}

	return spec, nil
}

func loadPipelineValidationSpec(path string) (api.PipelineSpec, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return api.PipelineSpec{}, xerror.Wrapf("read pipeline: %w", err)
	}

	pipeline, err := pipelinespec.Load(path, data)
	if err != nil {
		return api.PipelineSpec{}, xerror.Wrapf("load pipeline: %w", err)
	}

	return pipelineToAPIPipelineSpec(pipeline)
}

func pipelineToAPIPipelineSpec(pipeline *pipelinespec.Pipeline) (api.PipelineSpec, error) {
	data, err := json.Marshal(pipeline)
	if err != nil {
		return api.PipelineSpec{}, xerror.InvalidInput(xerror.JSON).With(err)
	}

	var spec api.PipelineSpec
	if err := json.Unmarshal(data, &spec); err != nil {
		return api.PipelineSpec{}, xerror.InvalidInput(xerror.JSON).With(err)
	}

	return spec, nil
}

func loadUpdatePipelineSpecs(path, pipelineSlug string) (api.PipelineSpec, api.PipelineSpecUpdate, error) {
	spec, err := loadPipelineSpecForProposal(path)
	if err != nil {
		return api.PipelineSpec{}, api.PipelineSpecUpdate{}, err
	}
	if spec.Metadata.Slug != pipelineSlug {
		return api.PipelineSpec{}, api.PipelineSpecUpdate{}, xerror.InvalidInput(xerror.Pipeline).Detailf(
			"pipeline slug mismatch: CLI slug %q does not match file slug %q",
			pipelineSlug,
			spec.Metadata.Slug,
		)
	}

	return spec, api.PipelineSpecUpdate{
		Metadata: api.PipelineMetadataUpdate{
			Name: spec.Metadata.Name,
		},
		Steps: spec.Steps,
	}, nil
}

func loadDraftSavePipelineSpec(path, pipelineSlug string) (api.PipelineSpecUpdate, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return api.PipelineSpecUpdate{}, xerror.Unexpected().Detailf("read %s: %w", path, err)
	}

	pipeline, err := pipelinespec.Load(path, data)
	if err != nil {
		return api.PipelineSpecUpdate{}, xerror.Wrapf("load %s: %w", path, err)
	}
	if pipeline.Metadata.Slug != "" && string(pipeline.Metadata.Slug) != pipelineSlug {
		return api.PipelineSpecUpdate{}, xerror.InvalidInput(xerror.Pipeline).Detailf(
			"pipeline slug mismatch: CLI slug %q does not match file slug %q",
			pipelineSlug,
			pipeline.Metadata.Slug,
		)
	}

	stepsJSON, err := json.Marshal(pipeline.Steps)
	if err != nil {
		return api.PipelineSpecUpdate{}, xerror.InvalidInput(xerror.JSON).With(err)
	}

	var steps []api.PipelineStep
	if err := json.Unmarshal(stepsJSON, &steps); err != nil {
		return api.PipelineSpecUpdate{}, xerror.InvalidInput(xerror.JSON).With(err)
	}

	return api.PipelineSpecUpdate{
		Metadata: api.PipelineMetadataUpdate{
			Name: pipeline.Metadata.Name,
		},
		Steps: steps,
	}, nil
}

type pipelineRunOutput struct {
	run *api.PipelineRunRecord
}

func writePipelineRunOutput(writer io.Writer, run *api.PipelineRunRecord, outputFormat string) error {
	return writeCLIOutput(writer, pipelineRunOutput{run: run}, outputFormat)
}

// JSONValue returns the generated API record for structured output.
func (o pipelineRunOutput) JSONValue() any {
	return o.run
}

// WriteCLIText renders the operator diagnostics view for a pipeline run.
// The generated API record remains the JSON source of truth; this wrapper is the
// CLI-specific text adapter used by commands that opt into --output text.
func (o pipelineRunOutput) WriteCLIText(writer io.Writer) error {
	if o.run == nil {
		return nil
	}
	run := o.run
	lines := []string{
		fmt.Sprintf("Run: %s", run.Metadata.RunSlug),
		fmt.Sprintf("Status: %s", run.Metadata.Status),
		fmt.Sprintf("Pipeline: %s", run.Metadata.PipelineSlug),
		fmt.Sprintf("Source type: %s", pipelineRunSourceTypeText(run.Metadata.Source)),
		fmt.Sprintf("Source: %s", pipelineRunSourceText(run.Metadata.Source)),
		fmt.Sprintf("Source actor: %s", pipelineRunSourceActorText(run.Metadata.Source)),
		fmt.Sprintf("Source request: %s", pipelineRunSourceRequestText(run.Metadata.Source)),
		fmt.Sprintf("Source parameters: %s", pipelineRunSourceParameterKeysText(run.Metadata.Source)),
		fmt.Sprintf("Source event: %s", pipelineRunSourceEventText(run.Metadata.Source)),
		fmt.Sprintf("Attempt: %s", pipelineRunSourceAttemptText(run.Metadata.Source)),
		fmt.Sprintf("Root run: %s", pipelineRunSourceRootRunText(run.Metadata.Source)),
		fmt.Sprintf("Retry of: %s", pipelineRunSourceRetryOfText(run.Metadata.Source)),
		fmt.Sprintf("Mode: %s", run.Metadata.Incremental.Mode),
		fmt.Sprintf("Checkpoint: %s", run.Metadata.Incremental.CheckpointStatus),
		fmt.Sprintf("Replay safety: %s", run.Metadata.Incremental.ReplaySafetyStatus),
	}
	if schedule := pipelineRunSourceScheduleText(run.Metadata.Source); schedule != "" {
		lines = append(lines, schedule)
	}
	if fire := pipelineRunSourceScheduleFireText(run.Metadata.Source); fire != "" {
		lines = append(lines, fire)
	}
	if run.Metadata.Incremental.SourceStepSlug != "" {
		lines = append(lines, fmt.Sprintf("Source step: %s", run.Metadata.Incremental.SourceStepSlug))
	}
	if run.Metadata.Incremental.BlockedReason != "" {
		lines = append(lines, fmt.Sprintf("Blocked reason: %s", run.Metadata.Incremental.BlockedReason))
	}
	if run.Metadata.Incremental.ReplayBlockedReason != "" {
		lines = append(lines, fmt.Sprintf("Replay blocked reason: %s", run.Metadata.Incremental.ReplayBlockedReason))
	}
	if run.Metadata.Incremental.BackfillSeriesSlug != "" {
		lines = append(lines, fmt.Sprintf(
			"Backfill: %s chunk %d/%d",
			run.Metadata.Incremental.BackfillSeriesSlug,
			run.Metadata.Incremental.BackfillChunkIndex,
			run.Metadata.Incremental.BackfillChunkCount,
		))
	}
	if run.Timeline.Failure != nil {
		failure := run.Timeline.Failure
		lines = append(lines, fmt.Sprintf("Failure step: %s", failure.Step))
		lines = append(lines, fmt.Sprintf("Failure kind: %s", failure.Kind))
		lines = append(lines, fmt.Sprintf("Failure side: %s", failure.Side))
		if failure.ReasonCode != nil && strings.TrimSpace(string(*failure.ReasonCode)) != "" {
			lines = append(lines, fmt.Sprintf("Failure reason: %s", strings.TrimSpace(string(*failure.ReasonCode))))
		}
		if failure.DataSourceSlug != nil && strings.TrimSpace(*failure.DataSourceSlug) != "" {
			lines = append(lines, fmt.Sprintf("Failure data source: %s", strings.TrimSpace(*failure.DataSourceSlug)))
		}
		if failure.Contract != nil {
			lines = append(lines, fmt.Sprintf(
				"Contract failure: contract=%s field=%s rule=%s violations=%d reason=%s",
				failure.Contract.ContractName,
				failure.Contract.FieldName,
				failure.Contract.Rule,
				failure.Contract.ViolationCount,
				failure.Contract.ReasonCode,
			))
		}
		lines = append(lines, fmt.Sprintf("Failure error: %s", failure.Error))
		if failure.ActionHint != nil && strings.TrimSpace(*failure.ActionHint) != "" {
			lines = append(lines, fmt.Sprintf("Action hint: %s", strings.TrimSpace(*failure.ActionHint)))
		}
	}
	for _, source := range run.IncrementalDiagnostics.Sources {
		lines = append(lines, fmt.Sprintf(
			"%s: %s",
			source.SourceStepSlug,
			source.Window.Summary,
		))
		lines = append(lines, fmt.Sprintf(
			"Source %s: previous=%s window=%s observed=%s commit=%s",
			source.SourceStepSlug,
			source.PreviousCheckpoint.Summary,
			source.Window.Summary,
			source.ObservedCheckpoint.Summary,
			source.CommitResult.Status,
		))
		if source.CommitResult.BlockedReason != "" {
			lines = append(lines, fmt.Sprintf(
				"Blocked by %s: %s",
				source.CommitResult.BlockedByStep,
				source.CommitResult.BlockedReason,
			))
		}
	}

	_, err := fmt.Fprintln(writer, strings.Join(lines, "\n"))
	if err != nil {
		return xerror.Unexpected().With(err)
	}
	return nil
}

// pipelineRunSourceTypeText renders the source discriminator or an explicit unavailable marker.
func pipelineRunSourceTypeText(source *api.PipelineRunSourceSummary) string {
	sourceType := strings.TrimSpace(apiPipelineRunSourceView(source).sourceType)
	if sourceType == "" {
		return unavailablePipelineRunSourceLabel
	}
	return sourceType
}

func pipelineRunSourceText(source *api.PipelineRunSourceSummary) string {
	summary := strings.TrimSpace(apiRunSourceSummary(source))
	if summary == "" {
		summary = unavailablePipelineRunSourceLabel
	}
	entrypoint := strings.TrimSpace(apiRunSourceEntrypoint(source))
	if entrypoint == "" {
		return summary
	}
	return summary + " via " + entrypoint
}

func pipelineRunSourceActorText(source *api.PipelineRunSourceSummary) string {
	view := apiPipelineRunSourceView(source)
	actorType := strings.TrimSpace(view.actorType)
	if actorType == "" || actorType == "unknown" {
		return unavailablePipelineRunSourceLabel
	}
	if view.actorSlug == nil || strings.TrimSpace(*view.actorSlug) == "" {
		return actorType
	}
	return actorType + " " + strings.TrimSpace(*view.actorSlug)
}

func pipelineRunSourceRequestText(source *api.PipelineRunSourceSummary) string {
	view := apiPipelineRunSourceView(source)
	parts := make([]string, 0, 2)
	if view.requestID != nil && strings.TrimSpace(*view.requestID) != "" {
		parts = append(parts, "request="+strings.TrimSpace(*view.requestID))
	}
	if view.traceID != nil && strings.TrimSpace(*view.traceID) != "" {
		parts = append(parts, "trace="+strings.TrimSpace(*view.traceID))
	}
	if len(parts) == 0 {
		return unavailablePipelineRunSourceLabel
	}
	return strings.Join(parts, " ")
}

func pipelineRunSourceParameterKeysText(source *api.PipelineRunSourceSummary) string {
	view := apiPipelineRunSourceView(source)
	if len(view.parameterKeys) == 0 {
		return "none"
	}
	return strings.Join(view.parameterKeys, ", ")
}

// pipelineRunSourceEventText renders the safe event linkage without exposing event payloads.
func pipelineRunSourceEventText(source *api.PipelineRunSourceSummary) string {
	view := apiPipelineRunSourceView(source)
	if view.sourceType == "" {
		return unavailablePipelineRunSourceLabel
	}
	return nullableSlugText(view.sourceEventSlug, unavailablePipelineRunSourceLabel)
}

// pipelineRunSourceAttemptText renders the retry-group attempt number captured in source metadata.
func pipelineRunSourceAttemptText(source *api.PipelineRunSourceSummary) string {
	view := apiPipelineRunSourceView(source)
	if view.attemptNumber <= 0 {
		return unavailablePipelineRunSourceLabel
	}
	return fmt.Sprintf("%d", view.attemptNumber)
}

// pipelineRunSourceRootRunText renders the retry-group root run slug when source metadata is available.
func pipelineRunSourceRootRunText(source *api.PipelineRunSourceSummary) string {
	view := apiPipelineRunSourceView(source)
	if view.sourceType == "" {
		return unavailablePipelineRunSourceLabel
	}
	if strings.TrimSpace(view.rootRunSlug) == "" {
		return unavailablePipelineRunSourceLabel
	}
	return strings.TrimSpace(view.rootRunSlug)
}

// pipelineRunSourceRetryOfText renders retry linkage or "none" for first attempts.
func pipelineRunSourceRetryOfText(source *api.PipelineRunSourceSummary) string {
	view := apiPipelineRunSourceView(source)
	if view.sourceType == "" {
		return unavailablePipelineRunSourceLabel
	}
	return nullableSlugText(view.retryOfRunSlug, "none")
}

// pipelineRunSourceScheduleText renders safe schedule trigger metadata for scheduled runs.
func pipelineRunSourceScheduleText(source *api.PipelineRunSourceSummary) string {
	schedule := apiPipelineRunSourceView(source).schedule
	if schedule == nil {
		return ""
	}
	return fmt.Sprintf(
		"Schedule: trigger=%s revision=%d cron=%q timezone=%s",
		slugText(schedule.TriggerSlug, unavailablePipelineRunSourceLabel),
		schedule.TriggerRevision,
		strings.TrimSpace(schedule.Cron),
		stringText(schedule.Timezone, unavailablePipelineRunSourceLabel),
	)
}

// pipelineRunSourceScheduleFireText renders the scheduled and actual fire diagnostics.
func pipelineRunSourceScheduleFireText(source *api.PipelineRunSourceSummary) string {
	schedule := apiPipelineRunSourceView(source).schedule
	if schedule == nil {
		return ""
	}
	return fmt.Sprintf(
		"Schedule fire: scheduled=%s actual=%s misfire=%s",
		utcTimestampText(schedule.ScheduledFireAt, unavailablePipelineRunSourceLabel),
		utcTimestampText(schedule.ActualFireAt, unavailablePipelineRunSourceLabel),
		stringText(string(schedule.MisfireResult), unavailablePipelineRunSourceLabel),
	)
}

func apiRunSourceSummary(source *api.PipelineRunSourceSummary) string {
	return apiPipelineRunSourceView(source).summary
}

func apiRunSourceEntrypoint(source *api.PipelineRunSourceSummary) string {
	return apiPipelineRunSourceView(source).entrypoint
}

type pipelineRunSourceView struct {
	sourceType      string
	summary         string
	entrypoint      string
	actorType       string
	actorSlug       *api.NullableSlug
	requestID       *string
	traceID         *string
	parameterKeys   []string
	sourceEventSlug *api.NullableSlug
	attemptNumber   int
	rootRunSlug     string
	retryOfRunSlug  *api.NullableSlug
	schedule        *api.PipelineRunSourceScheduleSummary
}

func apiPipelineRunSourceView(source *api.PipelineRunSourceSummary) pipelineRunSourceView {
	if source == nil {
		return pipelineRunSourceView{}
	}
	discriminator, err := source.Discriminator()
	if err != nil {
		return pipelineRunSourceView{}
	}
	switch discriminator {
	case "manual":
		manual, err := source.AsPipelineRunManualSourceSummary()
		if err != nil {
			return pipelineRunSourceView{}
		}
		return pipelineRunSourceView{
			sourceType:      string(manual.Type),
			summary:         manual.Summary,
			entrypoint:      string(manual.Entrypoint),
			actorType:       string(manual.ActorType),
			actorSlug:       manual.ActorSlug,
			requestID:       manual.RequestId,
			traceID:         manual.TraceId,
			parameterKeys:   manual.ParameterKeys,
			sourceEventSlug: manual.SourceEventSlug,
			attemptNumber:   manual.AttemptNumber,
			rootRunSlug:     manual.RootRunSlug,
			retryOfRunSlug:  manual.RetryOfRunSlug,
		}
	case "api":
		apiSource, err := source.AsPipelineRunAPISourceSummary()
		if err != nil {
			return pipelineRunSourceView{}
		}
		return pipelineRunSourceView{
			sourceType:      string(apiSource.Type),
			summary:         apiSource.Summary,
			entrypoint:      string(apiSource.Entrypoint),
			actorType:       string(apiSource.ActorType),
			actorSlug:       apiSource.ActorSlug,
			requestID:       apiSource.RequestId,
			traceID:         apiSource.TraceId,
			parameterKeys:   apiSource.ParameterKeys,
			sourceEventSlug: apiSource.SourceEventSlug,
			attemptNumber:   apiSource.AttemptNumber,
			rootRunSlug:     apiSource.RootRunSlug,
			retryOfRunSlug:  apiSource.RetryOfRunSlug,
		}
	case "schedule":
		scheduleSource, err := source.AsPipelineRunScheduleSourceSummary()
		if err != nil {
			return pipelineRunSourceView{}
		}
		schedule := scheduleSource.Schedule
		return pipelineRunSourceView{
			sourceType:      string(scheduleSource.Type),
			summary:         scheduleSource.Summary,
			entrypoint:      string(scheduleSource.Entrypoint),
			actorType:       string(scheduleSource.ActorType),
			actorSlug:       scheduleSource.ActorSlug,
			requestID:       scheduleSource.RequestId,
			traceID:         scheduleSource.TraceId,
			parameterKeys:   scheduleSource.ParameterKeys,
			sourceEventSlug: scheduleSource.SourceEventSlug,
			attemptNumber:   scheduleSource.AttemptNumber,
			rootRunSlug:     scheduleSource.RootRunSlug,
			retryOfRunSlug:  scheduleSource.RetryOfRunSlug,
			schedule:        &schedule,
		}
	default:
		return pipelineRunSourceView{}
	}
}
