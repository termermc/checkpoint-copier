package main

import (
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
)

// Client entrypoint.
// inputPath is presumed to exist and be a directory.
func clientMain(inputPath string) {
	var dlCountStr string
	if dlCountStr = os.Getenv("DL_COUNT"); dlCountStr == "" {
		dlCountStr = "4"
		fmt.Printf("Using default download concurrency count \"%s\". Use env var DL_COUNT to override.\n", dlCountStr)
	}
	dlCount, err := strconv.ParseUint(dlCountStr, 10, 8)
	if err != nil {
		panic(err)
	}
	if dlCount < 1 {
		panic("DL_COUNT must be at least 1")
	}

	var serverAddr string
	if serverAddr = os.Getenv("SERVER_ADDR"); serverAddr == "" {
		serverAddr = "127.0.0.1"
		fmt.Printf("Using default address \"%s\". Use env var SERVER_ADDR to override.\n", serverAddr)
	}
	var portStr string
	if portStr = os.Getenv("SERVER_PORT"); portStr == "" {
		portStr = "6655"
		fmt.Printf("Using default port \"%s\". Use env var SERVER_PORT to override.\n", portStr)
	}

	serverPort, err := strconv.ParseUint(portStr, 10, 16)
	if err != nil {
		panic(err)
	}

	clientSnap, err := ReadOrCreateSnapshot(inputPath)
	if err != nil {
		panic(err)
	}

	serverBase, _ := url.Parse("http://localhost:8080")

	toDl := make(chan SimpleDirEntry, 100_000_000)

	go func() {
		println("Connecting to server to get its snapshot...")
		if strings.Contains(serverAddr, ":") {
			serverBase.Host = fmt.Sprintf("[%s]:%d", serverAddr, serverPort)
		} else {
			serverBase.Host = fmt.Sprintf("%s:%d", serverAddr, serverPort)
		}

		httpClient := http.DefaultClient
		serverSnapRes, err := httpClient.Get(fmt.Sprintf("http://%s/snapshot.jsonl", serverBase.Host))
		if err != nil {
			panic(err)
		}
		if serverSnapRes.StatusCode != 200 {
			panic(fmt.Sprintf("server returned status %d when requesting its snapshot", serverSnapRes.StatusCode))
		}

		// Read server snapshot and find files that don't exist locally
		gotAny := false
		err = ParseSnapshotCallback(serverSnapRes.Body, func(entry SimpleDirEntry) {
			gotAny = true

			local, has := clientSnap[entry.RelativePath]
			if !has || local.ModTimeUnix < entry.ModTimeUnix {
				if entry.IsDir {
					clientPath := filepath.Join(inputPath, entry.RelativePath)
					err := os.MkdirAll(clientPath, entry.Mode)
					if err != nil {
						panic(fmt.Sprintf("failed to mkdir \"%s\": %v", clientPath, err))
					}
				} else {
					toDl <- entry
				}
			}
		})
		_ = serverSnapRes.Body.Close()
		if err != nil {
			panic(err)
		}
		if !gotAny {
			panic("Server returned empty snapshot")
		}

		close(toDl)
	}()

	var completeCount int64
	var failedCount int64

	httpChan := make(chan *http.Client, dlCount)
	for i := uint64(0); i < dlCount; i++ {
		httpChan <- &http.Client{}
	}

	for entry := range toDl {
		client := <-httpChan
		go func() {
			var err error

			defer func() {
				httpChan <- client
			}()

			destPath := filepath.Join(inputPath, entry.RelativePath)
			destFile, err := os.OpenFile(destPath, os.O_CREATE|os.O_WRONLY, entry.Mode)
			if err != nil {
				_, _ = fmt.Fprintf(os.Stderr, "failed to open dest file \"%s\" for writing: %v\n", destPath, err)
				atomic.AddInt64(&failedCount, 1)
				return
			}
			defer func() {
				_ = destFile.Close()
			}()

			fmt.Printf("DL: %s\n", entry.RelativePath)

			//goland:noinspection HttpUrlsUsage
			reqUrl, _ := url.Parse(fmt.Sprintf("http://%s/download?path=%s", serverBase.Host, url.QueryEscape(entry.RelativePath)))
			fileRes, err := client.Do(&http.Request{
				Proto:      "HTTP/1.1",
				ProtoMajor: 1,
				ProtoMinor: 1,
				Method:     http.MethodGet,
				URL:        reqUrl,
				Close:      false,
			})
			if err != nil {
				_, _ = fmt.Fprintf(os.Stderr, "request for file %s to server failed: %v\n", entry.RelativePath, err)
				atomic.AddInt64(&failedCount, 1)
				return
			}
			defer func() {
				_ = fileRes.Body.Close()
			}()
			fileLenStr := fileRes.Header.Get("Content-Length")
			fileLen, err := strconv.ParseInt(fileLenStr, 10, 64)
			if fileLen != entry.FileSize {
				_, _ = fmt.Fprintf(os.Stderr, "expected to get file of size %d but server sent %d, discarding", entry.FileSize, fileLen)
				atomic.AddInt64(&failedCount, 1)

				// Delete destination file
				defer func() {
					_ = os.Remove(destPath)
				}()

				return
			}

			_, err = io.Copy(destFile, fileRes.Body)
			if err != nil {
				_, _ = fmt.Fprintf(os.Stderr, "failed to write request body for %s to %s: %v\n", entry.RelativePath, destPath, err)
				atomic.AddInt64(&failedCount, 1)

				// Delete destination file
				defer func() {
					_ = os.Remove(destPath)
				}()
				return
			}

			atomic.AddInt64(&completeCount, 1)
		}()
	}

	fmt.Printf("Completed %d, failed %d\n", completeCount, failedCount)
	println("Retaking snapshot...")
	if _, err := SnapshotDir(inputPath); err != nil {
		panic(err)
	}
	println("Done")
}
