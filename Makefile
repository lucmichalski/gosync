.PHONY: all build install deps fmt release

all: deps build

build: main.go gosync/sync.go jobs/jobs.go
	go build ./...

install: main.go gosync/sync.go jobs/jobs.go
	go install

deps: main.go gosync/sync.go jobs/jobs.go
	go get -d -v ./...

fmt: main.go gosync/sync.go jobs/jobs.go 
	go fmt ./...

release:
	rm -rf build
	gox -output="build/{{.OS}}_{{.Arch}}/{{.Dir}}"
	gosync build s3://gosync

