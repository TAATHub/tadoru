package main

import (
	"os/exec"
	"path/filepath"
	"runtime"
)

func startDetached(name string, args ...string) error {
	cmd := exec.Command(name, args...)
	if err := cmd.Start(); err != nil {
		return err
	}
	go cmd.Wait()
	return nil
}

func openDefault(path string) error {
	if runtime.GOOS == "darwin" {
		return startDetached("open", path)
	}
	return startDetached("xdg-open", path)
}

func revealInFileManager(path string) error {
	if runtime.GOOS == "darwin" {
		return startDetached("open", "-R", path)
	}
	return startDetached("xdg-open", filepath.Dir(path))
}
