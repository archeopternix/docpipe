package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"

	"github.com/archeopternix/docpipe"
	"github.com/archeopternix/docpipe/ai"
	"github.com/archeopternix/docpipe/search"
	dpstore "github.com/archeopternix/docpipe/store"
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

	search, err := search.NewBleveSearch(filepath.Join(filepath.Dir(inputPath), "bleve-index"))
	if err != nil {
		log.Fatalf("NewBleveSearch() error = %v", err)
	}
	service := docpipe.NewService(dpstore.FS{BasePath: filepath.Join(filepath.Dir(inputPath), "docpipe-store")}, search)
	doc, err := service.ImportDocument(context.Background(), docpipe.ImportSource{
		Reader: in,
		Name:   filepath.Base(inputPath),
	})
	if err != nil {
		log.Fatalf("import %s: %v", inputPath, err)
	}

	var lang string

	lang, err = service.DetectLanguage(context.Background(), doc, ai.NewChatGPTClientFromEnv())
	if err != nil {
		log.Fatalf("language detection %v", err)
	}
	fm := docpipe.Frontmatter{Language: lang}
	service.WriteFrontmatter(context.Background(), doc, fm, docpipe.UpdateOptions{ArchivePrevious: true, BumpVersion: true})

	err = service.Clean(context.Background(), doc, docpipe.UpdateOptions{ArchivePrevious: true, BumpVersion: true})
	if err != nil {
		log.Fatalf("Cleaning %v", err)
	}

	outPath := filepath.Join(filepath.Dir(inputPath), doc.ID+".zip")

	fmt.Printf("input: %s\n", inputPath)
	fmt.Printf("document: %s\n", doc.ID)
	fmt.Printf("output: %s\n", outPath)
}

func defaultInputPath() string {
	//return filepath.Join("TestData", "strategy_IT_V1.3.docx")
	//return filepath.Join("TestData", "real.pptx")
	//return filepath.Join("TestData", "sample.md")
	return filepath.Join("TestData", "playbook.txt")
}
