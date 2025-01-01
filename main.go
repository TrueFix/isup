package main

import (
	"embed"
	"io/fs"
	"log"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"time"

	"github.com/mayankfawkes/isup/isup"
	"github.com/mayankfawkes/isup/logging"
	"github.com/mayankfawkes/isup/wss"
)

//go:embed sound/*
var sound embed.FS

//go:embed images/*
var images embed.FS

var logFile *os.File

func runInfinitely(url url.URL, done chan struct{}, isup *isup.IsUp) {
	defer isup.Down()

	if wss, err := wss.NewWSS(url); err != nil {
		close(done)
		return
	} else {
		// log.Printf("Worker connected to %s", url.String())
		isup.Up()
		wss.StartWorker(done)
	}
}

func init() {
	// Define application and image directory
	appDir := logging.GetLogDir("isup")
	if _, err := os.Stat(appDir); os.IsNotExist(err) {
		os.Mkdir(appDir, 0755)
	}

	imagesDir := filepath.Join(appDir, "images")
	if _, err := os.Stat(imagesDir); os.IsNotExist(err) {
		os.Mkdir(imagesDir, 0755)
	}

	// Read files in "images" directory
	files, err := fs.ReadDir(images, "images")
	if err != nil {
		log.Fatalf("Error reading directory: %v", err)
	}

	// Copy files if they don't already exist in the destination directory
	for _, file := range files {
		destPath := filepath.Join(imagesDir, file.Name())
		if _, err := os.Stat(destPath); os.IsNotExist(err) {
			// Copy the file if it doesn't exist in the destination
			data, err := images.ReadFile("images/" + file.Name())
			if err != nil {
				log.Fatalf("Error reading file: %v", err)
			}
			err = os.WriteFile(destPath, data, 0644)
			if err != nil {
				log.Fatalf("Error writing file: %v", err)
			}
			log.Printf("File %s copied to %s", file.Name(), destPath)
		} else {
			log.Printf("File %s already exists in the destination, skipping.", file.Name())
		}
	}

	// Set up logging
	logFileName := filepath.Join(appDir, "logfile.txt")
	log.Printf("Logging to %s", logFileName)

	logFile, err := os.OpenFile(logFileName, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0666)
	if err != nil {
		log.Fatalf("Error opening file: %v", err)
	}
	log.SetOutput(logFile)
}

func main() {
	url := url.URL{Scheme: "wss", Host: "echo.websocket.org", Path: "/"}

	_isup := isup.NewIsUp()
	isup.Sound = sound

	interrupt := make(chan os.Signal, 1)
	signal.Notify(interrupt, os.Interrupt)

	defer logFile.Close()

	for {
		done := make(chan struct{})
		go runInfinitely(url, done, _isup)

		// Wait for interrupt signal or completion of run
		select {
		case <-interrupt:
			// Handle interrupt signal
			log.Printf("Received interrupt signal, shutting down...")
			// Close the WebSocket connection and stop the loop
			close(done)

			// Wait for the worker to finish
			select {
			case <-done:
				log.Printf("Worker finished")
			case <-time.After(time.Second * 5):
				log.Printf("Worker did not finish in time")
			}

			return

		case <-done:
			// If runInfinitely finishes (done signal received)
			log.Printf("Reconnecting in 5 seconds, isup status: %s", _isup.Status())
			time.Sleep(5 * time.Second)
		}

	}

}
