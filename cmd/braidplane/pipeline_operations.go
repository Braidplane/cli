package main

import (
	"context"
	"strings"

	api "github.com/Braidplane/cli/internal/apiclient"
	"github.com/Braidplane/cli/pkg/xerror"
)

type pipelineDeploymentCommand struct {
	List listPipelineDeploymentsCommand `cmd:"" help:"List all current pipeline deployments for an environment."`
	Get  getPipelineDeploymentCommand   `cmd:"" help:"Get the current deployment for one pipeline in an environment."`
}

type listPipelineDeploymentsCommand struct {
	apiPipelineFlags
	Environment string `help:"Project environment slug." name:"environment" env:"BRAIDPLANE_ENVIRONMENT" required:""`
}

func (c *listPipelineDeploymentsCommand) Run(output *cliOutput, runtime *cliRuntime) error {
	client, project, environment, err := newPipelineEnvironmentAPIClient(c.apiPipelineFlags, c.Environment, runtime)
	if err != nil {
		return err
	}
	deployments, err := client.listPipelineDeployments(context.Background(), project, environment)
	if err != nil {
		return err
	}
	return writeCLIJSON(output.stdout, deployments)
}

type getPipelineDeploymentCommand struct {
	apiPipelineFlags
	Environment  string `help:"Project environment slug." name:"environment" env:"BRAIDPLANE_ENVIRONMENT" required:""`
	PipelineSlug string `arg:"" name:"pipeline-slug" required:"" help:"Pipeline slug."`
}

func (c *getPipelineDeploymentCommand) Run(output *cliOutput, runtime *cliRuntime) error {
	client, project, environment, err := newPipelineEnvironmentAPIClient(c.apiPipelineFlags, c.Environment, runtime)
	if err != nil {
		return err
	}
	deployment, err := client.getPipelineDeployment(context.Background(), project, environment, c.PipelineSlug)
	if err != nil {
		return err
	}
	return writeCLIJSON(output.stdout, deployment)
}

type pipelineValidationHistoryCommand struct {
	List       listPipelineValidationResultsCommand `cmd:"" help:"List persisted pipeline validation results for an environment."`
	Get        getPipelineValidationResultCommand   `cmd:"" help:"Get one persisted pipeline validation result."`
	Revalidate revalidatePipelineCommand            `cmd:"" help:"Revalidate a canonical pipeline without creating a run."`
}

type listPipelineValidationResultsCommand struct {
	apiPipelineFlags
	Environment  string `help:"Project environment slug." name:"environment" env:"BRAIDPLANE_ENVIRONMENT" required:""`
	PipelineSlug string `help:"Filter by pipeline slug." name:"pipeline"`
	Limit        int32  `help:"Maximum number of validation records to return." name:"limit"`
}

func (c *listPipelineValidationResultsCommand) Run(output *cliOutput, runtime *cliRuntime) error {
	client, project, environment, err := newPipelineEnvironmentAPIClient(c.apiPipelineFlags, c.Environment, runtime)
	if err != nil {
		return err
	}
	params, err := pipelineValidationResultsParams(c.Limit, c.PipelineSlug)
	if err != nil {
		return err
	}
	results, err := client.listPipelineValidationResults(context.Background(), project, environment, params)
	if err != nil {
		return err
	}
	return writeCLIJSON(output.stdout, results)
}

type getPipelineValidationResultCommand struct {
	apiPipelineFlags
	Environment    string `help:"Project environment slug." name:"environment" env:"BRAIDPLANE_ENVIRONMENT" required:""`
	ValidationSlug string `arg:"" name:"validation-slug" required:"" help:"Validation result slug."`
}

func (c *getPipelineValidationResultCommand) Run(output *cliOutput, runtime *cliRuntime) error {
	client, project, environment, err := newPipelineEnvironmentAPIClient(c.apiPipelineFlags, c.Environment, runtime)
	if err != nil {
		return err
	}
	result, err := client.getPipelineValidationResult(context.Background(), project, environment, c.ValidationSlug)
	if err != nil {
		return err
	}
	return writeCLIJSON(output.stdout, result)
}

type revalidatePipelineCommand struct {
	apiPipelineFlags
	Environment  string `help:"Project environment slug." name:"environment" env:"BRAIDPLANE_ENVIRONMENT" required:""`
	PipelineSlug string `arg:"" name:"pipeline-slug" required:"" help:"Pipeline slug."`
}

func (c *revalidatePipelineCommand) Run(output *cliOutput, runtime *cliRuntime) error {
	client, project, environment, err := newPipelineEnvironmentPATAPIClient(c.apiPipelineFlags, c.Environment, runtime)
	if err != nil {
		return err
	}
	result, err := client.revalidatePipeline(context.Background(), project, environment, c.PipelineSlug)
	if err != nil {
		return err
	}
	return writeCLIJSON(output.stdout, result)
}

func pipelineValidationResultsParams(limit int32, pipeline string) (*api.ListPipelineValidationResultsParams, error) {
	if limit < 0 || limit > 100 {
		return nil, xerror.InvalidInput(xerror.Pipeline).Detail("limit must be between 1 and 100")
	}
	params := &api.ListPipelineValidationResultsParams{}
	if limit > 0 {
		v := limit
		params.Limit = &v
	}
	if strings.TrimSpace(pipeline) != "" {
		v := strings.TrimSpace(pipeline)
		params.PipelineSlug = &v
	}
	return params, nil
}

type pipelineDriftCommand struct {
	List listDriftImpactReportsCommand `cmd:"" help:"List drift and impact reports for a project."`
	Get  getDriftImpactReportCommand   `cmd:"" help:"Get one drift or impact report."`
}

type listDriftImpactReportsCommand struct {
	apiPipelineFlags
	PipelineSlug   string `help:"Filter by pipeline slug." name:"pipeline"`
	DataSourceSlug string `help:"Filter by data-source slug." name:"data-source"`
	Status         string `help:"Filter by report status." name:"status"`
	Severity       string `help:"Filter by report severity." name:"severity"`
	Limit          int    `help:"Maximum number of reports to return." name:"limit"`
}

func (c *listDriftImpactReportsCommand) Run(output *cliOutput, runtime *cliRuntime) error {
	client, project, err := newPipelineAPIClient(c.apiPipelineFlags, runtime)
	if err != nil {
		return err
	}
	params, err := c.params()
	if err != nil {
		return err
	}
	reports, err := client.listDriftImpactReports(context.Background(), project, params)
	if err != nil {
		return err
	}
	return writeCLIJSON(output.stdout, reports)
}

func (c *listDriftImpactReportsCommand) params() (*api.ListDriftImpactReportsParams, error) {
	if c.Limit < 0 {
		return nil, xerror.InvalidInput(xerror.Pipeline).Detail("limit must be positive")
	}
	p := &api.ListDriftImpactReportsParams{}
	if c.Limit > 0 {
		p.Limit = &c.Limit
	}
	if v := strings.TrimSpace(c.PipelineSlug); v != "" {
		s := v
		p.PipelineSlug = &s
	}
	if v := strings.TrimSpace(c.DataSourceSlug); v != "" {
		s := v
		p.DataSourceSlug = &s
	}
	if v := strings.TrimSpace(c.Status); v != "" {
		s := api.DriftImpactReportStatus(v)
		if !s.Valid() {
			return nil, xerror.InvalidInput(xerror.Pipeline).Detail("unsupported drift report status")
		}
		p.Status = &s
	}
	if v := strings.TrimSpace(c.Severity); v != "" {
		s := api.DriftImpactReportSeverity(v)
		if !s.Valid() {
			return nil, xerror.InvalidInput(xerror.Pipeline).Detail("unsupported drift report severity")
		}
		p.Severity = &s
	}
	return p, nil
}

type getDriftImpactReportCommand struct {
	apiPipelineFlags
	ReportSlug string `arg:"" name:"report-slug" required:"" help:"Drift or impact report slug."`
}

func (c *getDriftImpactReportCommand) Run(output *cliOutput, runtime *cliRuntime) error {
	client, project, err := newPipelineAPIClient(c.apiPipelineFlags, runtime)
	if err != nil {
		return err
	}
	report, err := client.getDriftImpactReport(context.Background(), project, c.ReportSlug)
	if err != nil {
		return err
	}
	return writeCLIJSON(output.stdout, report)
}
