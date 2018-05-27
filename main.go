package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"time"

	expect "github.com/facchinm/goexpect"
	jobsui "github.com/mic90/go-jobs-ui"
	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"
	serial "go.bug.st/serial.v1"
	"go.bug.st/serial.v1/enumerator"
)

type firmwareFile struct {
	name string
	size int64
}

type context struct {
	flashBootloader    *bool
	serverAddr         string
	ipAddr             string
	bootloaderFirmware firmwareFile
	sysupgradeFirmware firmwareFile
	targetBoard        *string
}

func getFileSize(path string) int64 {
	file, _ := os.Open(path)
	fi, _ := file.Stat()
	return fi.Size()
}

// setup logger
func init() {
	logFileName := "updater.log"
	log.SetOutput(os.Stdout)
	os.Remove(logFileName)
	file, err := os.OpenFile(logFileName, os.O_CREATE|os.O_WRONLY, 0666)
	if err == nil {
		log.SetOutput(file)
	} else {
		log.Info("Failed to log to file, using default stderr")
	}
	log.SetLevel(log.DebugLevel)
}

func waitForKeyAndExit(ui *jobsui.UI, errorMessage string) {
	ui.SetStatus(fmt.Sprintf("Press any key to exit, error: %s", errorMessage))
	fmt.Scanln()
	os.Exit(1)
}

func main() {

	bootloaderFirmwareName := "u-boot-arduino-lede.bin"
	sysupgradeFirmwareName := "openwrt-ar71xx-generic-arduino-yun-squashfs-sysupgrade.bin"

	serverAddr := ""
	ipAddr := ""

	flashBootloader := flag.Bool("bl", true, "Flash bootloader too (danger zone)")
	targetBoard := flag.String("board", "Yun", "Update to target board")

	defaultServerAddr := flag.String("serverip", "", "<optional, only use if autodiscovery fails> Specify server IP address (this machine)")
	defaultBoardAddr := flag.String("boardip", "", "<optional, only use if autodiscovery fails> Specify YUN IP address")

	flag.Parse()

	ui := jobsui.NewUI()
	ui.AddJob("startTftp", "Start TFTP server")
	ui.AddJob("findBoardAddress", "Find board IP address")
	ui.AddJob("findOwnAddress", "Find own IP address")
	ui.AddJob("findSerialPort", "Find serial port for upload")
	ui.AddJob("uploadTerminalHex", "Flash MCU with serial terminal")
	ui.AddJob("flashBootloader", "Flash MPU bootloader")
	ui.AddJob("flashImage", "Flash MPU linux image")
	ui.AddJob("findSerialPortFirmware", "Find serial port for upload")
	ui.AddJob("uploadFirmware", "Flash MCU with final firmware")

	// start tftp server, exit on failure
	tftpErr := ServeTFTP()
	if tftpErr != nil {
		ui.SetJobStateWithInfo("startTftp", jobsui.Error, tftpErr.Error())
		log.Error(tftpErr)
		waitForKeyAndExit(ui, "unable to start TFTP server")
	}
	ui.SetJobState("startTftp", jobsui.Done)

	serverAddr = *defaultServerAddr
	ipAddr = *defaultBoardAddr

	if serverAddr == "" || ipAddr == "" {
		ipErr := GetServerAndBoardIP(&serverAddr, &ipAddr)
		if ipErr != nil {
			ui.SetJobStateWithInfo("findBoardAddress", jobsui.Error, ipErr.Error())
			ui.SetJobStateWithInfo("findOwnAddress", jobsui.Error, ipErr.Error())
			log.Fatal(ipErr)
			waitForKeyAndExit(ui, "unable to obtain self or board IP")
		}
	}
	ui.SetJobStateWithInfo("findBoardAddress", jobsui.Done, ipAddr)
	ui.SetJobStateWithInfo("findOwnAddress", jobsui.Done, serverAddr)
	log.Infof("Using %s as server address and %s as board address", serverAddr, ipAddr)

	// get serial ports attached
	ui.SetStatus("Searching for suitable serial port...")
	serialPortName, err := findSerialPortForFlashing()
	if err != nil {
		ui.SetJobStateWithInfo("findSerialPort", jobsui.Error, err.Error())
		log.Error(err)
		waitForKeyAndExit(ui, "unable to find serial port for flashing")
	}
	ui.SetJobStateWithInfo("findSerialPort", jobsui.Done, serialPortName)

	hexName := "mcu_serial_terminal.hex"
	ui.SetStatus(fmt.Sprintf("Flashing hex file: %s", hexName))
	port, err := FlashHexFile(serialPortName, hexName)
	if err != nil {
		ui.SetJobStateWithInfo("uploadTerminalHex", jobsui.Error, err.Error())
		log.Error(err)
		waitForKeyAndExit(ui, fmt.Sprintf("unable to flash %s", hexName))
	}
	ui.SetJobState("uploadTerminalHex", jobsui.Done)

	// start the expecter
	exp, _, err, serport := serialSpawn(port, time.Duration(10)*time.Second, expect.CheckDuration(100*time.Millisecond), expect.Verbose(false), expect.VerboseWriter(os.Stdout))
	if err != nil {
		ui.SetJobStateWithInfo("flashBootloader", jobsui.Error, "Unable to spawn serial port")
		log.Errorf("Unable to spawn serial port: %s", err.Error())
		waitForKeyAndExit(ui, "unable to spawn serial port")
	}

	execDir, _ := os.Executable()
	execDir = filepath.Dir(execDir)
	tftpDir := filepath.Join(execDir, "tftp")

	bootloaderSize := getFileSize(filepath.Join(tftpDir, bootloaderFirmwareName))
	sysupgradeSize := getFileSize(filepath.Join(tftpDir, sysupgradeFirmwareName))

	bootloaderFirmware := firmwareFile{name: bootloaderFirmwareName, size: bootloaderSize}
	sysupgradeFirmware := firmwareFile{name: sysupgradeFirmwareName, size: sysupgradeSize}

	ctx := context{flashBootloader: flashBootloader, serverAddr: serverAddr, ipAddr: ipAddr, bootloaderFirmware: bootloaderFirmware, sysupgradeFirmware: sysupgradeFirmware, targetBoard: targetBoard}

	lastline, err := FlashFirmwareAndBootlader(exp, ctx, ui)

	retryCount := 0
	for err != nil && retryCount < 3 /* && strings.Contains(lastline, "Loading: T ")*/ {
		//retry with different IP addresses
		ui.SetStatus("Firmware upload failed, retrying")
		log.Errorf("Firmware uload failed: %s, %s", lastline, err.Error())
		GetServerAndBoardIP(&serverAddr, &ipAddr)
		ctx.serverAddr = serverAddr
		ctx.ipAddr = ipAddr
		retryCount++
		lastline, err = FlashFirmwareAndBootlader(exp, ctx, ui)
	}

	if err != nil {
		exp.Close()
		serport.Close()
		log.Error(err)
		waitForKeyAndExit(ui, "unable to flash mpu, all retries failed")
	}
	exp.Close()
	serport.Close()

	// get serial ports attached
	ui.SetStatus("Searching for suitable serial port...")
	serialPortName, err = findSerialPortForFlashing()
	if err != nil {
		ui.SetJobStateWithInfo("findSerialPortFirmware", jobsui.Error, err.Error())
		log.Error(err)
		waitForKeyAndExit(ui, "unable to find serial port for flashing")
	}
	ui.SetJobStateWithInfo("findSerialPortFirmware", jobsui.Done, serialPortName)

	// upload the YunSerialTerminal to the board
	hexName = "mcu_firmware.hex"
	ui.SetStatus(fmt.Sprintf("Flashing hex file: %s", hexName))
	port, err = FlashHexFile(serialPortName, hexName)
	if err != nil {
		ui.SetJobStateWithInfo("uploadFirmware", jobsui.Error, err.Error())
		log.Error(err)
		waitForKeyAndExit(ui, fmt.Sprintf("unable to flash %s", hexName))
	}
	ui.SetJobState("uploadFirmware", jobsui.Done)

	ui.SetStatus("All done! You may now close the window, or wait 10s")
	log.Info("All done! You may now close the window, or wait 10s")
	time.Sleep(10 * time.Second)
}

func serialSpawn(port string, timeout time.Duration, opts ...expect.Option) (expect.Expecter, <-chan error, error, serial.Port) {
	// open the port with safe parameters
	mode := &serial.Mode{
		BaudRate: 115200,
	}
	serPort, err := serial.Open(port, mode)
	if err != nil {
		return nil, nil, err, nil
	}

	resCh := make(chan error)

	exp, ch, err := expect.SpawnGeneric(&expect.GenOptions{
		In:  serPort,
		Out: serPort,
		Wait: func() error {
			return <-resCh
		},
		Close: func() error {
			close(resCh)
			return nil
		},
		Check: func() bool { return true },
	}, timeout, opts...)

	return exp, ch, err, serPort
}

func findSerialPortForFlashing() (string, error) {
	var serialPort enumerator.PortDetails
	ports, err := enumerator.GetDetailedPortsList()
	if err != nil {
		return "", err
	}
	if len(ports) == 0 {
		return "", errors.New("No serial ports were found")
	}
	// find port which is suitable for uplaod based on its VID and PID values
	for _, port := range ports {
		if port.IsUSB {
			log.Infof("Found serial port: %s ID: %s:%s Serial number: %s", port.Name, port.VID, port.PID, port.SerialNumber)
			if canUse(port) {
				log.Info("Using it")
				serialPort = *port
				break
			}
		}
	}
	if serialPort.Name == "" {
		return "", errors.New("No serial port suitable for upload")
	}
	return serialPort.Name, nil
}

func canUse(port *enumerator.PortDetails) bool {
	if port.VID == "2341" && (port.PID == "8041" || port.PID == "0041" || port.PID == "8051" || port.PID == "0051") {
		return true
	}
	if port.VID == "2a03" && (port.PID == "8041" || port.PID == "0041") {
		return true
	}
	return false
}
