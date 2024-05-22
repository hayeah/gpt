package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

type ToolArgs struct {
	Code string `json:"code"`
}

func run() error {
	toolArgs := os.Getenv("TOOL_ARGS")
	if toolArgs == "" {
		return fmt.Errorf("TOOL_ARGS is not set")
	}

	var args ToolArgs
	err := json.Unmarshal([]byte(toolArgs), &args)
	if err != nil {
		return fmt.Errorf("error parsing TOOL_ARGS: %v", err)
	}

	if args.Code == "" {
		return fmt.Errorf("the code to evaluate is not provided in TOOL_ARGS")
	}

	// Write the provided code to a temporary file
	tempDir, err := os.MkdirTemp("./runGo", "run-*")
	if err != nil {
		return fmt.Errorf("error creating temporary directory: %v", err)
	}
	// defer os.RemoveAll(tempDir)

	tempFile := filepath.Join(tempDir, "main.go")
	err = os.WriteFile(tempFile, []byte(args.Code), 0644)
	if err != nil {
		return fmt.Errorf("error writing Go code to temporary file: %v", err)
	}

	// Compile the Go code
	binaryFile := filepath.Join(tempDir, "program")
	cmd := exec.Command("go", "build", "-o", binaryFile, tempFile)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("error compiling Go code: %v\nOutput: %s", err, string(output))
	}

	// Run the compiled binary
	cmd = exec.Command(binaryFile)
	cmdOutput, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("error running compiled program: %v\nOutput: %s", err, string(cmdOutput))
	}

	fmt.Printf("Success: %s\n", string(cmdOutput))
	return nil
}

func main() {
	if err := run(); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
}
