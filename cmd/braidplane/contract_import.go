package main

import (
	"context"
	"net/http"

	api "github.com/Braidplane/cli/internal/apiclient"
	"github.com/Braidplane/cli/pkg/xerror"
)

type importODCSArtifactsCommand struct {
	apiPipelineFlags
	Apply          bool   `help:"Apply the exact previewed Git commit." name:"apply"`
	ExpectedCommit string `help:"Exact commit from preview; required with --apply." name:"expected-commit"`
}

func (c *importODCSArtifactsCommand) Run(output *cliOutput, runtime *cliRuntime) error {
	client, project, err := newPipelineAPIClient(c.apiPipelineFlags, runtime)
	if err != nil {
		return err
	}
	if c.Apply && c.ExpectedCommit == "" {
		return xerror.ValidationFailed().Detail("--apply requires --expected-commit from preview")
	}
	response, err := client.openapi.ImportODCSArtifactsWithResponse(context.Background(), client.organization, project, api.ImportODCSArtifactsJSONRequestBody{Apply: c.Apply, ExpectedCommit: c.ExpectedCommit})
	if err != nil {
		return xerror.Unexpected().With(err)
	}
	if response.StatusCode() != http.StatusOK {
		return decodeAPIError(response.StatusCode(), response.Body)
	}
	if response.JSON200 == nil {
		return xerror.Unexpected().Detail("missing ODCS import manifest")
	}
	// Original authored bytes can contain private comments; routine CLI output is metadata only.
	for i := range response.JSON200.Artifacts {
		response.JSON200.Artifacts[i].Original = nil
	}
	return writeCLIJSON(output.stdout, response.JSON200)
}
