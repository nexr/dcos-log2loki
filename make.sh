#!/usr/bin/env bash

version=$(grep -F "VERSION = " version.go | cut -d\" -f2)

echo "Cross compiling dcos-log2loki version: $version"

echo "Compiling for linux-amd64..."
env GOOS=linux GOARCH=amd64 go build -ldflags "-s" -o "build/dcos-log2loki-linux-amd64-${version}" .

echo "Compiling for windows-amd64..."
env GOOS=windows GOARCH=amd64 go build -ldflags "-s" -o "build/dcos-log2loki-windows-amd64-${version}.exe" .
