package cli

import (
	"fmt"
	"github.com/alecthomas/kingpin"
	"github.com/howeyc/fsnotify"
	"io"
	"io/ioutil"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

var stopChannel chan bool
var changeChannel chan string
var doneChannel chan error
var isRunning = false

func init() {
	stopChannel = make(chan bool)
	changeChannel = make(chan string)
	doneChannel = make(chan error)

	cmd := Command("serve", "Run a Hemlock project")

	cmdSrc := cmd.Arg("dest", "").String()
	cmdWatch := cmd.Flag("watch", "Restart server when files change").Bool()

	cmd.Action(func(context *kingpin.ParseContext) error {
		if *cmdWatch {
			go buildAndRunApp(*cmdSrc)
			watchApp(*cmdSrc)
		} else {
			// Block until it finishes
			buildAndRunApp(*cmdSrc)
			<-doneChannel
		}

		return nil
	})
}

func buildPath() string {
	p := filepath.Join(tmpPath(), buildName())
	if runtime.GOOS == "windows" && filepath.Ext(p) != ".exe" {
		p += ".exe"
	}
	return p
}

func tmpPath() string {
	return os.TempDir()
}

func buildName() string {
	dir, _ := os.Getwd()
	return filepath.Base(dir)
}

func buildApp(srcDir string) error {
	fmt.Printf("Building %v...\n", filepath.Base(buildPath()))
	cmd := exec.Command("go", "build", "-o", buildPath(), ".")
	cmd.Dir = srcDir
	stderr, err := cmd.StderrPipe()
	if err != nil {
		panic(err)
	}

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		panic(err)
	}

	err = cmd.Start()
	if err != nil {
		return err
	}

	io.Copy(os.Stdout, stdout)
	io.Copy(os.Stderr, stderr)

	err = cmd.Wait()
	if _, ok := err.(*exec.ExitError); ok {
		return err
	}

	if err != nil {
		panic(err)
	}
	return nil
}

func runApp(srcDir string) {
	fmt.Printf("Running...\n")
	cmd := exec.Command(buildPath())
	cmd.Dir = srcDir

	stderr, err := cmd.StderrPipe()
	if err != nil {
		panic(err)
	}

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		panic(err)
	}

	err = cmd.Start()
	if err != nil {
		panic(err)
	}

	go func() {
		select {
		case <-stopChannel:
			cmd.Process.Kill()
		}
	}()

	isRunning = true

	go io.Copy(os.Stderr, stderr)
	go io.Copy(os.Stdout, stdout)

	go func() {
		cmd.Wait()
		isRunning = false
		doneChannel <- nil
	}()
}

func watchFolder(path string) {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		log.Fatal(err)
	}

	// Process events
	go func() {
		for {
			select {
			case ev := <-watcher.Event:
				changeChannel <- ev.Name
			case err := <-watcher.Error:
				fmt.Printf("File watch error: %v\n", err)
			}
		}
	}()

	err = watcher.Watch(path)
	if err != nil {
		log.Fatal(err)
	}

	fmt.Printf("Watching %v...\n", path)
}

func watchApp(srcDir string) {
	watchableDirs := make([]string, 0)
	filepath.Walk(".", func(path string, info os.FileInfo, err error) error {
		if info.IsDir() {
			if len(path) > 1 && strings.HasPrefix(filepath.Base(path), ".") {
				return filepath.SkipDir
			}

			if strings.HasPrefix(path, "node_modules/") {
				return filepath.SkipDir
			}

			if strings.HasPrefix(path, "vendor/") {
				return filepath.SkipDir
			}

			if strings.HasPrefix(filepath.Base(path), "_") {
				return filepath.SkipDir
			}

			things, _ := ioutil.ReadDir(path)
			for _, thing := range things {
				if strings.HasSuffix(thing.Name(), ".go") {
					watchableDirs = append(watchableDirs, path)
					break
				}
				if strings.HasSuffix(thing.Name(), ".md") {
					watchableDirs = append(watchableDirs, path)
					break
				}
				if strings.HasSuffix(thing.Name(), ".html") {
					watchableDirs = append(watchableDirs, path)
					break
				}
			}
		}

		return err
	})

	for _, path := range watchableDirs {
		watchFolder(path)
	}

	needsToBuild := false
	nextBuild := time.Now()
	fmt.Printf("Watching for changes...\n")
	for {
		select {
		case <-changeChannel:
			needsToBuild = true
		default:
			if needsToBuild && time.Now().After(nextBuild) {
				buildAndRunApp(srcDir)
				needsToBuild = false
				nextBuild = time.Now().Add(time.Second * 2)
			}
			// Sleep so we don't thrash too much
			time.Sleep(time.Millisecond * 200)
		}
	}
}

func buildAndRunApp(srcDir string) {
	// Build before we kill the existing one
	err := buildApp(srcDir)
	if err != nil {
		fmt.Printf("Build error: %v\n", err)
		return
	}

	if isRunning {
		stopChannel <- true
		<-doneChannel
	}

	runApp(srcDir)
}
