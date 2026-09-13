package main

import (
	"context"

	api "github.com/Braidplane/cli/internal/apiclient"
	"github.com/Braidplane/cli/pkg/xerror"
)

type proofCommand struct {
	Receipt proofReceiptCommand `cmd:"" help:"Inspect immutable Proof receipts."`
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
