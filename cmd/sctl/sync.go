package main

import (
	"context"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/gofrs/flock"
	"github.com/pkg/errors"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v2"
	"helm.sh/helm/v3/cmd/helm/search"
	helmcli "helm.sh/helm/v3/pkg/cli"
	"helm.sh/helm/v3/pkg/getter"
	"helm.sh/helm/v3/pkg/helmpath"
	"helm.sh/helm/v3/pkg/repo"
)

const searchMaxScore = 25

type pullCommandOptions struct {
	sourceURIString string
	targetURIString string
	version         string
}

func newPullCommand(out io.Writer) *cobra.Command {
	o := new(pullCommandOptions)

	cmd := &cobra.Command{
		Use:   "pull",
		Short: "Pull a helm chart.",
		RunE: func(cmd *cobra.Command, args []string) error {
			return pull(out, args)
		},
	}

	f := cmd.Flags()
	f.StringVar(&o.sourceURIString, "source", "", "source URI")
	f.StringVar(&o.targetURIString, "target", "", "target URI")

	return cmd
}

func pull(out io.Writer, args []string) error {
	if len(args) != 2 {
		return fmt.Errorf("pull requires 2 arguments, helm pull [source] [target]")
	}
	fromURI, err := url.Parse(args[0])
	if err != nil {
		return fmt.Errorf("invalid source URI: %w", err)
	}
	toURI, err := url.Parse(args[1])
	if err != nil {
		return fmt.Errorf("invalid target URI: %w", err)
	}
	if fromURI.Scheme != "helm" {
		return fmt.Errorf("source URI must be a helm chart")
	}
	if strings.Contains(fromURI.Path[1:], "/") {
		return fmt.Errorf("helm source URI must contain a single path segment (the chart name) was: %q", fromURI.Path[1:])
	}
	chartName := fromURI.Path[1:]
	if toURI.Scheme != "file" {
		return fmt.Errorf("target URI must be a file")
	}

	// Add the helm repository locally
	helmRepoName := helmHostToRepoName(fromURI.Host)
	helmRepoURL := "https://" + fromURI.Host
	if err := addHelmRepo(helmRepoName, helmRepoURL); err != nil {
		return fmt.Errorf("failed to add helm repository prior to pull: %w", err)
	}

	// Load the index file from the helm repository
	index, err := buildHelmIndex()
	if err != nil {
		return fmt.Errorf("failed to build the helm index: %w", err)
	}

	var count int
	for _, r := range index.All() {
		if r.Chart.Name != chartName {
			continue
		}
		// Pull chart
		pullChart(r, targetPath)
	}
	fmt.Fprintf(out, "found %d charts\n", count)
	return nil
}

func buildHelmIndex() (*search.Index, error) {
	settings := helmcli.New()
	repoFile := settings.RepositoryConfig
	repoCache := settings.RepositoryCache
	all := true

	// Load the repositories.yaml
	rf, err := repo.LoadFile(repoFile)
	if err != nil {
		return nil, errors.New("no repositories found")
	}

	i := search.NewIndex()
	for _, re := range rf.Repositories {
		n := re.Name
		f := filepath.Join(repoCache, helmpath.CacheIndexFile(n))
		ind, err := repo.LoadIndexFile(f)
		if err != nil {
			continue
		}

		i.AddRepo(n, ind, all)
	}
	return i, nil
}

func addHelmRepo(name, url string) error {
	settings := helmcli.New()
	repoFile := settings.RepositoryConfig
	repoCache := settings.RepositoryCache

	// Ensure the file directory exists as it is required for file locking
	err := os.MkdirAll(filepath.Dir(repoFile), os.ModePerm)
	if err != nil && !os.IsExist(err) {
		return err
	}

	// Acquire a file lock for process synchronization
	repoFileExt := filepath.Ext(repoFile)
	var lockPath string
	if len(repoFileExt) > 0 && len(repoFileExt) < len(repoFile) {
		lockPath = strings.TrimSuffix(repoFile, repoFileExt) + ".lock"
	} else {
		lockPath = repoFile + ".lock"
	}
	fileLock := flock.New(lockPath)
	lockCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	locked, err := fileLock.TryLockContext(lockCtx, time.Second)
	if err == nil && locked {
		defer fileLock.Unlock()
	}
	if err != nil {
		return err
	}

	b, err := os.ReadFile(repoFile)
	if err != nil && !os.IsNotExist(err) {
		return err
	}

	var f repo.File
	if err := yaml.Unmarshal(b, &f); err != nil {
		return err
	}

	c := repo.Entry{
		Name:                  name,
		URL:                   url,
		Username:              "",
		Password:              "",
		PassCredentialsAll:    false,
		CertFile:              "",
		KeyFile:               "",
		CAFile:                "",
		InsecureSkipTLSverify: false,
	}

	r, err := repo.NewChartRepository(&c, getter.All(settings))
	if err != nil {
		return err
	}

	if repoCache != "" {
		r.CachePath = repoCache
	}
	if _, err := r.DownloadIndexFile(); err != nil {
		return errors.Wrapf(err, "looks like %q is not a valid chart repository or cannot be reached", url)
	}

	f.Update(&c)

	if err := f.WriteFile(repoFile, 0600); err != nil {
		return err
	}
	fmt.Fprintf(os.Stdout, "%q has been added to your repositories\n", name)
	return nil
}

func helmHostToRepoName(host string) string {
	return strings.ReplaceAll(host, ".", "-")
}
