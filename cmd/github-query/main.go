package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/shurcooL/githubv4"
	"golang.org/x/oauth2"
)

var (
	listType string
	orgName  string
	repoName string
	sinceStr string
	untilStr string
)

func init() {
	flag.StringVar(&listType, "list", "", "<repos|commits|pull-requests> (Required)")
	flag.StringVar(&orgName, "org", "", "Organization name (Required)")
	flag.StringVar(&repoName, "repo", "", "Repository name (Required except to list repos)")
	flag.StringVar(&sinceStr, "since", "", "Start of date range (YYYY-MM-DD)")
	flag.StringVar(&untilStr, "until", "", "End of date range (YYYY-MM-DD)")
}

func main() {
	flag.Parse()
	// TODO: complain about unused args

	if err := validateOptions(listType, orgName, repoName); err != nil {
		fmt.Fprintln(os.Stderr, err)
		printUsage()
	}

	since := parseDate(sinceStr, time.Unix(0, 0)) // default is 1970-01-01
	until := parseDate(untilStr, time.Now())

	token, ok := os.LookupEnv("GITHUB_TOKEN")
	if !ok {
		fmt.Fprintln(os.Stderr, "Missing environment variable: GITHUB_TOKEN")
		printUsage()
	}
	tokenSource := oauth2.StaticTokenSource(&oauth2.Token{AccessToken: token})
	client := githubv4.NewClient(oauth2.NewClient(context.Background(), tokenSource))

	cmd := func() (interface{}, error) {
		switch listType {
		case "repos":
			return organizationRepositoryNames(*client, orgName)
		case "commits":
			return repositoryCommits(*client, orgName, repoName, since, until)
		case "pull-requests":
			return repositoryPullRequests(*client, orgName, repoName, since, until)
		default:
			panic("invalid list type")
		}
	}

	res, err := cmd()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	printJSON(res, os.Stdout)
}

// Prints usage and exits
func printUsage() {
	flag.Usage()
	fmt.Fprintln(os.Stderr, "\nNote: GITHUB_TOKEN environment variable is required.")
	os.Exit(1)
}

func validateOptions(listOpt string, orgOpt string, repoOpt string) error {
	if listOpt == "" {
		return errors.New("Missing option: list")
	}
	if orgOpt == "" {
		return errors.New("Missing option: org")
	}
	switch listOpt {
	case "repos":
		if repoOpt != "" {
			return errors.New("Incompatible option: repo")
		}
		// TODO: complain about unused since or until?
	case "commits", "pull-requests":
		if repoOpt == "" {
			return errors.New("Missing option: repo")
		}
		break
	default:
		return fmt.Errorf("Invalid list option: %s", listOpt)
	}
	return nil
}

// Parses the given timestr if not empty, else returns the provided default value.
// The time string is expected to conform to YYYY-MM-DD (ISO 8601) format.
func parseDate(timestr string, timedef time.Time) time.Time {
	if timestr != "" {
		since, err := time.Parse("2006-01-02", timestr)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			printUsage()
		}
		return since
	}
	return timedef
}

// Prints v as JSON to writer w. Panics on any error.
func printJSON(v interface{}, w io.Writer) {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	if err := enc.Encode(v); err != nil {
		panic(err)
	}
}

type RepositoryNodes []struct {
	Name        string
	Description string
	IsArchived  bool
	IsPrivate   bool
	CreatedAt   time.Time
	PushedAt    time.Time
}

// Collects repository names from a RepositoryNodes array
func (nodes RepositoryNodes) Names() (names []string) {
	for _, repo := range nodes {
		names = append(names, repo.Name)
	}
	return
}

// Returns the names of the repos belonging to the specified organization
func organizationRepositoryNames(client githubv4.Client, orgName string) ([]string, error) {
	var query struct {
		Organization struct {
			Repositories struct {
				TotalCount int
				PageInfo   struct {
					EndCursor   string
					HasNextPage bool
				}
				Nodes RepositoryNodes
			} `graphql:"repositories(first: 100, after: $after, orderBy: {field: NAME, direction: ASC})"`
		} `graphql:"organization(login: $login)"`
		RateLimit struct {
			Cost      int
			Limit     int
			Remaining int
			ResetAt   time.Time
		}
	}

	variables := map[string]interface{}{
		"login": githubv4.String(orgName),
		"after": (*githubv4.String)(nil), // first cursor is null
	}

	// Handle pagination
	var names []string
	for {
		if err := client.Query(context.Background(), &query, variables); err != nil {
			return nil, err
		}
		names = append(names, query.Organization.Repositories.Nodes.Names()...)
		pageInfo := query.Organization.Repositories.PageInfo
		if !pageInfo.HasNextPage {
			break
		}
		variables["after"] = githubv4.String(pageInfo.EndCursor)
	}
	return names, nil
}

// A merged pull request, including the merge commit and approving reviews
type PullRequestNode struct {
	Number      int
	MergedAt    time.Time
	HeadRefName string
	Title       string
	Author      struct {
		Login string
	}
	MergeCommit struct {
		MessageHeadline string
		AbbreviatedOid  string
	}
	Reviews struct {
		Nodes []struct {
			SubmittedAt time.Time
			Author      struct {
				Login string
			}
		}
	} `graphql:"reviews(first: 2, states: APPROVED)"`
}

type PullRequestNodes []PullRequestNode

// Collects pull requests within specified date range from a PullRequestNodes array
func (nodes PullRequestNodes) InRange(since time.Time, until time.Time) (list PullRequestNodes) {
	for _, node := range nodes {
		if !node.MergedAt.Before(since) && node.MergedAt.Before(until) {
			list = append(list, node)
		}
	}
	return
}

// Returns annotated pull requests that were merged to specified repo within the given time interval
func repositoryPullRequests(client githubv4.Client, orgName string, repoName string, since time.Time, until time.Time) ([]PullRequestNode, error) {
	var query struct {
		Repository struct {
			Name         string
			PullRequests struct {
				TotalCount int
				PageInfo   struct {
					EndCursor   string
					HasNextPage bool
				}
				Nodes PullRequestNodes
			} `graphql:"pullRequests(first: 100, after: $after)"`
		} `graphql:"repository(owner: $owner, name: $name)"`
		RateLimit struct {
			Cost      int
			Limit     int
			Remaining int
			ResetAt   time.Time
		}
	}

	variables := map[string]interface{}{
		"owner": githubv4.String(orgName),
		"name":  githubv4.String(repoName),
		"after": (*githubv4.String)(nil), // first cursor is null
	}

	// Handle pagination
	var pullRequests []PullRequestNode
	for {
		if err := client.Query(context.Background(), &query, variables); err != nil {
			return nil, err
		}
		pullRequests = append(pullRequests, query.Repository.PullRequests.Nodes.InRange(since, until)...)
		pageInfo := query.Repository.PullRequests.PageInfo
		if !pageInfo.HasNextPage {
			break
		}
		variables["after"] = githubv4.String(pageInfo.EndCursor)
	}
	return pullRequests, nil
}

type CommitData struct {
	AbbreviatedOid  string
	CommittedDate   time.Time
	MessageHeadline string
	Author          struct {
		Email string
	}
}

type CommitNodes []struct {
	Commit CommitData `graphql:"... on Commit"`
}

// Collects CommitData from a CommitNodes array
func (nodes CommitNodes) Commits() (commits []CommitData) {
	for _, node := range nodes {
		commits = append(commits, node.Commit)
	}
	return
}

// Returns commits within given time interval to default branch of specified repo
func repositoryCommits(client githubv4.Client, orgName string, repoName string, since time.Time, until time.Time) ([]CommitData, error) {
	var query struct {
		Repository struct {
			Name             string
			DefaultBranchRef struct {
				Target struct {
					Commit struct {
						History struct {
							TotalCount int
							PageInfo   struct {
								EndCursor   string
								HasNextPage bool
							}
							Nodes CommitNodes
						} `graphql:"history(first: 100, after: $after, since: $since, until: $until)"`
					} `graphql:"... on Commit"`
				}
			}
		} `graphql:"repository(owner: $owner, name: $name)"`
		RateLimit struct {
			Cost      int
			Limit     int
			Remaining int
			ResetAt   time.Time
		}
	}

	variables := map[string]interface{}{
		"owner": githubv4.String(orgName),
		"name":  githubv4.String(repoName),
		"after": (*githubv4.String)(nil), // first cursor is null
		"since": githubv4.GitTimestamp{Time: since},
		"until": githubv4.GitTimestamp{Time: until},
	}

	// Handle pagination
	var commits []CommitData
	for {
		if err := client.Query(context.Background(), &query, variables); err != nil {
			return nil, err
		}
		commits = append(commits, query.Repository.DefaultBranchRef.Target.Commit.History.Nodes.Commits()...)
		pageInfo := query.Repository.DefaultBranchRef.Target.Commit.History.PageInfo
		if !pageInfo.HasNextPage {
			break
		}
		variables["after"] = githubv4.String(pageInfo.EndCursor)
	}
	return commits, nil
}
