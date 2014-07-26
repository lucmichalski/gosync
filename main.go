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
	VERSION string = "0.1.3"
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
	if !ok {
		Stop("'%s' is not a valid AWS region. Please select"+
			" one from list below\n", c.String("region"))
		for k, _ := range aws.Regions {
			fmt.Println(k)
		}
		os.Exit(2)
	}
	s3Cli := s3.New(auth, region)

	// All ready. Construct a syncer and run it!
	s := &gosync.Syncer{
		BucketName: bucketName,
		KeyPrefix:  keyPrefix,
		Localdir:   local,
		JobRunner:  jobs.NewJobRunner(c.Int("concurrent")),
		FullSync:   c.Bool("full"),
		S3Cli:      s3Cli,
		Rules:      []*regexp.Regexp{postfixRule},
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
		cli.IntFlag{"concurrent, c", 20,
			"number of concurrent transfers"},
		cli.StringFlag{"log-level, l", "info", "log level"},
		cli.StringFlag{"accesskey, a", "", "AWS access key"},
		cli.StringFlag{"secretkey, s", "", "AWS secret key"},
		cli.BoolFlag{"full, f", "delete existing files/keys in " +
			"TARGET which do not appear in SOURCE"},
		cli.StringFlag{"region, r", "us-east-1", "Aws region"},
	}
	app.Action = AppAction
	app.Run(os.Args)
}
