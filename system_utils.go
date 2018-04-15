package main

import (
	"bufio"
	"os/exec"
	"runtime"
	"strings"

	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"
)

// ExecBinary spawns the given binary with the given args, logging the sdtout and stderr through the Logger
func ExecBinary(binary string, args []string) error {
	// remove quotes form binary command and args
	binary = strings.Replace(binary, "\"", "", -1)

	for i := range args {
		args[i] = strings.Replace(args[i], "\"", "", -1)
	}

	// find extension
	extension := ""
	if runtime.GOOS == "windows" {
		extension = ".exe"
	}

	cmd := exec.Command(binary, args...)

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return errors.Wrapf(err, "Retrieve output")
	}

	stderr, err := cmd.StderrPipe()
	if err != nil {
		return errors.Wrap(err, "Retrieve output")
	}

	log.Infof("Flashing with command: %s%s %s", binary, extension, strings.Join(args, " "))

	err = cmd.Start()

	stdoutCopy := bufio.NewScanner(stdout)
	stderrCopy := bufio.NewScanner(stderr)

	stdoutCopy.Split(bufio.ScanLines)
	stderrCopy.Split(bufio.ScanLines)

	go func() {
		for stdoutCopy.Scan() {
			log.Info(stdoutCopy.Text())
		}
	}()

	go func() {
		for stderrCopy.Scan() {
			log.Error(stderrCopy.Text())
		}
	}()

	err = cmd.Wait()
	if err != nil {
		return errors.Wrap(err, "Executing command")
	}
	return nil
}
