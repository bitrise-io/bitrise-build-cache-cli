package xcode

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/dustin/go-humanize"
	"github.com/pkg/xattr"

	"github.com/bitrise-io/go-utils/v2/log"
)

type FileGroupInfo struct {
	Files       []*FileInfo      `json:"files"`
	Directories []*DirectoryInfo `json:"directories"`
}

type DirectoryInfo struct {
	Path    string    `json:"path"`
	ModTime time.Time `json:"modTime"`
}

type FileInfo struct {
	Path       string            `json:"path"`
	Size       int64             `json:"size"`
	Hash       string            `json:"hash"`
	ModTime    time.Time         `json:"modTime"`
	Mode       os.FileMode       `json:"mode"`
	Attributes map[string]string `json:"attributes,omitempty"`
}

type fileGroupInfoCollector struct {
	Files           []*FileInfo
	Dirs            []*DirectoryInfo
	LargestFileSize int64
	mu              sync.Mutex
}

func (mc *fileGroupInfoCollector) AddFile(fileInfo *FileInfo) {
	mc.mu.Lock()
	defer mc.mu.Unlock()
	mc.Files = append(mc.Files, fileInfo)
	if fileInfo.Size > mc.LargestFileSize {
		mc.LargestFileSize = fileInfo.Size
	}
}

func (mc *fileGroupInfoCollector) AddDir(dirInfo *DirectoryInfo) {
	mc.mu.Lock()
	defer mc.mu.Unlock()
	mc.Dirs = append(mc.Dirs, dirInfo)
}

func collectFileGroupInfo(cacheDirPath string, rootDir string, collectAttributes, followSymlinks bool, logger log.Logger) (FileGroupInfo, error) {
	var dd FileGroupInfo

	fgi := fileGroupInfoCollector{
		Files: make([]*FileInfo, 0),
		Dirs:  make([]*DirectoryInfo, 0),
	}
	var wg sync.WaitGroup
	semaphore := make(chan struct{}, 10) // Limit parallelization

	err := filepath.WalkDir(cacheDirPath, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		wg.Add(1)
		semaphore <- struct{}{} // Block if there are too many goroutines are running

		go func(d fs.DirEntry) {
			defer wg.Done()
			defer func() { <-semaphore }() // Release a slot in the semaphore

			inf, err := d.Info()
			if err != nil {
				logger.Errorf("get file info: %v", err)

				return
			}

			if err := collectFileMetadata(path, inf, inf.IsDir(), &fgi, collectAttributes, followSymlinks, rootDir, logger); err != nil {
				logger.Errorf("Failed to collect metadata: %s", err)
			}
		}(d)

		return nil
	})

	wg.Wait()

	if err != nil {
		return FileGroupInfo{}, fmt.Errorf("walk dir: %w", err)
	}

	dd.Files = fgi.Files
	dd.Directories = fgi.Dirs

	logger.Infof("(i) Collected %d files and %d directories ", len(dd.Files), len(dd.Directories))
	logger.Debugf("(i) Largest processed file size: %s", humanize.Bytes(uint64(fgi.LargestFileSize)))

	return dd, nil
}

// nolint:wrapcheck
func collectSymlink(path string, fgi *fileGroupInfoCollector, followSymlinks bool, rootDir string, logger log.Logger) error {
	if !followSymlinks {
		logger.Debugf("Skipping symbolic link: %s", path)

		return nil
	}

	target, err := resolveSymlink(path)
	if err != nil {
		return fmt.Errorf("resolve symlink %s: %w", path, err)
	}
	logger.Debugf("Resolved symlink %s to %s", path, target)

	stat, err := os.Stat(target)
	if err != nil {
		return fmt.Errorf("stat target: %w", err)
	}
	if !stat.IsDir() {
		return collectFileMetadata(target, stat, false, fgi, false, followSymlinks, rootDir, logger)
	}

	logger.Debugf("Symlink target is a directory, walking it: %s", target)
	// Recursively walk the target directory, as it will not be included in this walk
	return filepath.WalkDir(target, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return fmt.Errorf("walk target dir: %w", err)
		}

		inf, err := d.Info()
		if err != nil {
			return fmt.Errorf("get file info: %w", err)
		}

		return collectFileMetadata(path, inf, inf.IsDir(), fgi, false, followSymlinks, rootDir, logger)
	})
}

func collectFileMetadata(path string, fileInfo fs.FileInfo, isDirectory bool, fgi *fileGroupInfoCollector, collectAttributes, followSymlinks bool, rootDir string, logger log.Logger) error {
	absPath, err := filepath.Abs(path)
	if err != nil {
		return fmt.Errorf("get absolute path: %w", err)
	}
	if isDirectory {
		fgi.AddDir(&DirectoryInfo{
			Path:    absPath,
			ModTime: fileInfo.ModTime(),
		})

		return nil
	}

	if fileInfo.Mode()&os.ModeSymlink != 0 {
		return collectSymlink(path, fgi, followSymlinks, rootDir, logger)
	}

	file, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("open file: %w", err)
	}
	defer file.Close()

	hasher := sha256.New()
	if _, err := io.Copy(hasher, file); err != nil {
		return fmt.Errorf("hash copy file content: %w", err)
	}
	hash := hex.EncodeToString(hasher.Sum(nil))

	savedPath := absPath
	if rootDir != "" {
		relPath, err := filepath.Rel(rootDir, path)
		if err != nil {
			return fmt.Errorf("get relative path: %w", err)
		}
		savedPath = relPath
	}

	var attrs map[string]string
	if collectAttributes {
		attrs, err = getAttributes(path)
		if err != nil {
			return fmt.Errorf("getting attributes: %w", err)
		}
	}

	fgi.AddFile(&FileInfo{
		Path:       savedPath,
		Size:       fileInfo.Size(),
		Hash:       hash,
		ModTime:    fileInfo.ModTime(),
		Mode:       fileInfo.Mode(),
		Attributes: attrs,
	})

	return nil
}

func getAttributes(path string) (map[string]string, error) {
	attributes := make(map[string]string)
	attrNames, err := xattr.List(path)
	if err != nil {
		return nil, fmt.Errorf("list attributes: %w", err)
	}

	for _, attr := range attrNames {
		value, err := xattr.Get(path, attr)
		if err != nil {
			return nil, fmt.Errorf("xattr get: %w", err)
		}
		attributes[attr] = string(value)
	}

	return attributes, nil
}

func setAttributes(path string, attributes map[string]string) error {
	for attr, value := range attributes {
		if err := xattr.Set(path, attr, []byte(value)); err != nil {
			return fmt.Errorf("xattr set: %w", err)
		}
	}

	return nil
}

func restoreFileInfo(fi FileInfo, rootDir string, logger log.Logger) bool {
	var path string
	if filepath.IsAbs(fi.Path) {
		path = fi.Path
	} else {
		path = filepath.Join(rootDir, fi.Path)
	}

	// Skip if file doesn't exist
	if _, err := os.Stat(path); os.IsNotExist(err) {
		logger.Debugf("File %s doesn't exist", fi.Path)

		return false
	}

	h, err := ChecksumOfFile(path)
	if err != nil {
		logger.Infof("Error hashing file %s: %v", fi.Path, err)

		return false
	}

	if h != fi.Hash {
		return false
	}

	if err := os.Chtimes(path, fi.ModTime, fi.ModTime); err != nil {
		logger.Debugf("Error setting modification time for %s: %v", fi.Path, err)

		return false
	}

	if err = os.Chmod(fi.Path, fi.Mode); err != nil {
		logger.Debugf("Error setting file mode time for %s: %v", fi.Path, err)

		return false
	}

	if len(fi.Attributes) > 0 {
		err = setAttributes(fi.Path, fi.Attributes)
		if err != nil {
			logger.Debugf("Error setting file attributes for %s: %v", fi.Path, err)

			return false
		}
	}

	return true
}

func restoreDirectoryInfo(dir DirectoryInfo, rootDir string) error {
	var path string
	if filepath.IsAbs(dir.Path) {
		path = dir.Path
	} else {
		path = filepath.Join(rootDir, dir.Path)
	}

	if err := os.MkdirAll(path, os.ModePerm); err != nil {
		return fmt.Errorf("create directory: %w", err)
	}

	if err := os.Chtimes(path, dir.ModTime, dir.ModTime); err != nil {
		return fmt.Errorf("set directory mod time: %w", err)
	}

	return nil
}

func resolveSymlink(path string) (string, error) {
	seen := make(map[string]struct{})
	current := path

	for {
		if _, visited := seen[current]; visited {
			return "", fmt.Errorf("circular symlink detected at %s", current)
		}
		seen[current] = struct{}{}

		target, err := os.Readlink(current)
		if err != nil {
			return "", fmt.Errorf("read symlink: %w", err)
		}

		if !filepath.IsAbs(target) {
			target = filepath.Join(filepath.Dir(current), target)
		}

		info, err := os.Lstat(target)
		if err != nil {
			return "", fmt.Errorf("lstat target: %w", err)
		}

		if info.Mode()&os.ModeSymlink == 0 {
			return target, nil
		}

		current = target
	}
}
