package main

import (
	"bytes"
	"context"
	"encoding/json"

	api "github.com/Braidplane/cli/internal/apiclient"
	"github.com/Braidplane/cli/pkg/xerror"
)

type proofCommand struct {
	Receipt       proofReceiptCommand       `cmd:"" help:"Inspect immutable Proof receipts."`
	Configuration proofConfigurationCommand `cmd:"" help:"Inspect and configure managed-source Proof assessments."`
}

type proofConfigurationCommand struct {
	Get getProofConfigurationCommand `cmd:"" help:"Get Proof settings, status, and available targets as JSON."`
	Set setProofConfigurationCommand `cmd:"" help:"Replace Proof settings using an explicit JSON request file."`
}

type getProofConfigurationCommand struct {
	apiAuthFlags
	DataSourceSlug string `arg:"" name:"data-source-slug" help:"Managed data source slug." required:""`
}

func (c *getProofConfigurationCommand) Run(output *cliOutput, runtime *cliRuntime) error {
	client, err := newAPIClient(c.apiAuthFlags, runtime)
	if err != nil {
		return err
	}
	configuration, err := client.getProofConfiguration(context.Background(), c.DataSourceSlug)
	if err != nil {
		return err
	}
	return writeCLIJSON(output.stdout, configuration)
}

type setProofConfigurationCommand struct {
	getProofConfigurationCommand
	File string `help:"JSON file with enabled, publish_checks, expected_repository_id, and targets." name:"file" type:"path" required:""`
}

func (c *setProofConfigurationCommand) Run(output *cliOutput, runtime *cliRuntime) error {
	data, err := optionalJSONSource("", c.File, "Proof settings")
	if err != nil {
		return err
	}
	var settings api.DataSourceProofSettings
	if err := decodeStrictJSON(data, &settings, "Proof settings"); err != nil {
		return err
	}
	// A full replacement must never infer false or empty targets from omitted fields.
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		return xerror.Wrapf("decode Proof settings: %w", err)
	}
	for _, name := range []string{"enabled", "publish_checks", "expected_repository_id", "targets"} {
		if value, ok := fields[name]; !ok || bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
			return xerror.ValidationFailed().Detailf("Proof settings require explicit %s", name)
		}
	}
	client, err := newAPIClient(c.apiAuthFlags, runtime)
	if err != nil {
		return err
	}
	configuration, err := client.setProofConfiguration(context.Background(), c.DataSourceSlug, settings)
	if err != nil {
		return err
	}
	return writeCLIJSON(output.stdout, configuration)
}

func (c *cliAPIClient) getProofConfiguration(ctx context.Context, dataSourceSlug string) (*api.DataSourceProofConfiguration, error) {
	resp, err := c.openapi.GetDataSourceProofConfigurationWithResponse(ctx, c.organization, dataSourceSlug)
	if err != nil {
		return nil, xerror.Wrapf("get Proof configuration: %w", err)
	}
	if resp.StatusCode() != 200 {
		return nil, decodeAPIError(resp.StatusCode(), resp.Body)
	}
	if resp.JSON200 == nil {
		return nil, xerror.Unexpected().Detail("unexpected API response: missing Proof configuration payload")
	}
	return resp.JSON200, nil
}

func (c *cliAPIClient) setProofConfiguration(ctx context.Context, dataSourceSlug string, settings api.DataSourceProofSettings) (*api.DataSourceProofConfiguration, error) {
	resp, err := c.openapi.SetDataSourceProofConfigurationWithResponse(ctx, c.organization, dataSourceSlug, settings)
	if err != nil {
		return nil, xerror.Wrapf("set Proof configuration: %w", err)
	}
	if resp.StatusCode() != 200 {
		return nil, decodeAPIError(resp.StatusCode(), resp.Body)
	}
	if resp.JSON200 == nil {
		return nil, xerror.Unexpected().Detail("unexpected API response: missing Proof configuration payload")
	}
	return resp.JSON200, nil
}

type proofReceiptCommand struct {
	Get getProofReceiptCommand `cmd:"" help:"Get one tenant-scoped Proof receipt as JSON."`
}

type getProofReceiptCommand struct {
	apiPipelineFlags
	Environment string `help:"Runtime environment slug. Defaults to the configured environment." name:"environment"`
	ReceiptSlug string `arg:"" name:"receipt-slug" help:"Proof receipt slug." required:""`
}

func (c *getProofReceiptCommand) Run(output *cliOutput, runtime *cliRuntime) error {
	client, projectSlug, environmentSlug, err := newPipelineEnvironmentAPIClient(c.apiPipelineFlags, c.Environment, runtime)
	if err != nil {
		return err
	}
	receipt, err := client.getProofReceipt(context.Background(), projectSlug, environmentSlug, c.ReceiptSlug)
	if err != nil {
		return err
	}
	return writeCLIJSON(output.stdout, receipt)
}

func (c *cliAPIClient) getProofReceipt(ctx context.Context, projectSlug, environmentSlug, receiptSlug string) (*api.ProofReceipt, error) {
	resp, err := c.openapi.GetProofReceiptWithResponse(ctx, c.organization, projectSlug, environmentSlug, receiptSlug)
	if err != nil {
		return nil, xerror.Wrapf("get proof receipt: %w", err)
	}
	if resp.StatusCode() != 200 {
		return nil, decodeAPIError(resp.StatusCode(), resp.Body)
	}
	if resp.JSON200 == nil {
		return nil, xerror.Unexpected().Detail("unexpected API response: missing proof receipt payload")
	}
	return resp.JSON200, nil
}
