package main

import (
	"fmt"

	"github.com/Braidplane/cli/pkg/xerror"
)

type logoutCommand struct{}

func (c *logoutCommand) Run(output *cliOutput, runtime *cliRuntime) error {
	configPath, err := runtime.resolveConfigPath()
	if err != nil {
		return err
	}
	if err := deleteLoginWithCredentialStore(configPath, runtime.resolveCredentialStore()); err != nil {
		return err
	}

	_, err = fmt.Fprintln(output.stdout, "Logged out")
	if err != nil {
		return xerror.Unexpected().With(err)
	}

	return nil
}
