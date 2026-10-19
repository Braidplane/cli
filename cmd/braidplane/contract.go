package main

import (
	"context"
	"encoding/json"
	"os"
	"strings"

	api "github.com/Braidplane/cli/internal/apiclient"
	"github.com/Braidplane/cli/pkg/slug"
	"github.com/Braidplane/cli/pkg/xerror"
)

type contractCommand struct {
	Import   importODCSArtifactsCommand `cmd:"" help:"Preview or apply contracts/ artifacts from the configured Git backend."`
	List     listODCSContractsCommand   `cmd:"" help:"List project-owned ODCS contracts."`
	Get      getODCSContractCommand     `cmd:"" help:"Get one project-owned ODCS contract."`
	Create   createODCSContractCommand  `cmd:"" help:"Create one project-owned ODCS contract."`
	Update   updateODCSContractCommand  `cmd:"" help:"Update mutable ODCS contract metadata."`
	Revision odcsRevisionCommand        `cmd:"" help:"Manage immutable ODCS contract revisions."`
}

type odcsRevisionCommand struct {
	Inspect inspectODCSRevisionCommand       `cmd:"" help:"Inspect an inert complete document and static declaration coverage (may include descriptive personal information)."`
	List    listODCSRevisionsCommand         `cmd:"" help:"List immutable revisions for an ODCS contract."`
	Get     getODCSRevisionCommand           `cmd:"" help:"Get one immutable ODCS contract revision."`
	Create  createODCSRevisionCommand        `cmd:"" help:"Record one immutable ODCS contract revision from JSON."`
	Impact  previewODCSRevisionImpactCommand `cmd:"" help:"Preview known pipeline consumers and safe diagnostics between two revisions."`
}

type listODCSContractsCommand struct {
	apiPipelineFlags
	Limit     int32  `help:"Maximum number of contracts to return." name:"limit"`
	Cursor    string `help:"Pagination cursor from a previous list response." name:"cursor"`
	Order     string `help:"Sort order: asc or desc." name:"order"`
	Direction string `help:"Pagination direction: next or prev." name:"direction"`
}

func (c *listODCSContractsCommand) Run(output *cliOutput, runtime *cliRuntime) error {
	client, project, err := newPipelineAPIClient(c.apiPipelineFlags, runtime)
	if err != nil {
		return err
	}
	params, err := odcsContractListParams(c.Limit, c.Cursor, c.Order, c.Direction)
	if err != nil {
		return err
	}
	result, err := client.listODCSContracts(context.Background(), project, params)
	if err != nil {
		return err
	}
	return writeCLIJSON(output.stdout, result)
}

type getODCSContractCommand struct {
	apiPipelineFlags
	ContractSlug string `arg:"" name:"contract-slug" help:"ODCS contract slug." required:""`
}

func (c *getODCSContractCommand) Run(output *cliOutput, runtime *cliRuntime) error {
	client, project, err := newPipelineAPIClient(c.apiPipelineFlags, runtime)
	if err != nil {
		return err
	}
	contract, err := requiredODCSSlug(c.ContractSlug, "contract slug")
	if err != nil {
		return err
	}
	result, err := client.getODCSContract(context.Background(), project, contract)
	if err != nil {
		return err
	}
	return writeCLIJSON(output.stdout, result)
}

type createODCSContractCommand struct {
	apiPipelineFlags
	Name   string `help:"ODCS contract display name." name:"name" required:""`
	Slug   string `help:"ODCS contract slug." name:"slug" required:""`
	ODCSID string `help:"Stable ODCS contract identifier." name:"odcs-id" required:""`
}

func (c *createODCSContractCommand) Run(output *cliOutput, runtime *cliRuntime) error {
	client, project, err := newPipelineAPIClient(c.apiPipelineFlags, runtime)
	if err != nil {
		return err
	}
	contract, err := requiredODCSSlug(c.Slug, "slug")
	if err != nil {
		return err
	}
	name, odcsID := strings.TrimSpace(c.Name), strings.TrimSpace(c.ODCSID)
	if name == "" || odcsID == "" {
		return xerror.ValidationFailed().Detail("name and odcs-id are required")
	}
	result, err := client.createODCSContract(context.Background(), project, api.NewODCSContract{Name: name, Slug: contract, OdcsId: odcsID})
	if err != nil {
		return err
	}
	return writeCLIJSON(output.stdout, result)
}

type updateODCSContractCommand struct {
	apiPipelineFlags
	ContractSlug string `arg:"" name:"contract-slug" help:"ODCS contract slug." required:""`
	Name         string `help:"ODCS contract display name." name:"name" required:""`
	Status       string `help:"Contract status: active or archived." name:"status" required:""`
}

func (c *updateODCSContractCommand) Run(output *cliOutput, runtime *cliRuntime) error {
	client, project, err := newPipelineAPIClient(c.apiPipelineFlags, runtime)
	if err != nil {
		return err
	}
	contract, err := requiredODCSSlug(c.ContractSlug, "contract slug")
	if err != nil {
		return err
	}
	status := api.UpdateODCSContractStatus(strings.TrimSpace(c.Status))
	if !status.Valid() {
		return xerror.ValidationFailed().Detail("status must be active or archived")
	}
	name := strings.TrimSpace(c.Name)
	if name == "" {
		return xerror.ValidationFailed().Detail("name is required")
	}
	result, err := client.updateODCSContract(context.Background(), project, contract, api.UpdateODCSContract{Name: name, Status: status})
	if err != nil {
		return err
	}
	return writeCLIJSON(output.stdout, result)
}

type listODCSRevisionsCommand struct {
	apiPipelineFlags
	ContractSlug string `arg:"" name:"contract-slug" help:"ODCS contract slug." required:""`
	Limit        int32  `help:"Maximum number of revisions to return." name:"limit"`
	Cursor       string `help:"Pagination cursor from a previous list response." name:"cursor"`
	Order        string `help:"Sort order: asc or desc." name:"order"`
	Direction    string `help:"Pagination direction: next or prev." name:"direction"`
}

func (c *listODCSRevisionsCommand) Run(output *cliOutput, runtime *cliRuntime) error {
	client, project, err := newPipelineAPIClient(c.apiPipelineFlags, runtime)
	if err != nil {
		return err
	}
	contract, err := requiredODCSSlug(c.ContractSlug, "contract slug")
	if err != nil {
		return err
	}
	params, err := odcsRevisionListParams(c.Limit, c.Cursor, c.Order, c.Direction)
	if err != nil {
		return err
	}
	result, err := client.listODCSRevisions(context.Background(), project, contract, params)
	if err != nil {
		return err
	}
	return writeCLIJSON(output.stdout, result)
}

type getODCSRevisionCommand struct {
	apiPipelineFlags
	ContractSlug string `arg:"" name:"contract-slug" help:"ODCS contract slug." required:""`
	RevisionSlug string `arg:"" name:"revision-slug" help:"Immutable ODCS revision slug." required:""`
}

func (c *getODCSRevisionCommand) Run(output *cliOutput, runtime *cliRuntime) error {
	client, project, err := newPipelineAPIClient(c.apiPipelineFlags, runtime)
	if err != nil {
		return err
	}
	contract, err := requiredODCSSlug(c.ContractSlug, "contract slug")
	if err != nil {
		return err
	}
	revision, err := requiredODCSSlug(c.RevisionSlug, "revision slug")
	if err != nil {
		return err
	}
	result, err := client.getODCSRevision(context.Background(), project, contract, revision)
	if err != nil {
		return err
	}
	return writeCLIJSON(output.stdout, odcsRevisionMetadata(result))
}

type createODCSRevisionCommand struct {
	apiPipelineFlags
	ContractSlug string `arg:"" name:"contract-slug" help:"ODCS contract slug." required:""`
	Slug         string `help:"Immutable ODCS revision slug." name:"slug" required:""`
	File         string `help:"Path to the ODCS JSON document." name:"file" type:"path" required:""`
}

func (c *createODCSRevisionCommand) Run(output *cliOutput, runtime *cliRuntime) error {
	client, project, err := newPipelineAPIClient(c.apiPipelineFlags, runtime)
	if err != nil {
		return err
	}
	contract, err := requiredODCSSlug(c.ContractSlug, "contract slug")
	if err != nil {
		return err
	}
	revision, err := requiredODCSSlug(c.Slug, "slug")
	if err != nil {
		return err
	}
	document, err := readODCSDocument(c.File)
	if err != nil {
		return err
	}
	result, err := client.createODCSRevision(context.Background(), project, contract, api.NewODCSRevision{Slug: revision, Document: document})
	if err != nil {
		return err
	}
	return writeCLIJSON(output.stdout, odcsRevisionMetadata(result))
}

type previewODCSRevisionImpactCommand struct {
	apiPipelineFlags
	ContractSlug string `arg:"" name:"contract-slug" help:"ODCS contract slug." required:""`
	From         string `help:"Existing immutable revision slug." name:"from" required:""`
	To           string `help:"Candidate immutable revision slug." name:"to" required:""`
}

func (c *previewODCSRevisionImpactCommand) Run(output *cliOutput, runtime *cliRuntime) error {
	client, project, err := newPipelineAPIClient(c.apiPipelineFlags, runtime)
	if err != nil {
		return err
	}
	contract, err := requiredODCSSlug(c.ContractSlug, "contract slug")
	if err != nil {
		return err
	}
	from, err := requiredODCSSlug(c.From, "from revision slug")
	if err != nil {
		return err
	}
	to, err := requiredODCSSlug(c.To, "to revision slug")
	if err != nil {
		return err
	}
	result, err := client.previewODCSRevisionImpact(context.Background(), project, contract, api.ODCSRevisionImpactRequest{FromRevisionSlug: from, ToRevisionSlug: to})
	if err != nil {
		return err
	}
	return writeCLIJSON(output.stdout, result)
}

func odcsContractListParams(limit int32, cursor, order, direction string) (*api.ListODCSContractsParams, error) {
	params := &api.ListODCSContractsParams{}
	if err := applyLimitCursor(limit, cursor, &params.Limit, &params.Cursor); err != nil {
		return nil, err
	}
	if order := strings.TrimSpace(order); order != "" && order != "asc" && order != "desc" {
		return nil, xerror.ValidationFailed().Detail("order must be asc or desc")
	} else if order != "" {
		value := api.ListODCSContractsParamsOrder(order)
		params.Order = &value
	}
	if direction := strings.TrimSpace(direction); direction != "" && direction != "next" && direction != "prev" {
		return nil, xerror.ValidationFailed().Detail("direction must be next or prev")
	} else if direction != "" {
		value := api.ListODCSContractsParamsDirection(direction)
		params.Direction = &value
	}
	return params, nil
}

func odcsRevisionListParams(limit int32, cursor, order, direction string) (*api.ListODCSRevisionsParams, error) {
	params := &api.ListODCSRevisionsParams{}
	if err := applyLimitCursor(limit, cursor, &params.Limit, &params.Cursor); err != nil {
		return nil, err
	}
	if order := strings.TrimSpace(order); order != "" && order != "asc" && order != "desc" {
		return nil, xerror.ValidationFailed().Detail("order must be asc or desc")
	} else if order != "" {
		value := api.ListODCSRevisionsParamsOrder(order)
		params.Order = &value
	}
	if direction := strings.TrimSpace(direction); direction != "" && direction != "next" && direction != "prev" {
		return nil, xerror.ValidationFailed().Detail("direction must be next or prev")
	} else if direction != "" {
		value := api.ListODCSRevisionsParamsDirection(direction)
		params.Direction = &value
	}
	return params, nil
}

func requiredODCSSlug(raw, label string) (string, error) {
	value := strings.TrimSpace(raw)
	if _, err := slug.New(value); err != nil {
		return "", xerror.ValidationFailed().Detailf("%s must be a valid slug", label)
	}
	return value, nil
}

func readODCSDocument(path string) (json.RawMessage, error) {
	data, err := os.ReadFile(strings.TrimSpace(path))
	if err != nil {
		return nil, xerror.Wrapf("read ODCS JSON document %s: %w", path, err)
	}
	if !json.Valid(data) {
		return nil, xerror.InvalidInput(xerror.JSON).Detail("ODCS document must be valid JSON")
	}
	return json.RawMessage(data), nil
}

func odcsRevisionMetadata(revision *api.ODCSRevision) api.ODCSRevisionMetadata {
	return api.ODCSRevisionMetadata{Slug: revision.Slug, Digest: revision.Digest, DocumentVersion: revision.DocumentVersion, DeclaredStatus: revision.DeclaredStatus, OdcsVersion: api.ODCSRevisionMetadataOdcsVersion(revision.OdcsVersion), Status: api.ODCSRevisionMetadataStatus(revision.Status), CreatedAt: revision.CreatedAt, SchemaObjects: revision.SchemaObjects}
}

type inspectODCSRevisionCommand struct {
	apiPipelineFlags
	ContractSlug string `arg:"" name:"contract-slug" required:""`
	RevisionSlug string `arg:"" name:"revision-slug" required:""`
	Object       string `name:"object" help:"Exact schema object ID for port-scoped coverage; omitted means no executable selection."`
}

func (c *inspectODCSRevisionCommand) Run(output *cliOutput, runtime *cliRuntime) error {
	client, project, err := newPipelineAPIClient(c.apiPipelineFlags, runtime)
	if err != nil {
		return err
	}
	contract, err := requiredODCSSlug(c.ContractSlug, "contract slug")
	if err != nil {
		return err
	}
	revision, err := requiredODCSSlug(c.RevisionSlug, "revision slug")
	if err != nil {
		return err
	}
	if revision == "latest" {
		return xerror.ValidationFailed().Detail("select an exact revision")
	}
	result, err := client.inspectODCSRevision(context.Background(), project, contract, revision, c.Object)
	if err != nil {
		return err
	}
	return writeCLIJSON(output.stdout, result)
}
