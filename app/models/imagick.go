package models

import (
	"bytes"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"time"

	"github.com/technoweenie/grohl"
)

type IMagick struct{}

// Process a remote asset url using graphicsmagick with the args supplied
// and write the response to w
func (p *IMagick) Process(w http.ResponseWriter, r *http.Request, args *ProcessArgs) (err error) {
	tempDir, err := createTemporaryWorkspace()
	if err != nil {
		return
	}
	// defer os.RemoveAll(tempDir)

	inFile, err := downloadRemote(tempDir, args.Url)
	if err != nil {
		return
	}

	preProcessedInFile, err := preProcessImage(tempDir, inFile, args)
	if err != nil {
		return
	}

	outFile, err := processImage(tempDir, preProcessedInFile, args)
	if err != nil {
		return
	}

	// serve response
	http.ServeFile(w, r, outFile)
	return
}

func createTemporaryWorkspace() (string, error) {
	return ioutil.TempDir("", "_firesize")
}

func downloadRemote(tempDir string, url string) (string, error) {
	inFile := filepath.Join(tempDir, "in")

	grohl.Log(grohl.Data{
		"processor": "imagick",
		"download":  url,
		"local":     inFile,
	})

	out, err := os.Create(inFile)
	if err != nil {
		return inFile, err
	}
	defer out.Close()

	resp, err := http.Get(url)
	if err != nil {
		return inFile, err
	}
	defer resp.Body.Close()

	_, err = io.Copy(out, resp.Body)

	return inFile, err
}

func preProcessImage(tempDir string, inFile string, args *ProcessArgs) (string, error) {
	if isAnimatedGif(inFile) {
		args.Format = "gif" // Total hack cos format is incorrectly .png on example
		return coalesceAnimatedGif(tempDir, inFile)
	} else {
		return inFile, nil
	}
}

func processImage(tempDir string, inFile string, args *ProcessArgs) (string, error) {
	outFile := filepath.Join(tempDir, "out")
	cmdArgs, outFileWithFormat := args.CommandArgs(inFile, outFile)

	grohl.Log(grohl.Data{
		"processor": "imagick",
		"args":      cmdArgs,
	})

	executable := "convert"
	cmd := exec.Command(executable, cmdArgs...)
	var outErr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &outErr, &outErr
	err := runWithTimeout(cmd, 60*time.Second)
	if err != nil {
		grohl.Log(grohl.Data{
			"processor": "imagick",
			"failure":   err,
			"args":      cmdArgs,
			"output":    string(outErr.Bytes()),
		})
	}

	return outFileWithFormat, err
}

func isAnimatedGif(inFile string) bool {
	// identify -format %n updates-product-click.gif # => 105
	cmd := exec.Command("identify", "-format", "%n", inFile)
	var out bytes.Buffer
	cmd.Stdout = &out
	err := runWithTimeout(cmd, 10*time.Second)
	if err == nil {
		numFrames, err := strconv.Atoi(string(out.Bytes()))
		if err == nil {
			grohl.Log(grohl.Data{
				"processor":  "imagick",
				"num-frames": numFrames,
			})
			return numFrames > 1
		}
	}
	// if anything fucks out assume not animated
	return false
}

func coalesceAnimatedGif(tempDir string, inFile string) (string, error) {
	outFile := filepath.Join(tempDir, "temp")

	// convert do.gif -coalesce temporary.gif
	cmd := exec.Command("convert", inFile, "-coalesce", outFile)
	_ = runWithTimeout(cmd, 60*time.Second)

	return outFile, nil
}

func runWithTimeout(cmd *exec.Cmd, timeout time.Duration) error {
	// Start the process
	err := cmd.Start()
	if err != nil {
		return err
	}

	// Kill the process if it doesn't exit in time
	defer time.AfterFunc(timeout, func() {
		fmt.Println("command timed out")
		cmd.Process.Kill()
	}).Stop()

	// Wait for the process to finish
	return cmd.Wait()
}
