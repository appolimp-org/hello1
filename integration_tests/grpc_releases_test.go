package integrationtests

import (
	"bytes"
	"common/functools"
	"common/grpc"
	"common/testutils/yarequire"
	"common/utils"
	"common/utils/rolesgenerator/iam"
	"context"
	"fmt"
	"gitcore/internal/entities"
	except "gitcore/internal/exceptions"
	grpc_marshalling "gitcore/internal/grpcserver/marshalling"
	"gitcore/internal/httpserver/schemas"
	"gitcore/internal/interfaces"
	"gitcore/internal/testutils"
	utils2 "gitcore/internal/utils"
	"io"
	"net/http"
	"os"
	"os/exec"
	pbPagination "private_api/generated/yandex/cloud/pagination"
	pb "private_api/generated/yandex/cloud/priv/gitcore/v1"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/go-git/go-git/v5/plumbing"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/protobuf/testing/protocmp"
	"google.golang.org/protobuf/types/known/fieldmaskpb"
)

type createReleaseOptions struct {
	Repo            *entities.Repository
	Tag             string
	TagSourceBranch *string
	Title           string
	ReleaseNotes    string
	Publish         *bool
	Assets          []*pb.ReleaseAssetInput
}

func (suite *RwApiTestSuite) createReleaseGRPC(
	t *testing.T,
	author *entities.User,
	opts *createReleaseOptions,
) *pb.Release {
	client := pb.NewGenericReleaseServiceClient(suite.grpcClient)

	opts, err := utils2.MergeOptions(opts, &createReleaseOptions{
		Repo:            suite.repos.Alpha,
		Tag:             "v1.0.0",
		TagSourceBranch: nil,
		Title:           "v1.0.0 - Default title",
		ReleaseNotes:    "Default release notes",
		Publish:         utils.PtrFromValue(true),
		Assets:          nil,
	})
	require.NoError(t, err)

	op, err := client.Create(testutils.AuthorizeGRPC(author.Identity), &pb.GenericCreateReleaseRequest{
		RepoId:        grpc.MarshalID(opts.Repo.ID),
		Tag:           opts.Tag,
		CreateTagFrom: opts.TagSourceBranch,
		Title:         opts.Title,
		ReleaseNotes:  opts.ReleaseNotes,
		Publish:       utils2.OrElse(opts.Publish, false),
		Assets:        opts.Assets,
	})
	require.NoError(t, err)

	release := new(pb.Release)
	require.NoError(t, op.GetResponse().UnmarshalTo(release))
	return release
}

func (suite *RwApiTestSuite) resolveBranch(t *testing.T, repo *entities.Repository, branch string) plumbing.Hash {
	authenticator := suite.getFakeAuthenticator(suite.users.Admin.Identity)
	commit, err := suite.GitcoreClient.GetCommit(context.Background(), authenticator, interfaces.GetCommitArgs{
		Revision: entities.NewGitRevisionFromBranch(repo.ID, branch),
	})
	require.NoError(t, err)
	return commit.Hash
}

func (suite *RwApiTestSuite) TestGrpcReleasesCRUD() {
	t := suite.T()
	client := pb.NewGenericReleaseServiceClient(suite.grpcClient)
	ctx := testutils.AuthorizeGRPC(suite.users.Admin.Identity)

	repo := suite.repos.Alpha
	repoID := grpc_marshalling.IDInverse(repo.ID)

	tmpAttachment := suite.UploadReleaseAttachment(suite.users.Admin.Identity, "document.pdf", repo)
	attachmentID, err := grpc_marshalling.IDDirect(tmpAttachment.ID)
	require.NoError(t, err)

	checkSHA256 := func(t *testing.T, fileName string, expectedChecksum string) {
		path := testutils.GetAttachmentPath(fileName)
		cmd := exec.Command("shasum", "-a", "256", path)
		var stdout bytes.Buffer
		cmd.Stdout = &stdout
		require.NoError(t, cmd.Run())

		out, err := io.ReadAll(&stdout)
		require.NoError(t, err)

		parts := strings.Split(string(out), " ")
		require.NotEmpty(t, parts)
		require.Equal(t, expectedChecksum, parts[0])
	}

	suite.mustBash(repo, `
		git checkout -b master
		git tag v1.0.0
		touch x
		git add . && git commit -m "new commit"
	`)

	require.NoError(t, err)
	release1 := suite.createReleaseGRPC(t, suite.users.Admin, &createReleaseOptions{
		Title:   "1.0.0 - from existing",
		Publish: utils.PtrFromValue(false),
	})
	release2 := suite.createReleaseGRPC(t, suite.users.Admin, &createReleaseOptions{
		Tag:             "v1.1.0",
		TagSourceBranch: utils.PtrFromValue("master"),
		Title:           "1.1.0 - create tag",
		Assets: []*pb.ReleaseAssetInput{
			{
				Name: "Default link",
				Reference: &pb.ReleaseAssetInput_Link{
					Link: "https://example.com",
				},
			},
			{
				Name: "Default attachment",
				Reference: &pb.ReleaseAssetInput_AttachmentId{
					AttachmentId: tmpAttachment.ID,
				},
			},
		},
	})

	releases := map[string]*pb.Release{
		release1.Id: release1,
		release2.Id: release2,
	}

	for i, releaseID := range []string{release1.Id, release2.Id} {
		t.Run(fmt.Sprintf("get release %d", i), func(t *testing.T) {
			release, err := client.Get(ctx, &pb.GenericGetReleaseRequest{
				Id: releaseID,
			})
			require.NoError(t, err)
			yarequire.ProtoCmp(t, releases[releaseID], release)
		})
	}

	t.Run("check assets", func(t *testing.T) {
		release, err := client.Get(ctx, &pb.GenericGetReleaseRequest{
			Id: release2.Id,
		})
		require.NoError(t, err)
		require.Len(t, release.Assets, 2)

		require.Equal(t, "Default link", release.Assets[0].Name)
		link := release.Assets[0].GetLink()
		require.Equal(t, "https://example.com", link)

		require.Equal(t, "Default attachment", release.Assets[1].Name)
		attachment := release.Assets[1].GetAttachment()
		require.NotNil(t, attachment)
		require.Equal(t, "document.pdf", attachment.Name)
		require.Equal(t, pb.Attachment_ENTITY_TYPE_RELEASE, attachment.EntityType)
		require.Equal(t, release2.Id, attachment.EntityId)
		require.NotNil(t, attachment.Sha256)
		checkSHA256(t, "document.pdf", *attachment.Sha256)

		attachmentEntity, err := suite.AttachmentRepo.Get(ctx, attachmentID)
		require.NoError(t, err)
		require.Equal(t, entities.AttachmentStatuses.Attached, attachmentEntity.Status)

		r, err := suite.client.
			As(suite.users.Admin.Identity).
			Get("/api/v1/attachments/" + attachment.GetId())
		yarequire.StatusCode(t, r, err, http.StatusOK)

		file, err := os.Open(testutils.GetAttachmentPath("document.pdf"))
		require.NoError(t, err)
		existingData, err := io.ReadAll(file)
		require.NoError(t, err)
		defer func() {
			require.NoError(t, file.Close())
		}()

		require.Equal(t, r.Body(), existingData)
	})

	t.Run("get by tag - not found", func(t *testing.T) {
		_, err := client.GetByTag(ctx, &pb.GetReleaseByTagRequest{
			Repo: &pb.GetReleaseByTagRequest_RepoId{
				RepoId: repoID,
			},
			Tag: "no-such-tag",
		})
		yarequire.ProtoStatusEqual(t, codes.NotFound, err)
	})

	t.Run("get by tag", func(t *testing.T) {
		release, err := client.GetByTag(ctx, &pb.GetReleaseByTagRequest{
			Repo: &pb.GetReleaseByTagRequest_RepoId{
				RepoId: repoID,
			},
			Tag: release1.Tag,
		})
		require.NoError(t, err)
		yarequire.ProtoCmp(t, release1, release)
	})

	t.Run("list", func(t *testing.T) {
		response, err := client.List(ctx, &pb.ListReleasesRequest{
			RepoId:   repoID,
			PageSize: utils.PtrFromValue(uint64(10)),
		})
		require.NoError(t, err)

		require.Len(t, response.Releases, 2)
		// by default ordered by created_at desc
		require.Equal(t, response.Releases[0].Id, release2.Id)
		require.Equal(t, response.Releases[1].Id, release1.Id)
		require.Greater(t, response.Releases[0].CreatedAt.AsTime().UnixMilli(), response.Releases[1].CreatedAt.AsTime().UnixMilli())

		require.Equal(t, response.Releases[0].ReleaseStatus, pb.Release_STATUS_PUBLISHED)
		require.Equal(t, response.Releases[0].CreatedAt.AsTime().UnixMilli(), response.Releases[0].ReleasedAt.AsTime().UnixMilli())
		require.Equal(t, response.Releases[1].ReleaseStatus, pb.Release_STATUS_DRAFT)
		require.Nil(t, response.Releases[1].ReleasedAt)

		//yarequire.ProtoDumpFixture(t, response)
		yarequire.ProtoCompareWithFixture(t, response,
			protocmp.IgnoreFields(&pb.ListReleasesResponse{}, "prev_page_token", "next_page_token"),
			protocmp.IgnoreFields(&pb.Release{}, "id", "created_at", "updated_at", "released_at", "hash"),
			protocmp.IgnoreFields(&pb.ReleaseAsset{}, "id"),
			protocmp.IgnoreFields(&pb.Attachment{}, "id", "entity_id"))
	})

	t.Run("list by outsider", func(t *testing.T) {
		response, err := client.List(context.Background(), &pb.ListReleasesRequest{
			RepoId:   repoID,
			PageSize: utils.PtrFromValue(uint64(10)),
		})
		require.NoError(t, err)

		require.Len(t, response.Releases, 1)
		require.Equal(t, response.Releases[0].Id, release2.Id)
	})

	t.Run("delete and list", func(t *testing.T) {
		_, err := client.Delete(ctx, &pb.GenericDeleteReleaseRequest{
			Id: release2.Id,
		})
		require.NoError(t, err)

		response, err := client.List(ctx, &pb.ListReleasesRequest{
			RepoId:   repoID,
			PageSize: utils.PtrFromValue(uint64(10)),
		})
		require.NoError(t, err)

		require.Len(t, response.Releases, 1)
		require.Equal(t, response.Releases[0].Id, release1.Id)

		t.Run("attachment cleanup", func(t *testing.T) {
			attachment, err := suite.AttachmentRepo.Get(ctx, attachmentID)
			require.NoError(t, err)

			require.True(t, attachment.IsDeleted)
			require.NoError(t, suite.AttachmentService.CleanupS3Files(ctx, 0, 0))

			_, _, err = suite.UploadService.Get(ctx, attachment)
			require.ErrorIs(t, err, except.StorageObjectNotFound)
		})
	})

	t.Run("update", func(t *testing.T) {
		suite.mustBash(repo, `
			touch y
			git add . && git commit -m "y"`,
		)

		attachment1 := suite.UploadReleaseAttachment(suite.users.Admin.Identity, "document.pdf", repo)
		attachment1ID, err := grpc_marshalling.IDDirect(attachment1.ID)
		require.NoError(t, err)

		attachment2 := suite.UploadReleaseAttachment(suite.users.Admin.Identity, "document.docx", repo)
		attachment2ID, err := grpc_marshalling.IDDirect(attachment2.ID)
		require.NoError(t, err)

		release3 := suite.createReleaseGRPC(t, suite.users.Admin, &createReleaseOptions{
			Tag:             "v1.1.1",
			TagSourceBranch: utils.PtrFromValue("master"),
			Title:           "1.1.1 - create tag",
			Assets: []*pb.ReleaseAssetInput{
				{
					Name: "Default link",
					Reference: &pb.ReleaseAssetInput_Link{
						Link: "https://example.com/1",
					},
				},
				{
					Name: "Link to be deleted",
					Reference: &pb.ReleaseAssetInput_Link{
						Link: "https://example.com/2",
					},
				},
				{
					Name: "Default attachment",
					Reference: &pb.ReleaseAssetInput_AttachmentId{
						AttachmentId: attachment1.ID,
					},
				},
				{
					Name: "Attachment to be deleted",
					Reference: &pb.ReleaseAssetInput_AttachmentId{
						AttachmentId: attachment2.ID,
					},
				},
			},
		})
		require.Len(t, release3.Assets, 4)

		attachment3 := suite.UploadReleaseAttachment(suite.users.Admin.Identity, "cat.jpeg", repo)
		attachment3ID, err := grpc_marshalling.IDDirect(attachment3.ID)
		require.NoError(t, err)

		op, err := client.Update(ctx, &pb.GenericUpdateReleaseRequest{
			Id:           release3.Id,
			Title:        "New title",
			ReleaseNotes: "New release notes",
			AssetDeltas: &pb.GenericUpdateReleaseRequest_AssetDeltas{
				RemoveIds: []string{
					release3.Assets[1].Id,
					release3.Assets[3].Id,
				},
				AddAssets: []*pb.ReleaseAssetInput{
					{
						Name: "Added link",
						Reference: &pb.ReleaseAssetInput_Link{
							Link: "https://example.com/3",
						},
					},
					{
						Name: "Added attachment",
						Reference: &pb.ReleaseAssetInput_AttachmentId{
							AttachmentId: attachment3.ID,
						},
					},
				},
			},
			UpdateMask: &fieldmaskpb.FieldMask{
				Paths: []string{"title", "release_notes", "asset_deltas"},
			},
		})
		require.NoError(t, err)
		result := new(pb.Release)
		require.NoError(t, op.GetResponse().UnmarshalTo(result))
		//yarequire.ProtoDumpFixture(t, result)

		release, err := client.Get(ctx, &pb.GenericGetReleaseRequest{
			Id: release3.Id,
		})
		require.NoError(t, err)
		yarequire.ProtoCompareWithFixture(t, release,
			protocmp.IgnoreFields(&pb.Release{}, "id", "created_at", "updated_at", "released_at", "hash"),
			protocmp.IgnoreFields(&pb.ReleaseAsset{}, "id"),
			protocmp.IgnoreFields(&pb.Attachment{}, "id", "entity_id"))

		require.Equal(t, "New title", release.Title)
		require.Equal(t, "New release notes", release.ReleaseNotes)

		require.Len(t, release.Assets, 4)
		require.Equal(t, "Default link", release.Assets[0].Name)
		require.Equal(t, "Default attachment", release.Assets[1].Name)
		require.Equal(t, "Added link", release.Assets[2].Name)
		require.Equal(t, "Added attachment", release.Assets[3].Name)

		attachments, err := suite.AttachmentRepo.GetBulk(ctx, []uint64{attachment1ID, attachment2ID, attachment3ID})
		require.NoError(t, err)
		require.Len(t, attachments, 3)
		attachmentsMap := functools.SliceToMap(attachments, (*entities.Attachment).GetID)

		require.False(t, attachmentsMap[attachment1ID].IsDeleted)
		require.True(t, attachmentsMap[attachment2ID].IsDeleted) // marked for removal from S3
		require.False(t, attachmentsMap[attachment3ID].IsDeleted)

		require.NotNil(t, attachmentsMap[attachment3ID].EntityID) // attached
		require.Equal(t, release3.Id, grpc_marshalling.IDInverse(*attachmentsMap[attachment3ID].EntityID))
	})
}

func (suite *RwApiTestSuite) TestGrpcReleases_Latest() {
	t := suite.T()
	client := pb.NewGenericReleaseServiceClient(suite.grpcClient)
	ctx := testutils.AuthorizeGRPC(suite.users.Admin.Identity)

	repo := suite.repos.Alpha
	repoID := grpc_marshalling.IDInverse(repo.ID)

	t.Run("no releases", func(t *testing.T) {
		_, err := client.GetLatest(ctx, &pb.GetLatestReleaseRequest{
			RepoId: repoID,
		})
		yarequire.ProtoStatusEqual(t, codes.NotFound, err)
	})

	for i := 0; i < 3; i++ {
		suite.mustBash(repo, fmt.Sprintf(`
			touch x-%d
			git add . && git commit -m "new commit"
			git tag new-tag-%d
		`, i, i))
	}

	t.Run("auto-latest", func(t *testing.T) {
		suite.createReleaseGRPC(t, suite.users.Admin, &createReleaseOptions{
			Tag:   "new-tag-0",
			Title: "Previous",
		})
		expected := suite.createReleaseGRPC(t, suite.users.Admin, &createReleaseOptions{
			Tag:   "new-tag-1",
			Title: "Latest",
		})
		suite.createReleaseGRPC(t, suite.users.Admin, &createReleaseOptions{
			Tag:     "new-tag-2",
			Title:   "Draft",
			Publish: utils.PtrFromValue(false),
		})

		release, err := client.GetLatest(ctx, &pb.GetLatestReleaseRequest{
			RepoId: repoID,
		})
		require.NoError(t, err)
		require.Equal(t, expected.Id, release.Id)

		resp, err := client.List(ctx, &pb.ListReleasesRequest{
			RepoId: repoID,
		})
		require.NoError(t, err)
		require.False(t, resp.Releases[0].IsLatest)
		require.True(t, resp.Releases[1].IsLatest)
		require.False(t, resp.Releases[2].IsLatest)
	})
}

func (suite *RwApiTestSuite) TestGrpcReleases_GetChanges() {
	t := suite.T()
	client := pb.NewGenericReleaseServiceClient(suite.grpcClient)
	ctx := testutils.AuthorizeGRPC(suite.users.Admin.Identity)

	repo := suite.repos.Alpha
	repoID := grpc_marshalling.IDInverse(repo.ID)
	var master plumbing.Hash

	checkCommitMessages := func(t *testing.T, commits []*pb.GetReleaseChangesResponse_CommitInfo, expectedMessages []string) {
		authenticator := suite.getFakeAuthenticator(suite.users.Admin.Identity)
		commitObjects, err := suite.GitcoreClient.GetCommitBulk(ctx, authenticator, interfaces.GetCommitBulkArgs{
			Revisions: functools.Map(commits, func(commit *pb.GetReleaseChangesResponse_CommitInfo) entities.GitRevision {
				return entities.NewGitRevisionFromCommit(repo.ID, plumbing.NewHash(commit.Hash))
			}),
		})
		require.NoError(t, err)

		actualMessages := functools.Map(commits, func(commit *pb.GetReleaseChangesResponse_CommitInfo) string {
			obj, ok := commitObjects[plumbing.NewHash(commit.Hash)]
			require.True(t, ok, "commit %s", commit.Hash)
			return strings.TrimSpace(obj.Message)
		})
		require.EqualValues(t, expectedMessages, actualMessages)
	}

	t.Run("initial", func(t *testing.T) {
		suite.mustBash(repo, `
			touch readme
			git add . && git commit -m "Initial"
		`)
		master = suite.resolveBranch(t, repo, "master")

		changes, err := client.GetChanges(ctx, &pb.GetReleaseChangesRequest{
			RepoId:      repoID,
			ReleaseHash: master.String(),
		})
		require.NoError(t, err)
		require.NotEmpty(t, changes.Commits)
		require.Equal(t, master.String(), changes.Commits[0].Hash)
	})

	release0 := suite.createReleaseGRPC(t, suite.users.Admin, &createReleaseOptions{
		Tag:             "v1.0.0",
		TagSourceBranch: utils.PtrFromValue("master"),
		Title:           "v1.0.0 - Initial",
	})

	t.Run("compared to auto-previous", func(t *testing.T) {
		suite.mustBash(repo, `
			touch x
			git add . && git commit -m "X"
			touch y
			git add . && git commit -m "Y"
		`)
		master = suite.resolveBranch(t, repo, "master")

		changes, err := client.GetChanges(ctx, &pb.GetReleaseChangesRequest{
			RepoId:      repoID,
			ReleaseHash: master.String(),
		})
		require.NoError(t, err)
		require.Len(t, changes.Commits, 2)
		require.Equal(t, master.String(), changes.Commits[0].Hash)
		checkCommitMessages(t, changes.Commits, []string{"Y", "X"})
	})

	suite.createReleaseGRPC(t, suite.users.Admin, &createReleaseOptions{
		Tag:             "v1.1.0",
		TagSourceBranch: utils.PtrFromValue("master"),
		Title:           "v1.1.0 - Next",
	})

	t.Run("manual compare to", func(t *testing.T) {
		suite.mustBash(repo, `
			touch z
			git add . && git commit -m "Z"
		`)
		master = suite.resolveBranch(t, repo, "master")

		changes, err := client.GetChanges(ctx, &pb.GetReleaseChangesRequest{
			RepoId:      repoID,
			ReleaseHash: master.String(),
		})
		require.NoError(t, err)
		require.Len(t, changes.Commits, 1)
		require.Equal(t, master.String(), changes.Commits[0].Hash)
		checkCommitMessages(t, changes.Commits, []string{"Z"})

		changes, err = client.GetChanges(ctx, &pb.GetReleaseChangesRequest{
			RepoId:            repoID,
			ReleaseHash:       master.String(),
			PreviousReleaseId: &release0.Id,
		})
		require.NoError(t, err)
		require.Len(t, changes.Commits, 3)
		require.Equal(t, master.String(), changes.Commits[0].Hash)
		checkCommitMessages(t, changes.Commits, []string{"Z", "Y", "X"})
	})

	t.Run("include merged PRs", func(t *testing.T) {
		prIDs := make([]string, 0, 4)

		// TODO this shouldn't be an issue - there should be an option for gitcore/internal/git/historywalker/historywalker.go
		// to enforce DFS over BFS (and to prioritize later parents first)
		// Also TODO: basic walker without path filter ignores 'FirstParent' option
		time.Sleep(time.Second) // to enforce commits being created STRICTLY LATER than master commits

		for i, params := range []struct {
			rebase bool
			squash bool
		}{
			{
				rebase: false,
				squash: false,
			},
			{
				rebase: false,
				squash: true,
			},
			{
				rebase: true,
				squash: false,
			},
			// squash and rebase doesn't differ from squash-only somehow...
		} {
			suite.mustBash(repo, fmt.Sprintf(`
				git checkout -b pr-releases-branch-%d
				touch something-%d
				git add . && git commit -m "From PR %d"
				touch something-else-%d
				git add . && git commit -m "From PR %d (another one)"
			`, i, i, i+1, i, i+1))

			pr := suite.makePullRequest(suite.users.Admin, &makePrOptions{
				Repo:   repo,
				Title:  fmt.Sprintf("Title %d", i+1),
				Source: fmt.Sprintf("pr-releases-branch-%d", i),
				Target: "master",
			})
			_, err := pb.NewPRServiceClient(suite.grpcClient).Merge(ctx, &pb.MergeRequest{
				PrId: grpc_marshalling.IDInverse(pr.ID),
				MergeParameters: &pb.MergeParameters{
					Rebase: utils.PtrFromValue(params.rebase),
					Squash: utils.PtrFromValue(params.squash),
				},
				Force: true,
			})
			require.NoError(t, err)

			suite.WaitForWorkflows(t, entities.WorkflowTypes.MergeV2PullRequest)
			pr, err = suite.PullRequestService.Get(ctx, pr.ID)
			require.NoError(t, err)
			require.Equal(t, entities.PRStatuses.Merged, pr.Status)

			prIDs = append(prIDs, grpc_marshalling.IDInverse(pr.ID))
		}

		master = suite.resolveBranch(t, repo, "master")
		changes, err := client.GetChanges(ctx, &pb.GetReleaseChangesRequest{
			RepoId:      repoID,
			ReleaseHash: master.String(),
		})
		require.NoError(t, err)
		require.Len(t, changes.Commits, 5)
		require.Equal(t, master.String(), changes.Commits[0].Hash)
		checkCommitMessages(t, changes.Commits, []string{
			"From PR 3 (another one)",
			"From PR 3",    // rebased, so all commits are shown
			"Title 2 (!2)", // squashed
			"Title 1 (!1)", // non-squashed (skip all but first parent)
			"Z",
		})

		require.NotNil(t, changes.Commits[0].PrId)
		require.Equal(t, prIDs[2], *changes.Commits[0].PrId)
		require.NotNil(t, changes.Commits[2].PrId)
		require.Equal(t, prIDs[1], *changes.Commits[2].PrId)
		require.NotNil(t, changes.Commits[3].PrId)
		require.Equal(t, prIDs[0], *changes.Commits[3].PrId)
	})
}

func (suite *RwApiTestSuite) TestReleases_DeleteTag() {
	t := suite.T()
	client := pb.NewGenericReleaseServiceClient(suite.grpcClient)
	ctx := testutils.AuthorizeGRPC(suite.users.Admin.Identity)

	repo := suite.repos.Alpha

	suite.mustBash(repo, `
		git tag v1.0.0
		touch x
		git add . && git commit -m "new commit"
	`)

	release := suite.createReleaseGRPC(t, suite.users.Admin, &createReleaseOptions{Tag: "v1.0.0"})
	cg, _ := suite.initCGit(suite.users.Admin, repo.FullSlug())
	cg.Must(t, "push", "origin", "--delete", "v1.0.0")

	updatedRelease, err := client.Get(ctx, &pb.GenericGetReleaseRequest{
		Id: release.Id,
	})
	require.NoError(t, err)
	require.Equal(t, pb.Release_STATUS_DRAFT, updatedRelease.ReleaseStatus)
}

func (suite *RwApiTestSuite) TestReleases_UpdateDiscard() {
	t := suite.T()
	client := pb.NewGenericReleaseServiceClient(suite.grpcClient)
	ctx := testutils.AuthorizeGRPC(suite.users.Admin.Identity)
	authenticator := suite.getFakeAuthenticator(suite.users.Admin.Identity)

	repo := suite.repos.Alpha
	repoID := grpc_marshalling.IDInverse(repo.ID)

	var release *pb.Release

	t.Run("create draft with no branch and no tag - fail", func(t *testing.T) {
		_, err := client.Create(ctx, &pb.GenericCreateReleaseRequest{
			RepoId:        repoID,
			Tag:           "v1.0.0",
			CreateTagFrom: nil,
			Title:         "Default",
			Publish:       false,
		})
		yarequire.ProtoStatusEqual(t, codes.NotFound, err)
	})

	t.Run("create draft - no new tag", func(t *testing.T) {
		release = suite.createReleaseGRPC(t, suite.users.Admin, &createReleaseOptions{
			Tag:             "v1.0.0",
			TagSourceBranch: utils.PtrFromValue("master"),
			Publish:         utils.PtrFromValue(false),
		})
		_, err := suite.GitcoreClient.GetCommit(context.Background(), authenticator, interfaces.GetCommitArgs{
			Revision: entities.NewGitRevisionFromTag(repo.ID, "v1.0.0"),
		})
		require.ErrorIs(t, err, except.ReferenceNotFound)
		require.Equal(t, "", release.Hash)
	})

	t.Run("edit draft tag", func(t *testing.T) {
		op, err := client.Update(ctx, &pb.GenericUpdateReleaseRequest{
			Id:            release.Id,
			Tag:           "new-tag",
			CreateTagFrom: utils.PtrFromValue("release/new-branch"),
			UpdateMask: &fieldmaskpb.FieldMask{
				Paths: []string{"tag", "create_tag_from"},
			},
		})
		require.NoError(t, err)

		require.NoError(t, op.GetResponse().UnmarshalTo(release))
		require.Equal(t, "", release.Hash)
		require.Equal(t, pb.Release_STATUS_DRAFT, release.ReleaseStatus)
		require.Equal(t, "new-tag", release.Tag)
		require.NotNil(t, release.CreateTagFrom)
		require.Equal(t, "release/new-branch", *release.CreateTagFrom)
	})

	t.Run("discard draft - fail", func(t *testing.T) {
		_, err := client.Discard(ctx, &pb.DiscardReleaseRequest{
			Id: release.Id,
		})
		yarequire.ProtoStatusEqual(t, codes.FailedPrecondition, err)
	})

	t.Run("edit and publish with branch", func(t *testing.T) {
		suite.mustBash(repo, `
			git checkout -b release/new-branch
			touch file
			git add . && git commit -m "New branch"
		`)
		head := suite.resolveBranch(t, repo, "release/new-branch")

		op, err := client.Update(ctx, &pb.GenericUpdateReleaseRequest{
			Id:      release.Id,
			Publish: true,
			Tag:     "v1.0.0",
			Title:   "Great published release",
			UpdateMask: &fieldmaskpb.FieldMask{
				Paths: []string{"publish", "tag", "title"},
			},
		})
		require.NoError(t, err)

		require.NoError(t, op.GetResponse().UnmarshalTo(release))
		require.Equal(t, head.String(), release.Hash)
		require.Equal(t, pb.Release_STATUS_PUBLISHED, release.ReleaseStatus)

		tag, err := suite.GitcoreClient.GetCommit(context.Background(), authenticator, interfaces.GetCommitArgs{
			Revision: entities.NewGitRevisionFromTag(repo.ID, "v1.0.0"),
		})
		require.NoError(t, err)
		require.Equal(t, head.String(), tag.Hash.String())
	})

	t.Run("edit published tag - fail", func(t *testing.T) {
		suite.mustBash(repo, `
			touch data
			git add . && git commit -m "New branch"
			git tag other-tag
		`)

		_, err := client.Update(ctx, &pb.GenericUpdateReleaseRequest{
			Id:  release.Id,
			Tag: "other-tag",
			UpdateMask: &fieldmaskpb.FieldMask{
				Paths: []string{"tag"},
			},
		})
		yarequire.ProtoStatusEqual(t, codes.FailedPrecondition, err)
	})

	t.Run("auto-draft", func(t *testing.T) {
		cg, _ := suite.initCGit(suite.users.Admin, repo.FullSlug())
		cg.Must(t, "push", "origin", "--delete", "v1.0.0")

		var err error
		release, err = client.Get(ctx, &pb.GenericGetReleaseRequest{
			Id: release.Id,
		})
		require.NoError(t, err)
		require.Equal(t, pb.Release_STATUS_DRAFT, release.ReleaseStatus)
	})

	t.Run("re-publish from other branch", func(t *testing.T) {
		head := suite.resolveBranch(t, repo, "master")

		op, err := client.Update(ctx, &pb.GenericUpdateReleaseRequest{
			Id:            release.Id,
			Publish:       true,
			CreateTagFrom: utils.PtrFromValue("master"),
			UpdateMask: &fieldmaskpb.FieldMask{
				Paths: []string{"publish", "create_tag_from"},
			},
		})
		require.NoError(t, err)

		require.NoError(t, op.GetResponse().UnmarshalTo(release))
		require.Equal(t, head.String(), release.Hash)
		require.Equal(t, pb.Release_STATUS_PUBLISHED, release.ReleaseStatus)
	})

	t.Run("discard", func(t *testing.T) {
		op, err := client.Discard(ctx, &pb.DiscardReleaseRequest{
			Id: release.Id,
		})
		require.NoError(t, err)
		require.NoError(t, op.GetResponse().UnmarshalTo(release))
		require.Equal(t, pb.Release_STATUS_DISCARDED, release.ReleaseStatus)
	})

	t.Run("re-publish - fail", func(t *testing.T) {
		_, err := client.Update(ctx, &pb.GenericUpdateReleaseRequest{
			Id:            release.Id,
			Publish:       true,
			CreateTagFrom: utils.PtrFromValue("master"),
			UpdateMask: &fieldmaskpb.FieldMask{
				Paths: []string{"publish"},
			},
		})
		yarequire.ProtoStatusEqual(t, codes.FailedPrecondition, err)
	})
}

func (suite *RwApiTestSuite) TestGrpcReleases_ListSortedFiltered() {
	t := suite.T()
	client := pb.NewGenericReleaseServiceClient(suite.grpcClient)
	ctx := testutils.AuthorizeGRPC(suite.users.Admin.Identity)

	repo := suite.repos.Alpha
	repoID := grpc_marshalling.IDInverse(repo.ID)

	var releaseTags []string
	for i, data := range []struct {
		tag          string
		title        string
		releaseNotes string
		publish      bool
	}{
		{
			tag:   "v1.0.0",
			title: "v1.0.0 - Normal release naming",
			releaseNotes: `# Header

- something
- something else
- new commit [ahfd53cc] by Krosh: PR !1: Release of releases
			`,
			publish: true,
		},
		{
			tag:   "v2.0-nightly",
			title: "Nightly build",
			releaseNotes: `# Notice

This is a nightly build and might behave unstably
			`,
			publish: true,
		},
		{
			tag:   "v2.0.1",
			title: "v2.0.1 - Something new",
			releaseNotes: `# Header

- something more
- new commit [bfbb66db] by Krosh: PR !2: message
- new commit [bfbb66da] by Barash: PR !3: message
			`,
			publish: false,
		},
		{
			tag:   "v2.1.0",
			title: "v2.1.0 - Latest stable",
			releaseNotes: `# Header

- something more
- new commit [bfbb66dc] by Barash: PR !4: message
- new commit [bfbb66db] by Krosh: PR !2: message
- new commit [bfbb66da] by Barash: PR !3: message
			`,
			publish: true,
		},
		{
			tag:     "not-a-version",
			title:   "Exclusive Summer Release",
			publish: false,
		},
	} {
		suite.mustBash(repo, fmt.Sprintf(`
			touch x-%d
			git add . && git commit -m "Something happened"
			git tag %s
		`, i+1, data.tag))

		release := suite.createReleaseGRPC(t, suite.users.Admin, &createReleaseOptions{
			Tag:          data.tag,
			Title:        data.title,
			ReleaseNotes: data.releaseNotes,
			Publish:      &data.publish,
		})
		releaseTags = append(releaseTags, release.Tag)
	}

	releaseTagsReversed := slices.Clone(releaseTags)
	slices.Reverse(releaseTagsReversed)

	for _, tc := range []struct {
		name        string
		query       string
		sortOptions []*pbPagination.SortOption
		expectTags  []string
	}{
		{
			name:       "no filter, default sort",
			query:      "",
			expectTags: releaseTagsReversed,
		},
		{
			name:  "no filter, sort by released_at desc",
			query: "",
			sortOptions: []*pbPagination.SortOption{
				{
					Column:    string(entities.ReleaseSorts.ReleasedAt),
					Direction: pbPagination.SortOption_DESC,
				},
				{
					Column:    string(entities.ReleaseSorts.CreatedAt),
					Direction: pbPagination.SortOption_ASC,
				},
			},
			expectTags: []string{releaseTags[3], releaseTags[1], releaseTags[0], releaseTags[2], releaseTags[4]},
		},
		{
			name:       "filter by tag, default sort",
			query:      "v2.",
			expectTags: releaseTagsReversed[1:4],
		},
		{
			name:  "filter by user mention, sort by released_at asc",
			query: "Krosh",
			sortOptions: []*pbPagination.SortOption{
				{
					Column:    string(entities.ReleaseSorts.ReleasedAt),
					Direction: pbPagination.SortOption_ASC,
				},
			},
			expectTags: []string{releaseTags[0], releaseTags[3], releaseTags[2]},
		},
		{
			name:       "filter by release notes message, default sort",
			query:      "Release of releases",
			expectTags: []string{releaseTags[0]},
		},
		{
			name:       "filter by commit mention, default sort",
			query:      "bfbb66d",
			expectTags: []string{releaseTags[3], releaseTags[2]},
		},
		{
			name:       "filter by nightly, default sort",
			query:      "nightly",
			expectTags: []string{releaseTags[1]},
		},
		{
			name:       "filter by title, default sort",
			query:      "summer",
			expectTags: []string{releaseTags[4]},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			response, err := client.List(ctx, &pb.ListReleasesRequest{
				RepoId:   repoID,
				Query:    tc.query,
				PageSize: utils.PtrFromValue(uint64(100)),
				SortBy:   tc.sortOptions,
			})
			require.NoError(t, err)

			gotTags := functools.Map(response.Releases, (*pb.Release).GetTag)
			require.EqualValues(t, tc.expectTags, gotTags)
		})
	}
}

func (suite *RwApiTestSuite) TestGrpcReleases_AuthMatrix() {
	t := suite.T()
	client := pb.NewGenericReleaseServiceClient(suite.grpcClient)
	ctx := testutils.AuthorizeGRPC(suite.users.Admin.Identity)

	repo := suite.repos.AuthRepoPublic
	repoID := grpc_marshalling.IDInverse(repo.ID)
	suite.addRole(t, suite.users.Admin, repo, iam.Roles.Admin)

	release := suite.createReleaseGRPC(t, suite.users.Admin, &createReleaseOptions{
		Repo:            repo,
		Tag:             "0.0.1",
		TagSourceBranch: utils.PtrFromValue("master"),
	})
	suite.mustBash(repo, `touch a && git add . && git commit -m "A"`)
	draft := suite.createReleaseGRPC(t, suite.users.Admin, &createReleaseOptions{
		Repo:            repo,
		Tag:             "0.0.2",
		TagSourceBranch: utils.PtrFromValue("master"),
		Publish:         utils.PtrFromValue(false),
	})
	suite.mustBash(repo, `touch b && git add . && git commit -m "B"`)
	discarded := suite.createReleaseGRPC(t, suite.users.Admin, &createReleaseOptions{
		Repo:            repo,
		Tag:             "0.0.3",
		TagSourceBranch: utils.PtrFromValue("master"),
	})
	_, err := client.Discard(ctx, &pb.DiscardReleaseRequest{
		Id: discarded.Id,
	})
	require.NoError(t, err)

	t.Run("get draft - maintainer and above", func(t *testing.T) {
		suite.AuthMatrix(t, func(ctx context.Context) error {
			_, err = client.Get(ctx, &pb.GenericGetReleaseRequest{
				Id: draft.Id,
			})
			return err
		}).SpecificUsers(suite.users.AuthMaintainer, suite.users.AuthAdmin)
	})
	t.Run("get discarded - maintainer and above", func(t *testing.T) {
		suite.AuthMatrix(t, func(ctx context.Context) error {
			_, err = client.Get(ctx, &pb.GenericGetReleaseRequest{
				Id: discarded.Id,
			})
			return err
		}).SpecificUsers(suite.users.AuthMaintainer, suite.users.AuthAdmin)
	})
	t.Run("list - everyone", func(t *testing.T) {
		gotIDs := make(map[uint64][]string)

		suite.AuthMatrix(t, func(ctx context.Context) error {
			userIdx := testutils.GetAuthorizedUser(ctx)
			require.NotNil(t, userIdx)
			user, err := suite.UserRepo.GetUser(ctx, *userIdx)
			require.NoError(t, err)

			result, err := client.List(ctx, &pb.ListReleasesRequest{
				RepoId:   repoID,
				PageSize: utils.PtrFromValue(uint64(10)),
			})
			if err != nil {
				return err
			}

			gotIDs[user.ID] = functools.Map(result.Releases, (*pb.Release).GetId)
			return nil
		}).AllUsers()

		// require.Equal(t, []string{discarded.Id, draft.Id, release.Id}, gotIDs[suite.users.AuthOwner.ID])
		require.Equal(t, []string{discarded.Id, draft.Id, release.Id}, gotIDs[suite.users.AuthAdmin.ID])
		require.Equal(t, []string{discarded.Id, draft.Id, release.Id}, gotIDs[suite.users.AuthMaintainer.ID])
		require.Equal(t, []string{release.Id}, gotIDs[suite.users.AuthDeveloper.ID])
		require.Equal(t, []string{release.Id}, gotIDs[suite.users.AuthContributor.ID])
		require.Equal(t, []string{release.Id}, gotIDs[suite.users.AuthMember.ID])
		require.Equal(t, []string{release.Id}, gotIDs[suite.users.AuthViewer.ID])
		require.Equal(t, []string{release.Id}, gotIDs[suite.users.AuthNobody.ID])
	})
	t.Run("update - maintainer and above", func(t *testing.T) {
		suite.AuthMatrix(t, func(ctx context.Context) error {
			_, err = client.Update(ctx, &pb.GenericUpdateReleaseRequest{
				Id:         release.Id,
				Title:      "New title",
				UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{"title"}},
			})
			return err
		}).SpecificUsers(suite.users.AuthMaintainer, suite.users.AuthAdmin)
	})
}

func (suite *RwApiTestSuite) TestGrpcReleaseAssets_Quotas() {
	t := suite.T()
	user := suite.users.Admin
	ctx := testutils.AuthorizeGRPC(user.Identity)
	client := pb.NewGenericReleaseServiceClient(suite.grpcClient)

	repo := suite.repos.Alpha
	orgID := repo.OrgID

	ensureOrgQuota := func(t *testing.T, expected int64) {
		assetsQuota, err := suite.quotaService.Get(ctx, orgID, entities.Quotas.ReleaseAssetsSize)
		require.NoError(t, err)
		require.EqualValues(t, expected, assetsQuota.Usage)
	}
	ensureUserTmpQuota := func(t *testing.T, expected int64) {
		userTmpQuota, err := suite.AttachmentRepo.GetTemporaryAttachmentsSize(ctx, user.ID)
		require.NoError(t, err)
		require.EqualValues(t, expected, userTmpQuota)
	}

	var attachment schemas.UploadAttachmentResponse
	var expectedAttachmentSize int64 = 6022
	release := suite.createReleaseGRPC(t, suite.users.Admin, &createReleaseOptions{
		Tag:             "1.0.0",
		TagSourceBranch: utils.PtrFromValue("master"),
	})

	t.Run("can't upload - user quota", func(t *testing.T) {
		tmp := suite.cfg.S3.Attachments.TemporaryAttachmentQuotaMB
		suite.cfg.S3.Attachments.TemporaryAttachmentQuotaMB = 0

		var result schemas.UploadAttachmentResponse
		// StorageLimitReached translates to BadRequest, TODO maybe fix?
		suite.uploadReleaseAttachment(t, user.Identity, "document.pdf", repo, &result, http.StatusBadRequest)
		ensureOrgQuota(t, 0)
		ensureUserTmpQuota(t, 0)

		suite.cfg.S3.Attachments.TemporaryAttachmentQuotaMB = tmp
	})

	t.Run("can't upload - max file size", func(t *testing.T) {
		tmp := suite.cfg.S3.Attachments.Release.MaxFileSizeMB
		suite.cfg.S3.Attachments.Release.MaxFileSizeMB = 0

		var result schemas.UploadAttachmentResponse
		// StorageLimitReached translates to BadRequest, TODO maybe fix?
		suite.uploadReleaseAttachment(t, user.Identity, "document.pdf", repo, &result, http.StatusBadRequest)
		ensureOrgQuota(t, 0)
		ensureUserTmpQuota(t, 0)

		suite.cfg.S3.Attachments.Release.MaxFileSizeMB = tmp
	})

	t.Run("can't upload - org quota", func(t *testing.T) {
		suite.setQuotaLimit(t, orgID, entities.Quotas.ReleaseAssetsSize, 0)

		var result schemas.UploadAttachmentResponse
		// QuotaLimitExceeded translates to TooManyRequests, TODO maybe fix?
		suite.uploadReleaseAttachment(t, user.Identity, "document.pdf", repo, &result, http.StatusTooManyRequests)
		ensureOrgQuota(t, 0)
		ensureUserTmpQuota(t, 0)
	})

	t.Run("can upload", func(t *testing.T) {
		suite.setQuotaLimit(t, orgID, entities.Quotas.ReleaseAssetsSize, 1024*1024*1024)

		suite.uploadReleaseAttachment(t, user.Identity, "document.pdf", repo, &attachment, http.StatusOK)
		ensureOrgQuota(t, 0)
		ensureUserTmpQuota(t, expectedAttachmentSize)
	})

	t.Run("can't attach uploaded", func(t *testing.T) {
		suite.setQuotaLimit(t, orgID, entities.Quotas.ReleaseAssetsSize, 0)

		_, err := client.Update(ctx, &pb.GenericUpdateReleaseRequest{
			Id: release.Id,
			AssetDeltas: &pb.GenericUpdateReleaseRequest_AssetDeltas{
				AddAssets: []*pb.ReleaseAssetInput{
					{
						Name: "document",
						Reference: &pb.ReleaseAssetInput_AttachmentId{
							AttachmentId: attachment.ID,
						},
					},
				},
			},
			UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{"asset_deltas"}},
		})
		yarequire.ProtoStatusEqual(t, codes.ResourceExhausted, err)
		ensureOrgQuota(t, 0)
		ensureUserTmpQuota(t, expectedAttachmentSize)
	})

	t.Run("can attach uploaded", func(t *testing.T) {
		suite.setQuotaLimit(t, orgID, entities.Quotas.ReleaseAssetsSize, 1024*1024*1024)

		_, err := client.Update(ctx, &pb.GenericUpdateReleaseRequest{
			Id: release.Id,
			AssetDeltas: &pb.GenericUpdateReleaseRequest_AssetDeltas{
				AddAssets: []*pb.ReleaseAssetInput{
					{
						Name: "document",
						Reference: &pb.ReleaseAssetInput_AttachmentId{
							AttachmentId: attachment.ID,
						},
					},
				},
			},
			UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{"asset_deltas"}},
		})
		require.NoError(t, err)
		ensureOrgQuota(t, expectedAttachmentSize)
		ensureUserTmpQuota(t, 0)
	})
}

func (suite *RwApiTestSuite) TestGrpcReleases_RecreateAfterSoftDelete() {
	t := suite.T()
	client := pb.NewGenericReleaseServiceClient(suite.grpcClient)
	ctx := testutils.AuthorizeGRPC(suite.users.Admin.Identity)

	repo := suite.repos.Alpha
	repoID := grpc_marshalling.IDInverse(repo.ID)

	suite.mustBash(repo, `
		git checkout -b master
		git tag v99.0.0
		touch x99
		git add . && git commit -m "commit v99"
	`)

	t.Run("recreate release after soft-delete: success", func(t *testing.T) {
		// Create first release
		firstRelease := suite.createReleaseGRPC(t, suite.users.Admin, &createReleaseOptions{
			Tag:          "v99.0.0",
			Title:        "First Release",
			ReleaseNotes: "Initial release notes",
			Publish:      utils.PtrFromValue(true),
		})
		require.NotEmpty(t, firstRelease.Id)

		// Soft-delete the release
		_, err := client.Delete(ctx, &pb.GenericDeleteReleaseRequest{
			Id: firstRelease.Id,
		})
		require.NoError(t, err)

		// Verify first release is deleted
		response, err := client.List(ctx, &pb.ListReleasesRequest{
			RepoId:   repoID,
			PageSize: utils.PtrFromValue(uint64(10)),
		})
		require.NoError(t, err)

		// First release should not be in the list
		for _, release := range response.Releases {
			require.NotEqual(t, firstRelease.Id, release.Id, "Deleted release should not appear in list")
		}

		// Create second release with the same tag
		secondRelease := suite.createReleaseGRPC(t, suite.users.Admin, &createReleaseOptions{
			Tag:          "v99.0.0",
			Title:        "Second Release",
			ReleaseNotes: "New release notes after soft-delete",
			Publish:      utils.PtrFromValue(true),
		})
		require.NotEmpty(t, secondRelease.Id)

		// Verify it's a different release
		require.NotEqual(t, firstRelease.Id, secondRelease.Id, "New release should have different ID")

		// Verify the new release has correct data
		require.Equal(t, "v99.0.0", secondRelease.Tag)
		require.Equal(t, "Second Release", secondRelease.Title)
		require.Equal(t, "New release notes after soft-delete", secondRelease.ReleaseNotes)
		require.Equal(t, pb.Release_STATUS_PUBLISHED, secondRelease.ReleaseStatus)

		// Verify we can retrieve the new release by tag
		retrievedRelease, err := client.GetByTag(ctx, &pb.GetReleaseByTagRequest{
			Repo: &pb.GetReleaseByTagRequest_RepoId{
				RepoId: repoID,
			},
			Tag: "v99.0.0",
		})
		require.NoError(t, err)
		require.Equal(t, secondRelease.Id, retrievedRelease.Id, "Should retrieve the new release, not the deleted one")
		require.Equal(t, "Second Release", retrievedRelease.Title)
	})
}

func (suite *RwApiTestSuite) UploadReleaseAttachment(
	identity entities.UserIdentity,
	fileName string,
	repo *entities.Repository,
) *schemas.UploadAttachmentResponse {
	var result schemas.UploadAttachmentResponse
	suite.uploadReleaseAttachment(suite.T(), identity, fileName, repo, &result, 200)
	return &result
}
