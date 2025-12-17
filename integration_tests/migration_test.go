package integrationtests

import (
	"bb.yandex-team.ru/cloud/cloud-go/genproto/privateapi/yandex/cloud/priv/operation"
	"common/cgit"
	"common/functools"
	"common/httprecorder"
	"common/services/quota"
	temporal_errors "common/temporal/errors"
	"common/testutils/yarequire"
	"common/utils"
	"context"
	"fmt"
	"gitcore/internal/entities"
	grpc_marshalling "gitcore/internal/grpcserver/marshalling"
	"gitcore/internal/testutils"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/testing/protocmp"
	"math"
	"os"
	"path"
	"path/filepath"
	pb "private_api/generated/yandex/cloud/priv/gitcore/v1"
	"slices"
	"testing"
)

type quotaSnapshot struct {
	repos          *quota.QuotaLimit
	storage        *quota.QuotaLimit
	reposPrivate   *quota.QuotaLimit
	storagePrivate *quota.QuotaLimit
}

func (q quotaSnapshot) check(t *testing.T, prevState quotaSnapshot, check func(require.TestingT, any, any, ...any)) {
	check(t, q.repos.Usage, prevState.repos.Usage)
	check(t, q.storage.Usage, prevState.storage.Usage)
}

func (q quotaSnapshot) checkPrivate(t *testing.T, prevState quotaSnapshot, check func(require.TestingT, any, any, ...any)) {
	check(t, q.reposPrivate.Usage, prevState.reposPrivate.Usage)
	check(t, q.storagePrivate.Usage, prevState.storagePrivate.Usage)
}

func (suite *MigrationTestSuite) getQuotas(ctx context.Context, t *testing.T) quotaSnapshot {
	reposCount, err := suite.quotaService.Get(ctx, suite.orgs.Yandex.ID, entities.Quotas.RepositoriesCount)
	require.NoError(t, err)
	storageSize, err := suite.quotaService.Get(ctx, suite.orgs.Yandex.ID, entities.Quotas.ObjectStorageSize)
	require.NoError(t, err)
	reposCountPriv, err := suite.quotaService.Get(ctx, suite.orgs.Yandex.ID, entities.Quotas.RepositoriesPrivateCount)
	require.NoError(t, err)
	storageSizePriv, err := suite.quotaService.Get(ctx, suite.orgs.Yandex.ID, entities.Quotas.ObjectStoragePrivateSize)
	require.NoError(t, err)

	return quotaSnapshot{
		repos:          reposCount,
		storage:        storageSize,
		reposPrivate:   reposCountPriv,
		storagePrivate: storageSizePriv,
	}
}

func (suite *MigrationTestSuite) TestRepoInaccessible() {
	t := suite.T()
	personalToken := suite.personalGithubPAT()
	if personalToken == "" {
		t.Skip("Local testing with GitHub PAT only")
	}

	migClient := pb.NewMigrationServiceClient(suite.grpcClient)
	ctx := testutils.AuthorizeGRPC(suite.users.Admin.Identity)

	// increase limit
	suite.setQuotaLimit(t, suite.orgs.Yandex.ID, entities.Quotas.PackFileSize, 5368709120 /* 5 Gb (1 * 1024^3)*/)
	suite.setQuotaLimit(t, suite.orgs.Yandex.ID, entities.Quotas.LFSStorageSize, math.MaxInt32)
	suite.setQuotaLimit(t, suite.orgs.Yandex.ID, entities.Quotas.ObjectStorageSize, 2147483648 /* 2 Gb (2 * 1024^3)*/)

	resp, err := migClient.Migrate(ctx, &pb.MigrateRequest{
		Org: &pb.MigrateRequest_OrgId{
			OrgId: grpc_marshalling.IDInverse(suite.orgs.Yandex.ID),
		},
		Url:        "https://github.com/this/repo-does-not-exist",
		Slug:       "not-found",
		Visibility: pb.ResourceVisibility_RESOURCE_PUBLIC,
		Credentials: &pb.MigrationCredentials{
			Creds: &pb.MigrationCredentials_Token{
				Token: personalToken,
			},
		},
	})
	require.NoError(t, err)

	migrationID := resp.GetId()
	require.NotEmpty(t, migrationID)
	require.NoError(t, suite.MigrationService.WaitForMigration(context.Background(), migrationID))
}

func (suite *MigrationTestSuite) TestMigration() {
	t := suite.T()
	personalToken := suite.personalGithubPAT()
	if personalToken == "" {
		t.Skip("Local testing with GitHub PAT only")
	}

	migClient := pb.NewMigrationServiceClient(suite.grpcClient)
	opClient := pb.NewOperationServiceClient(suite.grpcClient)
	ctx := testutils.AuthorizeGRPC(suite.users.Admin.Identity)

	// increase limit
	suite.setQuotaLimit(t, suite.orgs.Yandex.ID, entities.Quotas.PackFileSize, 5368709120 /* 5 Gb (1 * 1024^3)*/)
	suite.setQuotaLimit(t, suite.orgs.Yandex.ID, entities.Quotas.LFSStorageSize, math.MaxInt32)
	suite.setQuotaLimit(t, suite.orgs.Yandex.ID, entities.Quotas.ObjectStorageSize, 2147483648 /* 2 Gb (2 * 1024^3)*/)

	tests := []struct {
		org          *entities.Organization
		url          string
		slug         string
		visibility   pb.ResourceVisibility
		expectErrors map[pb.Step][]*pb.MigrationError
		description  string
		skipReason   string
	}{
		{
			org:        suite.orgs.Yandex,
			url:        "https://github.com/doreshnikov/demo-example",
			slug:       "demo-example",
			visibility: pb.ResourceVisibility_RESOURCE_PUBLIC,
			skipReason: "private repo",
		},
		{
			org:        suite.orgs.Yandex,
			url:        "https://github.com/src-migration-tests/generic",
			slug:       "generic",
			visibility: pb.ResourceVisibility_RESOURCE_PUBLIC,
		},
		{
			org:  suite.orgs.Yandex,
			url:  "https://github.com/src-migration-tests/lfs-missing",
			slug: "lfs-missing",
			expectErrors: map[pb.Step][]*pb.MigrationError{
				pb.Step_STEP_CODE: {
					{
						ErrorType:  pb.MigrationErrorType_MIGRATION_ERROR_ENTITY_INACCESSIBLE,
						EntityType: utils.PtrFromValue(pb.EntityType_ENTITY_LFS_OBJECT),
						EntityUrl:  utils.PtrFromValue("3dc49873215105dec45fe2770155fd902b17f7b6079d29028ba0eead0eb63bfc"),
					},
				},
			},
			visibility: pb.ResourceVisibility_RESOURCE_PUBLIC,
		},
		{
			org:        suite.orgs.Yandex,
			url:        "https://github.com/vi34/Proxy-Server",
			slug:       "proxy-server",
			visibility: pb.ResourceVisibility_RESOURCE_PUBLIC,
		},
		{
			org:        suite.orgs.Yandex,
			url:        "https://github.com/vi34/Proxy-Server",
			slug:       "proxy-server-private",
			visibility: pb.ResourceVisibility_RESOURCE_PRIVATE,
		},
		{
			org:        suite.orgs.Yandex,
			url:        "https://github.com/src-migration-tests/removed-prs",
			slug:       "removed-prs",
			visibility: pb.ResourceVisibility_RESOURCE_PUBLIC,
		},
		{
			org:         suite.orgs.Yandex,
			url:         "https://github.com/kamranahmedse/developer-roadmap.git",
			slug:        "developer-roadmap",
			visibility:  pb.ResourceVisibility_RESOURCE_PUBLIC,
			description: "Repo with empty lfs file list",
			skipReason:  "hangs on cloning stage, too big",
		},
		{
			org:        suite.orgs.Yandex,
			url:        "https://github.com/src-migration-tests/lfs",
			slug:       "lfs",
			visibility: pb.ResourceVisibility_RESOURCE_PUBLIC,
			skipReason: "error: failed to fetch some objects from 'https://****@github.com/src-migration-tests/lfs.git/info/lfs",
		},
		{
			org:        suite.orgs.Yandex,
			url:        "https://github.com/yandex/odyssey",
			slug:       "odyssey",
			visibility: pb.ResourceVisibility_RESOURCE_PUBLIC,
		},
		{
			org:        suite.orgs.Yandex,
			url:        "https://github.com/Khan/genqlient",
			slug:       "genqlient",
			visibility: pb.ResourceVisibility_RESOURCE_PUBLIC,
			skipReason: "cant' migrate https://github.com/Khan/genqlient/pull/66, can't open https://github.com/Khan/genqlient/pull/50 in src",
		},
	}
	for _, tt := range tests {
		if tt.skipReason != "" {
			t.Logf("Skipping %s: \n%s", tt.slug, tt.skipReason)
			continue
		}

		t.Run(tt.slug, func(t *testing.T) {
			cancel := temporal_errors.ToggleErrorMarshallingDebug() // to test errors marshalling on live repos
			defer cancel()

			quotasBefore := suite.getQuotas(ctx, t)

			resp, err := migClient.Migrate(ctx, &pb.MigrateRequest{
				Org: &pb.MigrateRequest_OrgId{
					OrgId: grpc_marshalling.IDInverse(tt.org.ID),
				},
				Url:        tt.url,
				Slug:       tt.slug,
				Visibility: tt.visibility,
				Credentials: &pb.MigrationCredentials{
					Creds: &pb.MigrationCredentials_Token{
						Token: personalToken,
					},
				},
			})
			require.NoError(t, err)
			migrationID := resp.GetId()
			require.NotEmpty(t, migrationID)
			require.NoError(t, suite.MigrationService.WaitForMigration(context.Background(), migrationID))
			resp, err = opClient.Get(ctx, &pb.GetOperationRequest{
				Id: migrationID,
			})
			require.NoError(t, err)
			suite.checkAllSteps(t, resp, tt.expectErrors)

			quotasAfter := suite.getQuotas(ctx, t)

			if grpc_marshalling.ResourceVisibilityDirect(tt.visibility) == entities.Visibilities.Public {
				quotasAfter.check(t, quotasBefore, require.Greater)
				quotasAfter.checkPrivate(t, quotasBefore, require.Equal)
			} else {
				quotasAfter.checkPrivate(t, quotasBefore, require.Greater)
				quotasAfter.check(t, quotasBefore, require.Equal)
			}
		})
	}
}

func (suite *MigrationTestSuite) personalGithubPAT() string {
	// We use token to run tests locally. GH anonymous rate limits are low
	// return "" // or you can paste your github PAT here
	return os.Getenv("GH_TOKEN")
}

func (suite *MigrationTestSuite) checkAllSteps(t *testing.T, resp *operation.Operation, expectErrors map[pb.Step][]*pb.MigrationError) *pb.MigrationMetadata {
	var metadata pb.MigrationMetadata
	err := resp.GetMetadata().UnmarshalTo(&metadata)
	require.NoError(t, err)

	require.Equal(t, pb.MigrationStatus_STATUS_SUCCESS, metadata.Status)
	require.Equal(t, 4, len(metadata.Steps), "Not all steps were executed during migration")

	for _, step := range metadata.Steps {
		require.Equal(t, pb.MigrationStatus_STATUS_SUCCESS, step.Status,
			"Step %v failed. Processed %v out of %v", step.Step, step.Processed, step.Total)

		if len(expectErrors[step.Step]) == 0 {
			require.Empty(t, step.Errors)
			continue
		}

		expectURLs := functools.Map(expectErrors[step.Step], (*pb.MigrationError).GetEntityUrl)
		errURLs := functools.Map(step.Errors, (*pb.MigrationError).GetEntityUrl)
		slices.Sort(expectURLs)
		slices.Sort(errURLs)
		require.Equal(t, expectURLs, errURLs)
	}

	return &metadata
}

func (suite *MigrationTestSuite) getFixturesRoot() string {
	return "./migration_fixtures"
}

func (suite *MigrationTestSuite) postProcessGitFixture(t *testing.T, repoPath string) {
	if _, err := os.Stat(path.Join(repoPath, "hooks")); err != os.ErrNotExist {
		require.NoError(t, os.RemoveAll(path.Join(repoPath, "hooks")))
	}
	if _, err := os.Stat(path.Join(repoPath, "info")); err != os.ErrNotExist {
		require.NoError(t, os.RemoveAll(path.Join(repoPath, "info")))
	}

	keepMeFile, err := os.Create(path.Join(repoPath, "refs", ".ignore"))
	require.NoError(t, err)
	require.NoError(t, keepMeFile.Close())
}

/*
TestDumps uses GitHub API and git fixtures instead of real requests and responses

To re-generate the fixtures, run this test with env:
  - GH_TOKEN=your_personal_github_pat
  - RECORD_MIGRATION=1

If you want to add another fixture,
 1. Add a new test case
 2. Run it with the same env (GH_TOKEN, RECORD_MIGRATION=1)
    - either from IDE
    - or as `make record-migration gh_token=<your_token> repo=<repo_slug>` from gitcore Makefile

To run the test with already recorded fixtures, RECORD_MIGRATION should have any other value and no token is needed
*/
func (suite *MigrationTestSuite) TestDumps() {
	t := suite.T()

	migClient := pb.NewMigrationServiceClient(suite.grpcClient)
	opClient := pb.NewOperationServiceClient(suite.grpcClient)
	ctx := testutils.AuthorizeGRPC(suite.users.Admin.Identity)

	// increase limit
	suite.setQuotaLimit(t, suite.orgs.Yandex.ID, entities.Quotas.PackFileSize, 5368709120 /* 5 Gb (1 * 1024^3)*/)
	suite.setQuotaLimit(t, suite.orgs.Yandex.ID, entities.Quotas.LFSStorageSize, math.MaxInt32)
	suite.setQuotaLimit(t, suite.orgs.Yandex.ID, entities.Quotas.ObjectStorageSize, 2147483648 /* 2 Gb (2 * 1024^3)*/)

	cancel := temporal_errors.ToggleErrorMarshallingDebug() // to test errors marshalling on live repos
	defer cancel()

	personalToken := suite.personalGithubPAT()
	mode := httprecorder.Modes.ReplayOnly
	if os.Getenv("RECORD_MIGRATION") == "1" {
		//mode = migration_recorder.Modes.RecordSafe
		mode = httprecorder.Modes.Record // Safe could be a better option, but we have some repeated requests, so not optimal
		if personalToken == "" {
			t.Skip("Recording is only available locally with personal GitHub PAT")
		}
	}

	require.NotNil(t, suite.Migrator)
	suite.Migrator.AddRecorder(httprecorder.NewTransport(
		httprecorder.WithFixturesLocation(suite.getFixturesRoot()),
		httprecorder.WithMode(mode),
		httprecorder.WithRequestMatcher(
			httprecorder.NewRequestMatcher(
				httprecorder.WithMatchBody(true),
				httprecorder.WithIgnoreAllHeaders(), // probably no need for now?
			),
		),
	))

	t.Logf("Running migration dump tests in mode '%s'", mode)

	tests := []struct {
		url          string
		slug         string
		visibility   pb.ResourceVisibility
		expectErrors map[pb.Step][]*pb.MigrationError
	}{
		{
			url:        "https://github.com/src-migration-tests/generic",
			slug:       "generic",
			visibility: pb.ResourceVisibility_RESOURCE_PUBLIC,
		},
		{
			url:        "https://github.com/src-migration-tests/removed-prs",
			slug:       "removed-prs",
			visibility: pb.ResourceVisibility_RESOURCE_PUBLIC,
		},
		{
			url:  "https://github.com/src-migration-tests/lfs-missing",
			slug: "lfs-missing",
			expectErrors: map[pb.Step][]*pb.MigrationError{
				pb.Step_STEP_CODE: {
					{
						ErrorType:  pb.MigrationErrorType_MIGRATION_ERROR_ENTITY_INACCESSIBLE,
						EntityType: utils.PtrFromValue(pb.EntityType_ENTITY_LFS_OBJECT),
						EntityUrl:  utils.PtrFromValue("3dc49873215105dec45fe2770155fd902b17f7b6079d29028ba0eead0eb63bfc"),
					},
				},
			},
			visibility: pb.ResourceVisibility_RESOURCE_PUBLIC,
		},
		{
			url:        "https://github.com/vi34/Proxy-Server",
			slug:       "proxy-server-private",
			visibility: pb.ResourceVisibility_RESOURCE_PRIVATE,
		},
		{
			url:        "https://github.com/yandex/odyssey",
			slug:       "odyssey",
			visibility: pb.ResourceVisibility_RESOURCE_PUBLIC,
		},
	}

	gitReposRoot := path.Join(suite.getFixturesRoot(), "@git")

	for _, tt := range tests {
		t.Run(tt.slug, func(t *testing.T) {
			repoLocalPath := path.Join(gitReposRoot, tt.slug)

			if mode.IsRecording() {
				if _, err := os.Stat(repoLocalPath); err != os.ErrNotExist {
					require.NoError(t, os.RemoveAll(repoLocalPath))
				}

				cg := cgit.NewCGit(gitReposRoot)
				err := cg.MirrorCloneRepo(context.Background(), tt.url, tt.slug)
				require.NoError(t, err)

				suite.postProcessGitFixture(t, repoLocalPath)
			} else {
				originalCloneURL, err := suite.Migrator.GetCloneURLFor(tt.url)
				require.NoError(t, err)

				repoAbsolutePath, err := filepath.Abs(repoLocalPath)
				require.NoError(t, err)
				suite.Migrator.RedirectCloneURL(originalCloneURL, fmt.Sprintf("file://%s", repoAbsolutePath))
			}

			resp, err := migClient.Migrate(ctx, &pb.MigrateRequest{
				Org: &pb.MigrateRequest_OrgId{
					OrgId: grpc_marshalling.IDInverse(suite.orgs.Yandex.ID),
				},
				Url:        tt.url,
				Slug:       tt.slug,
				Visibility: tt.visibility,
				Credentials: &pb.MigrationCredentials{
					Creds: &pb.MigrationCredentials_Token{
						Token: personalToken,
					},
				},
			})
			require.NoError(t, err)
			migrationID := resp.GetId()
			require.NotEmpty(t, migrationID)
			require.NoError(t, suite.MigrationService.WaitForMigration(context.Background(), migrationID))

			resp, err = opClient.Get(ctx, &pb.GetOperationRequest{
				Id: migrationID,
			})
			require.NoError(t, err)

			metadata := suite.checkAllSteps(t, resp, tt.expectErrors)
			if mode.IsRecording() {
				// intended behavior, should not be commented
				yarequire.ProtoDumpFixture(t, metadata) // nolint:forbidigo
			} else {
				yarequire.ProtoCompareWithFixture(t, metadata, protocmp.IgnoreFields(&pb.MigrationMetadata{},
					"repo_id", "started_at", "finished_at", "validation_id"))
			}
		})
	}
}
