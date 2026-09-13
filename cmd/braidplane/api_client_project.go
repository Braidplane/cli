package main

import (
	"context"
	"net/http"

	api "github.com/Braidplane/cli/internal/apiclient"
	"github.com/Braidplane/cli/pkg/xerror"
)

// listProjects fetches one page of organization projects through the API client.
func (c *cliAPIClient) listProjects(
	ctx context.Context,
	params *api.ListProjectsParams,
) (*api.ProjectListResponse, error) {
	resp, err := c.openapi.ListProjectsWithResponse(ctx, c.organization, params)
	if err != nil {
		return nil, xerror.Wrapf("list projects: %w", err)
	}
	if resp.StatusCode() != http.StatusOK {
		return nil, decodeAPIError(resp.StatusCode(), resp.Body)
	}
	if resp.JSON200 == nil {
		return nil, xerror.Unexpected().Detail("unexpected API response: missing project list payload")
	}

	return resp.JSON200, nil
}

// getProject fetches one organization project by slug.
func (c *cliAPIClient) getProject(ctx context.Context, projectSlug string) (*api.Project, error) {
	resp, err := c.openapi.GetProjectWithResponse(ctx, c.organization, projectSlug)
	if err != nil {
		return nil, xerror.Wrapf("get project: %w", err)
	}
	if resp.StatusCode() != http.StatusOK {
		return nil, decodeAPIError(resp.StatusCode(), resp.Body)
	}
	if resp.JSON200 == nil {
		return nil, xerror.Unexpected().Detail("unexpected API response: missing project payload")
	}

	return resp.JSON200, nil
}

// createProject creates one organization project and returns the API payload.
func (c *cliAPIClient) createProject(ctx context.Context, body api.NewProject) (*api.Project, error) {
	resp, err := c.openapi.CreateProjectWithResponse(ctx, c.organization, body)
	if err != nil {
		return nil, xerror.Wrapf("create project: %w", err)
	}
	if resp.StatusCode() != http.StatusCreated {
		return nil, decodeAPIError(resp.StatusCode(), resp.Body)
	}
	if resp.JSON201 == nil {
		return nil, xerror.Unexpected().Detail("unexpected API response: missing created project payload")
	}

	return resp.JSON201, nil
}

// updateProject replaces one project's mutable metadata through the API.
func (c *cliAPIClient) updateProject(
	ctx context.Context,
	projectSlug string,
	body api.UpdateProject,
) (*api.Project, error) {
	resp, err := c.openapi.UpdateProjectWithResponse(ctx, c.organization, projectSlug, body)
	if err != nil {
		return nil, xerror.Wrapf("update project: %w", err)
	}
	if resp.StatusCode() != http.StatusOK {
		return nil, decodeAPIError(resp.StatusCode(), resp.Body)
	}
	if resp.JSON200 == nil {
		return nil, xerror.Unexpected().Detail("unexpected API response: missing updated project payload")
	}

	return resp.JSON200, nil
}

// deleteProject deletes one project and optionally asks the API to clean up remote pipeline files.
func (c *cliAPIClient) deleteProject(ctx context.Context, projectSlug string, cleanupRemote bool) error {
	params := &api.DeleteProjectParams{}
	if cleanupRemote {
		params.CleanupRemote = &cleanupRemote
	}
	resp, err := c.openapi.DeleteProjectWithResponse(ctx, c.organization, projectSlug, params)
	if err != nil {
		return xerror.Wrapf("delete project: %w", err)
	}
	if resp.StatusCode() != http.StatusNoContent {
		return decodeAPIError(resp.StatusCode(), resp.Body)
	}

	return nil
}

// setProjectPipelineBackend attaches the project's git pipeline backend identity.
func (c *cliAPIClient) setProjectPipelineBackend(
	ctx context.Context,
	projectSlug string,
	body api.SetProjectPipelineBackendRequest,
) (*api.Project, error) {
	resp, err := c.openapi.SetProjectPipelineBackendWithResponse(ctx, c.organization, projectSlug, body)
	if err != nil {
		return nil, xerror.Wrapf("set project pipeline backend: %w", err)
	}
	if resp.StatusCode() != http.StatusOK {
		return nil, decodeAPIError(resp.StatusCode(), resp.Body)
	}
	if resp.JSON200 == nil {
		return nil, xerror.Unexpected().Detail("unexpected API response: missing project payload")
	}

	return resp.JSON200, nil
}
