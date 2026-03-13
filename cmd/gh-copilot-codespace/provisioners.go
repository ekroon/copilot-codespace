package main

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/ekroon/gh-copilot-codespace/internal/provisioner"
	"github.com/ekroon/gh-copilot-codespace/internal/ssh"
)

func loadProvisioners() []provisioner.Provisioner {
	config, err := provisioner.LoadSettings()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Warning: could not load provisioner config: %v\n", err)
		return builtinProvisioners(nil)
	}
	provs := builtinProvisioners(config.Builtins)
	return append(provs, provisioner.ProvisionersFromConfig(config)...)
}

func runProvisioners(ctx context.Context, provs []provisioner.Provisioner, codespaceName, repository, workdir string, sshClient *ssh.Client, isNewCodespace bool) {
	if len(provs) == 0 {
		return
	}

	target := &launcherCodespaceTarget{
		name:    codespaceName,
		repo:    repository,
		workdir: workdir,
		client:  sshClient,
	}
	rctx := provisioner.RunContext{
		Terminal:       provisioner.DetectedTerminal(os.Getenv("TERM")),
		Repository:     repository,
		IsNewCodespace: isNewCodespace,
	}
	provisioner.RunAll(ctx, provs, rctx, target)
}

func builtinProvisioners(settings map[string]bool) []provisioner.Provisioner {
	var provs []provisioner.Provisioner
	if builtinProvisionerEnabled(settings, "terminfo") {
		provs = append(provs, &provisioner.TerminfoProvisioner{})
	}
	if builtinProvisionerEnabled(settings, "git-fetch") {
		provs = append(provs, &provisioner.GitFetchProvisioner{})
	}
	return provs
}

func builtinProvisionerEnabled(settings map[string]bool, name string) bool {
	if settings == nil {
		return true
	}
	enabled, ok := settings[name]
	if !ok {
		return true
	}
	return enabled
}

type launcherCodespaceTarget struct {
	name    string
	repo    string
	workdir string
	client  *ssh.Client
}

func (t *launcherCodespaceTarget) CodespaceName() string { return t.name }
func (t *launcherCodespaceTarget) Repository() string    { return t.repo }
func (t *launcherCodespaceTarget) Workdir() string       { return t.workdir }

func (t *launcherCodespaceTarget) RunSSH(ctx context.Context, command string) (string, error) {
	stdout, stderr, exitCode, err := t.client.Exec(ctx, command)
	if err != nil {
		return "", err
	}
	if exitCode != 0 {
		return stdout, fmt.Errorf("exit %d: %s", exitCode, strings.TrimSpace(stderr))
	}
	return stdout, nil
}

func (t *launcherCodespaceTarget) UploadTerminfo(ctx context.Context, term string) error {
	return t.client.UploadTerminfo(ctx, term)
}
