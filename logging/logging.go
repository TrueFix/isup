package logging

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"runtime"
)

func GetLogDir(appName string) string {
	switch runtime.GOOS {
	case "windows":
		appDataDir, err := os.UserCacheDir()
		if err != nil {
			log.Fatal(err)
		}
		return filepath.Join(appDataDir, appName)
	case "linux":
		return fmt.Sprintf("/var/log/%s", appName)
	default:
		return fmt.Sprintf("./%s", appName)
	}
}
