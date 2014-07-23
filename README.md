# gosync

Concurrently sync files and directories to/from S3.

Fork of [brettweavnet](https://github.com/brettweavnet/gosync)'s repo.

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

Or specify them later as command line arguments:

    gosync --secretkey yyy --accesskey xxx SOURCE TARGET

# Usage

    gosync OPTIONS SOURCE TARGET

## Syncing from local directory to S3

Under refactoring as of version `0.1.0`

## Syncing from S3 to local directory

General download.

    gosync s3://bucket local_dir
    
    
Download only files from a bucket matching a prefix.

    gosync s3://bucket/subdir local_dir
    
Download with a wildcard.

    gosync s3://bucket/subdir*.json local_dir
    
Specify number of concurrent goroutines for downloading files (default is 20):

    gosync --concurrent 30 s3://bucket local_dir
    
_Full sync._ Delete local files that don't appear on S3.

    gosync --full s3://bucket local_dir
    
Full sync only applies to results matching a query. So the following command won't update or delete any local files outside of `local_dir/subdir` which don't match `*.json`

    gosync --full s3://bucket/subdir*.json local_dir
    
_Continue after interruption._ `gosync` compares each S3 key's MD5 hash against local files to see if it's already downloaded them. You can continue after an interruption without having to re-download the entire bucket. Subject to S3 MD5 issues (read more below).

    $ gosync s3://bucket/subdir local_dir
    [     26486 bytes] Downloaded file: subdir/mypicture.png
    $ ls local_dir/subdir
    mypicture.png
    $ gosync s3://bucket/subdir local_dir
    File already downloaded: subdir/mypicture.png


## Help

For full list of options and commands:

    gosync -h

## Contributing

For feature requests and issues, please create a github issue.

## S3 MD5 issues

MD5 hashes associated with S3 keys are computed differently for large files which require multipart uploads. Therefore, the [ETag attribute](https://github.com/mitchellh/goamz/blob/master/s3/s3.go#L377) of each key doesn't represent the value resulting from hashing the file, an issue which has already been brough up on the [AWS Dev Forums](https://forums.aws.amazon.com/thread.jspa?messageID=203510).

This feature prevents `gosync` from checking if large files in a local directory match a S3 key. It also makes validating a file after downloading it from S3 harder.

Comments and suggestion on this issue are very much welcome!
