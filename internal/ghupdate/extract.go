package ghupdate

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// extract extracts an archive file to the destination directory.
// Supports .zip and .tar.gz files based on the file extension.
func extract(srcPath, destDir string) error {
	if strings.HasSuffix(srcPath, ".tar.gz") {
		return extractTarGz(srcPath, destDir)
	}
	// Default to zip extraction
	return extractZip(srcPath, destDir)
}

// extractTarGz extracts a tar.gz archive to the destination directory.
func extractTarGz(srcPath, destDir string) error {
	src, err := os.Open(srcPath)
	if err != nil {
		return err
	}
	defer src.Close()

	gz, err := gzip.NewReader(src)
	if err != nil {
		return err
	}
	defer gz.Close()

	tr := tar.NewReader(gz)

	// normalize dest path to check later for Tar Slip
	base := filepath.Clean(destDir) + string(os.PathSeparator)

	for {
		header, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}

		target := filepath.Join(destDir, header.Name)
		// Reject path traversal ("../") entries that would escape destDir (Tar Slip).
		if !strings.HasPrefix(target, base) {
			return fmt.Errorf("invalid file path in archive: %s", header.Name)
		}

		switch header.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, 0o755); err != nil {
				return err
			}
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return err
			}
			outFile, err := os.Create(target)
			if err != nil {
				return err
			}
			// Bound decompression to guard against a tar bomb writing an oversized file.
			if _, err := io.Copy(outFile, io.LimitReader(tr, maxArchiveFileBytes)); err != nil {
				outFile.Close()
				return err
			}
			outFile.Close()
		default:
			// Skip symlinks, hardlinks, devices, fifos, etc. — they enable arbitrary
			// file writes/links outside destDir and are never part of a release archive.
			continue
		}
	}

	return nil
}

// maxArchiveFileBytes bounds the size of any single extracted file (defense against a
// malicious/oversized archive). Release binaries are well under this.
const maxArchiveFileBytes = 1 << 30 // 1 GiB

// extractZip extracts the zip archive at "src" to "dest".
//
// Note that only dirs and regular files will be extracted.
// Symbolic links, named pipes, sockets, or any other irregular files
// are skipped because they come with too many edge cases and ambiguities.
func extractZip(src, dest string) error {
	zr, err := zip.OpenReader(src)
	if err != nil {
		return err
	}
	defer zr.Close()

	// normalize dest path to check later for Zip Slip
	dest = filepath.Clean(dest) + string(os.PathSeparator)

	for _, f := range zr.File {
		err := extractFile(f, dest)
		if err != nil {
			return err
		}
	}

	return nil
}

// extractFile extracts the provided zipFile into "basePath/zipFileName" path,
// creating all the necessary path directories.
func extractFile(zipFile *zip.File, basePath string) error {
	path := filepath.Join(basePath, zipFile.Name)

	// check for Zip Slip
	if !strings.HasPrefix(path, basePath) {
		return fmt.Errorf("invalid file path: %s", path)
	}

	r, err := zipFile.Open()
	if err != nil {
		return err
	}
	defer r.Close()

	// allow only dirs or regular files
	if zipFile.FileInfo().IsDir() {
		if err := os.MkdirAll(path, os.ModePerm); err != nil {
			return err
		}
	} else if zipFile.FileInfo().Mode().IsRegular() {
		// ensure that the file path directories are created
		if err := os.MkdirAll(filepath.Dir(path), os.ModePerm); err != nil {
			return err
		}

		f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, zipFile.Mode())
		if err != nil {
			return err
		}
		defer f.Close()

		_, err = io.Copy(f, r)
		if err != nil {
			return err
		}
	}

	return nil
}
