package integrationtests

import (
	"common/testutils/yarequire"
	"common/utils"
	"common/utils/rolesgenerator/iam"
	"context"
	"gitcore/internal/adapters/opensearch/mappings"
	"gitcore/internal/entities"
	except "gitcore/internal/exceptions"
	grpc_marshalling "gitcore/internal/grpcserver/marshalling"
	"gitcore/internal/services/access"
	"gitcore/internal/testutils"
	pb "private_api/generated/yandex/cloud/priv/gitcore/v1"
	"private_api/generated/yandex/cloud/priv/saas"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func runDeleteInstance(ctx context.Context, suite *RwApiTestSuite, org *entities.Organization) {
	t := suite.T()

	instanceClient := saas.NewInstanceServiceClient(suite.grpcClient)
	operationClient := pb.NewOperationServiceClient(suite.grpcClient)

	resp, err := instanceClient.Delete(ctx, &saas.DeleteInstanceRequest{
		OrganizationId: org.Identity.ID, ServiceId: "src",
	})
	require.NoError(t, err)
	require.False(t, resp.Done)

	suite.WaitForWorkflows(t, entities.WorkflowTypes.DeleteInstance)

	op, err := operationClient.Get(ctx, &pb.GetOperationRequest{Id: resp.Id})
	require.NoError(t, err)
	require.True(t, op.Done)

	md := testutils.UnmarshalGrpcMetadata[*pb.OperationMetadata](t, op)
	require.Equal(t, pb.OperationMetadata_SUCCESS, md.Status)
}

func (suite *RwApiTestSuite) TestDeleteInstanceEmptyOrg() {
	t := suite.T()

	org := suite.orgs.Yandex42
	user := suite.users.Admin

	ctx := testutils.AuthorizeGRPC(user.Identity)

	err := suite.AccessBindingsService.CreateBindings(ctx, access.StubAuthenticator, []*entities.AccessBinding{{
		Subject: user.Subject(),
		Object: entities.IAMObject{
			Type: entities.ObjectTypes.Organization,
			ID:   org.ID,
		},
		Role: iam.Roles.InternalOrganizationManagerReaperAgent,
	}})
	require.NoError(t, err)

	runDeleteInstance(ctx, suite, org)

	_, err = suite.OrgService.GetOrganizationByID(ctx, nil, org.ID)
	require.ErrorIs(t, err, except.OrgNotFound)
}

func (suite *RwApiTestSuite) TestDeleteInstanceWithData() {
	suite.RestoreOpensearch()

	t := suite.T()

	org := suite.orgs.Yandex42
	user := suite.users.Admin
	forkUser := suite.users.Krosh

	ctx := testutils.AuthorizeGRPC(user.Identity)

	require.NoError(t, suite.OrgRepo.UpdateOrganizationByID(org.ID).SetVisibility(entities.Visibilities.Public).Commit(ctx))
	require.NoError(t, suite.AccessBindingsService.CreateBindings(ctx, access.NullAuthenticator, []*entities.AccessBinding{
		{Subject: forkUser.Subject(), Object: org.Object(), Role: iam.Roles.RepositoriesMaintainer},
	}))
	require.NoError(t, suite.AccessBindingsService.CreateBindings(ctx, access.NullAuthenticator, []*entities.AccessBinding{
		{Subject: user.Subject(), Object: org.Object(), Role: iam.Roles.RepositoriesMaintainer},
	}))

	po, err := suite.OrgService.GetPersonalOrganization(ctx, nil, forkUser)
	require.NoError(t, err)
	require.NoError(t, suite.AccessBindingsService.CreateBindings(ctx, access.NullAuthenticator, []*entities.AccessBinding{
		{Subject: user.Subject(), Object: po.Object(), Role: iam.Roles.RepositoriesMaintainer},
	}))

	_, err = suite.QuotaService.Get(ctx, org.Identity.ID)
	require.NoError(t, err)

	require.NoError(t, suite.AccessBindingsService.CreateBindings(ctx, access.StubAuthenticator, []*entities.AccessBinding{
		{Subject: user.Subject(), Object: org.Object(), Role: iam.Roles.InternalOrganizationManagerReaperAgent},
	}))

	suite.repos.Alpha = suite.ImportRepo(org, "alpha", testutils.BasicRepo, nil)
	suite.repos.History = suite.ImportRepo(org, "history", "history.git", nil)
	suite.repos.ListTree = suite.ImportRepo(org, "listtree", "generated/listtree", nil)
	repoIDs, err := suite.RepoRepo.ListAllRepoIDs(ctx, org.ID)
	require.NoError(t, err)
	require.Equal(t, 3, len(repoIDs))

	alphaIDstr := grpc_marshalling.IDInverse(suite.repos.Alpha.ID)
	orgIDstr := grpc_marshalling.IDInverse(org.ID)

	alphaFork := suite.forkRepo(t, forkUser, alphaIDstr)
	alphaForkID, err := grpc_marshalling.IDDirect(alphaFork.Id)
	require.NoError(t, err)
	af1 := suite.forkRepoToOrg(t, user, alphaIDstr, orgIDstr)
	_ = suite.forkRepoToOrg(t, user, af1.Id, orgIDstr) // af2
	afp := suite.forkRepo(t, forkUser, af1.Id)
	_ = suite.forkRepoToOrg(t, user, afp.Id, orgIDstr) // afp1

	// Yandex42 | alpha -> af1 -> af2   afp1
	//				 |        \         >
	//               V         V      /
	// Personal | alphaFork      afp

	packIDs, err := suite.StorageBackend.GetAllPackFileIDsS3(ctx, suite.repos.Alpha.ID)

	require.NoError(t, err)
	require.NotEqual(t, 0, len(packIDs))

	pr := suite.makePullRequest(user, &makePrOptions{
		Repo:    suite.repos.Alpha,
		Title:   "PR#1",
		Source:  "branch",
		Target:  "master",
		Publish: utils.PtrFromValue(true),
	})

	suite.makeIssueComment(user, &makeIssueCommentOptions{
		RepoID: &suite.repos.Alpha.ID,
		Body:   "comment",
	})
	suite.makeIssueComment(user, &makeIssueCommentOptions{
		RepoID: &suite.repos.History.ID,
		Body:   "comment",
	})
	issueIDs, err := suite.IssueRepo.ListAllIDsForRepos(ctx, []uint64{suite.repos.Alpha.ID, suite.repos.History.ID})
	require.NoError(t, err)
	require.Equal(t, 2, len(issueIDs))

	suite.WaitForWorkflows(t, entities.WorkflowTypes.SearchIndexWorkflow)
	suite.OpensearchKit.RefreshIndex(ctx)

	hits, err := suite.OpensearchKit.GetDocumentsByType(ctx, mappings.IssueMappingType)
	require.NoError(t, err)
	require.Equal(t, 2, len(hits))

	hits, err = suite.OpensearchKit.GetDocumentsByType(ctx, mappings.PullRequestMappingType)
	require.NoError(t, err)
	require.Equal(t, 1, len(hits))

	// Delete everything in given org
	runDeleteInstance(ctx, suite, org)

	// Check that fork is now "unforked"
	suite.WaitForWorkflows(t, entities.WorkflowTypes.DeleteInstance)
	forkEntity, err := suite.RepoService.Get(ctx, alphaForkID)
	require.NoError(t, err)
	require.Nil(t, forkEntity.ForkOriginID)
	packIDs, err = suite.StorageBackend.GetAllPackFileIDsS3(ctx, alphaForkID)
	require.NoError(t, err)
	require.Greater(t, len(packIDs), 0)
	gitFSLoader := suite.GitFSFactory.Build(alphaForkID)
	gitFS, closeFunc, err := gitFSLoader.Load(ctx)
	require.NoError(t, err)
	defer closeFunc()
	// just do smth with pack to check if it really exists...
	require.NoError(t, gitFS.DownloadPackAndIndex(ctx, packIDs[0]))

	fork2ID, err := grpc_marshalling.IDDirect(afp.Id)
	require.NoError(t, err)
	fork2, err := suite.RepoService.Get(ctx, fork2ID)
	require.NoError(t, err)
	require.Nil(t, fork2.ForkOriginID)

	// Now check that everything is deleted
	repoIDs, err = suite.RepoRepoWithDeleted.ListAllRepoIDs(ctx, org.ID)
	require.NoError(t, err)
	require.Equal(t, 0, len(repoIDs))

	packIDs, err = suite.StorageBackend.GetAllPackFileIDsS3(ctx, suite.repos.Alpha.ID)
	require.NoError(t, err)
	require.Equal(t, 0, len(packIDs))

	issueIDs, err = suite.IssueRepo.ListAllIDsForRepos(ctx, []uint64{suite.repos.Alpha.ID, suite.repos.History.ID})
	require.NoError(t, err)
	require.Equal(t, 0, len(issueIDs))

	_, err = suite.PullRequestService.Get(ctx, pr.ID)
	require.ErrorIs(t, err, except.EntityNotFound)

	_, err = suite.OrgService.GetOrganizationByID(ctx, nil, org.ID)
	require.ErrorIs(t, err, except.OrgNotFound)

	_, err = suite.QuotaService.Get(ctx, org.Identity.ID)
	yarequire.ProtoStatusEqual(t, codes.NotFound, err)

	suite.OpensearchKit.RefreshIndex(ctx)

	hits, err = suite.OpensearchKit.GetDocumentsByType(ctx, mappings.IssueMappingType)
	require.NoError(t, err)
	require.Equal(t, 0, len(hits))

	hits, err = suite.OpensearchKit.GetDocumentsByType(ctx, mappings.PullRequestMappingType)
	require.NoError(t, err)
	require.Equal(t, 0, len(hits))
}

func (suite *RwApiTestSuite) TestDeleteInstanceWithReservedSlugs() {
	t := suite.T()

	org := suite.orgs.Yandex42
	user := suite.users.Admin

	ctx := testutils.AuthorizeGRPC(user.Identity)

	require.NoError(t, suite.AccessBindingsService.CreateBindings(ctx, access.NullAuthenticator, []*entities.AccessBinding{
		{Subject: user.Subject(), Object: org.Object(), Role: iam.Roles.InternalOrganizationManagerReaperAgent},
	}))
	require.NoError(t, suite.AccessBindingsService.CreateBindings(ctx, access.NullAuthenticator, []*entities.AccessBinding{
		{Subject: user.Subject(), Object: org.Object(), Role: iam.Roles.OrganizationManagerAdmin},
	}))

	// Update slug to reserve previous one
	client := pb.NewOrgServiceClient(suite.grpcClient)
	_, err := client.UpdateSlug(ctx, &pb.UpdateOrgSlugRequest{
		Id:   grpc_marshalling.IDInverse(org.ID),
		Slug: "new-slug",
	})
	require.NoError(t, err)

	_, err = client.CreateOrg(ctx, &pb.CreateOrgRequest{
		Slug:        "new-slug",
		DisplayName: "new org",
	})
	require.Error(t, err)
	st, ok := status.FromError(err)
	require.True(t, ok)
	require.Equal(t, "slug \"new-slug\" is already occupied", st.Message())

	runDeleteInstance(ctx, suite, org)

	_, err = suite.OrgService.GetOrganizationByID(ctx, nil, org.ID)
	require.ErrorIs(t, err, except.OrgNotFound)

	_, err = client.CreateOrg(ctx, &pb.CreateOrgRequest{
		Slug:        "new-slug",
		DisplayName: "new org",
	})
	require.NoError(t, err)
}
