package main

import (
	"archive/zip"
	"context"
	"docpipe"
	dpstore "docpipe/store"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
)

func main() {
	inputPath := defaultInputPath()
	if len(os.Args) > 1 && strings.TrimSpace(os.Args[1]) != "" {
		inputPath = os.Args[1]
	}

	in, err := os.Open(inputPath)
	if err != nil {
		log.Fatalf("open %s: %v", inputPath, err)
	}
	defer in.Close()

	service := docpipe.NewService(dpstore.FS{BasePath: filepath.Join(filepath.Dir(inputPath), "docpipe-store")})
	doc, err := service.ImportDocument(context.Background(), docpipe.ImportSource{
		Reader: in,
		Name:   filepath.Base(inputPath),
	})
	if err != nil {
		log.Fatalf("import %s: %v", inputPath, err)
	}

	outPath := filepath.Join(filepath.Dir(inputPath), doc.ID+".zip")
	out, err := os.Create(outPath)
	if err != nil {
		log.Fatalf("create zip %s: %v", outPath, err)
	}
	defer out.Close()

	zw := zip.NewWriter(out)
	if err := service.ExportZip(context.Background(), doc, zw); err != nil {
		log.Fatalf("export zip: %v", err)
	}
	if err := zw.Close(); err != nil {
		log.Fatalf("close zip: %v", err)
	}

	fmt.Printf("input: %s\n", inputPath)
	fmt.Printf("document: %s\n", doc.ID)
	fmt.Printf("output: %s\n", outPath)
}

func defaultInputPath() string {
	return filepath.Join("TestData", "strategy_IT_V1.3.docx")
	// return filepath.Join("TestData", "real.pptx")
	// return filepath.Join("TestData", "sample.md")
}
