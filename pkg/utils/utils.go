// Package utils provides shared utilities for the audioTransfer tool.
package utils

import (
	"log"
	"os"
	"path/filepath"
	"strings"
)

// Audio extensions recognized.
var audioExts = map[string]bool{
	".m4b": true, ".m4a": true, ".mp3": true, ".aax": true,
	".ogg": true, ".wma": true, ".flac": true, ".wav": true,
	".aac": true, ".m4p": true,
}

// Cover image extensions.
var coverExts = map[string]bool{
	".jpg": true, ".jpeg": true, ".png": true, ".gif": true,
	".bmp": true, ".webp": true,
}

// Meta/skip extensions.
var metaExts = map[string]bool{
	".cue": true, ".nfo": true, ".json": true, ".epub": true,
}

// IsAudio checks if a path has an audio extension.
func IsAudio(path string) bool {
	return audioExts[strings.ToLower(filepath.Ext(path))]
}

// IsCover checks if a path has a cover image extension.
func IsCover(path string) bool {
	return coverExts[strings.ToLower(filepath.Ext(path))]
}

// CoverExts returns the set of cover image extensions.
func CoverExts() map[string]bool {
	return coverExts
}

// IsMeta checks if a path has a metadata extension.
func IsMeta(path string) bool {
	return metaExts[strings.ToLower(filepath.Ext(path))]
}

// TempDir creates a temporary directory and returns a cleanup function.
func TempDir(prefix string) (string, func(), error) {
	dir, err := os.MkdirTemp("", prefix)
	if err != nil {
		return "", nil, err
	}
	return dir, func() { os.RemoveAll(dir) }, nil
}

// Loggers for different severity levels.
var (
	Debug = log.New(os.Stderr, "[DEBUG] ", log.Ltime)
	Info  = log.New(os.Stderr, "[INFO]  ", log.Ltime)
	Warn  = log.New(os.Stderr, "[WARN]  ", log.Ltime)
	Error = log.New(os.Stderr, "[ERROR] ", log.Ltime)
)

// MustExpand expands ~/ to the user's home directory.
func MustExpand(path string) string {
	if strings.HasPrefix(path, "~/") {
		home, _ := os.UserHomeDir()
		return filepath.Join(home, path[2:])
	}
	return path
}
