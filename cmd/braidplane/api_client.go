package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	api "github.com/Braidplane/cli/internal/apiclient"
	"github.com/Braidplane/cli/pkg/xerror"
)

type cliAPIClient struct {
	openapi      *api.ClientWithResponses
	organization string
}

type cliAPIStatusError struct {
	statusCode int
	err        error
}

func (e *cliAPIStatusError) Error() string {
	if e == nil || e.err == nil {
		return fmt.Sprintf("API request failed: status %d", e.statusCode)
	}
	return e.err.Error()
}

func (e *cliAPIStatusError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.err
}

const cliAPIClientTimeout = 30 * time.Second

func newCLIAPIClient(cfg cliAuthConfig) (*cliAPIClient, error) {
	serverURL, err := normalizeAPIServerURL(cfg.ServerURL)
	if err != nil {
		return nil, err
	}

	openapiClient, err := api.NewClientWithResponses(
		serverURL,
		api.WithHTTPClient(&http.Client{
			Timeout:   cliAPIClientTimeout,
			Transport: bearerTransport{token: cfg.authToken()},
			// The transport attaches credentials to every request, including redirects.
			CheckRedirect: func(*http.Request, []*http.Request) error {
				return http.ErrUseLastResponse
			},
		}),
	)
	if err != nil {
		return nil, xerror.Wrapf("create OpenAPI client: %w", err)
	}

	return &cliAPIClient{
		openapi:      openapiClient,
		organization: cfg.Organization,
	}, nil
}

func normalizeAPIServerURL(raw string) (string, error) {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return "", xerror.Wrapf("parse server URL: %w", err)
	}
	if parsed.Scheme == "" || parsed.Host == "" {
		return "", xerror.Unexpected().Detail("server URL must include scheme and host")
	}

	parsed.Path = strings.TrimRight(parsed.Path, "/")

	return parsed.String(), nil
}

func (c *cliAPIClient) verifyOrganizationAccess(ctx context.Context) error {
	resp, err := c.openapi.GetOrganizationWithResponse(ctx, c.organization)
	if err != nil {
		return xerror.Wrapf("verify organization access: %w", err)
	}
	if resp.StatusCode() != http.StatusOK {
		return decodeAPIError(resp.StatusCode(), resp.Body)
	}

	return nil
}

func (c *cliAPIClient) listPipelines(
	ctx context.Context,
	projectSlug string,
	params *api.ListPipelinesParams,
) (*api.PipelineListResponse, error) {
	resp, err := c.openapi.ListPipelinesWithResponse(
		ctx,
		c.organization,
		projectSlug,
		params,
	)
	if err != nil {
		return nil, xerror.Wrapf("list pipelines: %w", err)
	}
	if resp.StatusCode() != http.StatusOK {
		return nil, decodeAPIError(resp.StatusCode(), resp.Body)
	}
	if resp.JSON200 == nil {
		return nil, xerror.Unexpected().Detail("unexpected API response: missing pipeline list payload")
	}

	return resp.JSON200, nil
}

func (c *cliAPIClient) getPipeline(
	ctx context.Context,
	projectSlug string,
	pipelineSlug string,
) (*api.Pipeline, error) {
	resp, err := c.openapi.GetPipelineWithResponse(
		ctx,
		c.organization,
		projectSlug,
		pipelineSlug,
	)
	if err != nil {
		return nil, xerror.Wrapf("get pipeline: %w", err)
	}
	if resp.StatusCode() != http.StatusOK {
		return nil, decodeAPIError(resp.StatusCode(), resp.Body)
	}
	if resp.JSON200 == nil {
		return nil, xerror.Unexpected().Detail("unexpected API response: missing pipeline payload")
	}

	return resp.JSON200, nil
}

func (c *cliAPIClient) updatePipeline(
	ctx context.Context,
	projectSlug string,
	pipelineSlug string,
	body api.UpdatePipeline,
) (*api.Pipeline, error) {
	resp, err := c.openapi.UpdatePipelineWithResponse(
		ctx,
		c.organization,
		projectSlug,
		pipelineSlug,
		body,
	)
	if err != nil {
		return nil, xerror.Wrapf("update pipeline: %w", err)
	}
	if resp.StatusCode() != http.StatusOK {
		return nil, decodeAPIError(resp.StatusCode(), resp.Body)
	}
	if resp.JSON200 == nil {
		return nil, xerror.Unexpected().Detail("unexpected API response: missing updated pipeline payload")
	}

	return resp.JSON200, nil
}

func (c *cliAPIClient) validatePipeline(
	ctx context.Context,
	projectSlug string,
	environmentSlug string,
	body api.PipelineValidationRequest,
) (*api.PipelineValidationResponse, error) {
	resp, err := c.openapi.ValidatePipelineWithResponse(
		ctx,
		c.organization,
		projectSlug,
		environmentSlug,
		body,
	)
	if err != nil {
		return nil, xerror.Wrapf("validate pipeline: %w", err)
	}
	if resp.StatusCode() != http.StatusOK {
		return nil, &cliAPIStatusError{
			statusCode: resp.StatusCode(),
			err:        decodeAPIError(resp.StatusCode(), resp.Body),
		}
	}
	if resp.JSON200 == nil {
		return nil, xerror.Unexpected().Detail("unexpected API response: missing validation payload")
	}

	return resp.JSON200, nil
}

func (c *cliAPIClient) formatPipelineSpec(
	ctx context.Context,
	body api.PipelineSpecFormatRequest,
) (*api.PipelineSpecFormatResponse, error) {
	resp, err := c.openapi.FormatPipelineSpecWithResponse(ctx, c.organization, body)
	if err != nil {
		return nil, xerror.Wrapf("format pipeline spec: %w", err)
	}
	if resp.StatusCode() != http.StatusOK {
		return nil, decodeAPIError(resp.StatusCode(), resp.Body)
	}
	if resp.JSON200 == nil {
		return nil, xerror.Unexpected().Detail("unexpected API response: missing pipeline spec format payload")
	}

	return resp.JSON200, nil
}

// listEvents fetches tenant-scoped durable event history.
func (c *cliAPIClient) listEvents(
	ctx context.Context,
	params *api.ListEventsParams,
) (*api.EventListResponse, error) {
	resp, err := c.openapi.ListEventsWithResponse(ctx, c.organization, params)
	if err != nil {
		return nil, xerror.Wrapf("list events: %w", err)
	}
	if resp.StatusCode() != http.StatusOK {
		return nil, decodeAPIError(resp.StatusCode(), resp.Body)
	}
	if resp.JSON200 == nil {
		return nil, xerror.Unexpected().Detail("unexpected API response: missing event list payload")
	}

	return resp.JSON200, nil
}

func (c *cliAPIClient) listPipelineVersions(
	ctx context.Context,
	projectSlug string,
	pipelineSlug string,
) (*api.PipelineVersionListResponse, error) {
	resp, err := c.openapi.ListPipelineVersionsWithResponse(
		ctx,
		c.organization,
		projectSlug,
		pipelineSlug,
	)
	if err != nil {
		return nil, xerror.Wrapf("list pipeline versions: %w", err)
	}
	if resp.StatusCode() != http.StatusOK {
		return nil, decodeAPIError(resp.StatusCode(), resp.Body)
	}
	if resp.JSON200 == nil {
		return nil, xerror.Unexpected().Detail(
			"unexpected API response: missing pipeline version list payload",
		)
	}

	return resp.JSON200, nil
}

func (c *cliAPIClient) listPipelineRuns(
	ctx context.Context,
	projectSlug string,
	environmentSlug string,
	params *api.ListPipelineRunsParams,
) (*api.PipelineRunListResponse, error) {
	resp, err := c.openapi.ListPipelineRunsWithResponse(
		ctx,
		c.organization,
		projectSlug,
		environmentSlug,
		params,
	)
	if err != nil {
		return nil, xerror.Wrapf("list pipeline runs: %w", err)
	}
	if resp.StatusCode() != http.StatusOK {
		return nil, decodeAPIError(resp.StatusCode(), resp.Body)
	}
	if resp.JSON200 == nil {
		return nil, xerror.Unexpected().Detail("unexpected API response: missing pipeline run list payload")
	}

	return resp.JSON200, nil
}

// listPipelineTriggers fetches trigger definitions for one project environment.
func (c *cliAPIClient) listPipelineTriggers(
	ctx context.Context,
	projectSlug string,
	environmentSlug string,
	params *api.ListPipelineTriggersParams,
) (*api.PipelineTriggerListResponse, error) {
	resp, err := c.openapi.ListPipelineTriggersWithResponse(
		ctx,
		c.organization,
		projectSlug,
		environmentSlug,
		params,
	)
	if err != nil {
		return nil, xerror.Wrapf("list pipeline triggers: %w", err)
	}
	if resp.StatusCode() != http.StatusOK {
		return nil, decodeAPIError(resp.StatusCode(), resp.Body)
	}
	if resp.JSON200 == nil {
		return nil, xerror.Unexpected().Detail("unexpected API response: missing pipeline trigger list payload")
	}

	return resp.JSON200, nil
}

// getPipelineTrigger fetches one trigger definition from a project environment.
func (c *cliAPIClient) getPipelineTrigger(
	ctx context.Context,
	projectSlug string,
	environmentSlug string,
	triggerSlug string,
) (*api.PipelineTrigger, error) {
	resp, err := c.openapi.GetPipelineTriggerWithResponse(
		ctx,
		c.organization,
		projectSlug,
		environmentSlug,
		triggerSlug,
	)
	if err != nil {
		return nil, xerror.Wrapf("get pipeline trigger: %w", err)
	}
	if resp.StatusCode() != http.StatusOK {
		return nil, decodeAPIError(resp.StatusCode(), resp.Body)
	}
	if resp.JSON200 == nil {
		return nil, xerror.Unexpected().Detail("unexpected API response: missing pipeline trigger payload")
	}

	return resp.JSON200, nil
}

// createPipelineTrigger persists one trigger definition in a project environment.
func (c *cliAPIClient) createPipelineTrigger(
	ctx context.Context,
	projectSlug string,
	environmentSlug string,
	body api.NewPipelineTrigger,
) (*api.PipelineTriggerIssuanceResponse, error) {
	resp, err := c.openapi.CreatePipelineTriggerWithResponse(
		ctx,
		c.organization,
		projectSlug,
		environmentSlug,
		body,
	)
	if err != nil {
		return nil, xerror.Wrapf("create pipeline trigger: %w", err)
	}
	if resp.StatusCode() != http.StatusCreated {
		return nil, decodeAPIError(resp.StatusCode(), resp.Body)
	}
	if resp.JSON201 == nil {
		return nil, xerror.Unexpected().Detail("unexpected API response: missing created pipeline trigger payload")
	}
	if body.Type == api.PipelineTriggerTypeWebhook && resp.JSON201.WebhookSecret == nil {
		return nil, xerror.Unexpected().Detail("unexpected API response: missing webhook secret issuance")
	}
	if body.Type == api.PipelineTriggerTypeSchedule && resp.JSON201.WebhookSecret != nil {
		return nil, xerror.Unexpected().Detail("unexpected API response: schedule trigger included a webhook secret")
	}

	return resp.JSON201, nil
}

// updatePipelineTrigger fully replaces mutable trigger fields.
func (c *cliAPIClient) updatePipelineTrigger(
	ctx context.Context,
	projectSlug string,
	environmentSlug string,
	triggerSlug string,
	body api.UpdatePipelineTrigger,
) (*api.PipelineTrigger, error) {
	resp, err := c.openapi.UpdatePipelineTriggerWithResponse(
		ctx,
		c.organization,
		projectSlug,
		environmentSlug,
		triggerSlug,
		body,
	)
	if err != nil {
		return nil, xerror.Wrapf("update pipeline trigger: %w", err)
	}
	if resp.StatusCode() != http.StatusOK {
		return nil, decodeAPIError(resp.StatusCode(), resp.Body)
	}
	if resp.JSON200 == nil {
		return nil, xerror.Unexpected().Detail("unexpected API response: missing updated pipeline trigger payload")
	}

	return resp.JSON200, nil
}

// deletePipelineTrigger removes one trigger definition from a project environment.
func (c *cliAPIClient) deletePipelineTrigger(
	ctx context.Context,
	projectSlug string,
	environmentSlug string,
	triggerSlug string,
) error {
	resp, err := c.openapi.DeletePipelineTriggerWithResponse(
		ctx,
		c.organization,
		projectSlug,
		environmentSlug,
		triggerSlug,
	)
	if err != nil {
		return xerror.Wrapf("delete pipeline trigger: %w", err)
	}
	if resp.StatusCode() != http.StatusNoContent {
		return decodeAPIError(resp.StatusCode(), resp.Body)
	}

	return nil
}

func (c *cliAPIClient) rotatePipelineWebhookSecret(
	ctx context.Context,
	projectSlug string,
	environmentSlug string,
	triggerSlug string,
) (*api.PipelineWebhookSecretIssuance, error) {
	resp, err := c.openapi.RotatePipelineWebhookSecretWithResponse(
		ctx,
		c.organization,
		projectSlug,
		environmentSlug,
		triggerSlug,
	)
	if err != nil {
		return nil, xerror.Wrapf("rotate pipeline webhook secret: %w", err)
	}
	if resp.StatusCode() != http.StatusOK {
		return nil, decodeAPIError(resp.StatusCode(), resp.Body)
	}
	if resp.JSON200 == nil {
		return nil, xerror.Unexpected().Detail("unexpected API response: missing webhook secret issuance")
	}
	return resp.JSON200, nil
}

func (c *cliAPIClient) getPipelineRun(
	ctx context.Context,
	projectSlug string,
	environmentSlug string,
	runSlug string,
) (*api.PipelineRunRecord, error) {
	resp, err := c.openapi.GetPipelineRunWithResponse(
		ctx,
		c.organization,
		projectSlug,
		environmentSlug,
		runSlug,
	)
	if err != nil {
		return nil, xerror.Wrapf("get pipeline run: %w", err)
	}
	if resp.StatusCode() != http.StatusOK {
		return nil, decodeAPIError(resp.StatusCode(), resp.Body)
	}
	if resp.JSON200 == nil {
		return nil, xerror.Unexpected().Detail("unexpected API response: missing pipeline run payload")
	}

	return resp.JSON200, nil
}

// listPipelineRunTriggerRetryAttempts fetches sanitized retry diagnostics for one run retry group.
func (c *cliAPIClient) listPipelineRunTriggerRetryAttempts(
	ctx context.Context,
	projectSlug string,
	environmentSlug string,
	runSlug string,
) (*api.PipelineRunTriggerRetryAttemptListResponse, error) {
	resp, err := c.openapi.ListPipelineRunTriggerRetryAttemptsWithResponse(
		ctx,
		c.organization,
		projectSlug,
		environmentSlug,
		runSlug,
	)
	if err != nil {
		return nil, xerror.Wrapf("list pipeline run trigger retry attempts: %w", err)
	}
	if resp.StatusCode() != http.StatusOK {
		return nil, decodeAPIError(resp.StatusCode(), resp.Body)
	}
	if resp.JSON200 == nil {
		return nil, xerror.Unexpected().Detail("unexpected API response: missing pipeline run retry attempts payload")
	}

	return resp.JSON200, nil
}

func (c *cliAPIClient) executePipeline(
	ctx context.Context,
	projectSlug string,
	environmentSlug string,
	pipelineSlug string,
	body api.ExecutePipelineRequest,
) (*api.PipelineRunRecord, error) {
	resp, err := c.openapi.ExecutePipelineWithResponse(
		ctx,
		c.organization,
		projectSlug,
		environmentSlug,
		pipelineSlug,
		body,
	)
	if err != nil {
		return nil, xerror.Wrapf("execute pipeline: %w", err)
	}
	switch resp.StatusCode() {
	case http.StatusAccepted:
		if resp.JSON202 == nil {
			return nil, xerror.Unexpected().Detail("unexpected API response: missing accepted pipeline run payload")
		}
		return resp.JSON202, nil
	default:
		return nil, decodeAPIError(resp.StatusCode(), resp.Body)
	}
}

func (c *cliAPIClient) deployPipeline(
	ctx context.Context,
	projectSlug string,
	environmentSlug string,
	pipelineSlug string,
	body api.PipelineDeploymentRequest,
) (*api.PipelineDeployment, error) {
	resp, err := c.openapi.DeployPipelineWithResponse(
		ctx,
		c.organization,
		projectSlug,
		environmentSlug,
		pipelineSlug,
		body,
	)
	if err != nil {
		return nil, xerror.Wrapf("deploy pipeline: %w", err)
	}
	if resp.StatusCode() != http.StatusOK {
		return nil, decodeAPIError(resp.StatusCode(), resp.Body)
	}
	if resp.JSON200 == nil {
		return nil, xerror.Unexpected().Detail("unexpected API response: missing pipeline deployment payload")
	}

	return resp.JSON200, nil
}

func (c *cliAPIClient) getPipelineVersion(
	ctx context.Context,
	projectSlug string,
	pipelineSlug, specHash string,
) (*api.PipelineVersion, error) {
	resp, err := c.openapi.GetPipelineVersionWithResponse(
		ctx,
		c.organization,
		projectSlug,
		pipelineSlug,
		specHash,
	)
	if err != nil {
		return nil, xerror.Wrapf("get pipeline version: %w", err)
	}
	if resp.StatusCode() != http.StatusOK {
		return nil, decodeAPIError(resp.StatusCode(), resp.Body)
	}
	if resp.JSON200 == nil {
		return nil, xerror.Unexpected().Detail(
			"unexpected API response: missing pipeline version payload",
		)
	}

	return resp.JSON200, nil
}

func (c *cliAPIClient) getPipelineDraft(
	ctx context.Context,
	projectSlug string,
	pipelineSlug string,
) (*api.PipelineDraftResponse, error) {
	resp, err := c.openapi.GetPipelineDraftWithResponse(
		ctx,
		c.organization,
		projectSlug,
		pipelineSlug,
	)
	if err != nil {
		return nil, xerror.Wrapf("get pipeline draft: %w", err)
	}
	if resp.StatusCode() != http.StatusOK {
		return nil, decodeAPIError(resp.StatusCode(), resp.Body)
	}
	if resp.JSON200 == nil {
		return nil, xerror.Unexpected().Detail("unexpected API response: missing pipeline draft payload")
	}

	return resp.JSON200, nil
}

func (c *cliAPIClient) deletePipelineDraft(
	ctx context.Context,
	projectSlug string,
	pipelineSlug string,
) error {
	resp, err := c.openapi.DeletePipelineDraftWithResponse(
		ctx,
		c.organization,
		projectSlug,
		pipelineSlug,
	)
	if err != nil {
		return xerror.Wrapf("delete pipeline draft: %w", err)
	}
	if resp.StatusCode() != http.StatusNoContent {
		return decodeAPIError(resp.StatusCode(), resp.Body)
	}

	return nil
}

func (c *cliAPIClient) savePipelineDraft(
	ctx context.Context,
	projectSlug string,
	pipelineSlug string,
	body api.SavePipelineDraftRequest,
) (*api.PipelineDraftResponse, error) {
	resp, err := c.openapi.SavePipelineDraftWithResponse(
		ctx,
		c.organization,
		projectSlug,
		pipelineSlug,
		body,
	)
	if err != nil {
		return nil, xerror.Wrapf("save pipeline draft: %w", err)
	}
	if resp.StatusCode() == http.StatusConflict {
		return nil, decodePipelineDraftConflictError(resp.StatusCode(), resp.Body)
	}
	if resp.StatusCode() != http.StatusOK {
		return nil, decodeAPIError(resp.StatusCode(), resp.Body)
	}
	if resp.JSON200 == nil {
		return nil, xerror.Unexpected().Detail("unexpected API response: missing saved pipeline draft payload")
	}

	return resp.JSON200, nil
}

func (c *cliAPIClient) publishPipelineDraft(
	ctx context.Context,
	projectSlug string,
	pipelineSlug string,
	body api.PublishPipelineDraftRequest,
) (*api.Pipeline, error) {
	resp, err := c.openapi.PublishPipelineDraftWithResponse(
		ctx,
		c.organization,
		projectSlug,
		pipelineSlug,
		body,
	)
	if err != nil {
		return nil, xerror.Wrapf("publish pipeline draft: %w", err)
	}
	if resp.StatusCode() == http.StatusConflict {
		return nil, decodePipelineDraftConflictError(resp.StatusCode(), resp.Body)
	}
	if resp.StatusCode() != http.StatusOK {
		return nil, decodeAPIError(resp.StatusCode(), resp.Body)
	}
	if resp.JSON200 == nil {
		return nil, xerror.Unexpected().Detail("unexpected API response: missing published pipeline payload")
	}

	return resp.JSON200, nil
}

func (c *cliAPIClient) applyPipelines(
	ctx context.Context,
	projectSlug string,
	body api.PipelineReconcileRequest,
) (*api.PipelineReconcileResponse, error) {
	resp, err := c.openapi.ReconcilePipelinesWithResponse(
		ctx,
		c.organization,
		projectSlug,
		body,
	)
	if err != nil {
		return nil, xerror.Wrapf("apply pipelines: %w", err)
	}
	if resp.StatusCode() != http.StatusOK {
		return nil, decodeAPIError(resp.StatusCode(), resp.Body)
	}
	if resp.JSON200 == nil {
		return nil, xerror.Unexpected().Detail("unexpected API response: missing pipeline apply payload")
	}

	return resp.JSON200, nil
}

func decodeAPIError(statusCode int, body []byte) error {
	var response api.ErrorResponse
	if err := json.Unmarshal(body, &response); err != nil {
		return xerror.Unexpected().Detailf(
			"unexpected API response: status %d",
			statusCode,
		)
	}

	parts := []string{
		fmt.Sprintf("API request failed: status %d", statusCode),
	}
	if response.Error.Code != "" {
		parts = append(parts, response.Error.Code)
	}
	if response.Error.Message != "" {
		parts = append(parts, response.Error.Message)
	}
	if response.Error.DetailedError != nil && *response.Error.DetailedError != "" {
		parts = append(parts, *response.Error.DetailedError)
	}

	return xerror.Unexpected().Detail(strings.Join(parts, ": "))
}

func decodePipelineDraftConflictError(statusCode int, body []byte) error {
	var response api.PipelineDraftConflictResponse
	if err := json.Unmarshal(body, &response); err != nil {
		return decodeAPIError(statusCode, body)
	}
	if response.Error.Code == "" || response.PipelineSlug == "" {
		return decodeAPIError(statusCode, body)
	}

	parts := []string{
		fmt.Sprintf("API request failed: status %d", statusCode),
		response.Error.Code,
	}
	if response.Error.Message != "" {
		parts = append(parts, response.Error.Message)
	}
	parts = append(parts, fmt.Sprintf("pipeline_slug=%s", response.PipelineSlug))
	if response.ExpectedSpecHash != nil {
		parts = append(parts, fmt.Sprintf("expected_spec_hash=%s", *response.ExpectedSpecHash))
	}
	if response.CurrentSpecHash != nil {
		parts = append(parts, fmt.Sprintf("current_spec_hash=%s", *response.CurrentSpecHash))
	}
	if response.DraftBasedOnSpecHash != nil {
		parts = append(parts, fmt.Sprintf("draft_based_on_spec_hash=%s", *response.DraftBasedOnSpecHash))
	}
	parts = append(parts, fmt.Sprintf("draft_preserved=%t", response.DraftPreserved))
	parts = append(parts, fmt.Sprintf("can_overwrite=%t", response.CanOverwrite))

	return xerror.Unexpected().Detail(strings.Join(parts, ": "))
}
