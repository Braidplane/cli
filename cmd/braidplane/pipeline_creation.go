package main

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"

	api "github.com/Braidplane/cli/internal/apiclient"
	pipelinespec "github.com/Braidplane/cli/pkg/pipeline/spec"
	pipelinevalidate "github.com/Braidplane/cli/pkg/pipeline/validate"
	"github.com/Braidplane/cli/pkg/slug"
	"github.com/Braidplane/cli/pkg/xerror"
)

type createPipelineCommand struct {
	apiPipelineFlags
	File           string `name:"file" type:"path" help:"Pipeline YAML or JSON definition; choose this or --from-query."`
	FromQuery      string `name:"from-query" help:"Saved query slug to turn into a one-step pipeline without creating a local file."`
	DataSource     string `name:"data-source" help:"Concrete source containing --from-query. Bind logical ref source to this connection after Git apply."`
	Name           string `name:"name" help:"Pipeline name for --from-query; defaults to the saved query name followed by pipeline."`
	Slug           string `name:"slug" help:"Pipeline slug for --from-query; defaults to a slug derived from its name."`
	ParametersJSON string `name:"parameters-json" help:"Query parameter values as a JSON object for --from-query; omitted values use saved defaults."`
}

func (c *createPipelineCommand) Run(output *cliOutput, runtime *cliRuntime) error {
	if (c.File == "") == (c.FromQuery == "") {
		return xerror.ValidationFailed().Detail("choose exactly one of --file or --from-query")
	}
	var spec api.PipelineSpec
	var err error
	if c.File != "" {
		if c.DataSource != "" || c.Name != "" || c.Slug != "" || c.ParametersJSON != "" {
			return xerror.ValidationFailed().Detail("--data-source, --name, --slug and --parameters-json apply only to --from-query")
		}
		spec, err = loadPipelineSpecForProposal(c.File)
		if err != nil {
			return err
		}
	}
	client, project, err := newPipelinePATAPIClient(c.apiPipelineFlags, runtime)
	if err != nil {
		return err
	}
	if c.FromQuery != "" {
		spec, err = c.specFromQuery(context.Background(), client)
		if err != nil {
			return err
		}
	}
	response, err := client.openapi.CreatePipelineProposalWithResponse(context.Background(), client.organization, project, spec)
	if err != nil {
		return xerror.ExecutionUnavailable().With(err).Detail("create pipeline proposal")
	}
	if response.StatusCode() != http.StatusOK {
		return decodeAPIError(response.StatusCode(), response.Body)
	}
	if response.JSON200 == nil {
		return xerror.Unexpected().Detail("missing pipeline creation proposal")
	}
	return writeCLIJSON(output.stdout, response.JSON200)
}

func (c *createPipelineCommand) specFromQuery(ctx context.Context, client *cliAPIClient) (api.PipelineSpec, error) {
	for _, value := range []string{c.FromQuery, c.DataSource} {
		if _, err := slug.New(value); err != nil {
			return api.PipelineSpec{}, xerror.ValidationFailed().Detail("--from-query and --data-source must be valid slugs")
		}
	}
	query, err := client.getQuery(ctx, c.DataSource, c.FromQuery)
	if err != nil {
		return api.PipelineSpec{}, err
	}
	if query.DeletedAt != nil {
		return api.PipelineSpec{}, xerror.InvalidInput(xerror.Query).Detail("restore the saved query before building a pipeline")
	}
	statementType, err := query.QueryStatement.Discriminator()
	if err != nil {
		return api.PipelineSpec{}, xerror.InvalidInput(xerror.Query).With(err)
	}
	operationType := ""
	for _, capability := range pipelinespec.AllConnectorCapabilities() {
		if capability.DataSourceType.String() == statementType && capability.SupportsPipelineSource {
			operationType = capability.QueryOperationType
			break
		}
	}
	if operationType == "" {
		return api.PipelineSpec{}, xerror.ValidationFailed().Detail("this saved query's connector does not support pipeline sources")
	}
	name := strings.TrimSpace(c.Name)
	if name == "" {
		name = query.Name + " pipeline"
	}
	var pipelineSlug slug.Slug
	if c.Slug == "" {
		pipelineSlug, err = slug.FromName(name)
	} else {
		pipelineSlug, err = slug.New(strings.TrimSpace(c.Slug))
	}
	if err != nil {
		return api.PipelineSpec{}, err
	}
	var parameters map[string]any
	if c.ParametersJSON != "" {
		decoder := json.NewDecoder(strings.NewReader(c.ParametersJSON))
		decoder.UseNumber()
		if err := decoder.Decode(&parameters); err != nil {
			return api.PipelineSpec{}, xerror.ValidationFailed().With(err).Detail("--parameters-json must be a JSON object")
		}
		var trailing any
		if decoder.Decode(&trailing) != io.EOF || parameters == nil {
			return api.PipelineSpec{}, xerror.ValidationFailed().Detail("--parameters-json must be a JSON object")
		}
	}
	for _, parameter := range query.Parameters {
		if parameter.IsRequired != nil && *parameter.IsRequired && parameter.DefaultValue == nil {
			if _, present := parameters[parameter.Name]; !present {
				return api.PipelineSpec{}, xerror.ValidationFailed().Detailf("supply required query parameter %q through --parameters-json", parameter.Name)
			}
		}
	}
	pipeline := &pipelinespec.Pipeline{Metadata: pipelinespec.Metadata{Slug: pipelineSlug, Name: name}, Steps: []pipelinespec.Step{
		{Kind: "query", Slug: "fetch", Query: &pipelinespec.QueryRef{DataSourceSlug: "source", OperationType: operationType, OperationRef: slug.Slug(query.Slug), Parameters: parameters}},
	}}
	if err := pipelinevalidate.Validate(pipeline); err != nil {
		return api.PipelineSpec{}, err
	}
	return pipelineToAPIPipelineSpec(pipeline)
}

type pipelineCreationCommand struct {
	List  listPipelineCreationsCommand `cmd:"" help:"Find recorded creation proposals and resume their exact identities."`
	Get   getPipelineCreationCommand   `cmd:"" help:"Inspect safe proposal progress; this is not canonical pipeline readiness."`
	Retry retryPipelineCreationCommand `cmd:"" help:"Retry the exact recorded definition with its original proposal identity."`
}

type listPipelineCreationsCommand struct {
	apiPipelineFlags
	Limit  int32  `name:"limit" default:"20" help:"Maximum proposals per page (1-100)."`
	Cursor string `name:"cursor" help:"Continuation cursor from the previous response."`
}

func (c *listPipelineCreationsCommand) Run(output *cliOutput, runtime *cliRuntime) error {
	if c.Limit < 1 || c.Limit > 100 {
		return xerror.ValidationFailed().Detail("limit must be between 1 and 100")
	}
	params := api.ListPipelineCreationProposalsParams{Limit: &c.Limit}
	if c.Cursor != "" {
		if _, err := slug.New(c.Cursor); err != nil {
			return err
		}
		params.Cursor = &c.Cursor
	}
	client, project, err := newPipelineAPIClient(c.apiPipelineFlags, runtime)
	if err != nil {
		return err
	}
	response, err := client.openapi.ListPipelineCreationProposalsWithResponse(context.Background(), client.organization, project, &params)
	if err != nil {
		return xerror.ExecutionUnavailable().With(err).Detail("list pipeline creation proposals")
	}
	if response.StatusCode() != http.StatusOK {
		return decodeAPIError(response.StatusCode(), response.Body)
	}
	if response.JSON200 == nil {
		return xerror.Unexpected().Detail("missing pipeline creation list")
	}
	return writeCLIJSON(output.stdout, response.JSON200)
}

type retryPipelineCreationCommand struct {
	apiPipelineFlags
	CreationSlug string `arg:"" name:"creation-slug" required:"" help:"Exact creation proposal slug returned by pipeline create."`
}

func (c *retryPipelineCreationCommand) Run(output *cliOutput, runtime *cliRuntime) error {
	client, project, err := newPipelinePATAPIClient(c.apiPipelineFlags, runtime)
	if err != nil {
		return err
	}
	stored, err := client.openapi.GetPipelineCreationProposalWithResponse(context.Background(), client.organization, project, c.CreationSlug)
	if err != nil {
		return xerror.ExecutionUnavailable().With(err).Detail("read original pipeline creation definition")
	}
	if stored.StatusCode() != http.StatusOK {
		return decodeAPIError(stored.StatusCode(), stored.Body)
	}
	if stored.JSON200 == nil || stored.JSON200.Slug != c.CreationSlug {
		return xerror.Unexpected().Detail("missing or mismatched pipeline creation proposal")
	}
	response, err := client.openapi.CreatePipelineProposalWithResponse(context.Background(), client.organization, project, stored.JSON200.Spec)
	if err != nil {
		return xerror.ExecutionUnavailable().With(err).Detail("retry pipeline creation proposal")
	}
	if response.StatusCode() != http.StatusOK {
		return decodeAPIError(response.StatusCode(), response.Body)
	}
	if response.JSON200 == nil || response.JSON200.Slug != c.CreationSlug {
		return xerror.Unexpected().Detail("pipeline creation retry returned a different proposal identity")
	}
	return writeCLIJSON(output.stdout, response.JSON200)
}

type getPipelineCreationCommand struct {
	apiPipelineFlags
	CreationSlug string `arg:"" name:"creation-slug" required:"" help:"Exact creation proposal slug returned by pipeline create."`
}

func (c *getPipelineCreationCommand) Run(output *cliOutput, runtime *cliRuntime) error {
	client, project, err := newPipelineAPIClient(c.apiPipelineFlags, runtime)
	if err != nil {
		return err
	}
	response, err := client.openapi.GetPipelineCreationProposalWithResponse(context.Background(), client.organization, project, c.CreationSlug)
	if err != nil {
		return xerror.ExecutionUnavailable().With(err).Detail("get pipeline creation proposal")
	}
	if response.StatusCode() != http.StatusOK {
		return decodeAPIError(response.StatusCode(), response.Body)
	}
	if response.JSON200 == nil {
		return xerror.Unexpected().Detail("missing pipeline creation proposal")
	}
	return writeCLIJSON(output.stdout, response.JSON200)
}
