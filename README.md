# gosync

Concurrently sync files and directories to/from S3.

Fork of [brettweavnet](https://github.com/brettweavnet/gosync)'s repo.

Attempted improvments:
* Don't wait to read entire list of keys from bucket before downloading.
* Stream everything. Never load entire file into memory.
* Always check actions against md5 hash.
* Selective downloads/uploads. (example: `s3://mybucket/*.json`)

# Installation

Ensure you have Go 1.2 or greater installed and your GOPATH is set.

Clone the repo:

    go get github.com/yhat/gosync

Change into the gosync directory and run make:

    cd $GOPATH/src/github.com/yhat/gosync/
    make

# Setup

Set environment variables:

    AWS_SECRET_ACCESS_KEY=yyy
    AWS_ACCESS_KEY_ID=xxx

Or specify them later as command line arguments.

# Usage

    gosync OPTIONS SOURCE TARGET

## Syncing from local directory to S3

    gosync /files s3://bucket/files

## Syncing from S3 to local directory

    gosync s3://bucket/files /files

## Help

For full list of options and commands:

    gosync -h

# Contributing

1. Fork it
2. Create your feature branch (git checkout -b my-new-feature)
3. Commit your changes (git commit -am 'Add some feature')
4. Push to the branch (git push origin my-new-feature)
5. Create new Pull Request
