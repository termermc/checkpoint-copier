package main

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"syscall"
)

const AppVersion = "v0.1.0"

type SimpleDirEntry struct {
	RelativePath string      `json:"relative_path"`
	FileSize     int64       `json:"file_size"`
	IsDir        bool        `json:"is_dir"`
	ModTimeUnix  int64       `json:"mod_time_unix"`
	Mode         os.FileMode `json:"mode"`
}

// DirSnapshot is a map where the key is the relative path and the value is the SimpleDirEntry.
type DirSnapshot map[string]SimpleDirEntry

func GetInputPathSnapshotFilePath(inputPath string) (string, error) {
	var err error
	inputPath, err = filepath.Abs(inputPath)
	if err != nil {
		return "", err
	}

	snapshotFilename := ".snapshot-" + strings.ReplaceAll(inputPath, "/", "__") + ".jsonl"
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}

	return filepath.Join(homeDir, snapshotFilename), nil
}

// OpenSnapshotIfExists will open the snapshot file for reading if it exists.
// The reader will be nil if it does not exist.
// It is the responsibility of the caller to close the file.
func OpenSnapshotIfExists(inputPath string) (io.ReadCloser, error) {
	snapPath, err := GetInputPathSnapshotFilePath(inputPath)
	if err != nil {
		return nil, err
	}
	f, err := os.OpenFile(snapPath, os.O_RDONLY, 0777)
	if err != nil {
		if errors.Is(err, os.ErrExist) || errors.Is(err, syscall.ENOENT) {
			return nil, nil
		} else {
			return nil, err
		}
	}

	println("Opened snapshot at " + snapPath)

	return f, nil
}

// ParseSnapshotCallback parses a snapshot line-by-line, calling cb for each parsed entry.
// The function will only return after all entries are parsed and corresponding callbacks are completed.
func ParseSnapshotCallback(reader io.Reader, cb func(entry SimpleDirEntry)) error {
	scan := bufio.NewScanner(reader)
	snap := make(DirSnapshot)

	for scan.Scan() {
		if err := scan.Err(); err != nil {
			return err
		}

		raw := scan.Bytes()
		var simple SimpleDirEntry
		err := json.Unmarshal(raw, &simple)
		if err != nil {
			return err
		}

		snap[simple.RelativePath] = simple
		cb(simple)
	}

	fmt.Printf("Parsed %d entries from snapshot\n", len(snap))

	return scan.Err()
}

// ParseSnapshot parses a snapshot file's contents into a DirSnapshot.
// It does not close the reader.
// If onEntry is not nil, it will be called for each parsed entry.
func ParseSnapshot(reader io.Reader) (DirSnapshot, error) {
	snap := make(DirSnapshot)
	return snap, ParseSnapshotCallback(reader, func(entry SimpleDirEntry) {
		snap[entry.RelativePath] = entry
	})
}

// SnapshotDir creates a snapshot of the input path and writes a snapshot file for it.
// Returns the snapshot once done.
func SnapshotDir(inputPath string) (DirSnapshot, error) {
	snap := make(DirSnapshot)

	var err error
	inputPath, err = filepath.Abs(inputPath)
	if err != nil {
		return nil, err
	}

	snapshotPath, err := GetInputPathSnapshotFilePath(inputPath)
	if err != nil {
		return nil, err
	}

	snapFile, err := os.OpenFile(snapshotPath, os.O_CREATE|os.O_WRONLY, 0777)
	if err != nil {
		panic(err)
	}
	defer func() {
		_ = snapFile.Close()
	}()

	err = filepath.WalkDir(inputPath, func(path string, entry fs.DirEntry, err error) error {
		path = "." + path[len(inputPath):]

		if err != nil {
			_, _ = fmt.Fprintf(os.Stderr, "cannot walk path \"%s\" because err: %v\n", path, err)
			return nil
		}

		info, err := entry.Info()
		if err != nil {
			_, _ = fmt.Fprintf(os.Stderr, "cannot stat path \"%s\" because err: %v\n", path, err)
			return nil
		}

		isDir := info.IsDir()
		if isDir {
			println("Walking dir " + path)
		}

		simple := SimpleDirEntry{
			RelativePath: path,
			FileSize:     info.Size(),
			IsDir:        isDir,
			ModTimeUnix:  info.ModTime().Unix(),
			Mode:         info.Mode(),
		}
		jsonEnc := json.NewEncoder(snapFile)
		err = jsonEnc.Encode(simple)
		if err != nil {
			return err
		}

		snap[path] = simple

		return nil
	})

	fmt.Printf("Snapshotted %d entries\n", len(snap))

	return snap, err
}

// ReadOrCreateSnapshot reads the snapshot for the specified input path, or creates a new snapshot if none exists.
func ReadOrCreateSnapshot(inputPath string) (DirSnapshot, error) {
	snapFile, err := OpenSnapshotIfExists(inputPath)
	if err != nil {
		return nil, err
	}

	if snapFile == nil {
		return SnapshotDir(inputPath)
	} else {
		defer func() {
			_ = snapFile.Close()
		}()

		return ParseSnapshot(snapFile)
	}
}

func usageDie() {
	_, _ = fmt.Fprintf(os.Stderr, "checkpoint-copier %s\nUsage: %s <server|client|snapshot> <directory>\n", AppVersion, os.Args[0])
	os.Exit(1)
}

func main() {
	args := os.Args[1:]
	if len(args) < 2 {
		usageDie()
	}

	mode := args[0]
	inputPath := args[1]

	if stat, err := os.Stat(inputPath); err != nil {
		panic(err)
	} else if !stat.IsDir() {
		panic("Path is not a directory")
	}

	if mode == "server" {
		serverMain(inputPath)
	} else if mode == "client" {
		clientMain(inputPath)
	} else if mode == "snapshot" {
		println("Creating up-to-date snapshot...")
		_, err := SnapshotDir(inputPath)
		if err != nil {
			panic(err)
		}
	} else {
		usageDie()
	}
}
