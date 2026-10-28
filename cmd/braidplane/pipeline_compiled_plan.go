package main

import (
	"context"
	"io"
	"net/http"
	"os"

	api "github.com/Braidplane/cli/internal/apiclient"
	"github.com/Braidplane/cli/pkg/xerror"
)

type compilePipelineVersionCommand struct {
	apiPipelineFlags
	PipelineSlug       string `arg:"" name:"pipeline-slug" required:""`
	SpecHash           string `arg:"" name:"spec-hash" required:"" help:"Full immutable retained version hash."`
	Environment        string `help:"Environment whose bindings select saved operations during compilation." name:"environment" env:"BRAIDPLANE_ENVIRONMENT"`
	ProjectionFile     string `help:"JSON rich projection selection: exact revision pins, explicit mapping declarations, non-secret values and local relationships." name:"projection-file" type:"path"`
	ResourceScopesFile string `help:"JSON resource scope selections: exact non-secret S3 confinement policies for pipeline steps." name:"resource-scopes-file" type:"path"`
}

func (c *compilePipelineVersionCommand) Run(output *cliOutput, runtime *cliRuntime) error {
	client, project, err := newPipelineAPIClient(c.apiPipelineFlags, runtime)
	if err != nil {
		return err
	}
	params := &api.CompilePipelineVersionParams{}
	if c.Environment != "" {
		environment, err := runtime.resolveEnvironmentSlug(c.Environment)
		if err != nil {
			return err
		}
		params.EnvironmentSlug = &environment
	}
	body := api.PipelineCompilationRequest{}
	if c.ProjectionFile != "" {
		file, err := os.Open(c.ProjectionFile)
		if err != nil {
			return xerror.InvalidInput(xerror.Pipeline).Detail("cannot read projection selection file")
		}
		defer func() { _ = file.Close() }()
		const maxProjectionSelectionBytes = 1 << 20
		raw, err := io.ReadAll(io.LimitReader(file, maxProjectionSelectionBytes+1))
		if err != nil || len(raw) > maxProjectionSelectionBytes {
			return xerror.InvalidInput(xerror.Pipeline).Detail("projection selection exceeds the 1 MiB limit or cannot be read")
		}
		body.Projection = new(api.ODCSProjectionSelection)
		if err := decodeStrictJSON(raw, body.Projection, "projection selection"); err != nil {
			return err
		}
	}
	if c.ResourceScopesFile != "" {
		file, err := os.Open(c.ResourceScopesFile)
		if err != nil {
			return xerror.InvalidInput(xerror.Pipeline).Detail("cannot read resource scopes file")
		}
		defer func() { _ = file.Close() }()
		const maxResourceScopesBytes = 1 << 20
		raw, err := io.ReadAll(io.LimitReader(file, maxResourceScopesBytes+1))
		if err != nil || len(raw) > maxResourceScopesBytes {
			return xerror.InvalidInput(xerror.Pipeline).Detail("resource scopes exceed the 1 MiB limit or cannot be read")
		}
		body.ResourceScopes = new([]api.PipelineResourceScopeSelection)
		if err := decodeStrictJSON(raw, body.ResourceScopes, "resource scopes"); err != nil {
			return err
		}
		if *body.ResourceScopes == nil || len(*body.ResourceScopes) > 128 {
			return xerror.InvalidInput(xerror.Pipeline).Detail("resource scopes must be an array of at most 128 selections")
		}
	}
	resp, err := client.openapi.CompilePipelineVersionWithResponse(context.Background(), client.organization, project, c.PipelineSlug, c.SpecHash, params, body)
	if err != nil {
		return xerror.Wrapf("compile pipeline version: %w", err)
	}
	if resp.StatusCode() != http.StatusOK {
		return decodeAPIError(resp.StatusCode(), resp.Body)
	}
	return writeCompiledPlan(output, resp.JSON200)
}

type getPipelineCompiledPlanCommand struct {
	apiPipelineFlags
	PipelineSlug string `arg:"" name:"pipeline-slug" required:""`
	Digest       string `arg:"" name:"digest" required:"" help:"Exact retained compiled plan digest."`
}

func (c *getPipelineCompiledPlanCommand) Run(output *cliOutput, runtime *cliRuntime) error {
	client, project, err := newPipelineAPIClient(c.apiPipelineFlags, runtime)
	if err != nil {
		return err
	}
	resp, err := client.openapi.GetPipelineCompiledPlanWithResponse(context.Background(), client.organization, project, c.PipelineSlug, c.Digest)
	if err != nil {
		return xerror.Wrapf("inspect compiled plan: %w", err)
	}
	if resp.StatusCode() != http.StatusOK {
		return decodeAPIError(resp.StatusCode(), resp.Body)
	}
	return writeCompiledPlan(output, resp.JSON200)
}

func writeCompiledPlan(output *cliOutput, plan *api.PipelineCompiledPlan) error {
	if plan == nil {
		return xerror.Unexpected().Detail("missing compiled plan summary")
	}
	return writeCLIJSON(output.stdout, plan)
}
