package exporter

import (
	"archive/zip"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// CreateZipArchive creates a ZIP file containing all exported files
// Uses streaming to avoid loading entire files into memory
func CreateZipArchive(outputDir string, files []string, zipPath string) error {
	zipFile, err := os.Create(zipPath)
	if err != nil {
		return fmt.Errorf("creating zip file: %w", err)
	}
	defer zipFile.Close()

	zipWriter := zip.NewWriter(zipFile)
	defer zipWriter.Close()

	for _, filePath := range files {
		if err := addFileToZip(zipWriter, filePath); err != nil {
			return fmt.Errorf("adding %s to zip: %w", filepath.Base(filePath), err)
		}
	}

	return nil
}

// addFileToZip adds a single file to the ZIP archive
func addFileToZip(zipWriter *zip.Writer, filePath string) error {
	file, err := os.Open(filePath)
	if err != nil {
		return err
	}
	defer file.Close()

	info, err := file.Stat()
	if err != nil {
		return err
	}

	header, err := zip.FileInfoHeader(info)
	if err != nil {
		return err
	}

	// Use only the base filename in the archive (flat structure)
	header.Name = filepath.Base(filePath)

	// Use Store method since Parquet files are already ZSTD compressed
	// This avoids double-compression overhead
	header.Method = zip.Store

	writer, err := zipWriter.CreateHeader(header)
	if err != nil {
		return err
	}

	_, err = io.Copy(writer, file)
	return err
}
