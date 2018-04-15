package main

import (
	"os"
	"path/filepath"
	"time"

	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"
	serial "go.bug.st/serial.v1"
)

// FlashHexFile flashes mcu connected to [port] serial port with hex file named [hexName]
func FlashHexFile(port, hexName string) (string, error) {
	port, err := reset(port, true)
	if err != nil {
		return "", err
	}

	time.Sleep(1 * time.Second)

	execDir, _ := os.Executable()
	execDir = filepath.Dir(execDir)
	binDir := filepath.Join(execDir, "avr")
	FWName := filepath.Join(binDir, hexName)
	args := []string{"-C" + binDir + "/etc/avrdude.conf", "-v", "-patmega32u4", "-cavr109", "-P" + port, "-b57600", "-D", "-Uflash:w:" + FWName + ":i"}
	err = ExecBinary(filepath.Join(binDir, "bin", "avrdude"), args)
	if err != nil {
		return "", err
	}
	ports, err := serial.GetPortsList()
	port = waitReset(ports, port, 5*time.Second)
	return port, nil
}

// reset opens the port at 1200bps. It returns the new port name (which could change
// sometimes) and an error (usually because the port listing failed)
func reset(port string, wait bool) (string, error) {
	log.Info("Restarting in bootloader mode")

	// Get port list before reset
	ports, err := serial.GetPortsList()
	log.Info("Get port list before reset")
	if err != nil {
		return "", errors.Wrap(err, "Get port list before reset")
	}

	// Touch port at 1200bps
	err = touchSerialPortAt1200bps(port)
	if err != nil {
		return "", errors.Wrap(err, "1200bps Touch")
	}

	// Wait for port to disappear and reappear
	if wait {
		port = waitReset(ports, port, 10*time.Second)
	}

	return port, nil
}

func touchSerialPortAt1200bps(port string) error {
	// Open port
	p, err := serial.Open(port, &serial.Mode{BaudRate: 1200})
	if err != nil {
		return errors.Wrapf(err, "Open port %s", port)
	}
	defer p.Close()

	// Set DTR
	err = p.SetDTR(false)
	if err != nil {
		return errors.Wrapf(err, "Can't set DTR")
	}

	// Wait a bit to allow restart of the board
	time.Sleep(200 * time.Millisecond)

	return nil
}

// waitReset is meant to be called just after a reset. It watches the ports connected
// to the machine until a port disappears and reappears. The port name could be different
// so it returns the name of the new port.
func waitReset(beforeReset []string, originalPort string, timeoutDuration time.Duration) string {
	var port string
	timeout := false

	go func() {
		time.Sleep(timeoutDuration)
		timeout = true
	}()

	// Wait for the port to disappear
	log.Info("Wait for the port to disappear")
	for {
		ports, err := serial.GetPortsList()
		port = differ(ports, beforeReset)

		if port != "" {
			break
		}
		if timeout {
			log.Info(ports, err, port)
			break
		}
		time.Sleep(time.Millisecond * 100)
	}

	// Wait for the port to reappear
	log.Info("Wait for the port to reappear")
	afterReset, _ := serial.GetPortsList()
	for {
		ports, _ := serial.GetPortsList()
		port = differ(ports, afterReset)
		if port != "" {
			log.Info("Found upload port: ", port)
			time.Sleep(time.Millisecond * 500)
			break
		}
		if timeout {
			break
		}
		time.Sleep(time.Millisecond * 100)
	}

	// try to upload on the existing port if the touch was ineffective
	if port == "" {
		port = originalPort
	}

	return port
}

// differ returns the first item that differ between the two input slices
func differ(slice1 []string, slice2 []string) string {
	m := map[string]int{}

	for _, s1Val := range slice1 {
		m[s1Val] = 1
	}
	for _, s2Val := range slice2 {
		m[s2Val] = m[s2Val] + 1
	}

	for mKey, mVal := range m {
		if mVal == 1 {
			return mKey
		}
	}

	return ""
}
