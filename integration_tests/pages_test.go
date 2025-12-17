package integrationtests

import (
	"bytes"
	"common/oyaml"
	"common/testutils/yarequire"
	"common/utils"
	"context"
	"fmt"
	"gitcore/internal/entities"
	grpc_marshalling "gitcore/internal/grpcserver/marshalling"
	"gitcore/internal/httpserver/pages"
	"gitcore/internal/testutils"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"os"
	"path"
	pb "private_api/generated/yandex/cloud/priv/gitcore/v1"
	"strings"
	"testing"

	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-resty/resty/v2"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/protobuf/types/known/fieldmaskpb"
)

func (suite *RwApiTestSuite) TestPages() {
	t := suite.T()

	repos := []*entities.Repository{}
	for i := 0; i < 13; i++ {
		repoID, _, _ := suite.makeRepoWithVisibility(t, suite.users.Admin, entities.Visibilities.Public)
		repo, err := suite.RepoRepo.GetRepositoryByID(context.Background(), repoID)
		require.NoError(t, err)
		repos = append(repos, repo)
	}

	indexHTML := "<!DOCTYPE html><html><body><h1>Title</h1></body></html>"
	textFile := "some text"
	rabbitJPG := suite.getOSFile("rabbit.jpg")

	repoWithoutConfig := repos[0]

	repoWithDefaultConfig := repos[1]
	suite.addTextFile(repoWithDefaultConfig, "main", oyaml.PagesPath, "\n")
	suite.addTextFile(repoWithDefaultConfig, "main", "some/path/index.html", indexHTML)
	suite.addTextFile(repoWithDefaultConfig, "main", "some/anotherpath/file.txt", textFile)
	suite.addFile(repoWithDefaultConfig, "main", "some/path/rabbit.jpg", rabbitJPG)

	repoWithInvalidConfig := repos[2]
	suite.addTextFile(repoWithInvalidConfig, "main", oyaml.PagesPath, "some invalid yaml")
	suite.addTextFile(repoWithInvalidConfig, "main", "some/path/index.html", indexHTML)

	repoWithCustomConfig := repos[3]
	suite.addTextFile(repoWithCustomConfig, "main", oyaml.PagesPath, `
site:
  root: /a/b
  ref: pages

`)
	suite.addTextFile(repoWithCustomConfig, "pages", "/a/b/index.html", indexHTML)

	repoPrivate := repos[4]
	suite.RepoService.UpdateVisibility(context.Background(), repoPrivate.OrgSlug, repoPrivate.Slug, entities.Visibilities.Private)
	suite.addTextFile(repoPrivate, "main", "some/path/index.html", indexHTML)

	repoLanding := repos[5]
	suite.cfg.Pages.LandingOrgSlug = repoLanding.OrgSlug
	suite.cfg.Pages.LandingRepoSlug = repoLanding.Slug
	suite.addTextFile(repoLanding, "main", oyaml.PagesPath, "")
	landingPage := "<!DOCTYPE html><html><body><h1>Landing</h1></body></html>"
	suite.addTextFile(repoLanding, "main", "index.html", landingPage)
	suite.addTextFile(repoLanding, "main", "/some/path/file.txt", textFile)

	repoWithOrgSlug := repos[6]
	err := suite.RepoRepo.UpdateRepository(repoWithOrgSlug.Slug).SetRepoSlug(repoWithOrgSlug.OrgSlug).Commit(context.Background())
	require.NoError(t, err)
	repoWithOrgSlug.Slug = repoWithOrgSlug.OrgSlug
	suite.addTextFile(repoWithOrgSlug, "main", oyaml.PagesPath, "")
	suite.addTextFile(repoWithOrgSlug, "main", "index.html", indexHTML)
	suite.addTextFile(repoWithOrgSlug, "main", "/some/path/file.txt", textFile)

	repoWithNotExistingRootPath := repos[7]
	suite.addTextFile(repoWithNotExistingRootPath, "main", oyaml.PagesPath, `
site:
  root: /a/b/c
`)

	repoWithNotExistingRef := repos[8]
	suite.addTextFile(repoWithNotExistingRef, "main", oyaml.PagesPath, `
site:
  root: /some/path
  ref: unknown
`)
	suite.addTextFile(repoWithNotExistingRef, "main", "some/path/index.html", indexHTML)

	repoWithRedirection := repos[9]
	suite.addTextFile(repoWithRedirection, "main", oyaml.PagesPath, "")
	suite.addTextFile(repoWithRedirection, "main", "/index.html", indexHTML)
	org, err := suite.OrgRepo.GetOrganizationByID(context.Background(), repoWithRedirection.OrgID)
	require.NoError(t, err)
	reservedSlug := org.Slug
	newSlug := reservedSlug + "-new"
	err = suite.OrgService.UpdateSlug(context.Background(), org, newSlug)
	require.NoError(t, err)

	repoWithRefAsTag := repos[10]
	suite.addTextFile(repoWithRefAsTag, "main", oyaml.PagesPath, `
site:
  ref: tag:some.tag

`)
	suite.addTextFile(repoWithRefAsTag, "main", "index.html", indexHTML)
	suite.createTag(suite.users.Admin, repoWithRefAsTag, plumbing.Main, "some.tag")

	repoWithRefAsHash := repos[11]
	suite.addTextFile(repoWithRefAsHash, "main", "index.html", indexHTML)
	commits := suite.getCommitHashes(t, suite.RepoURL(repoWithRefAsHash.OrgSlug, repoWithRefAsHash.Slug), suite.users.Admin)
	require.Len(t, commits, 1)
	suite.addTextFile(repoWithRefAsHash, "main", oyaml.PagesPath, fmt.Sprintf(`
site:
  ref: %s

`, commits[0].String()))

	repoWithDottedRootPath := repos[12]
	suite.addTextFile(repoWithDottedRootPath, "main", oyaml.PagesPath, `
site:
  root: /some/.path
`)
	suite.addTextFile(repoWithDottedRootPath, "main", "some/.path/index.html", indexHTML)

	tests := []struct {
		name           string
		host           string
		path           string
		expStatus      int
		expContent     []byte
		expContentType string

		apiMethod string
	}{
		{
			name:           "invalid api call",
			host:           "sourcecraft.site",
			apiMethod:      "POST",
			expStatus:      http.StatusNotFound,
			expContent:     pages.NotFoundResponse,
			expContentType: "text/html; charset=utf-8",
		},
		{
			name:           "invalid hostname",
			host:           "invalid.host",
			expStatus:      http.StatusNotFound,
			expContent:     pages.NotFoundResponse,
			expContentType: "text/html; charset=utf-8",
		},
		{
			name:           "invalid path",
			host:           repoWithoutConfig.OrgSlug + ".sourcecraft.site",
			path:           repoWithoutConfig.Slug + "/.path",
			expStatus:      http.StatusNotFound,
			expContent:     pages.NotFoundResponse,
			expContentType: "text/html; charset=utf-8",
		},
		{
			name:           "invalid root path",
			host:           repoWithDottedRootPath.OrgSlug + ".sourcecraft.site",
			path:           repoWithDottedRootPath.Slug,
			expStatus:      http.StatusNotFound,
			expContent:     pages.NotFoundResponse,
			expContentType: "text/html; charset=utf-8",
		},
		{
			name:           "no config",
			host:           repoWithoutConfig.OrgSlug + ".sourcecraft.site",
			path:           repoWithoutConfig.Slug,
			expStatus:      http.StatusNotFound,
			expContent:     pages.NotFoundResponse,
			expContentType: "text/html; charset=utf-8",
		},
		{
			name:           "invalid config",
			host:           repoWithInvalidConfig.OrgSlug + ".sourcecraft.site",
			path:           repoWithInvalidConfig.Slug + "/some/path/index.html",
			expStatus:      http.StatusNotFound,
			expContent:     pages.NotFoundResponse,
			expContentType: "text/html; charset=utf-8",
		},
		{
			name:           "not existing org",
			host:           "abc.sourcecraft.site",
			path:           repoWithDefaultConfig.Slug + "/some/path",
			expStatus:      http.StatusNotFound,
			expContent:     pages.NotFoundResponse,
			expContentType: "text/html; charset=utf-8",
		},
		{
			name:           "not existing repo",
			host:           repoWithDefaultConfig.OrgSlug + ".sourcecraft.site",
			path:           "abc" + "/some/path",
			expStatus:      http.StatusNotFound,
			expContent:     pages.NotFoundResponse,
			expContentType: "text/html; charset=utf-8",
		},
		{
			name:           "not existing path",
			host:           repoWithDefaultConfig.OrgSlug + ".sourcecraft.site",
			path:           repoWithDefaultConfig.Slug + "/some/unknown/path",
			expStatus:      http.StatusNotFound,
			expContent:     pages.NotFoundResponse,
			expContentType: "text/html; charset=utf-8",
		},
		{
			name:           "not existing root path",
			host:           repoWithNotExistingRootPath.OrgSlug + ".sourcecraft.site",
			path:           repoWithNotExistingRootPath.Slug,
			expStatus:      http.StatusNotFound,
			expContent:     pages.NotFoundResponse,
			expContentType: "text/html; charset=utf-8",
		},
		{
			name:           "not existing ref",
			host:           repoWithNotExistingRef.OrgSlug + ".sourcecraft.site",
			path:           repoWithNotExistingRef.Slug + "/some/path",
			expStatus:      http.StatusNotFound,
			expContent:     pages.NotFoundResponse,
			expContentType: "text/html; charset=utf-8",
		},
		{
			name:           "private repo",
			host:           repoPrivate.OrgSlug + ".sourcecraft.site",
			path:           repoPrivate.Slug + "/some/path/index.html",
			expStatus:      http.StatusNotFound,
			expContent:     pages.NotFoundResponse,
			expContentType: "text/html; charset=utf-8",
		},
		{
			name:           "landing page",
			host:           "sourcecraft.site",
			path:           "",
			expStatus:      http.StatusOK,
			expContent:     []byte(landingPage),
			expContentType: "text/html; charset=utf-8",
		},
		{
			name:           "landing page and path",
			host:           "sourcecraft.site",
			path:           "/some/path/file.txt",
			expStatus:      http.StatusOK,
			expContent:     []byte(textFile),
			expContentType: "text/plain; charset=utf-8",
		},
		{
			name:           "path to folder, no index.html",
			host:           repoWithDefaultConfig.OrgSlug + ".sourcecraft.site",
			path:           repoWithDefaultConfig.Slug + "/some/anotherpath/",
			expStatus:      http.StatusNotFound,
			expContent:     pages.NotFoundResponse,
			expContentType: "text/html; charset=utf-8",
		},
		{
			name:           "path to folder, has index.html",
			host:           repoWithDefaultConfig.OrgSlug + ".sourcecraft.site",
			path:           repoWithDefaultConfig.Slug + "/some/path/",
			expStatus:      http.StatusOK,
			expContent:     []byte(indexHTML),
			expContentType: "text/html; charset=utf-8",
		},
		{
			name:           "path to folder, no trailing slash, redirect",
			host:           repoWithDefaultConfig.OrgSlug + ".sourcecraft.site",
			path:           repoWithDefaultConfig.Slug + "/some/path",
			expStatus:      http.StatusFound,
			expContent:     []byte(indexHTML),
			expContentType: "text/html; charset=utf-8",
		},
		{
			name:           "path contains invalid symbols",
			host:           repoWithDefaultConfig.OrgSlug + ".sourcecraft.site",
			path:           repoWithDefaultConfig.Slug + "/some/path%ff",
			expStatus:      http.StatusNotFound,
			expContent:     pages.NotFoundResponse,
			expContentType: "text/html; charset=utf-8",
		},
		{
			name:           "repo_slug is org_slug",
			host:           repoWithOrgSlug.OrgSlug + ".sourcecraft.site",
			path:           "",
			expStatus:      http.StatusOK,
			expContent:     []byte(indexHTML),
			expContentType: "text/html; charset=utf-8",
		},
		{
			name:           "repo_slug is org_slug, not empty path",
			host:           repoWithOrgSlug.OrgSlug + ".sourcecraft.site",
			path:           "/some/path/file.txt",
			expStatus:      http.StatusOK,
			expContent:     []byte(textFile),
			expContentType: "text/plain; charset=utf-8",
		},
		{
			name:           "path to file",
			host:           repoWithDefaultConfig.OrgSlug + ".sourcecraft.site",
			path:           repoWithDefaultConfig.Slug + "/some/anotherpath/file.txt",
			expStatus:      http.StatusOK,
			expContent:     []byte(textFile),
			expContentType: "text/plain; charset=utf-8",
		},
		{
			name:           "path to image",
			host:           repoWithDefaultConfig.OrgSlug + ".sourcecraft.site",
			path:           repoWithDefaultConfig.Slug + "/some/path/rabbit.jpg",
			expStatus:      http.StatusOK,
			expContent:     rabbitJPG,
			expContentType: "image/jpeg",
		},
		{ // The Host header is sent in lowercase, but let's leave it as reminder
			name:           "case insensitive host",
			host:           strings.ToUpper(repoWithDefaultConfig.OrgSlug + ".sourcecraft.site"),
			path:           repoWithDefaultConfig.Slug + "/some/anotherpath/file.txt",
			expStatus:      http.StatusOK,
			expContent:     []byte(textFile),
			expContentType: "text/plain; charset=utf-8",
		},
		{
			name:           "case insensitive repo",
			host:           repoWithDefaultConfig.OrgSlug + ".sourcecraft.site",
			path:           strings.ToUpper(repoWithDefaultConfig.Slug) + "/some/anotherpath/file.txt",
			expStatus:      http.StatusOK,
			expContent:     []byte(textFile),
			expContentType: "text/plain; charset=utf-8",
		},
		{
			name:           "case sensitive path",
			host:           repoWithDefaultConfig.OrgSlug + ".sourcecraft.site",
			path:           repoWithDefaultConfig.Slug + "/some/anotherpath/File.txt",
			expStatus:      http.StatusNotFound,
			expContent:     pages.NotFoundResponse,
			expContentType: "text/html; charset=utf-8",
		},
		{
			name:           "custom config",
			host:           repoWithCustomConfig.OrgSlug + ".sourcecraft.site",
			path:           repoWithCustomConfig.Slug + "/",
			expStatus:      http.StatusOK,
			expContent:     []byte(indexHTML),
			expContentType: "text/html; charset=utf-8",
		},
		{
			name:           "reserved org slug is redirected",
			host:           reservedSlug + ".sourcecraft.site",
			path:           repoWithRedirection.Slug + "/",
			expStatus:      http.StatusFound,
			expContent:     []byte(indexHTML),
			expContentType: "text/html; charset=utf-8",
		},
		{
			name:           "tag as reference",
			host:           repoWithRefAsTag.OrgSlug + ".sourcecraft.site",
			path:           repoWithRefAsTag.Slug + "/",
			expStatus:      http.StatusOK,
			expContent:     []byte(indexHTML),
			expContentType: "text/html; charset=utf-8",
		},
		{
			name:           "hash as reference",
			host:           repoWithRefAsHash.OrgSlug + ".sourcecraft.site",
			path:           repoWithRefAsHash.Slug + "/",
			expStatus:      http.StatusOK,
			expContent:     []byte(indexHTML),
			expContentType: "text/html; charset=utf-8",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			method := "GET"
			if tc.apiMethod != "" {
				method = tc.apiMethod
			}

			resp, err := suite.pagesClient.RWoAuth().
				SetHeader("Host", tc.host).Execute(method, tc.path)

			if tc.expStatus == http.StatusFound {
				require.ErrorIs(t, err, resty.ErrAutoRedirectDisabled)
				require.Equal(t, tc.expStatus, resp.StatusCode())
				redirectURL := resp.Header().Get("Location")

				url, err := url.Parse(redirectURL)
				require.NoError(t, err)

				resp, err = suite.pagesClient.RWoAuth().
					SetHeader("Host", url.Hostname()).Execute(method, url.Path)
				require.NoError(t, err)
				require.Equal(t, http.StatusOK, resp.StatusCode())
			} else {
				require.NoError(t, err)
				require.Equal(t, tc.expStatus, resp.StatusCode())
			}
			if len(tc.expContent) != 0 {
				require.Equal(t, tc.expContent, resp.Body())
			}
			if tc.expContentType != "" {
				require.Equal(t, tc.expContentType, resp.Header().Get("Content-Type"))
			}
			require.Equal(t, "frame-ancestors 'none'; worker-src 'none';", resp.Header().Get("Content-Security-Policy"))
		})
	}
}

func (suite *RwApiTestSuite) TestPagesInfo() {
	t := suite.T()

	repos := []*entities.Repository{}
	for i := 0; i < 3; i++ {
		repoID, _, _ := suite.makeRepoWithVisibility(t, suite.users.Admin, entities.Visibilities.Public)
		repo, err := suite.RepoRepo.GetRepositoryByID(context.Background(), repoID)
		require.NoError(t, err)
		repos = append(repos, repo)
	}

	repoWithoutConfig := repos[0]

	repoWithConfig := repos[1]
	suite.addTextFile(repoWithConfig, "main", oyaml.PagesPath, "")

	repoWithInvalidConfig := repos[2]
	suite.addTextFile(repoWithInvalidConfig, "main", oyaml.PagesPath, "root: /")

	tests := []struct {
		name       string
		repoID     uint64
		expStatus  codes.Code
		expURL     *string
		expEnabled bool
	}{
		{
			name:       "repo without config",
			repoID:     repoWithoutConfig.ID,
			expStatus:  codes.OK,
			expEnabled: false,
		},
		{
			name:       "repo with invalid config",
			repoID:     repoWithInvalidConfig.ID,
			expStatus:  codes.OK,
			expEnabled: false,
		},
		{
			name:       "repo with config",
			repoID:     repoWithConfig.ID,
			expStatus:  codes.OK,
			expEnabled: true,
			expURL:     utils.PtrFromValue("https://" + repoWithConfig.OrgSlug + ".sourcecraft.site/" + repoWithConfig.Slug),
		},
	}

	client := pb.NewPagesServiceClient(suite.grpcClient)
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ctx := testutils.AuthorizeGRPC(suite.users.Admin.Identity)
			resp, err := client.GetInfo(ctx, &pb.GetPagesInfoRequest{
				Id: grpc_marshalling.IDInverse(tc.repoID),
			})
			yarequire.ProtoStatusEqual(t, tc.expStatus, err)
			if tc.expStatus != codes.OK {
				return
			}
			require.NoError(t, err)
			require.Equal(t, tc.expEnabled, resp.Enabled)
			require.Equal(t, tc.expURL, resp.Url)
		})
	}
}

func (suite *RwApiTestSuite) TestPagesStats() {
	t := suite.T()

	repoID, _, _ := suite.makeRepoWithVisibility(t, suite.users.Admin, entities.Visibilities.Public)
	repo, err := suite.RepoRepo.GetRepositoryByID(context.Background(), repoID)
	require.NoError(t, err)

	check := func(expStatus bool) {
		stats, err := suite.RepoStatsRepo.Get(context.Background(), repoID)
		require.NoError(t, err)
		require.Equal(t, expStatus, stats.HasPages)
	}

	// empty repo, no pages
	check(false)

	// add some file, no pages
	suite.addTextFile(repo, "main", "some/file.txt", "file")
	check(false)

	// add some file to another branch, no pages
	suite.addTextFile(repo, "test", "some/anotherfile.txt", "file")
	check(false)

	// add invaid config, no pages
	suite.addTextFile(repo, "main", oyaml.PagesPath, "some invalid yaml")
	check(false)

	// add valid config, has pages
	suite.addTextFile(repo, "main", oyaml.PagesPath, "\n")
	check(true)

	// delete config, no pages
	suite.deleteFile(repo, "main", oyaml.PagesPath)
	check(false)

	// add config again, has pages
	suite.addTextFile(repo, "main", oyaml.PagesPath, "\n")
	check(true)

	// switch default branch, no pages
	client := pb.NewRepoServiceClient(suite.grpcClient)
	_, err = client.Update(testutils.AuthorizeGRPC(suite.users.Admin.Identity), &pb.UpdateRepositoryRequest{
		Id:            grpc_marshalling.IDInverse(repoID),
		DefaultBranch: "test",
		UpdateMask:    &fieldmaskpb.FieldMask{Paths: []string{"default_branch"}},
	})
	require.NoError(t, err)
	check(false)

	// add config again, has pages
	suite.addTextFile(repo, "test", oyaml.PagesPath, "\n")
	check(true)

	// switch default branch back, has pages
	_, err = client.Update(testutils.AuthorizeGRPC(suite.users.Admin.Identity), &pb.UpdateRepositoryRequest{
		Id:            grpc_marshalling.IDInverse(repoID),
		DefaultBranch: "main",
		UpdateMask:    &fieldmaskpb.FieldMask{Paths: []string{"default_branch"}},
	})
	require.NoError(t, err)
	check(true)
}

func (suite *RwApiTestSuite) addTextFile(repo *entities.Repository, ref string, filePath string, content string) {
	suite.addFile(repo, ref, filePath, []byte(content))
}

func (suite *RwApiTestSuite) addFile(repo *entities.Repository, ref string, filePath string, content []byte) {
	t := suite.T()

	protocol := suite.HTTPSProtocol()
	tmpDir := testutils.TempDir(t, "", "")
	w := testutils.NewWorkdir(t, tmpDir)
	cg := protocol.PrepareCGit(w, suite.users.Admin.Identity)
	cg.Must(t, "clone", protocol.RepoURL(repo.OrgSlug, repo.Slug), repo.Slug)
	w = testutils.NewWorkdir(t, path.Join(tmpDir, repo.Slug))

	cg = protocol.PrepareCGit(w, suite.users.Admin.Identity).WithAuthTokenSite(protocol.RepoURL(repo.OrgSlug, repo.Slug))
	_, _, err := cg.Exec("checkout", "-b", ref)
	if err != nil {
		cg.Must(t, "checkout", ref)
	}

	dir := path.Dir(filePath)
	if dir != "." {
		require.NoError(t, os.MkdirAll(path.Join(cg.Path(), dir), 0755))
	}
	nf, err := os.Create(path.Join(cg.Path(), filePath))
	require.NoError(t, err)

	_, err = nf.Write(content)
	require.NoError(t, err)
	require.NoError(t, nf.Close())

	suite.commitAll(cg, ".", ref)
}

func (suite *RwApiTestSuite) deleteFile(repo *entities.Repository, ref string, filePath string) {
	t := suite.T()

	protocol := suite.HTTPSProtocol()
	tmpDir := testutils.TempDir(t, "", "")
	w := testutils.NewWorkdir(t, tmpDir)
	cg := protocol.PrepareCGit(w, suite.users.Admin.Identity)
	cg.Must(t, "clone", protocol.RepoURL(repo.OrgSlug, repo.Slug), repo.Slug)
	w = testutils.NewWorkdir(t, path.Join(tmpDir, repo.Slug))

	cg = protocol.PrepareCGit(w, suite.users.Admin.Identity).WithAuthTokenSite(protocol.RepoURL(repo.OrgSlug, repo.Slug))
	_, _, err := cg.Exec("checkout", "-b", ref)
	if err != nil {
		cg.Must(t, "checkout", ref)
	}

	err = os.RemoveAll(path.Join(cg.Path(), filePath))
	require.NoError(t, err)

	suite.commitAll(cg, ".", ref)
}

func (suite *RwApiTestSuite) getOSFile(fileName string) []byte {
	t := suite.T()

	file, err := os.Open(testutils.GetAttachmentPath(fileName))
	require.NoError(t, err)
	defer func(file *os.File) {
		if err = file.Close(); err != nil {
			t.Error(err)
		}
	}(file)

	buf := bytes.Buffer{}
	writer := multipart.NewWriter(&buf)
	fileWriter, err := writer.CreateFormFile("file", fileName)
	require.NoError(t, err)
	_, err = io.Copy(fileWriter, file)
	require.NoError(t, err)
	err = writer.Close()
	require.NoError(t, err)

	return buf.Bytes()
}
