package xcode

import (
	"github.com/bitrise-io/go-utils/v2/log"
	"sync"
)

type MetadataCollector struct {
	Files           []*FileInfo
	Dirs            []*DirectoryInfo
	LargestFileSize int64
	mu              sync.Mutex
	logger          log.Logger
}

func NewMetadataCollector(logger log.Logger) *MetadataCollector {
	return &MetadataCollector{
		Files:  make([]*FileInfo, 0),
		Dirs:   make([]*DirectoryInfo, 0),
		logger: logger,
	}
}

func (mc *MetadataCollector) AddFile(fileInfo *FileInfo) {
	mc.mu.Lock()
	defer mc.mu.Unlock()
	mc.Files = append(mc.Files, fileInfo)
	if fileInfo.Size > mc.LargestFileSize {
		mc.LargestFileSize = fileInfo.Size
	}
}

func (mc *MetadataCollector) AddDir(dirInfo *DirectoryInfo) {
	mc.mu.Lock()
	defer mc.mu.Unlock()
	mc.Dirs = append(mc.Dirs, dirInfo)
}
