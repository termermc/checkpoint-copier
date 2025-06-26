# checkpoint-copier

`checkpoint-copier` is a tool like rsync that saves a snapshot of a directory's contents on a server and a client and restores from it concurrently.

It checks last modified timestamps and file existence to find out which files to copy to the client from the server.

Subsequent launches read from the snapshot file stored at `~/.snapshot-*.jsonl`. New snapshots will not be created unless it is deleted or a manual snapshot is requested.

This is a niche tool that you should care about if:
 - It takes a long time to traverse a directory you need to copy (hundreds of thousands of files in a network attached drive, for example)
 - Concurrent downloading is beneficial
 - You trust last modified timestamps on the source and destination directories

# Download & Build

See the releases tab to download binaries.

Otherwise, run:

```bash
go build -o checkpoint-copier github.com/termermc/checkpoint-copier/cmd/app 
```

# Usage

Besides the below sections, running `checkpoint-copier snapshot <directory>` will re-snapshot the specified directory.

## Server (Source)

Run:

```bash
checkpoint-copier server <directory>
```

This will create or load a snapshot of the directory and then listen on `127.0.0.1:6655`.

To override the listen host or port, use the env vars `SERVER_ADDR` and `SERVER_PORT`.

## Client (Destination)

First, you should create an SSH tunnel to the server as the program listens on `127.0.0.1` by default and does not encrypt traffic:

```shell
ssh -L 6655:localhost:6655 <user>@<server>
```

This will tunnel the server's `127.0.0.1:6655` to the client's `127.0.0.1:6655`.

Start the client with the following:

```bash
checkpoint-copier client <directory>
```

This will create or load a snapshot of the directory and try to connect to `127.0.0.1:6655`.

To override the server host or port, use the env vars `SERVER_ADDR` and `SERVER_PORT`.

Once connected to the server, it will download the server's snapshot and diff it with the local one, then begin downloading missing or newer files.

By default, 4 concurrent connections will be used for downloads. To override, set the env var `DL_COUNT` to the desired count.

If the download is interrupted or closed, it is beneficial to re-snapshot before running the client again, otherwise the files downloaded from the last session will not be accounted for.
