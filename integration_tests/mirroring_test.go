package integrationtests

import (
	"common/grpc"
	"common/pkgenerator"
	"common/utils"
	"common/utils/rolesgenerator/iam"
	"context"
	"fmt"
	"gitcore/internal/entities"
	"gitcore/internal/httpserver/gitserver"
	"gitcore/internal/interfaces"
	"gitcore/internal/testutils"
	pb "private_api/generated/yandex/cloud/priv/gitcore/v1"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func (suite *RwApiTestSuite) makeMirroredRepo(t *testing.T, owner *entities.User, syncedRefs []string) (
	repoID uint64,
	orgSlug string,
	repoSlug string,
) {
	ctx := context.Background()
	id, err := pkgenerator.GetNextID()
	require.NoError(t, err)

	// Create organization and repository slugs
	orgSlug = fmt.Sprintf("org%d", id)
	orgSchema := suite.OrganizationFixture(0, orgSlug, suite.users.Admin, nil)
	org, err := suite.OrgRepo.GetOrganization(ctx, orgSchema.Slug)
	require.NoError(t, err)

	repoSlug = fmt.Sprintf("repo%d", id)

	// Create a migration ID (ensure it's less than 20 characters)
	// Use only the last 10 digits of the ID to keep the migration ID short
	shortID := id % 10000000000
	migrationID := fmt.Sprintf("mig-%d", shortID)

	repoParams := &interfaces.CreateRepositoryArgs{
		Name:        repoSlug,
		Slug:        repoSlug,
		OrgID:       org.ID,
		CreatedBy:   owner.ID,
		Visibility:  entities.Visibilities.Public,
		MigrationID: &migrationID,
		MigratedRepository: &entities.MigratedRepository{
			URL:                 "https://github.com/example/repo",
			Domain:              "github.com",
			Mirror:              true,
			MirrorStatus:        &entities.MirroringStatuses.Enabled,
			LastMirroringID:     utils.PtrFromValue("test-mirroring-id"),
			MirrorScheduleID:    utils.PtrFromValue("test-schedule-id"),
			CredentialsSecretID: "test-credentials-id",
			SyncedRefs:          syncedRefs,
			UpdatedAt:           time.Now().UTC(),
		},
	}

	repoID, err = suite.RepoRepo.CreateRepository(ctx, repoParams)
	require.NoError(t, err)

	repo, err := suite.RepoRepo.GetRepositoryByID(ctx, repoID)
	require.NoError(t, err)
	suite.addRole(t, owner, repo, iam.Roles.RepositoriesAdmin)

	return repoID, orgSlug, repoSlug
}

func (suite *RwApiTestSuite) TestMirroredBranchPushRejection() {
	t := suite.T()

	// Create a mirrored repository with specific synced refs
	syncedRefs := []string{"master", "feature/*"}
	_, orgSlug, repoSlug := suite.makeMirroredRepo(t, suite.users.Kopatych, syncedRefs)

	// Set up a git client
	protocol := suite.HTTPSProtocol()
	tmpDir := testutils.TempDir(t, "", "testrepo")
	w := testutils.NewWorkdir(t, tmpDir)
	cg := protocol.PrepareCGit(w, testutils.UserIdentities.Kopatych)

	// Initialize the repository
	cg.Must(t, "init", ".")
	cg.Must(t, "remote", "add", "origin", protocol.RepoURL(orgSlug, repoSlug))

	t.Run("master push", func(t *testing.T) {
		cg.Must(t, "branch", "-m", "master")
		suite.makeNewFileWithContent(cg, "1.txt", "hello")
		cg.Must(t, "add", "*")
		cg.Must(t, "commit", "-m", "initial commit")

		stdout, stderr, err := cg.Exec("push", "-u", "origin", "master")
		require.NotNilf(t, err, "Stdout: %s, Stderr: %s\n", stdout, stderr)
		require.Contains(t, stderr, "Cannot push to mirrored branches")
	})

	t.Run("feature push", func(t *testing.T) {
		cg.Must(t, "checkout", "-b", "feature/test")
		suite.makeNewFileWithContent(cg, "2.txt", "hello")
		cg.Must(t, "add", "*")
		cg.Must(t, "commit", "-m", "feature commit")

		stdout, stderr, err := cg.Exec("push", "-u", "origin", "feature/test")
		require.NotNilf(t, err, "Stdout: %s, Stderr: %s\n", stdout, stderr)
		require.Contains(t, stderr, "Cannot push to mirrored branches")
	})

	t.Run("non-mirrored branch push", func(t *testing.T) {
		cg.Must(t, "checkout", "-b", "development")
		suite.makeNewFileWithContent(cg, "3.txt", "hello")
		suite.commitAll(cg, "development commit", "development")
	})

	t.Run("migrator can push", func(t *testing.T) {
		cg.Must(t, "checkout", "master")
		suite.makeNewFileWithContent(cg, "4.txt", "hello")

		cg.AddExtraHeader(gitserver.SourcecraftEventHeader, gitserver.MigrateRepoEvent)
		suite.commitAll(cg, "initial commit", "master")
	})
}

func (suite *RwApiTestSuite) TestMirroredRepoHasState() {
	t := suite.T()
	ctx := testutils.AuthorizeGRPC(suite.users.Kopatych.Identity)

	// Create a mirrored repository with specific synced refs
	syncedRefs := []string{"master", "feature/*"}
	repoID, _, _ := suite.makeMirroredRepo(t, suite.users.Kopatych, syncedRefs)

	repoClient := pb.NewRepoServiceClient(suite.grpcClient)
	repoResponse, err := repoClient.Get(ctx, &pb.GetRepositoryRequest{
		Repo: &pb.GetRepositoryRequest_Id{
			Id: grpc.MarshalID(repoID),
		},
	})
	require.NoError(t, err)
	require.NotNil(t, repoResponse.MirrorState)
	require.Equal(t, pb.MirroringStatus_MIRROR_STATUS_ENABLED, repoResponse.MirrorState.Status)
}
