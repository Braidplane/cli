package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"time"

	api "github.com/Braidplane/cli/internal/apiclient"
	"github.com/Braidplane/cli/pkg/xerror"
)

type projectRepositoryCommand struct {
	Abandon        abandonProjectRepositorySetupCommand   `cmd:"" help:"Archive an incomplete attempt without deleting its resources, then choose a new target."`
	CompleteGitHub completeProjectRepositoryGitHubCommand `cmd:"" name:"complete-github" help:"Complete GitHub consent using a temporary authorization JSON file and your user credentials."`
	Create         createProjectRepositoryCommand         `cmd:"" help:"Create and attach a private repository using managed GitHub or GitLab authorization. Resubmit the same request to resume."`
	Get            getProjectRepositorySetupCommand       `cmd:"" help:"Inspect resumable repository setup progress."`
}

type createProjectRepositoryCommand struct {
	apiAuthFlags
	ProjectSlug             string `arg:"" name:"project-slug" required:"" help:"Project slug."`
	Provider                string `name:"provider" required:"" help:"Managed provider: github or gitlab."`
	Owner                   string `name:"owner" required:"" help:"Customer-owned user or group namespace."`
	RepositoryName          string `name:"repository-name" required:"" help:"Private repository name."`
	TrackedBranch           string `name:"tracked-branch" default:"main" help:"Initial writable branch."`
	PipelineDir             string `name:"pipeline-dir" default:"pipelines" help:"Repository-relative pipeline directory."`
	CreatePrivateRepository bool   `name:"create-private-repository" help:"Consent to create and initialize the private repository."`
	Wait                    bool   `name:"wait" help:"Wait for this exact setup after browser consent; print final progress as JSON."`
	TimeoutSec              int    `name:"timeout-seconds" default:"600" help:"Maximum seconds for --wait, including creation and consent."`
}

func (c *createProjectRepositoryCommand) Run(output *cliOutput, runtime *cliRuntime) error {
	provider := api.CreateProjectRepositoryRequestProvider(strings.TrimSpace(c.Provider))
	if !provider.Valid() || !c.CreatePrivateRepository {
		return xerror.ValidationFailed().Detail("provider must be github or gitlab and --create-private-repository is required")
	}
	if c.Wait && c.TimeoutSec <= 0 {
		return xerror.ValidationFailed().Detail("--timeout-seconds must be greater than zero with --wait")
	}
	client, err := newAPIClient(c.apiAuthFlags, runtime)
	if err != nil {
		return err
	}
	ctx := context.Background()
	if c.Wait {
		var stop context.CancelFunc
		ctx, stop = signal.NotifyContext(ctx, os.Interrupt)
		defer stop()
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, time.Duration(c.TimeoutSec)*time.Second)
		defer cancel()
	}
	resp, err := client.openapi.CreateProjectRepositoryWithResponse(ctx, client.organization, c.ProjectSlug, api.CreateProjectRepositoryRequest{
		Provider: provider, Owner: c.Owner, RepositoryName: c.RepositoryName, TrackedBranch: &c.TrackedBranch, PipelineDir: &c.PipelineDir, CreatePrivateRepository: api.CreateProjectRepositoryRequestCreatePrivateRepository(true),
	})
	if err != nil {
		return xerror.Wrapf("create project repository: %w", err)
	}
	if resp.StatusCode() != http.StatusOK {
		return decodeAPIError(resp.StatusCode(), resp.Body)
	}
	if resp.JSON200 == nil {
		return xerror.Unexpected().Detail("missing repository setup payload")
	}
	if c.Wait {
		return waitForProjectRepository(ctx, client, c.ProjectSlug, resp.JSON200, output)
	}
	return writeCLIJSON(output.stdout, resp.JSON200)
}

func waitForProjectRepository(ctx context.Context, client *cliAPIClient, project string, setup *api.ProjectRepositorySetup, output *cliOutput) error {
	if setup.Slug == "" || setup.ProjectSlug != project {
		return xerror.Unexpected().Detail("repository setup returned an unexpected identity")
	}
	expected := setup.Slug
	if setup.AuthorizationUrl != nil && *setup.AuthorizationUrl != "" {
		if _, err := fmt.Fprintf(output.stderr, "Sign into Braidplane as the user who started this setup, then authorize in your browser:\n%s\nWaiting for setup %s. Ctrl-C keeps your saved progress.\n", *setup.AuthorizationUrl, expected); err != nil {
			return xerror.Unexpected().Detail("write repository consent instructions").With(err)
		}
	}
	finish := func(err error) error {
		if writeErr := writeCLIJSON(output.stdout, setup); writeErr != nil {
			return writeErr
		}
		return err
	}
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	for {
		if setup.AbandonedAt != nil {
			return finish(xerror.ExecutionFailed().Detail("repository setup was archived; inspect the preserved attempt before choosing a new target"))
		}
		if setup.Status == api.ProjectRepositorySetupStatusAttached {
			return finish(nil)
		}
		if setup.FailureCode != nil && *setup.FailureCode != "" {
			if *setup.FailureCode == "authorization_denied" || *setup.FailureCode == "authorization_failed" {
				return finish(xerror.ExecutionFailed().Detailf("repository authorization stopped at %s; repeat the same create command to start fresh authorization", *setup.FailureCode))
			}
			return finish(xerror.ExecutionFailed().Detailf("repository setup stopped at %s; correct access or test the saved connection, then repeat the same create command", *setup.FailureCode))
		}
		response, err := client.openapi.GetProjectRepositorySetupAttemptWithResponse(ctx, client.organization, project, expected)
		if err != nil {
			if ctx.Err() != nil {
				return finish(xerror.ExecutionUnavailable().Detail("repository setup wait ended; progress is saved; inspect it with project repository get or repeat the same create command"))
			}
			return finish(xerror.ExecutionUnavailable().Detail("repository setup progress could not be read").With(err))
		}
		if response.StatusCode() != http.StatusOK {
			return finish(decodeAPIError(response.StatusCode(), response.Body))
		}
		if response.JSON200 == nil || response.JSON200.Slug != expected || response.JSON200.ProjectSlug != project {
			return finish(xerror.Unexpected().Detail("repository setup inspection returned an unexpected identity"))
		}
		setup = response.JSON200
		if setup.AbandonedAt != nil || setup.Status == api.ProjectRepositorySetupStatusAttached || (setup.FailureCode != nil && *setup.FailureCode != "") {
			continue
		}
		select {
		case <-ctx.Done():
			return finish(xerror.ExecutionUnavailable().Detail("repository setup wait ended; progress is saved; inspect it with project repository get or repeat the same create command"))
		case <-ticker.C:
		}
	}
}

type getProjectRepositorySetupCommand struct {
	apiAuthFlags
	ProjectSlug string `arg:"" name:"project-slug" required:"" help:"Project slug."`
	Setup       string `name:"setup" help:"Inspect an exact current or abandoned setup slug instead of the active attempt."`
}

func (c *getProjectRepositorySetupCommand) Run(output *cliOutput, runtime *cliRuntime) error {
	client, err := newAPIClient(c.apiAuthFlags, runtime)
	if err != nil {
		return err
	}
	if c.Setup != "" {
		resp, err := client.openapi.GetProjectRepositorySetupAttemptWithResponse(context.Background(), client.organization, c.ProjectSlug, c.Setup)
		if err != nil {
			return xerror.Wrapf("get repository setup attempt: %w", err)
		}
		if resp.StatusCode() != http.StatusOK {
			return decodeAPIError(resp.StatusCode(), resp.Body)
		}
		if resp.JSON200 == nil {
			return xerror.Unexpected().Detail("missing repository setup attempt")
		}
		return writeCLIJSON(output.stdout, resp.JSON200)
	}
	resp, err := client.openapi.GetProjectRepositorySetupWithResponse(context.Background(), client.organization, c.ProjectSlug)
	if err != nil {
		return xerror.Wrapf("get project repository setup: %w", err)
	}
	if resp.StatusCode() == http.StatusNoContent {
		return writeCLIJSON(output.stdout, nil)
	}
	if resp.StatusCode() != http.StatusOK {
		return decodeAPIError(resp.StatusCode(), resp.Body)
	}
	if resp.JSON200 == nil {
		return xerror.Unexpected().Detail("missing repository setup payload")
	}
	return writeCLIJSON(output.stdout, resp.JSON200)
}

type abandonProjectRepositorySetupCommand struct {
	apiAuthFlags
	ProjectSlug       string `arg:"" name:"project-slug" required:"" help:"Project slug."`
	Setup             string `name:"setup" required:"" help:"Exact incomplete setup slug to archive."`
	PreserveResources bool   `name:"preserve-resources" help:"Acknowledge that existing or partially created resources remain in your account."`
}

func (c *abandonProjectRepositorySetupCommand) Run(output *cliOutput, runtime *cliRuntime) error {
	if !c.PreserveResources {
		return xerror.ValidationFailed().Detail("--preserve-resources is required; changing targets keeps any existing repositories and connections")
	}
	client, err := newAPIClient(c.apiAuthFlags, runtime)
	if err != nil {
		return err
	}
	resp, err := client.openapi.AbandonProjectRepositorySetupWithResponse(context.Background(), client.organization, c.ProjectSlug, api.AbandonProjectRepositorySetupRequest{
		SetupSlug: c.Setup, PreserveResources: api.AbandonProjectRepositorySetupRequestPreserveResources(true),
	})
	if err != nil {
		return xerror.Wrapf("abandon repository setup: %w", err)
	}
	if resp.StatusCode() != http.StatusOK {
		return decodeAPIError(resp.StatusCode(), resp.Body)
	}
	if resp.JSON200 == nil {
		return xerror.Unexpected().Detail("missing archived repository setup")
	}
	return writeCLIJSON(output.stdout, resp.JSON200)
}

type completeProjectRepositoryGitHubCommand struct {
	apiAuthFlags
	ProjectSlug string `arg:"" name:"project-slug" required:"" help:"Project slug."`
	File        string `name:"file" type:"path" required:"" help:"Temporary JSON file containing state, code and installation_id from provider consent. Values are never saved by Braidplane."`
}

func (c *completeProjectRepositoryGitHubCommand) Run(output *cliOutput, runtime *cliRuntime) error {
	data, err := optionalJSONSource("", c.File, "GitHub authorization")
	if err != nil {
		return err
	}
	var request api.ProjectRepositoryGitHubAuthorization
	if err := decodeStrictJSON(data, &request, "GitHub authorization"); err != nil {
		return err
	}
	if len(request.State) < 32 || len(request.State) > 256 || strings.TrimSpace(request.Code) == "" || len(request.Code) > 1024 || request.InstallationId <= 0 {
		return xerror.ValidationFailed().Detail("GitHub authorization requires valid state, code and installation_id")
	}
	client, err := newAPIClient(c.apiAuthFlags, runtime)
	if err != nil {
		return err
	}
	response, err := client.openapi.CompleteProjectRepositoryGitHubAuthorizationWithResponse(context.Background(), client.organization, c.ProjectSlug, request)
	if err != nil {
		return xerror.Wrapf("complete GitHub repository authorization: %w", err)
	}
	if response.StatusCode() != http.StatusOK {
		return decodeAPIError(response.StatusCode(), response.Body)
	}
	if response.JSON200 == nil {
		return xerror.Unexpected().Detail("missing repository setup payload")
	}
	return writeCLIJSON(output.stdout, response.JSON200)
}
