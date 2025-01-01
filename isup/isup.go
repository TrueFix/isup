package isup

import (
	"embed"
	"log"
	"path/filepath"

	"github.com/gen2brain/beeep"
	"github.com/mayankfawkes/isup/logging"
)

var Sound embed.FS
var Images embed.FS

type IsUp struct {
	IsUp   bool
	appDir string
}

func NewIsUp() *IsUp {
	return &IsUp{
		IsUp:   false,
		appDir: logging.GetLogDir("isup"),
	}
}

func (i *IsUp) isUp() bool {
	return i.IsUp
}

func (i *IsUp) IsDown() bool {
	return !i.IsUp
}

func (i *IsUp) Up() {
	if i.isUp() {
		return
	}

	i.IsUp = true
	log.Println("Internet status changed to UP")
	go func() {
		err := beeep.Notify("Internet Up", "You are connected to the internet", filepath.Join(i.appDir, "images/connected.png"))
		if err != nil {
			log.Println(err)
		}
	}()
	Play("connected")
}

func (i *IsUp) Down() {
	if i.IsDown() {
		return
	}
	i.IsUp = false
	log.Println("Internet status changed to DOWN")
	go func() {
		err := beeep.Alert("Internet Down", "You are disconnected from the internet", filepath.Join(i.appDir, "images/disconnected.png"))
		if err != nil {
			log.Println(err)
		}
	}()
	Play("disconnected")
}

func (i *IsUp) Status() string {
	if i.IsUp {
		return "UP"
	}

	return "DOWN"
}
