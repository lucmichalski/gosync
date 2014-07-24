package sync

import (
	"crypto/md5"
	"errors"
	"fmt"
	"github.com/ericchiang/goamz/s3"
	"io"
	"net/url"
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
	Mode       JobType
	NTries     int
	FullSync   bool
	S3Cli      *s3.S3
}

func (s *Syncer) Run() {
	re := regexp.MustCompile("^s3://")
	s.S3Query = string(re.ReplaceAll([]byte(s.S3Query), []byte("")))
	switch s.Mode {
	case UPLOAD:
		fmt.Println("Uploading file")
		bucketName, prefix, postfix := SplitS3Query(s.S3Query)
		fmt.Printf("Bucket name: %s\n", bucketName)
		fmt.Printf("Prefix     : %s\n", prefix)
		if postfix != "" {
			fmt.Fprintf(os.Stderr,
				"Wildcard not allowed in TARGET\n")
			os.Exit(2)
		}
		tmp, localrule := SplitWildcard(s.Localdir)
		s.Localdir = tmp
		fmt.Printf("Localpath: %s\n", s.Localdir)
		fmt.Printf("Localrule: %s\n", localrule)
		localrule = fmt.Sprintf("%s$", regexp.QuoteMeta(localrule))
		localruleRegexp := regexp.MustCompile(localrule)
		fileRules := []*regexp.Regexp{localruleRegexp}
		bucket := GetBucket(s.S3Cli, bucketName)
		s.UploadDirectory(bucket, prefix, fileRules)
	case DOWNLOAD:
		bucketName, prefix, postfix := SplitS3Query(s.S3Query)
		fmt.Printf("Bucket name : %s\n", bucketName)
		fmt.Printf("Prefix      : %s\n", prefix)
		fmt.Printf("Postfix rule: %s\n", postfix)
		// Generate regexp rule from postfix
		postfix = fmt.Sprintf("%s$", regexp.QuoteMeta(postfix))
		postfixRegexp := regexp.MustCompile(postfix)
		fileRules := []*regexp.Regexp{postfixRegexp}
		bucket := s.S3Cli.Bucket(bucketName)
		fmt.Printf("Looking for keys in: %s\n", bucket.Name)
		s.DownloadBucket(bucket, prefix, fileRules)
	}
}

// Given a query split it into a bucketname and a prefix
func SplitS3Query(query string) (bucket, prefix, postfix string) {
	query, postfix = SplitWildcard(query)
	path := strings.Split(query, "/")
	bucket = path[0]
	prefix = strings.Join(path[1:], "/")
	return
}

// Split along wildcard and make sure there's no additional
func SplitWildcard(query string) (prequery, postfix string) {
	wildcardSplit := strings.Split(query, "*")
	switch len(wildcardSplit) {
	case 1:
		prequery = query
		postfix = ""
	case 2:
		prequery = wildcardSplit[0]
		postfix = wildcardSplit[1]
	default:
		fmt.Fprintf(os.Stderr, "Sorry. Currently SOURCE paths can't "+
			"contain more than one wildcard ('*')\n")
		os.Exit(2)
	}
	return
}

// Given a bucket name either get that bucket or create it if it doesn't exist
func GetBucket(s3Cli *s3.S3, bucketName string) *s3.Bucket {
	fmt.Printf("Searching for bucket '%s' on s3\n", bucketName)
	buckets, err := s3Cli.ListBuckets()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error attempting to list buckets on "+
			"S3\n")
		os.Exit(2)
	}
	for _, bucket := range buckets.Buckets {
		if bucket.Name == bucketName {
			fmt.Println("Found bucket!")
			return &bucket
		}
	}
	// if not create a private bucket
	newBucket := s3Cli.Bucket(bucketName)
	err = newBucket.PutBucket(s3.Private)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error attempting to create bucket "+
			"'%s'\n", bucketName)
	}
	return newBucket
}

type SyncJob struct {
	// Upload or download?
	JobType JobType
	// Bucket and key to upload/download from/to
	Bucket *s3.Bucket
	Key    s3.Key
	// Local directory and relative path to upload/download from/to
	Localdir string
	Filepath string
	// All synced files must match these rules
	Rules        []*regexp.Regexp
	Prefix       string
	NTries       int
	IsSuccessful bool
}

func (s *Syncer) DownloadBucket(bucket *s3.Bucket, prefix string,
	rules []*regexp.Regexp) {
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
		keyList, err := bucket.List(prefix, "", lastKey, 200)
		if err != nil {
			fmt.Fprintf(os.Stderr,
				"Could not find bucket '%s' in region '%s'\n",
				bucket.Name, s.S3Cli.Region.Name)
			os.Exit(2)
		}
		keys := keyList.Contents
		nJobs += len(keys)
		for _, key := range keys {
			sj := &SyncJob{
				Bucket:       bucket,
				Key:          key,
				Localdir:     s.Localdir,
				JobType:      DOWNLOAD,
				NTries:       nTries,
				Filepath:     "",
				Prefix:       prefix,
				IsSuccessful: false,
				Rules:        rules,
			}
			// Execute a goroutine to run the job
			go sj.RunJob(jobPool, doneJobs)
		}
		// Once the returned list is not truncated we're done!
		if !keyList.IsTruncated {
			break
		}
		lastKey = keys[nJobs-1].Key
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
	// Full sync will also remove local files which don't have a matching
	// path on the s3 bucket
	if !s.FullSync {
		return
	}
	// Time to do a cleanup!
	fmt.Printf("Doing cleanup for full sync on dir: %s\n", s.Localdir)
	// Collect all the filenames successfully synced from s3 to local and
	// sort those names to allow for binary search (it's really fast)
	filenames := make([]string, len(jobs))
	i := 0
	for _, job := range jobs {
		if !job.IsSuccessful {
			continue
		}
		filenames[i] = job.Filepath
		i++
	}
	filenames = filenames[:i]
	sort.Strings(filenames)

	// construct a prefix regexp to check if local file is within the scope
	// of a sync
	pathPrefix := filepath.Join(strings.Split(prefix, "/")...)
	pathPrefix = filepath.Join(s.Localdir, pathPrefix)
	pathPrefix = fmt.Sprintf("^%s", regexp.QuoteMeta(pathPrefix))
	prefixRegexp := regexp.MustCompile(pathPrefix)

	rules = append(rules, prefixRegexp)

	FileCheck := func(currpath string, info os.FileInfo, err error) error {
		// We don't care about directories
		if info.IsDir() {
			return nil
		}
		for _, rule := range rules {
			if !rule.MatchString(currpath) {
				return nil
			}
		}
		// See if this path is among those successfully synced
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
	} else {
		fmt.Println("Full sync cleanup done")
	}
}

func (s *Syncer) UploadDirectory(bucket *s3.Bucket, prefix string,
	rules []*regexp.Regexp) {
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
	UpWalk := func(currpath string, info os.FileInfo, err error) error {
		if info.IsDir() {
			return nil
		}
		for _, rule := range rules {
			if !rule.MatchString(currpath) {
				return nil
			}
		}
		sj := &SyncJob{
			Bucket:       bucket,
			Localdir:     s.Localdir,
			JobType:      UPLOAD,
			NTries:       nTries,
			Filepath:     currpath,
			Prefix:       prefix,
			IsSuccessful: false,
			Rules:        rules,
		}
		nJobs++
		go sj.RunJob(jobPool, doneJobs)
		return nil
	}
	err := filepath.Walk(s.Localdir, UpWalk)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error uploading directory: %s\n", err)
		os.Exit(2)
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
	// Make sure this key matches all Regexp rules passed to the job.
	// For example a rule could require a certain file extention.
	for _, rule := range sj.Rules {
		if !rule.MatchString(sj.Key.Key) {
			fmt.Printf("Ignoring file: %s\n", sj.Key.Key)
			sj.IsSuccessful = true
			return true
		}
	}
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
	keyName := strings.Replace(sj.Key.Key, " ", "+", -1)
	responseReader, err := sj.Bucket.GetReader(keyName)
	if err != nil {
		fmt.Printf("Error making request for %s: %s\n",
			keyName, err)
		return false
	}
	defer responseReader.Close()
	// Read response to file
	nbytes, err := io.Copy(fi, responseReader)
	if err != nil {
		fmt.Printf("Error writing file to disk: %s\n", err)
		return false
	}
	fmt.Printf("[%10d bytes] Downloaded file: %s\n", nbytes, sj.Key.Key)
	sj.IsSuccessful = true
	return true
}

func EscapeS3Key(s3key string) string {
	pathParts := strings.Split(s3key, "/")
	for i := 0; i < len(pathParts); i++ {
		pathParts[i] = url.QueryEscape(pathParts[i])
	}
	return strings.Join(pathParts, "/")
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
		target = filepath.Join(target, pathPart)
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
	relpath, _ := filepath.Rel(sj.Localdir, sj.Filepath)
	// to slash (from os file sep) for s3 path upload
	keypath := filepath.ToSlash(relpath)
	keypath = path.Join(sj.Prefix, keypath)
	_, err := os.Open(sj.Filepath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error opening file for upload:%s\n",
			sj.Filepath)
	}
	_, err = sj.Bucket.Head(keypath)
	if err != nil {
		// File isn't already on s3
	} else {

	}
	return true
}
