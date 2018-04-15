package main

import (
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/pin/tftp"
	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"
)

const port = ":69"

// readHandler is called when client starts file download from server
func readHandler(filename string, rf io.ReaderFrom) error {
	execDir, _ := os.Executable()
	execDir = filepath.Dir(execDir)
	file, err := os.Open(filepath.Join(execDir, "tftp", filename))
	if err != nil {
		log.Errorf("%v\n", err)
		return err
	}
	n, err := rf.ReadFrom(file)
	if err != nil {
		log.Errorf("%v\n", err)
		return err
	}
	log.Infof("%d bytes sent\n", n)
	return nil
}

// ServeTFTP stars new tftp server at port 69
func ServeTFTP() error {
	// only read capabilities
	s := tftp.NewServer(readHandler, nil)
	s.SetTimeout(5 * time.Second) // optional
	go func() {
		time.Sleep(1 * time.Second)
		s.Shutdown()
	}()
	err := s.ListenAndServe(port) // blocks until s.Shutdown() is called
	if err != nil {
		return errors.Wrap(err, "Can't start tftp server")
	}
	// respawn as goroutine
	go s.ListenAndServe(port)
	log.Infof("Started tftp server at port %s", port)
	return nil
}
