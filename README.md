# gosync

Concurrently sync files and directories to/from S3.

Fork of [brettweavnet](https://github.com/brettweavnet/gosync)'s repo.

# Installation

Install from source:

    go get -u github.com/yhat/gosync

Donwload binary (compiled with [gox](https://github.com/mitchellh/gox)):

* [darwin_386](https://s3.amazonaws.com/gosync/darwin_386/gosync)
* [darwin_amd64](https://s3.amazonaws.com/gosync/darwin_amd64/gosync)
* [freebsd_386](https://s3.amazonaws.com/gosync/freebsd_386/gosync)
* [freebsd_amd64](https://s3.amazonaws.com/gosync/freebsd_amd64/gosync)
* [freebsd_arm](https://s3.amazonaws.com/gosync/freebsd_arm/gosync)
* [linux_386](https://s3.amazonaws.com/gosync/linux_386/gosync)
* [linux_amd64](https://s3.amazonaws.com/gosync/linux_amd64/gosync)
* [linux_arm](https://s3.amazonaws.com/gosync/linux_arm/gosync)
* [netbsd_386](https://s3.amazonaws.com/gosync/netbsd_386/gosync)
* [netbsd_amd64](https://s3.amazonaws.com/gosync/netbsd_amd64/gosync)
* [netbsd_arm](https://s3.amazonaws.com/gosync/netbsd_arm/gosync)
* [openbsd_386](https://s3.amazonaws.com/gosync/openbsd_386/gosync)
* [openbsd_amd64](https://s3.amazonaws.com/gosync/openbsd_amd64/gosync)
* [plan9_386](https://s3.amazonaws.com/gosync/plan9_386/gosync)
* [windows_386](https://s3.amazonaws.com/gosync/windows_386/gosync.exe)
* [windows_amd64](https://s3.amazonaws.com/gosync/windows_amd64/gosync.exe)

# Setup

Set environment variables:

    AWS_SECRET_ACCESS_KEY=yyy
    AWS_ACCESS_KEY_ID=xxx

Or specify them later as command line arguments:

    gosync --secretkey yyy --accesskey xxx SOURCE TARGET

# Usage

    gosync OPTIONS SOURCE TARGET

## Syncing from local directory to S3

General upload.

    gosync local_dir s3://bucket
    
Upload a sub directory to a bucket. This example will sync all the files in `subdir` as keys in `bucket`.

    gosync local_dir/subdir s3://bucket

Upload with a prefix. This example will prepend all resuling keys with `prefix` durning the sync.

    gosync local_dir s3://bucket/prefix
    

Specify number of concurrent goroutines for uploading files (default is 20):

    gosync --concurrent 30 local_dir s3://bucket
    
_Upload with a wildcard._ Only upload files from a local directory which match an extension.

    gosync local_dir*.json s3://bucket
    
_Full sync._ Delete S3 keys that don't appear in local directory.

    gosync --full local_dir s3://bucket

Full sync only applies to results matching a query. So the following command won't update or delete any s3 keys without the prefix of `subdir` which don't match `*.json`

    gosync --full local_dir*.json s3://bucket/subdir

_Continue after interruption._ `gosync` compares each local file's MD5 hash against its corresponding S3 key's hash to see if it's already uploaded it. You can continue after an interruption without having to re-upload the entire local directory.

    $ gosync local_dir s3://bucket
    [     26486 bytes] Uploaded file: local_dir/mypicture.png
    $ gosync local_dir s3://bucket
    File already uploaded: local_dir/mypicture.png

## Syncing from S3 to local directory

General download.

    gosync s3://bucket local_dir
    
    
Download only files from a bucket matching a prefix.

    gosync s3://bucket/subdir local_dir
    
Specify number of concurrent goroutines for downloading files (default is 20):

    gosync --concurrent 30 s3://bucket local_dir
    
_Download with a wildcard._ Only download keys from a bucket which match an extension.

    gosync s3://bucket/subdir*.json local_dir

    
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

For full list of options and commands run `gosync -h`:

```
$ gosync -h
NAME:
   gosync - gosync OPTIONS SOURCE TARGET

USAGE:
   gosync [global options] command [command options] [arguments...]

VERSION:
   0.1.4

COMMANDS:
   help, h	Shows a list of commands or help for one command

GLOBAL OPTIONS:
   --concurrent, -c '20'	number of concurrent transfers
   --log-level, -l 'info'   log level
   --accesskey, -a          AWS access key
   --secretkey, -s 		    AWS secret key
   --full, -f			    delete existing files/keys in TARGET which do not appear in SOURCE
   --region, -r 'us-east-1'	Aws region
   --help, -h			    show help
   --version, -v		    print the version
```

## Contributing

For feature requests and issues, please create a github issue.

## S3 MD5 issues

MD5 hashes associated with S3 keys are computed differently for large files which require multipart uploads. Therefore, the [ETag attribute](https://github.com/mitchellh/goamz/blob/master/s3/s3.go#L377) of each key doesn't represent the value resulting from hashing the file, an issue which has already been brough up on the [AWS Dev Forums](https://forums.aws.amazon.com/thread.jspa?messageID=203510).

This feature prevents `gosync` from checking if large files in a local directory match a S3 key. It also makes validating a file after downloading it from S3 harder.

Comments and suggestion on this issue are very much welcome!
