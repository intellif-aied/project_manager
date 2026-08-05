//go:build !windows

package main

import "os/exec"

func configureAutoSyncBackgroundCommand(*exec.Cmd) {}
