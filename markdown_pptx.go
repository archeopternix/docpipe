package docpipe

import (
	"archive/zip"
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
)

var pptxLibreOfficeSlideSuffixPattern = regexp.MustCompile(`(?i)\s*\d+\s*$`)

type PowerPointParams struct {
	IncludeSlides bool
	IncludeImages bool
}

func CreateFromPowerPoint(path string, params *PowerPointParams) (*Markdown, error) {
	if params == nil {
		params = &PowerPointParams{
			IncludeSlides: true,
			IncludeImages: true,
		}
	}

	if _, err := os.Stat(path); err != nil {
		return nil, err
	}
	if mdNormalizeExtension(filepath.Ext(path)) != ".pptx" {
		return nil, fmt.Errorf("powerpoint conversion not supported for %q", filepath.Ext(path))
	}

	doc, err := officeNewDocument(path)
	if err != nil {
		return nil, err
	}
	workDir, err := os.MkdirTemp("", "pptx2md-*")
	if err != nil {
		return nil, err
	}
	defer func() { _ = os.RemoveAll(workDir) }()

	sourcePath, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}

	markdownFile := mdFileName(doc.metaData)
	args := []string{
		sourcePath,
		"-o", markdownFile,
	}
	if params.IncludeImages {
		args = append(args, "-i", "media")
	}

	cmd := exec.Command("pptx2md", args...)
	cmd.Dir = workDir
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		if stderr.Len() > 0 {
			return nil, fmt.Errorf("%s", strings.TrimSpace(stderr.String()))
		}
		return nil, err
	}

	body, err := os.ReadFile(filepath.Join(workDir, markdownFile))
	if err != nil {
		return nil, err
	}

	if params.IncludeImages {
		if err := filepath.Walk(filepath.Join(workDir, "media"), func(path string, info os.FileInfo, err error) error {
			if err != nil {
				if os.IsNotExist(err) {
					return nil
				}
				return err
			}
			if info == nil || info.IsDir() {
				return nil
			}
			relPath, err := filepath.Rel(filepath.Join(workDir, "media"), path)
			if err != nil {
				return err
			}
			fileBody, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			doc.extractedImages[filepath.ToSlash(relPath)] = bytes.NewBuffer(fileBody)
			return nil
		}); err != nil {
			return nil, err
		}
	}

	if params.IncludeSlides {
		screenshotAvailable := false
		switch runtime.GOOS {
		case "windows":
			if err := pptxCommandAvailable("powershell", "pwsh"); err == nil {
				cmd := exec.Command("reg", "query", `HKCR\PowerPoint.Application`)
				screenshotAvailable = cmd.Run() == nil
			}
		case "linux":
			screenshotAvailable = pptxCommandAvailable("soffice", "libreoffice") == nil
		}
		if screenshotAvailable {
			screensDir, err := os.MkdirTemp("", "pptx-slides-*")
			if err != nil {
				return nil, err
			}
			defer func() { _ = os.RemoveAll(screensDir) }()

			if err := pptxExportSlideScreenshots(path, screensDir); err != nil {
				return nil, err
			}
			entries, err := os.ReadDir(screensDir)
			if err != nil {
				return nil, err
			}
			for _, entry := range entries {
				if entry.IsDir() || strings.ToLower(filepath.Ext(entry.Name())) != ".png" {
					continue
				}
				slidePath := filepath.Join(screensDir, entry.Name())
				slideBody, err := os.ReadFile(slidePath)
				if err != nil {
					return nil, err
				}
				doc.extractedSlides[entry.Name()] = bytes.NewBuffer(slideBody)
			}
		}
	}

	text := officeCleanupMarkdownContent(string(body))
	if params.IncludeSlides {
		var slideLinks []string
		for name := range doc.extractedSlides {
			if strings.ToLower(filepath.Ext(name)) != ".png" {
				continue
			}
			slideLinks = append(slideLinks, filepath.ToSlash(filepath.Join("slides", name)))
		}
		sort.Strings(slideLinks)
		lines := strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n")
		var headingIdx []int
		for i, line := range lines {
			if strings.HasPrefix(strings.TrimSpace(line), "# ") {
				headingIdx = append(headingIdx, i)
			}
		}
		if len(headingIdx) == 0 {
			var builder strings.Builder
			builder.WriteString(strings.TrimRight(text, "\n"))
			builder.WriteString("\n\n")
			for _, link := range slideLinks {
				builder.WriteString(fmt.Sprintf("[Slide screenshot](%s)\n\n", link))
			}
			text = builder.String()
		} else {
			var out []string
			linkPos := 0
			for i, line := range lines {
				out = append(out, line)
				nextHeading := false
				for _, idx := range headingIdx[1:] {
					if i+1 == idx {
						nextHeading = true
						break
					}
				}
				lastLine := i == len(lines)-1
				if (nextHeading || lastLine) && linkPos < len(slideLinks) {
					if len(out) > 0 && strings.TrimSpace(out[len(out)-1]) != "" {
						out = append(out, "")
					}
					out = append(out, fmt.Sprintf("[Slide screenshot](%s)", slideLinks[linkPos]), "")
					linkPos++
				}
			}
			for ; linkPos < len(slideLinks); linkPos++ {
				out = append(out, fmt.Sprintf("[Slide screenshot](%s)", slideLinks[linkPos]), "")
			}
			text = strings.Join(out, "\n")
		}
	}
	doc.markdownFile = bytes.NewBufferString(text)
	mdApplyMetaDataFrontmatter(doc)
	zipName := markdownZipFileName(mdFileName(doc.metaData))
	if err != nil {
		return nil, err
	}
	doc.fileName = zipName

	return doc, nil
}

func pptxExportSlideScreenshots(sourcePath, outputDir string) error {
	sourcePath, err := filepath.Abs(sourcePath)
	if err != nil {
		return err
	}
	outputDir, err = filepath.Abs(outputDir)
	if err != nil {
		return err
	}
	if outputDir != "" {
		if err := os.MkdirAll(outputDir, 0o755); err != nil {
			return err
		}
	}

	switch runtime.GOOS {
	case "windows":
		return pptxExportSlideScreenshotsWindows(sourcePath, outputDir)
	case "linux":
		return pptxExportSlideScreenshotsLinux(sourcePath, outputDir)
	default:
		return fmt.Errorf("PPTX screenshots are not supported on %s", runtime.GOOS)
	}
}

func pptxExportSlideScreenshotsWindows(sourcePath, outputDir string) error {
	scriptPath := filepath.Join(outputDir, "export_slides.ps1")
	script := strings.TrimSpace(`
param(
	[string]$SourcePath,
	[string]$OutputDir
)

$ErrorActionPreference = "Stop"

$powerPoint = $null
$presentation = $null

try {
	$powerPoint = New-Object -ComObject PowerPoint.Application
	$presentation = $powerPoint.Presentations.Open($SourcePath, $false, $true, $false)

	foreach ($slide in $presentation.Slides) {
		$fileName = "slide-{0:D3}.png" -f [int]$slide.SlideIndex
		$targetPath = Join-Path $OutputDir $fileName
		$slide.Export($targetPath, "PNG")
	}
}
finally {
	if ($presentation -ne $null) {
		$presentation.Close()
	}
	if ($powerPoint -ne $null) {
		$powerPoint.Quit()
	}
	[GC]::Collect()
	[GC]::WaitForPendingFinalizers()
}
`) + "\n"

	if err := os.WriteFile(scriptPath, []byte(script), 0o600); err != nil {
		return err
	}

	cmd := exec.Command(
		"powershell",
		"-NoProfile",
		"-NonInteractive",
		"-ExecutionPolicy", "Bypass",
		"-File", scriptPath,
		"-SourcePath", sourcePath,
		"-OutputDir", outputDir,
	)
	output, err := cmd.CombinedOutput()
	_ = os.Remove(scriptPath)

	if err != nil {
		message := strings.TrimSpace(string(output))
		if message == "" {
			return err
		}
		return fmt.Errorf("%s", message)
	}
	return nil
}

func pptxExportSlideScreenshotsLinux(sourcePath, outputDir string) error {
	command := ""
	for _, name := range []string{"soffice", "libreoffice"} {
		if _, err := exec.LookPath(name); err == nil {
			command = name
			break
		}
	}
	if command == "" {
		return fmt.Errorf("LibreOffice not found")
	}

	reader, err := zip.OpenReader(sourcePath)
	if err != nil {
		return err
	}
	defer reader.Close()

	var slideEntries []string
	for _, file := range reader.File {
		if strings.HasPrefix(file.Name, "ppt/slides/slide") && strings.HasSuffix(strings.ToLower(file.Name), ".xml") {
			slideEntries = append(slideEntries, file.Name)
		}
	}
	sort.Strings(slideEntries)
	slideCount := len(slideEntries)
	if slideCount == 0 {
		return fmt.Errorf("no slides found in PPTX")
	}

	workDir, err := os.MkdirTemp("", "pptx-slides-*")
	if err != nil {
		return err
	}
	defer func() { _ = os.RemoveAll(workDir) }()

	baseName := strings.TrimSuffix(filepath.Base(sourcePath), filepath.Ext(sourcePath))
	for page := 1; page <= slideCount; page++ {
		filter := fmt.Sprintf(
			`png:impress_png_Export:{"PixelWidth":{"type":"long","value":"1920"},"PixelHeight":{"type":"long","value":"1080"},"PageNumber":{"type":"long","value":"%d"}}`,
			page,
		)
		cmd := exec.Command(
			command,
			"--headless",
			"--convert-to", filter,
			"--outdir", workDir,
			sourcePath,
		)
		output, runErr := cmd.CombinedOutput()

		if runErr != nil {
			message := strings.TrimSpace(string(output))
			if message == "" {
				return runErr
			}
			return fmt.Errorf("%s", message)
		}

		entries, err := os.ReadDir(workDir)
		if err != nil {
			return err
		}
		exportedPath := ""
		normalizedBaseName := strings.ToLower(strings.TrimSpace(baseName))
		for _, entry := range entries {
			if entry.IsDir() {
				continue
			}
			name := entry.Name()
			if strings.ToLower(filepath.Ext(name)) != ".png" {
				continue
			}

			stem := strings.ToLower(strings.TrimSuffix(name, filepath.Ext(name)))
			trimmedStem := pptxLibreOfficeSlideSuffixPattern.ReplaceAllString(stem, "")
			if stem == normalizedBaseName || strings.TrimSpace(trimmedStem) == normalizedBaseName {
				exportedPath = filepath.Join(workDir, name)
				break
			}
		}
		if exportedPath == "" {
			return fmt.Errorf("LibreOffice did not create a PNG file for %s", baseName)
		}

		targetName := fmt.Sprintf("slide-%03d.png", page)
		targetPath := filepath.Join(outputDir, targetName)
		if err := os.Rename(exportedPath, targetPath); err != nil {
			in, err := os.Open(exportedPath)
			if err != nil {
				return err
			}
			if err := os.MkdirAll(filepath.Dir(targetPath), 0o755); err != nil {
				_ = in.Close()
				return err
			}

			out, err := os.Create(targetPath)
			if err != nil {
				_ = in.Close()
				return err
			}

			_, copyErr := out.ReadFrom(in)
			syncErr := out.Sync()
			closeOutErr := out.Close()
			closeInErr := in.Close()
			switch {
			case copyErr != nil:
				return copyErr
			case syncErr != nil:
				return syncErr
			case closeOutErr != nil:
				return closeOutErr
			case closeInErr != nil:
				return closeInErr
			}
			if err := os.Remove(exportedPath); err != nil {
				return err
			}
		}
	}

	return nil
}

func pptxCommandAvailable(commands ...string) error {
	var lastErr error
	for _, command := range commands {
		if _, err := exec.LookPath(command); err == nil {
			return nil
		} else {
			lastErr = err
		}
	}
	if lastErr == nil {
		return fmt.Errorf("no command configured")
	}
	return lastErr
}
