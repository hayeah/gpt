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

	tempDir, err := os.MkdirTemp("./runCode", "run-*")
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
		cmdOutput, err := cmd.CombinedOutput()
		if err != nil {
			return fmt.Errorf("error running Python script: %v\nOutput: %s", err, string(cmdOutput))
		}

		fmt.Printf("Success: %s\n", string(cmdOutput))

	} else if toolName == "evalGolang" {
		tempFile := filepath.Join(tempDir, "main.go")
		err = os.WriteFile(tempFile, []byte(args.Code), 0644)
		if err != nil {
			return fmt.Errorf("error writing Go code to temporary file: %v", err)
		}

		binaryFile := filepath.Join(tempDir, "program")
		cmd := exec.Command("go", "build", "-o", binaryFile, tempFile)
		output, err := cmd.CombinedOutput()
		if err != nil {
			return fmt.Errorf("error compiling Go code: %v\nOutput: %s", err, string(output))
		}

		cmd = exec.Command(binaryFile)
		cmdOutput, err := cmd.CombinedOutput()
		if err != nil {
			return fmt.Errorf("error running compiled program: %v\nOutput: %s", err, string(cmdOutput))
		}

		fmt.Printf("Success: %s\n", string(cmdOutput))

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
