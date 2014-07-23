package sync

import (
	"crypto/md5"
	"errors"
	"fmt"
	"github.com/mitchellh/goamz/aws"
	"github.com/mitchellh/goamz/s3"
	"io"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

type JobType int

const (
	UPLOAD JobType = iota
	DOWNLOAD
)

type Syncer struct {
	S3Query    string
	Localdir   string
	Concurrent int
	Auth       aws.Auth
	JobTypes   JobType
	NTries     int
	Full       bool
}

func (s *Syncer) Run() {
	bucketName, prefix := SplitQuery(s.S3Query)
	fmt.Printf("Bucket name: %s\n", bucketName)
	fmt.Printf("Prefix name: %s\n", prefix)
	bucket, err := FindBucket(bucketName, s.Auth)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Could not find bucket: %s\n",
			bucketName)
		os.Exit(2)
	}
	fmt.Printf("Looking for keys in: %s\n", bucket.Name)
	s.DownloadBucket(bucket, prefix)
}

// Given a query split it into a bucketname and a prefix
func SplitQuery(query string) (bucket, prefix string) {
	re := regexp.MustCompile("^s3://")
	// strip `s3://` form query
	query = string(re.ReplaceAll([]byte(query), []byte("")))
	path := strings.Split(query, "/")
	bucket = path[0]
	prefix = strings.Join(path[1:], "/")
	return
}

// Given a bucket name and some auths, try to find the bucket's region
// TODO: Make these requests concurrent
func FindBucket(bucketName string, auth aws.Auth) (*s3.Bucket, error) {
	for regionName, region := range aws.Regions {
		fmt.Printf("Looking for bucket in region: %s\n", regionName)
		// create a s3 client and try to find the bucket
		bucket := s3.New(auth, region).Bucket(bucketName)
		_, err := bucket.List("", "", "", 0)
		if err == nil {
			fmt.Println("Found bucket!")
			return bucket, nil
		}
	}
	return nil, errors.New("Could not locate s3 bucket")
}

type SyncJob struct {
	Bucket       *s3.Bucket
	Key          s3.Key
	Localdir     string
	JobType      JobType
	NTries       int
	Filepath     string
	IsSuccessful bool
}

func (s *Syncer) DownloadBucket(bucket *s3.Bucket, prefix string) {
	maxJobs := 0
	if s.Concurrent > 0 {
		maxJobs = s.Concurrent
	}
	nTries := 1
	if s.NTries > 0 {
		nTries = s.NTries
	}
	jobPool := make(chan int, maxJobs)
	doneJobs := make(chan *SyncJob)
	nJobs := 0
	lastKey := ""
	// Iterate through bucket keys
	for {
		keyList, err := bucket.List(prefix, "", lastKey, 1000)
		if err != nil {
			panic(err)
		}
		keys := keyList.Contents
		nKeys := len(keys)
		for _, key := range keys {
			nJobs++
			sj := &SyncJob{
				Bucket:       bucket,
				Key:          key,
				Localdir:     s.Localdir,
				JobType:      DOWNLOAD,
				NTries:       nTries,
				Filepath:     "",
				IsSuccessful: false,
			}
			// Execute a goroutine to run the job
			go sj.RunJob(jobPool, doneJobs)
		}
		// Once the returned list is not truncated we're done!
		if !keyList.IsTruncated {
			break
		}
		lastKey = keys[nKeys-1].Key
	}
	jobs := make([]*SyncJob, nJobs)
	// Wait for all jobs to finish
	for {
		j := <-doneJobs
		nJobs--
		jobs[nJobs] = j
		if nJobs == 0 {
			break
		}
	}
	if !s.Full {
		return
	}
	// clean up target
	filenames := make([]string, len(jobs))
	i := 0
	for _, job := range jobs {
		if !job.IsSuccessful {
			continue
		}
		filenames[i] = job.Filepath
		i++
	}
	pathPrefix := filepath.Join(strings.Split(prefix, "/")...)
	pathPrefix = filepath.Join(s.Localdir, pathPrefix)
	pathPrefix = fmt.Sprintf("^%s", regexp.QuoteMeta(pathPrefix))
	fmt.Printf("Path prefix: %s\n", pathPrefix)
	prefixRegexp := regexp.MustCompile(pathPrefix)

	filenames = filenames[:i]
	sort.Strings(filenames)

	FileCheck := func(currpath string, info os.FileInfo, err error) error {
		if info.IsDir() {
			return nil
		}
		if !prefixRegexp.Match([]byte(currpath)) {
			return nil
		}
		// See if this path was succcessfully downloaded
		pathIndex := sort.SearchStrings(filenames, currpath)
		if pathIndex != len(filenames) {
			if filenames[pathIndex] == currpath {
				return nil
			}
		}
		fmt.Printf("Removing unmatched local file: %s\n", currpath)
		return os.Remove(currpath)
	}
	err := filepath.Walk(s.Localdir, FileCheck)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error cleaning directory: %s\n", err)
	}
}

// Run an upload or download job through a job pool
func (sj *SyncJob) RunJob(jobPool chan int, doneJobs chan *SyncJob) {
	jobPool <- 1
	defer func() {
		if r := recover(); r != nil {
			fmt.Printf("Error downloading file: %s\n", sj.Key.Key)
			fmt.Printf("%s\n", r)
		}
		<-jobPool
		doneJobs <- sj
	}()
	switch sj.JobType {
	case UPLOAD:
		for i := 0; i < sj.NTries; i++ {
			if sj.Upload() {
				return
			}
		}
	case DOWNLOAD:
		for i := 0; i < sj.NTries; i++ {
			if sj.Download() {
				return
			}
		}
	default:
		fmt.Println("Unknown job type")
		panic("")
	}
	fmt.Printf("Hit max tries attempting to download: %s\n", sj.Key.Key)
}

// Download a file from S3
func (sj *SyncJob) Download() bool {
	target, err := sj.CreateDownloadPath()
	if err != nil {
		panic(err)
	}
	sj.Filepath = target
	if sj.IsHashSame() {
		fmt.Printf("File already downloaded: %s\n", sj.Key.Key)
		sj.IsSuccessful = true
		return true
	}
	// Construct file
	fi, err := os.Create(target)
	if err != nil {
		fmt.Printf("Could not create file: %s\n", target)
		return false
	}
	defer fi.Close()
	// Get response reader
	responseReader, err := sj.Bucket.GetReader(sj.Key.Key)
	if err != nil {
		fmt.Printf("Error making request for %s: %s\n",
			sj.Key.Key, err)
		return false
	}
	defer responseReader.Close()
	// Write to file and hasher at the same time
	// h := md5.New()
	// t := io.MultiWriter(h, fi)
	nbytes, err := io.Copy(fi, responseReader)
	if err != nil {
		fmt.Printf("Error writing file to disk: %s\n", err)
		return false
	}
	/*
		fileHash := fmt.Sprintf("\"%x\"", h.Sum(nil))
		if fileHash != sj.Key.ETag {
			fmt.Printf("Hashes did not match for file: %s\n", sj.Key.Key)
			return false
		}
	*/
	fmt.Printf("[%10d bytes] Downloaded file: %s\n", nbytes, sj.Key.Key)
	sj.IsSuccessful = true
	return true
}

// Have to create a filepath to download file to.
// Lots of file io error handling. Fuuunnnnn
func (sj *SyncJob) CreateDownloadPath() (filename string, funcErr error) {
	s3Path := strings.Split(sj.Key.Key, "/")
	target := sj.Localdir
	for _, pathPart := range s3Path {
		fi, err := os.Stat(target)
		if err == nil {
			if !fi.IsDir() {
				errMsg := fmt.Sprintf("%s is a directory"+
					" expected directory", target)
				funcErr = errors.New(errMsg)
				return
			}
		} else {
			err = os.Mkdir(target, 0755)
			if err != nil {
				funcErr = err
				return
			}
		}
		target = path.Join(target, pathPart)
	}
	filename = target
	return
}

// Is the local hash of the file the same as the key on s3?
func (sj *SyncJob) IsHashSame() bool {
	fi, err := os.Open(sj.Filepath)
	if err != nil {
		// local file doesn't exist, so no they aren't the same
		return false
	}
	defer fi.Close()
	h := md5.New()
	_, err = io.Copy(h, fi)
	if err != nil {
		// Some fishy stuff is happening, better overwrite
		return false
	}
	fileHash := fmt.Sprintf("\"%x\"", h.Sum(nil))
	// return if the local hash and the remote hash are the same
	return fileHash == sj.Key.ETag
}

// Upload a file to S3
func (sj *SyncJob) Upload() bool {
	fmt.Printf("Uploading file %s to %s:%s\n", sj.Filepath,
		sj.Bucket.Name, sj.Key.Key)
	return true
}
