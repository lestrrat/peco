package main

import (
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

/* This script evists because godep deprecated -copy=false, and I really
 * don't agree that importing the actual source code for peco is the
 * correct choice
 */

var pwd string

func init() {
	var err error
	if pwd, err = os.Getwd(); err != nil {
		panic(err)
	}
}

func main() {
	switch os.Args[1] {
	case "deps":
		setupDeps()
	case "build":
		setupDeps()
		buildBinaries()
	default:
		panic("Unknown action: " + os.Args[1])
	}
}

func setupDeps() {
	deps := map[string]string{
		"github.com/jessevdk/go-flags":  "8ec9564882e7923e632f012761c81c46dcf5bec1",
		"github.com/mattn/go-runewidth": "63c378b851290989b19ca955468386485f118c65",
		"github.com/nsf/termbox-go":     "9e7f2135126fcf13f331e7b24f5d66fd8e8e1690",
	}

	var err error

	baseDir := "/work/src"
	for dir, hash := range deps {
		repo := repoURL(dir)
		dir = filepath.Join(baseDir, dir)
		if _, err = os.Stat(dir); err != nil {
			if err = run("git", "clone", repo, dir); err != nil {
				panic(err)
			}
		}

		if err = os.Chdir(dir); err != nil {
			panic(err)
		}

		if err = run("git", "reset", "--hard"); err != nil {
			panic(err)
		}

		if err = run("git", "checkout", "master"); err != nil {
			panic(err)
		}

		if err = run("git", "pull"); err != nil {
			panic(err)
		}

		if err = run("git", "checkout", hash); err != nil {
			panic(err)
		}

		if err = os.Chdir(pwd); err != nil {
			panic(err)
		}
	}
}

func buildBinaries() {
	goxcArgs := []string{
		"-tasks", "xc archive",
		"-bc", "linux windows darwin",
		"-wd", "/work/src/github.com/peco/peco",
		"-d", os.Args[2],
		"-resources-include", "README*,Changes",
		"-main-dirs-exclude", "_demos,examples,build",
	}
	if err := run("goxc", goxcArgs...); err != nil {
		panic(err)
	}
}

func run(name string, args ...string) error {
	splat := []string{name}
	splat = append(splat, args...)
	log.Printf("---> Running %v...\n", splat)
	cmd := exec.Command(name, args...)
	out, err := cmd.CombinedOutput()
	for _, line := range strings.Split(string(out), "\n") {
		log.Print(line)
	}
	log.Println("")
	log.Println("<---DONE")
	return err
}

func repoURL(spec string) string {
	return "https://" + spec + ".git"
}
