// Package zed opens folders in Zed.
package zed

import (
	"errors"
	"fmt"
	"os/exec"
	"strings"
)

// Open runs the configured Zed command (e.g. "zed" or "zeditor --new") on
// paths without waiting for it. A folder and a file in it open as one
// workspace showing the file.
func Open(command string, paths ...string) error {
	fields := strings.Fields(command)
	if len(fields) == 0 {
		return errors.New("no Zed command configured (Settings > Appearance)")
	}
	bin, err := exec.LookPath(fields[0])
	if err != nil {
		return fmt.Errorf("%s is not on PATH (set the Zed command in Settings > Appearance)", fields[0])
	}
	cmd := exec.Command(bin, append(fields[1:], paths...)...)
	if err := cmd.Start(); err != nil {
		return err
	}
	go func() { _ = cmd.Wait() }()
	return nil
}

// Available reports whether the Zed command is on PATH.
func Available(command string) bool {
	fields := strings.Fields(command)
	if len(fields) == 0 {
		return false
	}
	_, err := exec.LookPath(fields[0])
	return err == nil
}
