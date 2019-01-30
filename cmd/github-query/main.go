package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/shurcooL/githubv4"
	"golang.org/x/oauth2"
)

var (
	listName string
	orgName  string
	repoName string
	sinceStr string
	untilStr string
)

func init() {
	flag.StringVar(&listName, "list", "", "<repos|commits|pull-requests> (Required)")
	flag.StringVar(&orgName, "org", "", "Organization name (Required)")
	flag.StringVar(&repoName, "repo", "", "Repository name (Required except to list repos)")
	flag.StringVar(&sinceStr, "since", "", "Start of date range (YYYY-MM-DD)")
	flag.StringVar(&untilStr, "until", "", "End of date range (YYYY-MM-DD)")
}

func main() {
	flag.Parse()
	// TODO: complain about unused args

	if orgName == "" {
		fmt.Fprintln(os.Stderr, "Missing option: org")
		printUsage()
	}

	var since = parseDate(sinceStr, time.Date(1970, 1, 1, 0, 0, 0, 0, time.UTC))
	var until = parseDate(untilStr, time.Now())

	token, ok := os.LookupEnv("GITHUB_TOKEN")
	if !ok {
		fmt.Fprintln(os.Stderr, "Missing environment variable: GITHUB_TOKEN")
		printUsage()
	}
	tokenSource := oauth2.StaticTokenSource(&oauth2.Token{AccessToken: token})
	client := githubv4.NewClient(oauth2.NewClient(context.Background(), tokenSource))

	var cmd func() (interface{}, error)
	switch listName {
	case "repos":
		// TODO: unused repoName, since and until
		cmd = func() (interface{}, error) {
			return organizationRepositoryNames(*client, orgName)
		}
		break
	case "commits":
		requireRepoName()
		cmd = func() (interface{}, error) {
			return repositoryCommits(*client, orgName, repoName, since, until)
		}
		break
	case "pull-requests":
		requireRepoName()
		cmd = func() (interface{}, error) {
			return repositoryPullRequests(*client, orgName, repoName, since, until)
		}
		break
	default:
		printUsage()
	}

	res, err := cmd()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	printJSON(res)
}

// Prints usage and exits
func printUsage() {
	flag.Usage()
	fmt.Fprintln(os.Stderr, "\nThe GITHUB_TOKEN environment variable is required.")
	os.Exit(1)
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

// Bails if repoName option is empty
func requireRepoName() {
	if repoName == "" {
		fmt.Fprintln(os.Stderr, "Missing option: repo")
		printUsage()
	}
}

// Prints v as JSON to stdout. Panics on any error.
func printJSON(v interface{}) {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	err := enc.Encode(v)
	if err != nil {
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
		err := client.Query(context.Background(), &query, variables)
		if err != nil {
			return nil, err
		}
		names = append(names, query.Organization.Repositories.Nodes.Names()...)
		if !query.Organization.Repositories.PageInfo.HasNextPage {
			break
		}
		variables["after"] = githubv4.String(query.Organization.Repositories.PageInfo.EndCursor)
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
		err := client.Query(context.Background(), &query, variables)
		if err != nil {
			return nil, err
		}
		pullRequests = append(pullRequests, query.Repository.PullRequests.Nodes.InRange(since, until)...)
		if !query.Repository.PullRequests.PageInfo.HasNextPage {
			break
		}
		variables["after"] = githubv4.String(query.Repository.PullRequests.PageInfo.EndCursor)
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
		err := client.Query(context.Background(), &query, variables)
		if err != nil {
			return nil, err
		}
		commits = append(commits, query.Repository.DefaultBranchRef.Target.Commit.History.Nodes.Commits()...)
		if !query.Repository.DefaultBranchRef.Target.Commit.History.PageInfo.HasNextPage {
			break
		}
		variables["after"] = githubv4.String(query.Repository.DefaultBranchRef.Target.Commit.History.PageInfo.EndCursor)
	}
	return commits, nil
}
