package gitlab

import (
	"os"
	"path/filepath"

	"github.com/xanzy/go-gitlab"
)

/*
 * GitLab Client Interfaces & Wrapper
 */

// GitLab Repository Service Interface
type gitlabRepositoriesService interface {
	ListTree(pid interface{}, opt *gitlab.ListTreeOptions, options ...gitlab.OptionFunc) ([]*gitlab.TreeNode, *gitlab.Response, error)
}

// GitLab Branches Service Interface
type gitlabBranchesService interface {
	GetBranch(pid interface{}, branch string, options ...gitlab.OptionFunc) (*gitlab.Branch, *gitlab.Response, error)
}

// GitLab RepositoryFiles Service Interface
type gitlabRepositoryFilesService interface {
	GetRawFile(pid interface{}, fileName string, opt *gitlab.GetRawFileOptions, options ...gitlab.OptionFunc) ([]byte, *gitlab.Response, error)
}

// GitHub Client Wrapper
type GitLabClient struct {
	GitLabClient    *gitlab.Client
	Repositories    gitlabRepositoriesService
	Branches        gitlabBranchesService
	RepositoryFiles gitlabRepositoryFilesService
}

func NewGitLabClient(token string, isMock bool) *GitLabClient {
	if isMock {
		// return mock gitlab client for testing purposes
		workDir, _ := os.Getwd()
		testFileFetcher := localFileFetcher{
			SourceDir: filepath.Join(workDir, "..", "..", "..", "..", "gitlab-mock"),
			FileExt:   ".json",
		}
		return &GitLabClient{
			GitLabClient:    nil,
			Repositories:    &mockRepositoriesService{client: testFileFetcher},
			Branches:        &mockBranchesService{client: testFileFetcher},
			RepositoryFiles: &mockRepositoryFilesService{client: testFileFetcher},
		}
	} else {
		// return GitLab API client with/without authentication
		client := gitlab.NewClient(nil, token)

		return &GitLabClient{
			GitLabClient:    client,
			Repositories:    client.Repositories,
			Branches:        client.Branches,
			RepositoryFiles: client.RepositoryFiles,
		}
	}
}
