package main

import (
	"fmt"
	"github.com/codegangsta/cli"
	"github.com/ericchiang/goamz/s3"
	"github.com/mitchellh/goamz/aws"
	"github.com/yhat/gosync/gosync"
	"github.com/yhat/gosync/jobs"
	"os"
	"regexp"
	"strings"
)

type JobType int

const (
	UPLOAD JobType = iota
	DOWNLOAD
	VERSION string = "0.1.4"
)

// Shout out to #rstats
func Stop(format string, args ...interface{}) {
	fmt.Fprintf(os.Stderr, "[ERROR] ")
	fmt.Fprintf(os.Stderr, format, args...)
	os.Exit(2)
}

func AppAction(c *cli.Context) {
	if len(c.Args()) != 2 {
		Stop("Bad number of arguments. Run `gosync -h` for help.\n")
	}
	source := c.Args()[0]
	fmt.Printf("Setting source to '%s'\n", source)
	target := c.Args()[1]
	fmt.Printf("Setting target to '%s'\n", target)

	// This will default to reading the env variables if keys aren't
	// passed as command line arguments
	auth, err := aws.GetAuth(c.String("accesskey"),
		c.String("secretkey"))
	if err != nil {
		Stop("Please specify both of your aws keys\n")
	}

	if strings.Count(target, "*") > 0 {
		Stop("TARGET cannot contain a wildcard '*'\n")
	}

	postfix := ""
	sourceSplit := strings.Split(source, "*")
	if len(sourceSplit) == 2 {
		source = sourceSplit[0]
		postfix = sourceSplit[1]
	} else if len(sourceSplit) > 2 {
		Stop("Sorry. Currently SOURCE paths can't contain more than" +
			" one wildcard '*'\n")
	}
	postfix = fmt.Sprintf("%s$", regexp.QuoteMeta(postfix))
	// Rules are regexps that all synced files must match. This one matches
	// a trailing string specified by the user (e.g. `*.json`)
	postfixRule := regexp.MustCompile(postfix)

	// Make sure there's one s3 path and one local path
	s3Regexp := regexp.MustCompile("^s3://")
	mode := UPLOAD
	local := source
	remote := target
	if s3Regexp.MatchString(source) {
		if s3Regexp.MatchString(target) {
			Stop("SOURCE and TARGET can't both be S3 paths!\n")
		}
		mode = DOWNLOAD
		local = target
		remote = source
	} else if !s3Regexp.MatchString(target) {
		Stop("SOURCE (x)or TARGET must be a S3 path!\n")
	}
	// Strip "s3://" off the remote
	remote = s3Regexp.ReplaceAllString(remote, "")
	// A Bucket on S3 is only the root directory of a path. All sub paths
	// are just part of the S3 Key name.
	remotePath := strings.Split(remote, "/")
	bucketName := remotePath[0]
	keyPrefix := strings.Join(remotePath[1:], "/")

	if c.Bool("full") {
		fmt.Println("Doing a full sync! You've been warned.")
	}

	region, ok := aws.Regions[c.String("region")]
	var s *gosync.Syncer
	if !ok {
		regions := []string{
			"us-east-1",
			"us-west-1",
			"us-west-2",
			"eu-west-1",
			"eu-central-1",
			"ap-northeast-1",
			"ap-southeast-1",
			"ap-southeast-2",
			"sa-east-1",
		}
		for _, region := range regions {
			s3Cli := s3.New(auth, aws.Regions[region])
			// All ready. Construct a syncer and run it!
			s = &gosync.Syncer{
				BucketName: bucketName,
				KeyPrefix:  keyPrefix,
				Localdir:   local,
				JobRunner:  jobs.NewJobRunner(c.Int("concurrent")),
				FullSync:   c.Bool("full"),
				S3Cli:      s3Cli,
				Rules:      []*regexp.Regexp{postfixRule},
			}
			if s.BucketExists(bucketName) == true {
				break
			}
		}
	} else {
		s3Cli := s3.New(auth, region)
		// All ready. Construct a syncer and run it!
		s = &gosync.Syncer{
			BucketName: bucketName,
			KeyPrefix:  keyPrefix,
			Localdir:   local,
			JobRunner:  jobs.NewJobRunner(c.Int("concurrent")),
			FullSync:   c.Bool("full"),
			S3Cli:      s3Cli,
			Rules:      []*regexp.Regexp{postfixRule},
		}
	}

	switch mode {
	case DOWNLOAD:
		s.Download()
	case UPLOAD:
		s.Upload()
	}
}

func main() {
	app := cli.NewApp()
	app.Name = "gosync"
	app.Usage = "gosync OPTIONS SOURCE TARGET"
	app.Version = VERSION
	app.Flags = []cli.Flag{
		cli.IntFlag{
			Name:  "concurrent, c",
			Value: 20,
			Usage: "number of concurrent transfers",
		},
		cli.StringFlag{
			Name:  "log-level, l",
			Value: "info",
			Usage: "loglevel",
		},
		cli.StringFlag{
			Name:  "accesskey, a",
			Value: "",
			Usage: "AWS accesskey",
		},
		cli.StringFlag{
			Name:  "secretkey, s",
			Value: "",
			Usage: "AWS secretkey",
		},
		cli.BoolFlag{
			Name:  "full, f",
			Usage: "delete existing files/keys in TARGET which do not appear in SOURCE",
		},
		cli.StringFlag{
			Name:  "region, r",
			Value: "us-east-1",
			Usage: "Aws region",
		},
	}
	app.Action = AppAction
	app.Run(os.Args)
}
