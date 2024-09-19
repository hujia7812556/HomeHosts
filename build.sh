#!/bin/bash
rootPath=$(cd "$(dirname "$0")";pwd)
go mod tidy
GOOS=darwin GOARCH=arm64 go build -o ${rootPath}/bin/home-hosts