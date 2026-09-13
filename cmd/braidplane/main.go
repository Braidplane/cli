package main

import (
	"fmt"
	"io"
	"log"
	"os"

	"github.com/Braidplane/cli/pkg/kongcmd"
	"github.com/Braidplane/cli/pkg/xerror"
)

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}

func run() error {
	return runWithArgs(os.Args, os.Stdout, os.Stderr)
}

type cliOutput struct {
	stdout io.Writer
	stderr io.Writer
}

type cliRuntime struct {
	configPath      string
	credentialStore cliCredentialStore
}

type braidplaneCLI struct {
	Login              loginCommand              `cmd:"" help:"Save default API authentication settings."`
	Logout             logoutCommand             `cmd:"" help:"Delete saved API authentication settings."`
	User               userCommand               `cmd:"" help:"Manage the authenticated user's profile."`
	Organization       organizationCommand       `cmd:"" help:"Manage organizations."`
	Member             memberCommand             `cmd:"" help:"Manage organization members."`
	Invitation         invitationCommand         `cmd:"" help:"Manage organization member invitations."`
	Role               roleCommand               `cmd:"" help:"Manage organization roles."`
	APIKey             apiKeyCommand             `cmd:"" name:"api-key" help:"Manage same-role organization API keys using an explicitly opted-in API key."`
	Event              eventCommand              `cmd:"" help:"Inspect durable organization events."`
	Notification       notificationCommand       `cmd:"" help:"Inspect and manage organization notifications."`
	Alert              alertCommand              `cmd:"" help:"Manage alert destinations and alert policies."`
	Project            projectCommand            `cmd:"" help:"Manage organization projects."`
	Environment        environmentCommand        `cmd:"" help:"Manage organization runtime environment catalog entries and project enablement."`
	ProjectEnvironment projectEnvironmentCommand `cmd:"" name:"project-environment" help:"Manage project-environment enablement for catalog environments."`
	DataSource         dataSourceCommand         `cmd:"" name:"data-source" help:"Manage concrete organization data sources."`
	DataSourceRef      dataSourceRefCommand      `cmd:"" name:"data-source-ref" help:"Manage pipeline logical data-source references."`
	DataSourceBinding  dataSourceBindingCommand  `cmd:"" name:"data-source-binding" help:"Manage project environment data-source bindings."`
	Query              queryCommand              `cmd:"" help:"Manage saved data-source queries."`
	Pipeline           pipelineCommand           `cmd:"" help:"Manage pipelines."`
	Proof              proofCommand              `cmd:"" help:"Inspect Braidplane Proof receipts."`
}

func runWithArgs(args []string, stdout, stderr io.Writer) error {
	return runWithRuntime(args, stdout, stderr, cliRuntime{})
}

func runWithRuntime(args []string, stdout, stderr io.Writer, runtime cliRuntime) error {
	parseArgs := args
	if len(parseArgs) > 0 {
		parseArgs = parseArgs[1:]
	}

	result, err := kongcmd.Parse(
		parseArgs,
		braidplaneCLI{},
		"braidplane",
		"Manage Braidplane resources.",
		stdout,
		stderr,
		func(code int) error {
			return xerror.Unexpected().Detailf("CLI exited with status %d", code)
		},
	)
	if result.Exited {
		return err
	}
	if err != nil {
		_, writeErr := fmt.Fprintln(stderr, err)
		if writeErr != nil {
			return xerror.Unexpected().With(writeErr)
		}

		return xerror.ValidationFailed().Detailf("parse CLI arguments: %w", err)
	}

	if err := result.Context.Run(&cliOutput{stdout: stdout, stderr: stderr}, &runtime); err != nil {
		return xerror.Wrapf("run CLI command: %w", err)
	}

	return nil
}
