package integrationtests

import (
	"common/testutils/yarequire"
	"common/utils"
	"common/utils/rolesgenerator/iam"
	"context"
	"fmt"
	"gitcore/internal/entities"
	git_utils "gitcore/internal/git/utils"
	grpc_marshalling "gitcore/internal/grpcserver/marshalling"
	"gitcore/internal/httpserver/httperrors"
	"gitcore/internal/httpserver/schemas"
	"gitcore/internal/interfaces"
	"gitcore/internal/testutils"
	"math"
	"os"
	"path"
	pb "private_api/generated/yandex/cloud/priv/gitcore/v1"
	"testing"

	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-resty/resty/v2"
	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
)

func (suite *RwApiTestSuite) TestLfsObject_BatchUploadAndDownload() {
	t := suite.T()
	ctx := context.Background()

	suite.addRole(t, suite.users.Kopatych, suite.repos.Alpha, iam.Roles.RepositoriesDeveloper)

	repoID := suite.repos.Alpha.ID
	lfsRepo := suite.LFSRepoFactory.Build(repoID, nil)

	data := []byte("some data")
	objects := []*schemas.LfsObject{
		{
			Oid:  "123",
			Size: uint64(len(data)),
		},
		{
			Oid:  "456",
			Size: uint64(len(data)),
		},
	}
	objEntities := []*entities.LFSObject{
		{
			RepoID:         repoID,
			Hash:           plumbing.NewHash("11111111111111111111"),
			LFSPointerInfo: entities.LFSPointerInfo{OID: "123", Size: uint64(len(data))},
		},
		{
			RepoID:         repoID,
			Hash:           plumbing.NewHash("22222222222222222222"),
			LFSPointerInfo: entities.LFSPointerInfo{OID: "456", Size: uint64(len(data))},
		},
	}

	t.Run("upload", func(t *testing.T) {

		req := &schemas.LfsObjectBatchRequest{
			Operation: schemas.LfsObjectOperationTypes.Upload,
			Objects:   objects,
		}

		res := &schemas.LfsObjectBatchResponse{}
		testutils.Expect(suite.gitClient.AsBasic(suite.users.Kopatych).
			SetHeader(echo.HeaderContentType, "application/vnd.git-lfs+json").
			SetResult(res).
			SetBody(req).
			Post("/yandex/alpha.git/info/lfs/objects/batch")).
			MustBe(t, 200)

		require.Equal(t, schemas.LfsObjectTransferTypes.Basic, res.Transfer)
		require.Equal(t, 2, len(res.Objects))

		httpClient := resty.New()
		for _, objResp := range res.Objects {
			require.Nil(t, objResp.Error)
			require.Equal(t, 1, len(objResp.Actions))

			href := objResp.Actions[schemas.LfsObjectOperationTypes.Upload].Href

			testutils.Expect(httpClient.NewRequest().
				SetHeader(echo.HeaderContentType, echo.MIMEOctetStream).
				SetBody(data).
				Put(href)).
				MustBe(t, 200)
		}

		require.NoError(t, lfsRepo.CreateLFSObjectsBulk(ctx, objEntities))
	})

	t.Run("upload second time", func(t *testing.T) {
		req := &schemas.LfsObjectBatchRequest{
			Operation: schemas.LfsObjectOperationTypes.Upload,
			Objects:   objects,
		}

		res := &schemas.LfsObjectBatchResponse{}
		testutils.Expect(suite.gitClient.AsBasic(suite.users.Kopatych).
			SetHeader(echo.HeaderContentType, "application/vnd.git-lfs+json").
			SetResult(res).
			SetBody(req).
			Post("/yandex/alpha.git/info/lfs/objects/batch")).
			MustBe(t, 200)

		require.Equal(t, schemas.LfsObjectTransferTypes.Basic, res.Transfer)
		require.Equal(t, 2, len(res.Objects))

		for _, objResp := range res.Objects {
			require.Nil(t, objResp.Error)
			require.Equal(t, 0, len(objResp.Actions))
		}
	})

	t.Run("download", func(t *testing.T) {
		req := &schemas.LfsObjectBatchRequest{
			Operation: schemas.LfsObjectOperationTypes.Download,
			Objects:   objects,
		}

		res := &schemas.LfsObjectBatchResponse{}
		testutils.Expect(suite.gitClient.AsBasic(suite.users.Kopatych).
			SetHeader(echo.HeaderContentType, "application/vnd.git-lfs+json").
			SetResult(res).
			SetBody(req).
			Post("/yandex/alpha.git/info/lfs/objects/batch")).
			MustBe(t, 200)

		require.Equal(t, schemas.LfsObjectTransferTypes.Basic, res.Transfer)
		require.Equal(t, 2, len(res.Objects))

		httpClient := resty.New()
		for _, objResp := range res.Objects {
			require.Nil(t, objResp.Error)
			require.Equal(t, 1, len(objResp.Actions))

			href := objResp.Actions[schemas.LfsObjectOperationTypes.Download].Href

			resp, err := httpClient.NewRequest().
				SetHeader(echo.HeaderAccept, echo.MIMEOctetStream).
				Get(href)

			require.NoError(t, err)
			require.Equal(t, 200, resp.StatusCode())
			require.EqualValues(t, []byte("some data"), resp.Body())
		}
	})

	t.Run("download not existing", func(t *testing.T) {
		req := &schemas.LfsObjectBatchRequest{
			Operation: schemas.LfsObjectOperationTypes.Download,
			Objects: []*schemas.LfsObject{
				{
					Oid:  "789",
					Size: 123,
				},
			},
		}

		res := &schemas.LfsObjectBatchResponse{}
		testutils.Expect(suite.gitClient.AsBasic(suite.users.Kopatych).
			SetHeader(echo.HeaderContentType, "application/vnd.git-lfs+json").
			SetResult(res).
			SetBody(req).
			Post("/yandex/alpha.git/info/lfs/objects/batch")).
			MustBe(t, 200)

		require.Equal(t, schemas.LfsObjectTransferTypes.Basic, res.Transfer)
		require.Equal(t, 1, len(res.Objects))

		for _, objResp := range res.Objects {
			require.NotNil(t, objResp.Error)
			require.Equal(t, 404, objResp.Error.Code)
			require.Equal(t, "Object does not exist", objResp.Error.Message)
			require.Equal(t, 0, len(objResp.Actions))
		}
	})

	t.Run("quota", func(t *testing.T) {
		suite.repoQuotaChecker.DisableBypass()
		defer suite.repoQuotaChecker.EnableBypass()

		cancel := suite.setQuotaLimit(t, suite.repos.Alpha.OrgID, entities.Quotas.LFSStorageSize, 50)
		defer cancel()

		objects = []*schemas.LfsObject{
			{
				Oid:  "789",
				Size: 100,
			},
		}

		req := &schemas.LfsObjectBatchRequest{
			Operation: schemas.LfsObjectOperationTypes.Upload,
			Objects:   objects,
		}

		res := &schemas.LfsObjectBatchResponse{}
		testutils.Expect(suite.gitClient.AsBasic(suite.users.Kopatych).
			SetHeader(echo.HeaderContentType, "application/vnd.git-lfs+json").
			SetResult(res).
			SetBody(req).
			Post("/yandex/alpha.git/info/lfs/objects/batch")).
			MustBe(t, 429)

		// increase limit
		suite.setQuotaLimit(t, suite.repos.Alpha.OrgID, entities.Quotas.LFSStorageSize, math.MaxInt32)

		testutils.Expect(suite.gitClient.AsBasic(suite.users.Kopatych).
			SetHeader(echo.HeaderContentType, "application/vnd.git-lfs+json").
			SetResult(res).
			SetBody(req).
			Post("/yandex/alpha.git/info/lfs/objects/batch")).
			MustBe(t, 200)
		require.NoError(t, lfsRepo.CreateLFSObjectsBulk(ctx, []*entities.LFSObject{
			{
				RepoID:         repoID,
				Hash:           plumbing.NewHash("33333333333333333333"),
				LFSPointerInfo: entities.LFSPointerInfo{OID: "789", Size: 100},
			},
		}))
		objs, err := lfsRepo.GetAllLFSObjects(ctx)
		require.NoError(t, err)
		require.NotNil(t, objs)
	})

	t.Run("download/upload with forks", func(t *testing.T) {

		// create fork
		forkID, err := suite.RepoRepo.CreateRepository(ctx, &interfaces.CreateRepositoryArgs{
			Name:         "alpha-fork",
			Slug:         "alpha-fork",
			OrgID:        suite.repos.Alpha.OrgID,
			CreatedBy:    suite.users.Kopatych.ID,
			Visibility:   entities.Visibilities.Public,
			ForkOriginID: utils.PtrFromValue(repoID),
		})
		require.NoError(t, err)
		fork, err := suite.RepoRepo.GetRepositoryByID(ctx, forkID)
		require.NoError(t, err)

		suite.addRole(t, suite.users.Kopatych, fork, iam.Roles.RepositoriesDeveloper)

		chain, err := suite.RepoRepo.GetForkOriginsChain(ctx, forkID)
		require.NoError(t, err)
		forkedLfsRepo := suite.LFSRepoFactory.Build(forkID, chain)

		// upload to fork
		req := &schemas.LfsObjectBatchRequest{
			Operation: schemas.LfsObjectOperationTypes.Upload,
			Objects:   []*schemas.LfsObject{{Oid: "999", Size: uint64(len(data))}},
		}
		res := &schemas.LfsObjectBatchResponse{}
		testutils.Expect(suite.gitClient.AsBasic(suite.users.Kopatych).
			SetHeader(echo.HeaderContentType, "application/vnd.git-lfs+json").
			SetResult(res).
			SetBody(req).
			Post("/yandex/alpha-fork.git/info/lfs/objects/batch")).
			MustBe(t, 200)
		require.Equal(t, 1, len(res.Objects))

		href := res.Objects[0].Actions[schemas.LfsObjectOperationTypes.Upload].Href
		httpClient := resty.New()
		testutils.Expect(httpClient.NewRequest().
			SetHeader(echo.HeaderContentType, echo.MIMEOctetStream).
			SetBody(data).
			Put(href)).
			MustBe(t, 200)

		require.NoError(t, forkedLfsRepo.CreateLFSObjectsBulk(ctx, []*entities.LFSObject{
			{
				RepoID:         forkID,
				Hash:           plumbing.NewHash("44444444444444444444"),
				LFSPointerInfo: entities.LFSPointerInfo{OID: "999", Size: uint64(len(data))},
			},
		}))

		// get all objects through fork (should be OK for all)
		req = &schemas.LfsObjectBatchRequest{
			Operation: schemas.LfsObjectOperationTypes.Download,
			Objects: []*schemas.LfsObject{
				{Oid: "123", Size: uint64(len(data))},
				{Oid: "456", Size: uint64(len(data))},
				{Oid: "999", Size: uint64(len(data))},
			},
		}
		res = &schemas.LfsObjectBatchResponse{}
		testutils.Expect(suite.gitClient.AsBasic(suite.users.Kopatych).
			SetHeader(echo.HeaderContentType, "application/vnd.git-lfs+json").
			SetResult(res).
			SetBody(req).
			Post("/yandex/alpha-fork.git/info/lfs/objects/batch")).
			MustBe(t, 200)
		require.Equal(t, schemas.LfsObjectTransferTypes.Basic, res.Transfer)
		require.Equal(t, 3, len(res.Objects))
		for _, objResp := range res.Objects {
			require.Nil(t, objResp.Error)
		}

		// get object from fork through original (should fail with 404)
		req = &schemas.LfsObjectBatchRequest{
			Operation: schemas.LfsObjectOperationTypes.Download,
			Objects:   []*schemas.LfsObject{{Oid: "999", Size: uint64(len(data))}},
		}
		res = &schemas.LfsObjectBatchResponse{}
		testutils.Expect(suite.gitClient.AsBasic(suite.users.Kopatych).
			SetHeader(echo.HeaderContentType, "application/vnd.git-lfs+json").
			SetResult(res).
			SetBody(req).
			Post("/yandex/alpha.git/info/lfs/objects/batch")).
			MustBe(t, 200)
		require.Equal(t, schemas.LfsObjectTransferTypes.Basic, res.Transfer)
		require.Equal(t, 1, len(res.Objects))
		require.NotNil(t, res.Objects[0].Error)
		require.Equal(t, 404, res.Objects[0].Error.Code)
		require.Equal(t, 0, len(res.Objects[0].Actions))
	})
}

func (suite *RwApiTestSuite) TestLfsObject_Validation() {
	t := suite.T()

	objects := []*schemas.LfsObject{
		{
			Oid:  "123",
			Size: 123,
		},
		{
			Oid:  "456",
			Size: 456,
		},
	}

	t.Run("no auth", func(t *testing.T) {
		req := &schemas.LfsObjectBatchRequest{
			Operation: schemas.LfsObjectOperationTypes.Upload,
			Objects:   objects,
		}

		httpErr := &httperrors.APIError{}
		testutils.Expect(suite.gitClient.RWoAuth().
			SetHeader(echo.HeaderContentType, "application/vnd.git-lfs+json").
			SetError(httpErr).
			SetBody(req).
			Post("/yandex/alpha.git/info/lfs/objects/batch")).
			MustBe(t, 401)
	})

	t.Run("auth matrix", func(t *testing.T) {
		for _, repository := range suite.AllAuthRepos {
			suite.AuthMatrixHTTP(t, func(user *entities.User) (*resty.Response, error) {
				return suite.gitClient.AsBasic(user).
					SetHeader(echo.HeaderContentType, "application/vnd.git-lfs+json").
					SetBody(&schemas.LfsObjectBatchRequest{
						Operation: schemas.LfsObjectOperationTypes.Upload,
						Objects:   objects,
					}).Post(fmt.Sprintf("/%s.git/info/lfs/objects/batch", repository.FullSlug()))
			}).SpecificUsers(suite.users.AuthAdmin, suite.users.AuthDeveloper, suite.users.AuthMaintainer, suite.users.AuthOwner)
		}
	})

}

func (suite *RwApiTestSuite) TestLfsObject_Push() {
	t := suite.T()

	client := pb.NewRepoServiceClient(suite.grpcClient)

	user := suite.users.Kopatych
	repo := suite.repos.Alpha

	suite.addRole(t, user, repo, iam.Roles.RepositoriesDeveloper)
	repoURL := suite.URL(repo)

	protocol := suite.HTTPSProtocol()
	tmpDir := testutils.TempDir(t, "", "")

	cg := protocol.PrepareCGit(testutils.NewWorkdir(t, tmpDir), user.Identity).WithAuthTokenSite(repoURL)
	cg.Must(t, "clone", repoURL, "alpha")
	alphaDir := testutils.NewWorkdir(t, path.Join(tmpDir, "alpha"))

	cg = protocol.PrepareCGit(alphaDir, user.Identity).WithAuthTokenSite(repoURL)

	fileName := "file.txt"
	fileContent := "some test content"
	suite.makeNewFileWithContent(cg, fileName, fileContent)
	cg.Must(t, "lfs", "track", "*.txt")
	cg.Must(t, "add", ".")
	cg.Must(t, "commit", "-m", "\"some file\"")
	cg.Must(t, "push")

	// clone and leave the file as a pointer
	t.Run("pointer", func(t *testing.T) {
		tmpDir = testutils.TempDir(t, "", "")

		cg = protocol.PrepareCGit(testutils.NewWorkdir(t, tmpDir), user.Identity).WithAuthTokenSite(repoURL)
		cg.Must(t, "clone", repoURL, "alpha")

		content, err := os.ReadFile(path.Join(tmpDir, "alpha", fileName))
		require.NoError(t, err)
		isLFSPointer, _ := git_utils.IsLfsPointer(content)
		require.True(t, isLFSPointer)
	})

	// clone and fetch an original file
	t.Run("fetch original file", func(t *testing.T) {
		tmpDir = testutils.TempDir(t, "", "")

		cg = protocol.PrepareCGit(testutils.NewWorkdir(t, tmpDir), user.Identity).WithAuthTokenSite(repoURL).WithDownloadLFS()
		cg.Must(t, "clone", repoURL, "alpha")

		content, err := os.ReadFile(path.Join(tmpDir, "alpha", fileName))
		require.NoError(t, err)
		require.Equal(t, fileContent, string(content))
	})
	var oid string
	t.Run("lfs in pathinfo", func(t *testing.T) {
		ctx := testutils.AuthorizeGRPC(user.Identity)
		info, err := client.PathInfo(ctx, &pb.PathInfoRequest{
			Id:   grpc_marshalling.IDInverse(repo.ID),
			Path: fileName,
		})
		require.NoError(t, err)
		lfsMetadata := info.DirEntry.GetFileMetadata().GetLfsMetadata()
		require.NotNil(t, lfsMetadata)
		oid = lfsMetadata.Oid
	})

	t.Run("Get LFS info", func(t *testing.T) {
		ctx := testutils.AuthorizeGRPC(user.Identity)
		_, err := client.LFSInfo(ctx, &pb.LFSInfoRequest{
			RepoId:   grpc_marshalling.IDInverse(repo.ID),
			ObjectId: oid,
		})
		require.NoError(t, err)
	})
}

func (suite *RwApiTestSuite) TestLfsObject_DeleteRepo() {
	t := suite.T()

	user := suite.users.Admin

	client := pb.NewRepoServiceClient(suite.grpcClient)
	ctx := testutils.AuthorizeGRPC(user.Identity)

	resp, err := client.Create(ctx,
		&pb.CreateRepositoryRequest{
			Org: &pb.CreateRepositoryRequest_OrgId{
				OrgId: grpc_marshalling.IDInverse(suite.orgs.Yandex.ID),
			},
			Slug: "foo",
		})
	require.NoError(t, err)

	repo, err := grpc_marshalling.OperationResponse(resp, &pb.Repository{})
	require.NoError(t, err)

	repoURL := suite.RepoURL(suite.orgs.Yandex.Slug, repo.Slug)

	protocol := suite.HTTPSProtocol()
	tmpDir := testutils.TempDir(t, "", "")

	cg := protocol.PrepareCGit(testutils.NewWorkdir(t, tmpDir), user.Identity).WithAuthTokenSite(repoURL)
	cg.Must(t, "clone", repoURL, "alpha")
	alphaDir := testutils.NewWorkdir(t, path.Join(tmpDir, "alpha"))

	cg = protocol.PrepareCGit(alphaDir, user.Identity).WithAuthTokenSite(repoURL)

	fileName := "file.txt"
	fileContent := "some test content"
	suite.makeNewFileWithContent(cg, fileName, fileContent)
	cg.Must(t, "lfs", "track", "*.txt")
	cg.Must(t, "add", ".")
	cg.Must(t, "commit", "-m", "\"some file\"")
	cg.Must(t, "push")

	_, err = client.Delete(
		testutils.AuthorizeGRPC(user.Identity),
		&pb.DeleteRepositoryRequest{Id: repo.Id},
	)
	yarequire.ProtoStatusEqual(t, codes.OK, err)

	suite.WaitForWorkflows(t, entities.WorkflowTypes.DeleteRepoDeps)

	res, err := suite.S3Client.ListObjectsV2(context.Background(), &s3.ListObjectsV2Input{
		Bucket: utils.PtrFromValue(suite.cfg.S3.LfsObjects.Bucket),
		Prefix: utils.PtrFromValue(repo.Id),
	})
	require.NoError(t, err)
	require.Equal(t, 0, len(res.Contents))
}
