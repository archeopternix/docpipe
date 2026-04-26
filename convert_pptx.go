package docpipe

import (
	"archive/zip"
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"runtime"
	"sort"
	"strings"

	"github.com/archeopternix/docpipe/clean"
)

func convertPptx(ctx context.Context, sourcePath string, src ImportSource, opt PptxOptions) (importedDocument, error) {
	ctx = contextOrBackground(ctx)
	if err := ctx.Err(); err != nil {
		return importedDocument{}, err
	}
	if mdNormalizeExtension(filepath.Ext(sourcePath)) != ".pptx" {
		return importedDocument{}, ErrUnsupported
	}

	pptx2mdPath, err := requiredTool("pptx2md")
	if err != nil {
		return importedDocument{}, err
	}

	meta, err := officeFrontmatter(sourcePath, src)
	if err != nil {
		return importedDocument{}, err
	}
	workDir, err := os.MkdirTemp("", "pptx2md-*")
	if err != nil {
		return importedDocument{}, err
	}
	defer func() { _ = os.RemoveAll(workDir) }()

	absSourcePath, err := filepath.Abs(sourcePath)
	if err != nil {
		return importedDocument{}, err
	}

	markdownFile := mdFileName(meta)
	args := []string{
		absSourcePath,
		"-o", markdownFile,
	}
	if opt.IncludeImages {
		args = append(args, "-i", "media")
	}

	cmdCtx, cancel, timeout := contextWithToolTimeout(ctx, defaultExternalToolTimeout)
	defer cancel()
	cmd := exec.CommandContext(cmdCtx, pptx2mdPath, args...)
	cmd.Dir = workDir
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return importedDocument{}, commandRunError(cmdCtx, "pptx2md", timeout, err, stderr.Bytes())
	}

	body, err := os.ReadFile(filepath.Join(workDir, markdownFile))
	if err != nil {
		return importedDocument{}, err
	}

	imported := importedDocument{
		Media:  map[string][]byte{},
		Slides: map[string][]byte{},
	}
	if opt.IncludeImages {
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
			imported.Media[filepath.ToSlash(filepath.Join("media", relPath))] = fileBody
			return nil
		}); err != nil {
			return importedDocument{}, err
		}
	}

	if opt.IncludeSlides {
		slides, err := pptxSlideScreenshots(ctx, sourcePath)
		if err != nil {
			if !strings.Contains(err.Error(), ErrUnsupported.Error()) && !strings.Contains(err.Error(), ErrToolMissing.Error()) {
				return importedDocument{}, err
			}
		}
		for name, body := range slides {
			imported.Slides[name] = body
		}
	}

	text := clean.Normalize(string(body), clean.Options{CleanTables: true})
	if opt.IncludeSlides {
		text = pptxAppendSlideLinks(text, imported.Slides)
	}
	imported.Root = []byte(mdComposeMarkdownWithMeta(meta, text))
	return imported, nil
}

func pptxAppendSlideLinks(text string, slides map[string][]byte) string {
	var slideLinks []string
	for name := range slides {
		if strings.ToLower(filepath.Ext(name)) != ".png" {
			continue
		}
		slideLinks = append(slideLinks, filepath.ToSlash(name))
	}
	sort.Strings(slideLinks)
	if len(slideLinks) == 0 {
		return text
	}

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

func pptxSlideScreenshots(ctx context.Context, sourcePath string) (map[string][]byte, error) {
	screenshotAvailable := false
	switch runtime.GOOS {
	case "windows":
		if err := pptxCommandAvailable("powershell", "pwsh"); err == nil {
			regCtx, cancel, _ := contextWithToolTimeout(ctx, defaultExternalToolTimeout)
			cmd := exec.CommandContext(regCtx, "reg", "query", `HKCR\PowerPoint.Application`)
			screenshotAvailable = cmd.Run() == nil
			cancel()
		}
	case "linux":
		screenshotAvailable = pptxCommandAvailable("soffice", "libreoffice") == nil
	default:
		return nil, fmt.Errorf("%w: PPTX screenshots are not supported on %s", ErrUnsupported, runtime.GOOS)
	}
	if !screenshotAvailable {
		return nil, fmt.Errorf("%w: PPTX screenshots are not available", ErrUnsupported)
	}

	screensDir, err := os.MkdirTemp("", "pptx-slides-*")
	if err != nil {
		return nil, err
	}
	defer func() { _ = os.RemoveAll(screensDir) }()

	if err := pptxExportSlideScreenshots(ctx, sourcePath, screensDir); err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(screensDir)
	if err != nil {
		return nil, err
	}

	slides := map[string][]byte{}
	for _, entry := range entries {
		if entry.IsDir() || strings.ToLower(filepath.Ext(entry.Name())) != ".png" {
			continue
		}
		slidePath := filepath.Join(screensDir, entry.Name())
		slideBody, err := os.ReadFile(slidePath)
		if err != nil {
			return nil, err
		}
		slides[filepath.ToSlash(filepath.Join("slides", entry.Name()))] = slideBody
	}
	return slides, nil
}

func pptxExportSlideScreenshots(ctx context.Context, sourcePath, outputDir string) error {
	ctx = contextOrBackground(ctx)
	if err := ctx.Err(); err != nil {
		return err
	}

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
		return pptxExportSlideScreenshotsWindows(ctx, sourcePath, outputDir)
	case "linux":
		return pptxExportSlideScreenshotsLinux(ctx, sourcePath, outputDir)
	default:
		return fmt.Errorf("%w: PPTX screenshots are not supported on %s", ErrUnsupported, runtime.GOOS)
	}
}

func pptxExportSlideScreenshotsWindows(ctx context.Context, sourcePath, outputDir string) error {
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

	powershellPath, err := firstAvailableTool("powershell", "pwsh")
	if err != nil {
		_ = os.Remove(scriptPath)
		return err
	}

	cmdCtx, cancel, timeout := contextWithToolTimeout(ctx, defaultScreenshotToolTimeout)
	defer cancel()
	cmd := exec.CommandContext(
		cmdCtx,
		powershellPath,
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
		return commandRunError(cmdCtx, "powershell", timeout, err, output)
	}
	return nil
}

func pptxExportSlideScreenshotsLinux(ctx context.Context, sourcePath, outputDir string) error {
	command, err := firstAvailableTool("soffice", "libreoffice")
	if err != nil {
		return err
	}

	reader, err := zip.OpenReader(sourcePath)
	if err != nil {
		return err
	}
	defer reader.Close()

	var slideEntries []string
	for _, file := range reader.File {
		if path.Dir(file.Name) == "ppt/slides" &&
			strings.HasPrefix(path.Base(file.Name), "slide") &&
			strings.HasSuffix(strings.ToLower(path.Base(file.Name)), ".xml") {
			slideEntries = append(slideEntries, file.Name)
		}
	}
	sort.Strings(slideEntries)
	slideCount := len(slideEntries)
	if slideCount == 0 {
		return fmt.Errorf("%w: no slides found in PPTX", ErrInvalidInput)
	}

	workDir, err := os.MkdirTemp("", "pptx-slides-*")
	if err != nil {
		return err
	}
	defer func() { _ = os.RemoveAll(workDir) }()

	cmdCtx, cancel, timeout := contextWithToolTimeout(ctx, defaultScreenshotToolTimeout)
	defer cancel()
	cmd := exec.CommandContext(
		cmdCtx,
		command,
		"--headless",
		"--convert-to", "png",
		"--outdir", workDir,
		sourcePath,
	)
	output, runErr := cmd.CombinedOutput()
	if runErr != nil {
		return commandRunError(cmdCtx, filepath.Base(command), timeout, runErr, output)
	}

	entries, err := os.ReadDir(workDir)
	if err != nil {
		return err
	}
	var pngs []string
	for _, entry := range entries {
		if entry.IsDir() || strings.ToLower(filepath.Ext(entry.Name())) != ".png" {
			continue
		}
		pngs = append(pngs, filepath.Join(workDir, entry.Name()))
	}
	sort.Strings(pngs)
	if len(pngs) != slideCount {
		return fmt.Errorf("%w: LibreOffice exported %d PNG slide(s), expected %d", ErrUnsupported, len(pngs), slideCount)
	}

	for i, exportedPath := range pngs {
		targetName := fmt.Sprintf("slide-%03d.png", i+1)
		targetPath := filepath.Join(outputDir, targetName)
		if err := moveFile(exportedPath, targetPath); err != nil {
			return err
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
		return fmt.Errorf("%w: no command configured", ErrToolMissing)
	}
	return fmt.Errorf("%w: %w", ErrToolMissing, lastErr)
}

func moveFile(sourcePath, targetPath string) error {
	if err := os.MkdirAll(filepath.Dir(targetPath), 0o755); err != nil {
		return err
	}
	if err := os.Rename(sourcePath, targetPath); err == nil {
		return nil
	}

	in, err := os.Open(sourcePath)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.Create(targetPath)
	if err != nil {
		return err
	}

	_, copyErr := out.ReadFrom(in)
	syncErr := out.Sync()
	closeErr := out.Close()
	if copyErr != nil {
		return copyErr
	}
	if syncErr != nil {
		return syncErr
	}
	if closeErr != nil {
		return closeErr
	}
	return os.Remove(sourcePath)
}
