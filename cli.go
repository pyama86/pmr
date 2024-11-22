package main

import (
	"bufio"
	"crypto/tls"
	"flag"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"golang.org/x/sync/errgroup"

	"github.com/sirupsen/logrus"
)

// Exit codes are int values that represent an exit code for a particular error.
const (
	ExitCodeOK    int = 0
	ExitCodeError int = 1 + iota
)

const (
	initScanTokenSize int = 1024 * 4
	MaxScanTokenSize  int = 1024 * 64
)

// CLI is the command line object
type CLI struct {
	// outStream and errStream are the stdout and stderr
	// to write message from the CLI.
	outStream, errStream io.Writer
}

// Run invokes the CLI with the given arguments.
func (cli *CLI) Run(args []string) int {
	var (
		timeout     int
		concurrency int
		url         string
		insecure    bool
		skipErrors  bool

		version bool
	)

	// Define option flag parse
	flags := flag.NewFlagSet(Name, flag.ContinueOnError)
	flags.SetOutput(cli.errStream)

	flags.IntVar(&concurrency, "concurrency", 5, "request concurrency")
	flags.IntVar(&concurrency, "c", 5, "request concurrency(Short)")
	flags.IntVar(&timeout, "timeout", 3, "request timeout sec")
	flags.IntVar(&timeout, "t", 3, "request timeout sec(Short)")
	flags.StringVar(&url, "url", "", "url")
	flags.StringVar(&url, "u", "", "url(Short)")
	flags.BoolVar(&insecure, "insecure", false, "Allow connections to SSL sites without certs")
	flags.BoolVar(&insecure, "k", false, "Allow connections to SSL sites without certs(Short)")
	flags.BoolVar(&skipErrors, "skip-errors", false, "Skip errors if HTTP GET request fails")

	flags.BoolVar(&version, "version", false, "Print version information and quit.")

	// Parse commandline flag
	if err := flags.Parse(args[1:]); err != nil {
		return ExitCodeError
	}

	// Show version
	if version {
		fmt.Fprintf(cli.errStream, "%s version %s\n", Name, Version)
		return ExitCodeOK
	}

	body, err := ioutil.ReadAll(os.Stdin)
	if err != nil {
		logrus.Fatal(err)
	}
	lines := strings.Split(string(body), "\n")

	c := make(chan bool, concurrency)
	eg := errgroup.Group{}
	for _, l := range lines {
		l := l
		c := c
		if l == "" || l == "\n" {
			continue
		}
		c <- true
		eg.Go(func() error {
			defer func() { <-c }()
			return request(url, timeout, insecure, l, skipErrors)
		})
	}
	if err := eg.Wait(); err != nil {
		logrus.Fatal(err)
	}
	return ExitCodeOK
}

func request(url string, timeout int, insecure bool, filePath string, skipErrors bool) error {
	u, err := urlJoin(url, filePath)
	if err != nil {
		return err
	}
	tr := &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: insecure},
	}
	client := &http.Client{
		Transport: tr,
		Timeout:   time.Duration(timeout) * time.Second,
	}
	req, err := http.NewRequest("GET", u, nil)
	if err != nil {
		return err
	}
	ua := fmt.Sprintf("%s/%s", "PyamaMultiRequest", Version)
	req.Header.Set("User-Agent", ua)

	r, err := client.Do(req)
	if err != nil {
		if skipErrors {
			logrus.Error(err)
			return nil
		} else {
			return err
		}
	}

	defer r.Body.Close()
	body, err := ioutil.ReadAll(r.Body)
	if err != nil {
		return err
	}

	st := fmt.Sprintf("request: %s %s", u, r.Status)
	if r.StatusCode != http.StatusOK &&
		r.StatusCode != http.StatusNotFound &&
		r.StatusCode != http.StatusForbidden {
		logrus.Warnf(st)
		return nil
	} else {
		logrus.Infof(st)
	}
	lines, err := getFileHead(filePath)
	if err != nil {
		return err
	}

	if len(lines) == 0 && len(body) > 0 {
		return nil
	}

	for _, l := range lines {
		if strings.Index(string(body), l) < 0 {
			return nil
		}
	}
	logrus.Warnf("This file is published %s", filePath)
	return nil
}

func urlJoin(base, path string) (string, error) {
	u, err := url.Parse(path)
	if err != nil {
		return "", err
	}
	pb, err := url.Parse(base)
	if err != nil {
		return "", err
	}
	return pb.ResolveReference(u).String(), nil
}

func getFileHead(path string) ([]string, error) {
	fp, err := os.Open(path)
	if err != nil {
		return nil, err
	}

	cnt := 0
	lines := []string{}

	defer fp.Close()
	scanner := bufio.NewScanner(fp)
	buf := make([]byte, 0, initScanTokenSize)
	scanner.Buffer(buf, MaxScanTokenSize)
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
		cnt++
		if cnt > 10 {
			break
		}
	}
	return lines, nil
}
