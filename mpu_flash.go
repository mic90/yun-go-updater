package main

import (
	"strconv"
	"time"

	expect "github.com/facchinm/goexpect"
	jobsui "github.com/mic90/go-jobs-ui"
	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"
)

// FlashFirmwareAndBootlader flashes the linux image and board bootloader if given cli argument was passed
func FlashFirmwareAndBootlader(exp expect.Expecter, ctx context, ui *jobsui.UI) (string, error) {
	res, err := exp.ExpectBatch([]expect.Batcher{
		&expect.BSnd{S: "\n"},
		&expect.BExp{R: "root@"},
		&expect.BSnd{S: "reboot -f\n"},
	}, time.Duration(5)*time.Second)

	if err != nil {
		ui.SetStatus("Reboot the board using YUN RST button")
		log.Info("Reboot the board using YUN RST button")
	} else {
		ui.SetStatus("Rebooting the board")
		log.Info("Rebooting the board")
	}

	err = nil

	// in bootloader mode:
	// understand which version of the BL we are in
	res, err = exp.ExpectBatch([]expect.Batcher{
		&expect.BExp{R: "(stop with '([a-z]+)'|Hit any key to stop autoboot|type '([a-z]+)' to enter u-boot console)"},
	}, time.Duration(20)*time.Second)

	if err != nil {
		return "", err
	}

	stopCommand := res[0].Match[len(res[0].Match)-1]

	if stopCommand == "" {
		stopCommand = res[0].Match[len(res[0].Match)-2]
	}

	if res[0].Match[0] == "Hit any key to stop autoboot" {
		log.Info("Old YUN detected")
		stopCommand = ""
	}

	log.Infof("Using stop command: %s", stopCommand)
	ui.SetStatus("Board rebooted, flashing...")

	// call stop and detect firmware version (if it needs to be updated)
	res, err = exp.ExpectBatch([]expect.Batcher{
		&expect.BSnd{S: stopCommand + "\n"},
		&expect.BSnd{S: "printenv ipaddr\n"},
		&expect.BExp{R: "([0-9a-zA-Z]+)>"},
	}, time.Duration(5)*time.Second)

	if err != nil {
		return "", err
	}

	fwShell := res[0].Match[len(res[0].Match)-1]
	log.Infof("Got shell: %s", fwShell)

	if fwShell != "arduino" {
		*ctx.flashBootloader = true
		log.Infof("fwShell: %s", fwShell)
	}

	time.Sleep(1 * time.Second)

	if *ctx.flashBootloader {

		log.Info("Flashing Bootloader")
		ui.SetStatus("Flashing bootloader...")

		err = errors.New("ping")

		retry := 0
		for err != nil && retry < 4 {
			// set server and board ip
			res, err = exp.ExpectBatch([]expect.Batcher{
				&expect.BSnd{S: "setenv serverip " + ctx.serverAddr + "\n"},
				&expect.BExp{R: fwShell + ">"},
				&expect.BSnd{S: "printenv serverip\n"},
				&expect.BExp{R: "serverip=" + ctx.serverAddr},
				&expect.BSnd{S: "setenv ipaddr " + ctx.ipAddr + "\n"},
				&expect.BSnd{S: "printenv ipaddr\n"},
				&expect.BExp{R: "ipaddr=" + ctx.ipAddr},
				&expect.BSnd{S: "ping " + ctx.serverAddr + "\n"},
				&expect.BExp{R: "host " + ctx.serverAddr + " is alive"},
			}, time.Duration(10)*time.Second)
			retry++
			if err != nil {
				GetServerAndBoardIP(&ctx.serverAddr, &ctx.ipAddr)
			}
		}

		if err != nil {
			ui.SetJobFailedText("flashBootloader", err.Error())
			return res[len(res)-1].Output, err
		}

		time.Sleep(2 * time.Second)

		// flash new bootloader
		res, err = exp.ExpectBatch([]expect.Batcher{
			&expect.BSnd{S: "printenv ipaddr\n"},
			&expect.BExp{R: fwShell + ">"},
			&expect.BSnd{S: "tftp 0x80060000 " + ctx.bootloaderFirmware.name + "\n"},
			&expect.BExp{R: "Bytes transferred = " + strconv.FormatInt(ctx.bootloaderFirmware.size, 10)},
			&expect.BSnd{S: "erase 0x9f000000 +0x40000\n"},
			&expect.BExp{R: "Erased 4 sectors"},
			&expect.BSnd{S: "cp.b $fileaddr 0x9f000000 $filesize\n"},
			&expect.BExp{R: "done"},
			&expect.BSnd{S: "erase 0x9f040000 +0x10000\n"},
			&expect.BExp{R: "Erased 1 sectors"},
			&expect.BSnd{S: "reset\n"},
		}, time.Duration(30)*time.Second)

		if err != nil {
			ui.SetJobFailedText("flashBootloader", err.Error())
			return res[len(res)-1].Output, err
		}

		// New bootloader flashed, stop with 'ard' and shell is 'arduino>'

		time.Sleep(1 * time.Second)

		// set new name
		res, err = exp.ExpectBatch([]expect.Batcher{
			&expect.BExp{R: "autoboot in"},
			&expect.BSnd{S: "ard\n"},
			&expect.BExp{R: "arduino>"},
			&expect.BSnd{S: "printenv ipaddr\n"},
			&expect.BExp{R: "arduino>"},
			&expect.BSnd{S: "setenv board " + *ctx.targetBoard + "\n"},
			&expect.BExp{R: "arduino>"},
			&expect.BSnd{S: "saveenv\n"},
			&expect.BExp{R: "arduino>"},
		}, time.Duration(10)*time.Second)

		if err != nil {
			ui.SetJobFailedText("flashBootloader", err.Error())
			return res[len(res)-1].Output, err
		}
		ui.SetJobDone("flashBootloader")
	} else {
		log.Info("Bootloader flash skipped")
		ui.SetJobDisabled("flashBootloader")
	}

	log.Info("Setting up IP addresses")

	err = errors.New("ping")
	retry := 0
	for err != nil && retry < 4 {
		// set server and board ip
		res, err = exp.ExpectBatch([]expect.Batcher{
			&expect.BSnd{S: "setenv serverip " + ctx.serverAddr + "\n"},
			&expect.BExp{R: fwShell + ">"},
			&expect.BSnd{S: "printenv serverip\n"},
			&expect.BExp{R: "serverip=" + ctx.serverAddr},
			&expect.BSnd{S: "setenv ipaddr " + ctx.ipAddr + "\n"},
			&expect.BSnd{S: "printenv ipaddr\n"},
			&expect.BExp{R: "ipaddr=" + ctx.ipAddr},
			&expect.BSnd{S: "ping " + ctx.serverAddr + "\n"},
			&expect.BExp{R: "host " + ctx.serverAddr + " is alive"},
		}, time.Duration(10)*time.Second)
		retry++
		if err != nil {
			GetServerAndBoardIP(&ctx.serverAddr, &ctx.ipAddr)
		}
	}

	if err != nil {
		ui.SetJobFailedText("flashImage", err.Error())
		return res[len(res)-1].Output, err
	}

	log.Info("Flashing sysupgrade image")
	ui.SetStatus("Flashing sysupgrade image...")

	// ping the serverIP; if ping is not working, try another network interface
	/*
		res, err = exp.ExpectBatch([]expect.Batcher{
			&expect.BSnd{S: "ping " + ctx.serverAddr + "\n"},
			&expect.BExp{R: "is alive"},
		}, time.Duration(6)*time.Second)

		if err != nil {
			return res[len(res)-1].Output, err
		}
	*/

	time.Sleep(2 * time.Second)

	// flash sysupgrade
	res, err = exp.ExpectBatch([]expect.Batcher{
		&expect.BSnd{S: "printenv board\n"},
		&expect.BExp{R: "board=" + *ctx.targetBoard},
		&expect.BSnd{S: "tftp 0x80060000 " + ctx.sysupgradeFirmware.name + "\n"},
		&expect.BExp{R: "Bytes transferred = " + strconv.FormatInt(ctx.sysupgradeFirmware.size, 10)},
		&expect.BSnd{S: `erase 0x9f050000 +0x` + strconv.FormatInt(ctx.sysupgradeFirmware.size, 16) + "\n"},
		&expect.BExp{R: "Erased [0-9]+ sectors"},
		&expect.BSnd{S: "printenv serverip\n"},
		&expect.BExp{R: "arduino>"},
		&expect.BSnd{S: "cp.b $fileaddr 0x9f050000 $filesize\n"},
		&expect.BExp{R: "done"},
		&expect.BSnd{S: "printenv serverip\n"},
		&expect.BExp{R: "arduino>"},
		&expect.BSnd{S: "reset\n"},
		&expect.BExp{R: "Transferring control to Linux"},
	}, time.Duration(90)*time.Second)

	if err != nil {
		ui.SetJobFailedText("flashImage", err.Error())
		return res[len(res)-1].Output, err
	}
	ui.SetJobDone("flashImage")
	return res[len(res)-1].Output, nil
}
