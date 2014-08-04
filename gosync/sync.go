package gosync

import (
	"crypto/md5"
	"errors"
	"fmt"
	"github.com/ericchiang/goamz/s3"
	"github.com/yhat/gosync/jobs"
	"io"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

type Syncer struct {
	BucketName string
	KeyPrefix  string
	Localdir   string
	JobRunner  *jobs.JobRunner
	FullSync   bool
	S3Cli      *s3.S3
	Rules      []*regexp.Regexp
}

// A job to upload a file to S3
type UploadJob struct {
	Localfile    string
	Localdir     string
	Bucket       *s3.Bucket
	KeyPrefix    string
	ResultPath   string
	IsSuccessful bool
}

func (uj UploadJob) Run() (jobs.Job, error) {
	relPath, err := filepath.Rel(uj.Localdir, uj.Localfile)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Could not get relative path of file:"+
			" %s\n", uj.Localfile)
		return uj, err
	}
	// Filepaths may use backslashes (PCs), must convert to slash for S3.
	s3Path := filepath.ToSlash(relPath)
	s3Path = path.Join(uj.KeyPrefix, s3Path)
	uj.ResultPath = s3Path
	// Open file
	fi, err := os.Open(uj.Localfile)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error opening file: %s\n",
			uj.Localfile)
		return uj, err
	}
	defer fi.Close()
	// compute file hash and count number of bytes in file
	h := md5.New()
	nbytes, err := io.Copy(h, fi)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error computing file hash: %s\n",
			uj.Localfile)
		return uj, err
	}
	filehash := fmt.Sprintf("\"%x\"", h.Sum(nil))
	// Check if there's a Key with the same hash on s3
	if AlreadyUploaded(uj.Bucket, s3Path, filehash) {
		fmt.Printf("File already uploaded: %s\n", uj.Localfile)
		uj.IsSuccessful = true
		return uj, nil
	}
	// bring it back yal
	currOffset, err := fi.Seek(0, os.SEEK_SET)
	if err != nil || currOffset != 0 {
		fmt.Printf("IO error seeking file: %s\n", uj.Localfile)
		return uj, err
	}
	// Try to upload the context of a file to S3
	err = uj.Bucket.PutReader(s3Path, fi, nbytes, "content-type",
		s3.Private)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error uploading file '%s' to S3: %s",
			uj.Localfile, err)
		return uj, err
	}
	fmt.Printf("[%10d bytes] Uploaded file: '%s'\n", nbytes,
		uj.Localfile)
	uj.IsSuccessful = true
	return uj, nil
}

// Check if a file has already been uploaded by comparing hash
func AlreadyUploaded(bucket *s3.Bucket, keyPath, filehash string) bool {
	head, err := bucket.Head(keyPath)
	if err != nil {
		return false
	}
	eTag, ok := head.Header["Etag"]
	if !ok {
		return false
	}
	if len(eTag) < 1 {
		return false
	}
	return eTag[0] == filehash
}

type DeleteJob struct {
	Bucket       *s3.Bucket
	KeyPath      string
	IsSuccessful bool
}

func (dj DeleteJob) Run() (jobs.Job, error) {
	err := dj.Bucket.Del(dj.KeyPath)
	if err == nil {
		dj.IsSuccessful = true
		fmt.Printf("Successfully deleted key: '%s'\n", dj.KeyPath)
	} else {
		fmt.Printf("Error deleting key: '%s'\n", dj.KeyPath)
	}
	return dj, err
}

func (s *Syncer) Upload() {
	// Get bucket, if that bucket doesn't exist create it
	bucket := GetBucket(s.S3Cli, s.BucketName)
	nJobs := 0
	walkFunc := func(currpath string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		// The file must match all regexp rules
		for _, rule := range s.Rules {
			if !rule.MatchString(currpath) {
				return nil
			}
		}
		uj := UploadJob{
			Localfile:    currpath,
			Localdir:     s.Localdir,
			Bucket:       bucket,
			KeyPrefix:    s.KeyPrefix,
			IsSuccessful: false,
		}
		nJobs++
		s.JobRunner.RunJob(uj)
		return nil
	}
	err := filepath.Walk(s.Localdir, walkFunc)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error uploading directory: %s\n", err)
		os.Exit(2)
	}
	resultKeys := make([]string, nJobs)
	nSuccessful := 0
	jobs := make([]UploadJob, nJobs)
	for i := 0; i < nJobs; i++ {
		j := s.JobRunner.Get()
		// Type cast back to an UploadJob
		job, _ := j.(UploadJob)
		if job.IsSuccessful {
			resultKeys[nSuccessful] = job.ResultPath
			nSuccessful++
		}
		jobs[i] = job
	}
	resultKeys = resultKeys[0:nSuccessful]
	if !s.FullSync {
		return
	}
	sort.Strings(resultKeys)
	fmt.Println("Doing a full sync")
	lastKey := ""
	delJobs := 0
	for {
		keyList, err := bucket.List(s.KeyPrefix, "", lastKey, 1000)
		if err != nil {
			fmt.Fprintf(os.Stderr,
				"Could not find bucket '%s' in region '%s'\n",
				bucket.Name, s.S3Cli.Region.Name)
			os.Exit(2)
		}
		keys := keyList.Contents
		for _, key := range keys {
			runJob := true
			// Make sure this key matches all Regexp rules passed
			// to the job. For example a rule could require a
			// certain file extention.
			for _, rule := range s.Rules {
				if !rule.MatchString(key.Key) {
					runJob = false
					break
				}
			}
			if !runJob {
				continue
			}
			// Has this key already been uploaded?
			i := sort.SearchStrings(resultKeys, key.Key)
			if i < len(resultKeys) {
				if resultKeys[i] == key.Key {
					continue
				}
			}
			delJobs++
			dj := DeleteJob{
				Bucket:       bucket,
				KeyPath:      key.Key,
				IsSuccessful: false,
			}
			s.JobRunner.RunJob(dj)
		}
		// Once the returned list is not truncated we're done!
		if !keyList.IsTruncated {
			break
		}
		lastKey = keys[len(keys)-1].Key
	}
	for i := 0; i < delJobs; i++ {
		// clear the 'done job' channel
		s.JobRunner.Get()
	}
	fmt.Println("Full sync cleanup done")
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
			return &bucket
		}
	}
	fmt.Printf("Bucket '%s' not found: creating\n", bucketName)
	// if not create a private bucket
	newBucket := s3Cli.Bucket(bucketName)
	err = newBucket.PutBucket(s3.Private)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error attempting to create bucket "+
			"'%s'\n", bucketName)
		os.Exit(2)
	}
	return newBucket
}

// A job to download a file from S3
type DownloadJob struct {
	Bucket       *s3.Bucket
	Key          s3.Key
	Localdir     string
	ResultFile   string
	IsSuccessful bool
}

// Download a file from S3
func (dj DownloadJob) Run() (jobs.Job, error) {
	target, err := CreateDownloadPath(dj.Key, dj.Localdir)
	if err != nil {
		fmt.Printf("Error getting local file path: %s\n", err)
		return dj, nil
	}
	dj.ResultFile = target
	if IsHashSame(dj.Key.ETag, target) {
		fmt.Printf("File already downloaded: %s\n", dj.Key.Key)
		dj.IsSuccessful = true
		return dj, nil
	}
	// Construct file
	fi, err := os.Create(target)
	if err != nil {
		fmt.Printf("Could not create file: %s\n", target)
		return dj, err
	}
	defer fi.Close()
	// Spaces need to be "+"
	keyName := strings.Replace(dj.Key.Key, " ", "+", -1)
	// Get response reader
	responseReader, err := dj.Bucket.GetReader(keyName)
	if err != nil {
		fmt.Printf("Error making request for %s: %s\n", keyName, err)
		return dj, err
	}
	defer responseReader.Close()
	// Read response to file
	nbytes, err := io.Copy(fi, responseReader)
	if err != nil {
		fmt.Printf("Error writing file to disk: %s\n", err)
		return dj, err
	}
	fmt.Printf("[%10d bytes] Downloaded file: %s\n", nbytes, dj.Key.Key)
	dj.IsSuccessful = true
	return dj, nil
}

func (s *Syncer) Download() {
	// Split the S3 path into a bucket/prefix combination
	bucket := s.S3Cli.Bucket(s.BucketName)
	nJobs := 0
	lastKey := ""
	// Iterate through bucket keys
	for {
		keyList, err := bucket.List(s.KeyPrefix, "", lastKey, 1000)
		if err != nil {
			fmt.Fprintf(os.Stderr,
				"Could not find bucket '%s' in region '%s'\n",
				bucket.Name, s.S3Cli.Region.Name)
			os.Exit(2)
		}
		keys := keyList.Contents
		for _, key := range keys {
			runJob := true
			// Make sure this key matches all Regexp rules passed
			// to the job. For example a rule could require a
			// certain file extention.
			for _, rule := range s.Rules {
				if !rule.MatchString(key.Key) {
					fmt.Printf("Ignoring key: %s\n",
						key.Key)
					runJob = false
					break
				}
			}
			if runJob {
				dj := DownloadJob{
					Bucket:       bucket,
					Key:          key,
					Localdir:     s.Localdir,
					IsSuccessful: false,
				}
				// Execute a goroutine to run the job
				s.JobRunner.RunJob(dj)
				nJobs++
			}
		}
		// Once the returned list is not truncated we're done!
		if !keyList.IsTruncated {
			break
		}
		lastKey = keys[len(keys)-1].Key
	}
	jobs := make([]DownloadJob, nJobs)
	// Wait for all jobs to finish
	for i := 0; i < nJobs; i++ {
		j := s.JobRunner.Get()
		// Typecast back to DownloadJob
		job, _ := j.(DownloadJob)
		jobs[i] = job
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
		if job.IsSuccessful {
			filenames[i] = job.ResultFile
			i++
		}
	}
	filenames = filenames[:i]
	sort.Strings(filenames)

	// construct a prefix regexp to check if local file is within the scope
	// of a sync
	pathPrefix := filepath.Join(strings.Split(s.KeyPrefix, "/")...)
	pathPrefix = filepath.Join(s.Localdir, pathPrefix)
	pathPrefix = fmt.Sprintf("^%s", regexp.QuoteMeta(pathPrefix))
	prefixRegexp := regexp.MustCompile(pathPrefix)

	rules := s.Rules
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

// Have to create a filepath to download file to.
// Lots of file io error handling. Fuuunnnnn
func CreateDownloadPath(key s3.Key, localdir string) (string, error) {
	s3Path := strings.Split(key.Key, "/")
	target := localdir
	for _, pathPart := range s3Path {
		fi, err := os.Stat(target)
		if err == nil {
			if !fi.IsDir() {
				errMsg := fmt.Sprintf("%s is a directory"+
					" expected directory", target)
				return "", errors.New(errMsg)
			}
		} else {
			err = os.Mkdir(target, 0755)
			if err != nil {
				return "", err
			}
		}
		target = filepath.Join(target, pathPart)
	}
	return target, nil
}

// Is the local hash of the file the same as the key on s3?
func IsHashSame(eTag, filepath string) bool {
	fi, err := os.Open(filepath)
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
	return fileHash == eTag
}
