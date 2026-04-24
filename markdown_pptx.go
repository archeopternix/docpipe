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

var markdownPPTXLibreOfficeSlideSuffixPattern = regexp.MustCompile(`(?i)\s*\d+\s*$`)

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
	if markdownMDNormalizeExtension(filepath.Ext(path)) != ".pptx" {
		return nil, fmt.Errorf("powerpoint conversion not supported for %q", filepath.Ext(path))
	}

	doc, err := markdownOfficeNewDocument(path)
	if err != nil {
		return nil, err
	}
	if err := markdownPPTXConvertToMarkdown(path, doc, params); err != nil {
		return nil, err
	}
	doc.fileName = markdownMDZipFileName(markdownMDFileName(doc.metaData))

	return doc, nil
}

func markdownPPTXConvertToMarkdown(path string, doc *Markdown, params *PowerPointParams) error {
	workDir, err := os.MkdirTemp("", "pptx2md-*")
	if err != nil {
		return err
	}
	defer func() { _ = os.RemoveAll(workDir) }()

	sourcePath, err := filepath.Abs(path)
	if err != nil {
		return err
	}

	markdownFile := markdownMDFileName(doc.metaData)
	args := []string{
		sourcePath,
		"-o", markdownFile,
	}
	if params.IncludeImages {
		args = append(args, "-i", "media")
	}

	if err := markdownPPTXRunCommandInDir(workDir, nil, "pptx2md", args...); err != nil {
		return err
	}

	body, err := os.ReadFile(filepath.Join(workDir, markdownFile))
	if err != nil {
		return err
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
			return err
		}
	}

	if params.IncludeSlides {
		if err := markdownPPTXCheckScreenshotAvailability(); err == nil {
			screensDir, err := os.MkdirTemp("", "pptx-slides-*")
			if err != nil {
				return err
			}
			defer func() { _ = os.RemoveAll(screensDir) }()

			if err := markdownPPTXExportSlideScreenshots(path, screensDir); err != nil {
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
				doc.extractedSlides[entry.Name()] = bytes.NewBuffer(slideBody)
			}
		}
	}

	text := markdownOfficeCleanupMarkdownContent(string(body))
	if params.IncludeSlides {
		text = markdownPPTXInjectSlideLinks(text, markdownPPTXStateSlideLinks(doc))
	}
	doc.markdownFile = bytes.NewBufferString(text)
	markdownMDApplyMetaDataFrontmatter(doc)

	return nil
}

func markdownPPTXCheckScreenshotAvailability() error {
	switch runtime.GOOS {
	case "windows":
		if err := markdownPPTXCommandAvailable("powershell", "pwsh"); err != nil {
			return fmt.Errorf("PowerShell not found: %w", err)
		}

		cmd := exec.Command("reg", "query", `HKCR\PowerPoint.Application`)
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("Microsoft PowerPoint is not installed or not registered for COM")
		}
		return nil
	case "linux":
		if err := markdownPPTXCommandAvailable("soffice", "libreoffice"); err != nil {
			return fmt.Errorf("LibreOffice not found: %w", err)
		}
		return nil
	default:
		return fmt.Errorf("PPTX screenshots are not supported on %s", runtime.GOOS)
	}
}

func markdownPPTXExportSlideScreenshots(sourcePath, outputDir string) error {
	sourcePath, err := filepath.Abs(sourcePath)
	if err != nil {
		return err
	}
	outputDir, err = filepath.Abs(outputDir)
	if err != nil {
		return err
	}

	if err := markdownPPTXEnsureDirs(outputDir); err != nil {
		return err
	}

	switch runtime.GOOS {
	case "windows":
		return markdownPPTXExportSlideScreenshotsWindows(sourcePath, outputDir)
	case "linux":
		return markdownPPTXExportSlideScreenshotsLinux(sourcePath, outputDir)
	default:
		return fmt.Errorf("PPTX screenshots are not supported on %s", runtime.GOOS)
	}
}

func markdownPPTXExportSlideScreenshotsWindows(sourcePath, outputDir string) error {
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

func markdownPPTXExportSlideScreenshotsLinux(sourcePath, outputDir string) error {
	command, err := markdownPPTXResolveLibreOfficeCommand()
	if err != nil {
		return err
	}

	slideCount, err := markdownPPTXCountSlides(sourcePath)
	if err != nil {
		return err
	}
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
		filter := markdownPPTXBuildLibreOfficePngFilter(page)
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

		exportedPath, err := markdownPPTXFindLibreOfficeExportedPng(workDir, baseName)
		if err != nil {
			return err
		}

		targetName := fmt.Sprintf("slide-%03d.png", page)
		targetPath := filepath.Join(outputDir, targetName)
		if err := os.Rename(exportedPath, targetPath); err != nil {
			if err := markdownPPTXCopyFile(exportedPath, targetPath); err != nil {
				return err
			}
			_ = os.Remove(exportedPath)
		}
	}

	return nil
}

func markdownPPTXStateSlideLinks(doc *Markdown) []string {
	var slideLinks []string
	for name := range doc.extractedSlides {
		if strings.ToLower(filepath.Ext(name)) != ".png" {
			continue
		}
		slideLinks = append(slideLinks, filepath.ToSlash(filepath.Join("slides", name)))
	}
	sort.Strings(slideLinks)
	return slideLinks
}

func markdownPPTXInjectSlideLinks(markdown string, slideLinks []string) string {
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

func markdownPPTXResolveLibreOfficeCommand() (string, error) {
	for _, name := range []string{"soffice", "libreoffice"} {
		if _, err := exec.LookPath(name); err == nil {
			return name, nil
		}
	}
	return "", fmt.Errorf("LibreOffice not found")
}

func markdownPPTXBuildLibreOfficePngFilter(pageNumber int) string {
	return fmt.Sprintf(
		`png:impress_png_Export:{"PixelWidth":{"type":"long","value":"1920"},"PixelHeight":{"type":"long","value":"1080"},"PageNumber":{"type":"long","value":"%d"}}`,
		pageNumber,
	)
}

func markdownPPTXFindLibreOfficeExportedPng(dir, baseName string) (string, error) {
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
		trimmedStem := markdownPPTXLibreOfficeSlideSuffixPattern.ReplaceAllString(stem, "")
		if stem == baseName || strings.TrimSpace(trimmedStem) == baseName {
			return filepath.Join(dir, name), nil
		}
	}

	return "", fmt.Errorf("LibreOffice did not create a PNG file for %s", baseName)
}

func markdownPPTXCountSlides(path string) (int, error) {
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

	return 0, fmt.Errorf("no PPTX slides found")
}

func markdownPPTXRunCommandInDir(dir string, logger func(string, ...any), command string, args ...string) error {
	cmd := exec.Command(command, args...)
	cmd.Dir = dir
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

func markdownPPTXCommandAvailable(commands ...string) error {
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

func markdownPPTXEnsureDirs(dirs ...string) error {
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

func markdownPPTXCopyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	if err := markdownPPTXEnsureDirs(filepath.Dir(dst)); err != nil {
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
