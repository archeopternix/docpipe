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

	var meta docio.MetaData
	body, err := docio.ConvertFile(inputPath, &meta)
	if err != nil {
		log.Fatalf("convert %s: %v", inputPath, err)
	}

	markdown := docio.ApplyMetaDataFrontmatter(body.String(), meta)
	outputPath := markdownOutputPath(inputPath)
	if err := os.WriteFile(outputPath, []byte(markdown), 0o644); err != nil {
		log.Fatalf("write %s: %v", outputPath, err)
	}

	fmt.Printf("input: %s\n", inputPath)
	fmt.Printf("output: %s\n", outputPath)
}

func defaultInputPath() string {
	return filepath.Join("TestData", "strategy.docx")
}

func markdownOutputPath(inputPath string) string {
	dir := filepath.Dir(inputPath)
	ext := strings.ToLower(filepath.Ext(inputPath))
	base := strings.TrimSuffix(filepath.Base(inputPath), filepath.Ext(inputPath))

	if ext == ".md" || ext == ".markdown" {
		return filepath.Join(dir, base+".converted.md")
	}
	return filepath.Join(dir, base+".md")
}
