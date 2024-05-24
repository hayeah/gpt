package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"
)

type ToolArgs struct {
	Code string `json:"code"`
}

func createTempDir(baseDir string) (string, error) {
	// Ensure the base directory exists
	if err := os.MkdirAll(baseDir, 0755); err != nil {
		return "", fmt.Errorf("error creating base directory: %v", err)
	}

	// Generate an order/shell friendly timestamp
	timestamp := time.Now().Format("20060102-150405")

	// Create the temporary directory with the timestamp
	tempDir, err := os.MkdirTemp(baseDir, fmt.Sprintf("run-%s-", timestamp))
	if err != nil {
		return "", fmt.Errorf("error creating temporary directory: %v", err)
	}

	return tempDir, nil
}
func run() error {
	toolArgs := os.Getenv("TOOL_ARGS")
	if toolArgs == "" {
		return fmt.Errorf("TOOL_ARGS is not set")
	}

	toolName := os.Getenv("TOOL_NAME")
	if toolName == "" {
		return fmt.Errorf("TOOL_NAME is not set")
	}

	var args ToolArgs
	err := json.Unmarshal([]byte(toolArgs), &args)
	if err != nil {
		return fmt.Errorf("error parsing TOOL_ARGS: %v", err)
	}

	if args.Code == "" {
		return fmt.Errorf("the code to evaluate is not provided in TOOL_ARGS")
	}

	tempDir, err := createTempDir("./runCode")
	if err != nil {
		return fmt.Errorf("error creating temporary directory: %v", err)
	}
	// defer os.RemoveAll(tempDir)

	if toolName == "evalPython" {
		tempFile := filepath.Join(tempDir, "main.py")
		err = os.WriteFile(tempFile, []byte(args.Code), 0644)
		if err != nil {
			return fmt.Errorf("error writing Python code to temporary file: %v", err)
		}

		cmd := exec.Command("python3", tempFile)
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr

		err := cmd.Run()

		if _, ok := err.(*exec.ExitError); ok {
			exitCode := cmd.ProcessState.ExitCode()
			os.Exit(exitCode)
		}

		if err != nil {
			return err
		}
	} else if toolName == "evalGolang" {
		tempFile := filepath.Join(tempDir, "main.go")
		err = os.WriteFile(tempFile, []byte(args.Code), 0644)
		if err != nil {
			return fmt.Errorf("error writing Go code to temporary file: %v", err)
		}

		binaryFile := filepath.Join(tempDir, "program")
		cmd := exec.Command("go", "build", "-o", binaryFile, tempFile)
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr

		err := cmd.Run()
		if err != nil {
			return fmt.Errorf("error compiling Go code: %w", err)
		}

		cmd = exec.Command(binaryFile)
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr

		err = cmd.Run()
		if err != nil {
			return fmt.Errorf("error running compiled program: %v", err)
		}
	} else {
		return fmt.Errorf("unsupported TOOL_NAME: %s", toolName)
	}

	return nil
}

func main() {
	if err := run(); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
}
