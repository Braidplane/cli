package main

import (
	"context"
	"net/http"

	api "github.com/Braidplane/cli/internal/apiclient"
	"github.com/Braidplane/cli/pkg/slug"
	"github.com/Braidplane/cli/pkg/xerror"
)

type pipelineRecencyCommand struct {
	Get getPipelineRecencyCommand `cmd:"" help:"Inspect current successful-run recency and retained breach/recovery evidence."`
	Set setPipelineRecencyCommand `cmd:"" help:"Create, change or disable an expectation when supported by the server."`
}

type getPipelineRecencyCommand struct {
	apiPipelineFlags
	PipelineSlug string `arg:"" name:"pipeline-slug" required:""`
	Environment  string `name:"environment" env:"BRAIDPLANE_ENVIRONMENT" help:"Project environment slug."`
}

func (c *getPipelineRecencyCommand) Run(output *cliOutput, runtime *cliRuntime) error {
	if _, err := slug.New(c.PipelineSlug); err != nil {
		return err
	}
	client, project, environment, err := newPipelineEnvironmentAPIClient(c.apiPipelineFlags, c.Environment, runtime)
	if err != nil {
		return err
	}
	result, err := client.getPipelineRecency(context.Background(), project, environment, c.PipelineSlug)
	if err != nil {
		return err
	}
	return writeCLIJSON(output.stdout, result)
}

type setPipelineRecencyCommand struct {
	getPipelineRecencyCommand
	ExpectedRevision  int64 `name:"expected-revision" required:"" help:"Revision from get; use 0 to create. Reload after a conflict."`
	MaximumAgeSeconds int64 `name:"maximum-age-seconds" required:"" help:"Maximum age of a qualifying success, in whole seconds."`
	Enabled           bool  `name:"enabled" default:"true" negatable:"" help:"Enable the expectation; use --no-enabled to disable it."`
}

func (c *setPipelineRecencyCommand) Run(output *cliOutput, runtime *cliRuntime) error {
	if _, err := slug.New(c.PipelineSlug); err != nil {
		return err
	}
	if c.ExpectedRevision < 0 || c.ExpectedRevision == 9223372036854775807 || c.MaximumAgeSeconds <= 0 || c.MaximumAgeSeconds > 9223372036 {
		return xerror.ValidationFailed().Detail("use a nonnegative expected revision and a maximum age between 1 and 9223372036 seconds")
	}
	client, project, environment, err := newPipelineEnvironmentAPIClient(c.apiPipelineFlags, c.Environment, runtime)
	if err != nil {
		return err
	}
	ctx := context.Background()
	capabilities, err := client.pipelineOperationalCapabilities(ctx)
	if err != nil {
		return err
	}
	if !capabilities.SuccessfulRunRecency {
		return xerror.ValidationFailed().Detail("successful-run recency configuration is unavailable on this server")
	}
	response, err := client.openapi.UpdateSuccessfulRunRecencyWithResponse(ctx, client.organization, project, environment, c.PipelineSlug, api.SuccessfulRunRecencyUpdate{ExpectedRevision: c.ExpectedRevision, MaximumAgeSeconds: c.MaximumAgeSeconds, Enabled: c.Enabled})
	if err != nil {
		return xerror.Wrapf("set successful-run recency: %w", err)
	}
	if response.StatusCode() != http.StatusOK {
		return decodeAPIError(response.StatusCode(), response.Body)
	}
	if response.JSON200 == nil {
		return xerror.Unexpected().Detail("missing successful-run recency response")
	}
	return writeCLIJSON(output.stdout, response.JSON200)
}

type pipelineCapabilitiesCommand struct{ apiPipelineFlags }

func (c *pipelineCapabilitiesCommand) Run(output *cliOutput, runtime *cliRuntime) error {
	client, err := newAPIClient(c.apiAuthFlags, runtime)
	if err != nil {
		return err
	}
	capabilities, err := client.pipelineOperationalCapabilities(context.Background())
	if err != nil {
		return err
	}
	return writeCLIJSON(output.stdout, capabilities)
}

func (c *cliAPIClient) pipelineOperationalCapabilities(ctx context.Context) (*api.PipelineOperationalCapabilities, error) {
	response, err := c.openapi.GetPipelineOperationalCapabilitiesWithResponse(ctx, c.organization)
	if err != nil {
		return nil, xerror.Wrapf("get pipeline capabilities: %w", err)
	}
	if response.StatusCode() != http.StatusOK {
		return nil, decodeAPIError(response.StatusCode(), response.Body)
	}
	if response.JSON200 == nil {
		return nil, xerror.Unexpected().Detail("missing pipeline capabilities response")
	}
	return response.JSON200, nil
}

func (c *cliAPIClient) getPipelineRecency(ctx context.Context, project, environment, pipeline string) (*api.SuccessfulRunRecency, error) {
	response, err := c.openapi.GetSuccessfulRunRecencyWithResponse(ctx, c.organization, project, environment, pipeline)
	if err != nil {
		return nil, xerror.Wrapf("inspect successful-run recency: %w", err)
	}
	if response.StatusCode() != http.StatusOK {
		return nil, decodeAPIError(response.StatusCode(), response.Body)
	}
	if response.JSON200 == nil {
		return nil, xerror.Unexpected().Detail("missing successful-run recency response")
	}
	return response.JSON200, nil
}
