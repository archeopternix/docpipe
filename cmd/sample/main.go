package main

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"

	docio "docpipe/io"
)

func main() {
	inputPath := defaultInputPath()
	if len(os.Args) > 1 && strings.TrimSpace(os.Args[1]) != "" {
		inputPath = os.Args[1]
	}

	docs, err := docio.ConvertFile(inputPath)
	if err != nil {
		log.Fatalf("convert %s: %v", inputPath, err)
	}

	err = docs.SaveAsZip(filepath.Dir(inputPath))
	if err != nil {
		log.Fatalf("save zip: %v", err)
	}

	fmt.Printf("input: %s\n", inputPath)
	fmt.Printf("output: %s\n", filepath.Dir(inputPath))
}

func defaultInputPath() string {
	//return filepath.Join("TestData", "strategy_IT_V1.3.docx")
	return filepath.Join("TestData", "real.pptx")
}
