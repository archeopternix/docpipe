package io

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

var libreOfficeSlideSuffixPattern = regexp.MustCompile(`(?i)\s*\d+\s*$`)

func pptxFileConverter(path string, docs *Documents) error {
	workDir, err := os.MkdirTemp("", "pptx2md-*")
	if err != nil {
		return err
	}
	defer func() { _ = os.RemoveAll(workDir) }()

	markdownFile := docs.MetaData.ParseFileNameFromMetaData()

	if err := runCommand(nil, "pptx2md",
		path,
		"-o", filepath.Join(workDir, markdownFile),
		"-i", filepath.Join(workDir, "media"),
	); err != nil {
		return err
	}

	/*
		body, err := os.ReadFile(filepath.Join(workDir, markdownFile))
		if err != nil {
			return err
		}
	*/

	// attach downloaded media files
	filepath.Walk(filepath.Join(workDir, "media"), func(path string, info os.FileInfo, err error) error {
		if err != nil {
			if os.IsNotExist(err) {
				return nil
			}
			return err
		}
		if info.IsDir() {
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
		docs.ExtractedImages[filepath.ToSlash(relPath)] = bytes.NewBuffer(fileBody)
		return nil
	})
	// adjust media links in markdown

	// Make screenshots of slides
	if err := checkScreenshotAvailability(); err != nil {

		screensDir, err := os.MkdirTemp("", "pptx-slides-*")
		if err != nil {
			return err
		}
		defer func() { _ = os.RemoveAll(screensDir) }()

		if err := exportPptxSlideScreenshots(path, screensDir); err != nil {
			return err
		}
		entries, err := os.ReadDir(screensDir)
		if err != nil {
			return err
		}
		for _, entry := range entries {
			if entry.IsDir() || strings.ToLower(filepath.Ext(entry.Name())) != ".png" {
				continue
			}
			slidePath := filepath.Join(screensDir, entry.Name())
			slideBody, err := os.ReadFile(slidePath)
			if err != nil {
				return err
			}
			docs.ExtractedSlides[entry.Name()] = bytes.NewBuffer(slideBody)
		}
	}
	return nil
}

// CheckScreenshotAvailability verifies that slide screenshot export can use
// the locally installed backend for the current OS.
func checkScreenshotAvailability() error {
	switch runtime.GOOS {
	case "windows":
		if err := commandAvailable("powershell", "pwsh"); err != nil {
			return fmt.Errorf("PowerShell nicht gefunden: %w", err)
		}

		cmd := exec.Command("reg", "query", `HKCR\PowerPoint.Application`)
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("Microsoft PowerPoint ist nicht installiert oder nicht fuer COM registriert")
		}
		return nil
	case "linux":
		if err := commandAvailable("soffice", "libreoffice"); err != nil {
			return fmt.Errorf("LibreOffice nicht gefunden: %w", err)
		}
		return nil
	default:
		return fmt.Errorf("PPTX-Screenshots werden auf %s nicht unterstuetzt", runtime.GOOS)
	}
}

func exportPptxSlideScreenshots(sourcePath, outputDir string) error {
	if err := ensureDirs(outputDir); err != nil {
		return err
	}

	switch runtime.GOOS {
	case "windows":
		return exportPptxSlideScreenshotsWindows(sourcePath, outputDir)
	case "linux":
		return exportPptxSlideScreenshotsLinux(sourcePath, outputDir)
	default:
		return fmt.Errorf("PPTX-Screenshots werden auf %s nicht unterstuetzt", runtime.GOOS)
	}
}

func exportPptxSlideScreenshotsWindows(sourcePath, outputDir string) error {
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

func exportPptxSlideScreenshotsLinux(sourcePath, outputDir string) error {
	command, err := resolveLibreOfficeCommand()
	if err != nil {
		return err
	}

	slideCount, err := countPptxSlides(sourcePath)
	if err != nil {
		return err
	}
	if slideCount == 0 {
		return fmt.Errorf("keine Slides in PPTX gefunden")
	}

	workDir, err := os.MkdirTemp("", "pptx-slides-*")
	if err != nil {
		return err
	}
	defer func() { _ = os.RemoveAll(workDir) }()

	baseName := strings.TrimSuffix(filepath.Base(sourcePath), filepath.Ext(sourcePath))
	for page := 1; page <= slideCount; page++ {
		filter := buildLibreOfficePngFilter(page)
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

		exportedPath, err := findLibreOfficeExportedPng(workDir, baseName)
		if err != nil {
			return err
		}

		targetName := fmt.Sprintf("slide-%03d.png", page)
		targetPath := filepath.Join(outputDir, targetName)
		if err := os.Rename(exportedPath, targetPath); err != nil {
			if err := copyFile(exportedPath, targetPath); err != nil {
				return err
			}
			_ = os.Remove(exportedPath)
		}
	}

	return nil
}

func stateSlideLinks(doc *Documents) []string {
	var slideLinks []string
	for name := range doc.ExtractedSlides {
		if strings.ToLower(filepath.Ext(name)) != ".png" || !strings.HasPrefix(filepath.ToSlash(name), "slides/") {
			continue
		}
		slideLinks = append(slideLinks, filepath.ToSlash(name))
	}
	sort.Strings(slideLinks)
	return slideLinks
}

func injectSlideLinks(markdown string, slideLinks []string) string {
	lines := strings.Split(strings.ReplaceAll(markdown, "\r\n", "\n"), "\n")
	var headingIdx []int
	for i, line := range lines {
		if strings.HasPrefix(strings.TrimSpace(line), "# ") {
			headingIdx = append(headingIdx, i)
		}
	}
	if len(headingIdx) == 0 {
		var builder strings.Builder
		builder.WriteString(strings.TrimRight(markdown, "\n"))
		builder.WriteString("\n\n")
		for _, link := range slideLinks {
			builder.WriteString(fmt.Sprintf("[Slide screenshot](%s)\n\n", link))
		}
		return builder.String()
	}

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
	return strings.Join(out, "\n")
}

func resolveLibreOfficeCommand() (string, error) {
	for _, name := range []string{"soffice", "libreoffice"} {
		if _, err := exec.LookPath(name); err == nil {
			return name, nil
		}
	}
	return "", fmt.Errorf("LibreOffice nicht gefunden")
}

func buildLibreOfficePngFilter(pageNumber int) string {
	return fmt.Sprintf(
		`png:impress_png_Export:{"PixelWidth":{"type":"long","value":"1920"},"PixelHeight":{"type":"long","value":"1080"},"PageNumber":{"type":"long","value":"%d"}}`,
		pageNumber,
	)
}

func findLibreOfficeExportedPng(dir, baseName string) (string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return "", err
	}

	baseName = strings.ToLower(strings.TrimSpace(baseName))
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if strings.ToLower(filepath.Ext(name)) != ".png" {
			continue
		}

		stem := strings.ToLower(strings.TrimSuffix(name, filepath.Ext(name)))
		trimmedStem := libreOfficeSlideSuffixPattern.ReplaceAllString(stem, "")
		if stem == baseName || strings.TrimSpace(trimmedStem) == baseName {
			return filepath.Join(dir, name), nil
		}
	}

	return "", fmt.Errorf("LibreOffice hat keine PNG-Datei fuer %s erzeugt", baseName)
}

func countPptxSlides(path string) (int, error) {
	reader, err := zip.OpenReader(path)
	if err != nil {
		return 0, err
	}
	defer reader.Close()

	var slideEntries []string
	for _, file := range reader.File {
		if strings.HasPrefix(file.Name, "ppt/slides/slide") && strings.HasSuffix(strings.ToLower(file.Name), ".xml") {
			slideEntries = append(slideEntries, file.Name)
		}
	}
	sort.Strings(slideEntries)
	if len(slideEntries) > 0 {
		return len(slideEntries), nil
	}

	return 0, fmt.Errorf("keine PPTX-Slides gefunden")
}

func runCommand(logger func(string, ...any), command string, args ...string) error {
	cmd := exec.Command(command, args...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if logger != nil {
		logger("Running: %s", strings.Join(cmd.Args, " "))
	}
	if err := cmd.Run(); err != nil {
		if stderr.Len() > 0 {
			return fmt.Errorf("%s", strings.TrimSpace(stderr.String()))
		}
		return err
	}
	return nil
}

func commandAvailable(commands ...string) error {
	var lastErr error
	for _, command := range commands {
		if _, err := exec.LookPath(command); err == nil {
			return nil
		} else {
			lastErr = err
		}
	}
	if lastErr == nil {
		return fmt.Errorf("kein Befehl konfiguriert")
	}
	return lastErr
}

// EnsureDirs creates each non-empty directory path if it does not already exist.
func ensureDirs(dirs ...string) error {
	for _, dir := range dirs {
		if dir == "" {
			continue
		}
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
	}
	return nil
}

// CopyFile copies a single file to dst and creates the parent directory first.
func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	if err := ensureDirs(filepath.Dir(dst)); err != nil {
		return err
	}

	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer func() { _ = out.Close() }()

	if _, err := out.ReadFrom(in); err != nil {
		return err
	}
	return out.Sync()
}
