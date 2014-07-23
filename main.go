package main

import (
	sync "./gosync"
	"fmt"
	"github.com/codegangsta/cli"
	"github.com/mitchellh/goamz/aws"
	"os"
	"regexp"
)

func main() {
	app := cli.NewApp()
	app.Name = "gosync"
	app.Usage = "gosync OPTIONS SOURCE TARGET"
	app.Version = "0.1.0"
	app.Flags = []cli.Flag{
		cli.IntFlag{"concurrent, c", 20,
			"number of concurrent transfers"},
		cli.StringFlag{"log-level, l", "info", "log level"},
		cli.StringFlag{"accesskey, a", "", "AWS access key"},
		cli.StringFlag{"secretkey, s", "", "AWS secret key"},
		cli.IntFlag{"ntries", 2, "n tries to get the hash right"},
		cli.BoolFlag{"full, f", "delete existing files/keys in " +
			"TARGET which do not appear in SOURCE"},
	}
	app.Action = func(c *cli.Context) {
		// This will default to reading the env variables if keys
		// aren't passed ass command line arguments
		auth, err := aws.GetAuth(c.String("accesskey"),
			c.String("secretkey"))
		if err != nil {
			fmt.Fprintf(os.Stderr, "Please specify both of your"+
				" aws keys\n")
			os.Exit(2)
		}
		if len(c.Args()) != 2 {
			fmt.Fprintf(os.Stderr, "Invalid number of arguments."+
				" Run `gosync -h` for help.\n")
			os.Exit(2)
		}
		source := c.Args()[0]
		fmt.Printf("Setting source to '%s'\n", source)
		target := c.Args()[1]
		fmt.Printf("Setting target to '%s'\n", target)

		// Make sure there's one s3 path and one local path
		s3Regexp := regexp.MustCompile("^s3://")
		mode := sync.UPLOAD
		local := source
		remote := target
		if s3Regexp.MatchString(source) {
			if s3Regexp.MatchString(target) {
				fmt.Fprintf(os.Stderr, "SOURCE and TARGET"+
					" can't both be s3 paths!\n")
				os.Exit(2)
			}
			mode = sync.DOWNLOAD
			local = target
			remote = source
		} else if !s3Regexp.MatchString(target) {
			fmt.Fprintf(os.Stderr, "SOURCE (x)or TARGET must be"+
				" a s3 path!\n")
		}

		if c.Bool("full") {
			fmt.Println("Doing a full sync! You've been warned.")
		}

		// All ready. Construct a syncer and run it!
		s := &sync.Syncer{remote, local, c.Int("concurrent"), auth,
			mode, c.Int("ntries"), c.Bool("full")}
		s.Run()
	}
	app.Run(os.Args)
}
