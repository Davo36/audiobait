package main

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/TheCacophonyProject/window"
	arg "github.com/alexflint/go-arg"
	"github.com/godbus/dbus"
)

// version is populated at link time via goreleaser
var version = "No version provided"

type argSpec struct {
	ConfigFile string `arg:"-c,--config" help:"path to configuration file"`
	Timestamps bool   `arg:"-t,--timestamps" help:"include timestamps in log output"`
}

func (argSpec) Version() string {
	return version
}

func procArgs() argSpec {
	var args argSpec
	args.ConfigFile = "/etc/audiobait.yaml"
	arg.MustParse(&args)
	return args
}

func main() {
	err := runMain()
	if err != nil {
		log.Fatal(err)
	}
}

func runMain() error {
	args := procArgs()
	if !args.Timestamps {
		log.SetFlags(0) // Removes default timestamp flag
	}

	log.Printf("version %s", version)
	conf, err := ParseConfigFile(args.ConfigFile)
	if err != nil {
		return err
	}

	log.Printf("setting card %d %q to 100%%", conf.Card, conf.VolumeControl)
	if err := setVolume(conf.Card, conf.VolumeControl, 100); err != nil {
		return err
	}

	audioFileName := filepath.Join(conf.AudioDir, conf.Play.File)
	log.Printf("using " + audioFileName)

	log.Printf("playback window: %02d:%02d to %02d:%02d",
		conf.WindowStart.Hour(), conf.WindowStart.Minute(),
		conf.WindowEnd.Hour(), conf.WindowEnd.Minute())
	win := window.New(conf.WindowStart, conf.WindowEnd)

	for {
		toWindow := win.Until()
		if toWindow == time.Duration(0) {
			log.Print("starting burst")
			for count := 0; count < conf.Play.BurstRepeat; count++ {
				now := time.Now()
				err := play(conf.Card, audioFileName)
				if err != nil {
					return err
				}
				if err := queueEvent(now, audioFileName); err != nil {
					log.Printf("failed to queue event: %v", err)
				}
				time.Sleep(conf.Play.IntraSleep)
			}
			log.Print("sleeping")
			time.Sleep(conf.Play.InterSleep)
		} else {
			log.Printf("sleeping until next window (%s)", toWindow)
			time.Sleep(toWindow)
		}
	}
}

func setVolume(card int, controlName string, percent int) error {
	cmd := exec.Command(
		"amixer",
		"-c", fmt.Sprint(card),
		"sset",
		controlName,
		fmt.Sprintf("%d%%", percent),
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("volume set failed: %v\noutput:\n%s", err, out)
	}
	return nil
}

func play(card int, filename string) error {
	cmd := exec.Command("play", "-q", filename)
	cmd.Env = append(os.Environ(), fmt.Sprintf("AUDIODEV=hw:%d", card))
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("play failed: %v\noutput:\n%s", err, out)
	}
	return nil
}

func queueEvent(ts time.Time, filename string) error {
	eventDetails := map[string]interface{}{
		"description": map[string]interface{}{
			"type": "audioBait",
			"details": map[string]interface{}{
				"filename": filepath.Base(filename),
				"volume":   100,
			},
		},
	}
	detailsJSON, err := json.Marshal(&eventDetails)
	if err != nil {
		return err
	}

	conn, err := dbus.SystemBus()
	if err != nil {
		return err
	}
	obj := conn.Object("org.cacophony.Events", "/org/cacophony/Events")
	call := obj.Call("org.cacophony.Events.Queue", 0, detailsJSON, ts.UnixNano())
	return call.Err
}
