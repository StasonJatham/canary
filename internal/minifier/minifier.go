package minifier

import (
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"

	"github.com/tdewolff/minify/v2"
	"github.com/tdewolff/minify/v2/css"
	"github.com/tdewolff/minify/v2/html"
	"github.com/tdewolff/minify/v2/js"
)

// BuildDist creates a dist directory with minified assets from web directory
func BuildDist(sourceDir, distDir string) error {
	log.Printf("Building minified assets from %s to %s...", sourceDir, distDir)

	// Create minifier
	m := minify.New()
	m.AddFunc("text/html", html.Minify)
	m.AddFunc("text/css", css.Minify)
	m.AddFunc("application/javascript", js.Minify)

	// Remove old dist directory
	if err := os.RemoveAll(distDir); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to remove old dist: %w", err)
	}

	// Create new dist directory
	if err := os.MkdirAll(distDir, 0755); err != nil {
		return fmt.Errorf("failed to create dist: %w", err)
	}

	// Walk through source directory
	filesProcessed := 0
	err := filepath.Walk(sourceDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		// Skip directories and backup files
		if info.IsDir() || strings.HasSuffix(path, ".old") {
			return nil
		}

		// Get relative path
		relPath, err := filepath.Rel(sourceDir, path)
		if err != nil {
			return err
		}

		destPath := filepath.Join(distDir, relPath)

		// Create destination directory if needed
		destDir := filepath.Dir(destPath)
		if err := os.MkdirAll(destDir, 0755); err != nil {
			return fmt.Errorf("failed to create dest dir %s: %w", destDir, err)
		}

		// Check if file should be minified
		ext := strings.ToLower(filepath.Ext(path))
		shouldMinify := ext == ".html" || ext == ".css" || ext == ".js"

		if shouldMinify {
			if err := minifyFile(m, path, destPath); err != nil {
				log.Printf("Warning: failed to minify %s: %v (copying original)", relPath, err)
				// Fall back to copying original file
				if err := copyFile(path, destPath); err != nil {
					return fmt.Errorf("failed to copy %s: %w", relPath, err)
				}
			}
		} else {
			// Copy non-minifiable files (images, fonts, etc.)
			if err := copyFile(path, destPath); err != nil {
				return fmt.Errorf("failed to copy %s: %w", relPath, err)
			}
		}

		filesProcessed++
		return nil
	})

	if err != nil {
		return fmt.Errorf("failed to build dist: %w", err)
	}

	// Copy dashboard.html to index.html for convenience
	dashboardPath := filepath.Join(distDir, "dashboard.html")
	indexPath := filepath.Join(distDir, "index.html")
	if _, err := os.Stat(dashboardPath); err == nil {
		if err := copyFile(dashboardPath, indexPath); err != nil {
			log.Printf("Warning: failed to copy dashboard.html to index.html: %v", err)
		}
	}

	log.Printf("✓ Built dist with %d files", filesProcessed)
	return nil
}

// minifyFile minifies a source file and writes it to dest
func minifyFile(m *minify.M, src, dest string) error {
	// Open source file
	srcFile, err := os.Open(src)
	if err != nil {
		return err
	}
	defer srcFile.Close()

	// Create destination file
	destFile, err := os.Create(dest)
	if err != nil {
		return err
	}
	defer destFile.Close()

	// Determine content type
	ext := strings.ToLower(filepath.Ext(src))
	var contentType string
	switch ext {
	case ".html":
		contentType = "text/html"
	case ".css":
		contentType = "text/css"
	case ".js":
		contentType = "application/javascript"
	default:
		return fmt.Errorf("unsupported file type for minification: %s", ext)
	}

	// Minify
	if err := m.Minify(contentType, destFile, srcFile); err != nil {
		return err
	}

	return nil
}

// copyFile copies a file from src to dest
func copyFile(src, dest string) error {
	srcFile, err := os.Open(src)
	if err != nil {
		return err
	}
	defer srcFile.Close()

	destFile, err := os.Create(dest)
	if err != nil {
		return err
	}
	defer destFile.Close()

	if _, err := io.Copy(destFile, srcFile); err != nil {
		return err
	}

	return destFile.Sync()
}
