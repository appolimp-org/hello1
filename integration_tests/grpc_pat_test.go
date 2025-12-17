package integrationtests

import (
	"common/cgit"
	"common/functools"
	commongrpc "common/grpc"
	"common/pkgenerator"
	"common/testutils/yarequire"
	"common/utils"
	"common/utils/rolesgenerator/iam"
	"context"
	"fmt"
	"gitcore/internal/entities"
	grpc_marshalling "gitcore/internal/grpcserver/marshalling"
	"gitcore/internal/services/access/pat/common/jwt"
	"gitcore/internal/testutils"
	githubjwt "github.com/golang-jwt/jwt/v4"
	"path"
	pb "private_api/generated/yandex/cloud/priv/gitcore/v1"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/protobuf/types/known/fieldmaskpb"
)

func (suite *RwApiTestSuite) TestGrpcPAT_CRUD() {
	t := suite.T()

	user := suite.users.Pikachu

	pat1ID, _ := suite.createSimplePAT(t, user)
	pat2ID, _ := suite.createSimplePAT(t, user)
	pat3ID, _ := suite.createPATWithScope(t, user, []uint64{suite.repos.Alpha.ID}, nil, "")
	pat4ID, _ := suite.createPATWithScope(t, user, []uint64{suite.repos.Alpha.ID}, &iam.Roles.RepositoriesContributor, "")
	pat5ID, _ := suite.createEternalPAT(t, user)
	suite.createPAT(t, user, nil, nil, utils.PtrFromValue(time.Now().Add(time.Hour)), false, true, "")
	patServiceID, _ := suite.createServicePAT(t, user)
	pat6ID, _ := suite.createCodeAssistPAT(t, user, utils.PtrFromValue(time.Now().Add(time.Hour)), "vscode")
	pat7ID, _ := suite.createCodeAssistPAT(t, user, utils.PtrFromValue(time.Now().Add(time.Hour)), "vscode")

	client := pb.NewMeServiceClient(suite.grpcClient)
	ctx := testutils.AuthorizeGRPC(user.Identity)

	t.Run("list excluding service pat", func(t *testing.T) {
		suite.checkList(t, user, false, []string{pat1ID, pat2ID, pat3ID, pat4ID, pat5ID})
	})

	t.Run("list codeAssist pats", func(t *testing.T) {
		suite.checkList(t, user, true, []string{pat6ID, pat7ID})
	})

	t.Run("update", func(t *testing.T) {
		_, err := client.UpdatePAT(ctx, &pb.UpdatePATRequest{
			Id:          pat1ID,
			Name:        utils.PtrFromValue(user.Identity.ID + "updatedName"),
			Description: utils.PtrFromValue("testpat"),
			UpdateMask:  &fieldmaskpb.FieldMask{Paths: []string{"name", "description"}},
		})
		require.NoError(t, err)

		resp, err := client.ListPATs(ctx, &pb.ListPATsRequest{})
		require.NoError(t, err)

		res := functools.Filter(resp.Pats, func(pat *pb.PATInfo) bool {
			return pat.Id == pat1ID
		})
		require.Len(t, res, 1)
		require.Equal(t, user.Identity.ID+"updatedName", res[0].Name)
		require.Equal(t, "testpat", res[0].Description)

	})

	t.Run("cannot update service pat", func(t *testing.T) {
		_, err := client.UpdatePAT(ctx, &pb.UpdatePATRequest{
			Id:          patServiceID,
			Name:        utils.PtrFromValue(user.Identity.ID + "updatedName"),
			Description: utils.PtrFromValue("testpat"),
			UpdateMask:  &fieldmaskpb.FieldMask{Paths: []string{"name", "description"}},
		})
		yarequire.ProtoStatusEqual(t, codes.NotFound, err)
	})

	t.Run("cannot delete other user's pat", func(t *testing.T) {
		_, err := client.DeletePAT(testutils.AuthorizeGRPC(suite.users.Kopatych.Identity),
			&pb.DeletePATRequest{
				Id: pat1ID,
			})
		yarequire.ProtoStatusEqual(t, codes.NotFound, err)
	})

	t.Run("cannot delete service pat", func(t *testing.T) {
		_, err := client.DeletePAT(ctx, &pb.DeletePATRequest{
			Id: patServiceID,
		})
		yarequire.ProtoStatusEqual(t, codes.NotFound, err)
	})

	t.Run("delete", func(t *testing.T) {
		_, err := client.DeletePAT(ctx, &pb.DeletePATRequest{
			Id: pat1ID,
		})
		require.NoError(t, err)

		suite.checkList(t, user, false, []string{pat2ID, pat3ID, pat4ID, pat5ID})
	})
}

func (suite *RwApiTestSuite) TestGrpcPAT_Auth() {
	t := suite.T()

	_, orgSlug, repoSlug := suite.makeRepoWithVisibility(t, suite.users.Pikachu, entities.Visibilities.Private)
	repoURL := fmt.Sprintf("%s%s/%s.git", suite.gitHost, orgSlug, repoSlug)
	tmpDir := testutils.TempDir(t, "", repoSlug)

	client := pb.NewMeServiceClient(suite.grpcClient)
	ctx := testutils.AuthorizeGRPC(suite.users.Pikachu.Identity)

	t.Run("invalid pat", func(t *testing.T) {
		cg := cgit.NewCGit(tmpDir).WithAuthToken("123")
		_, _, err := cg.Exec("clone", repoURL)
		require.Error(t, err)
	})
	t.Run("deleted pat", func(t *testing.T) {
		patID, key := suite.createSimplePAT(t, suite.users.Pikachu)
		_, err := client.DeletePAT(ctx, &pb.DeletePATRequest{
			Id: patID,
		})
		require.NoError(t, err)
		cg := cgit.NewCGit(tmpDir).WithAuthToken(key)
		_, _, err = cg.Exec("clone", repoURL)
		require.Error(t, err)
	})
	t.Run("expired pat", func(t *testing.T) {
		expiresAt := time.Now().Add(time.Millisecond * 10)
		_, key := suite.createPATWithExpiration(t, suite.users.Pikachu, &expiresAt)

		time.Sleep(time.Millisecond * 10)

		cg := cgit.NewCGit(tmpDir).WithAuthToken(key)
		_, _, err := cg.Exec("clone", repoURL)
		require.Error(t, err)
	})
	t.Run("happy path", func(t *testing.T) {
		_, key := suite.createSimplePAT(t, suite.users.Pikachu)

		cg := cgit.NewCGit(tmpDir).WithAuthToken(key)
		cg.Must(t, "clone", repoURL)
		cg = cgit.NewCGit(path.Join(tmpDir, repoSlug)).WithAuthToken(key)
		cg.Must(t, "checkout", "-b", "feature")
		commit(t, &cg, path.Join(tmpDir, repoSlug, "file"), "content")
		cg.Must(t, "push", "--set-upstream", "origin", "feature")
	})
	t.Run("jwt pat", func(t *testing.T) {
		expiresAt := time.Now().Add(time.Hour)
		_, key := suite.createPAT(t, suite.users.Pikachu, nil, nil, &expiresAt, false, true, "")

		repoSlug = repoSlug + "jwt"
		tmpDir = testutils.TempDir(t, "", repoSlug)

		cg := cgit.NewCGit(tmpDir).WithAuthToken(key)
		cg.Must(t, "clone", repoURL)
	})
	t.Run("expired jwt pat", func(t *testing.T) {
		expiresAt := time.Now().Add(time.Millisecond * 10)
		_, key := suite.createPAT(t, suite.users.Pikachu, nil, nil, &expiresAt, false, true, "")

		time.Sleep(time.Millisecond * 10)

		cg := cgit.NewCGit(tmpDir).WithAuthToken(key)
		_, _, err := cg.Exec("clone", repoURL)
		require.Error(t, err)
	})
}

func (suite *RwApiTestSuite) TestCICDPAT_Subject() {
	t := suite.T()

	repoID, orgSlug, repoSlug := suite.makeRepoWithVisibility(t, suite.users.Pikachu, entities.Visibilities.Private)

	client := pb.NewMeServiceClient(suite.grpcClient)
	ctx := testutils.AuthorizeGRPC(suite.users.Pikachu.Identity)

	scope := &pb.CreatePATRequest_Scope{
		Role:    utils.PtrFromValue(string(iam.Roles.RepositoriesViewer)),
		RepoIds: []string{strconv.FormatUint(repoID, 10)},
	}

	tcases := []struct {
		cicd *pb.CreatePATRequest_CICDScope
		subj string
	}{
		{
			cicd: &pb.CreatePATRequest_CICDScope{
				FluxId:    1,
				Reference: utils.PtrFromValue("branch"),
			},
			subj: "repo:" + orgSlug + "/" + repoSlug + ":ref:branch",
		},
		{
			cicd: &pb.CreatePATRequest_CICDScope{
				FluxId:       1,
				SubjectScope: utils.PtrFromValue(pb.CreatePATRequest_CICDScope_SUBJECT_SCOPE_REFERENCE),
				Reference:    utils.PtrFromValue("branch"),
			},
			subj: "repo:" + orgSlug + "/" + repoSlug + ":ref:branch",
		},
		{
			cicd: &pb.CreatePATRequest_CICDScope{
				FluxId:       1,
				SubjectScope: utils.PtrFromValue(pb.CreatePATRequest_CICDScope_SUBJECT_SCOPE_REPO),
			},
			subj: "repo:" + orgSlug + "/" + repoSlug,
		},
		{
			cicd: &pb.CreatePATRequest_CICDScope{
				FluxId:       1,
				SubjectScope: utils.PtrFromValue(pb.CreatePATRequest_CICDScope_SUBJECT_SCOPE_ORG),
			},
			subj: "repo:" + orgSlug + "/*",
		},
	}

	for _, tt := range tcases {
		expiresAt := time.Now().Add(time.Hour)
		pat, err := client.CreatePAT(ctx, &pb.CreatePATRequest{
			ExpiresAt: commongrpc.TimeToProtocTsNullable(&expiresAt),
			Scope:     scope,
			IsJwt:     utils.PtrFromValue(true),
			Cicd:      tt.cicd,
		})
		require.NoError(t, err)

		var resp pb.CreatePATResponse
		require.NoError(t, pat.GetResponse().UnmarshalTo(&resp))

		claims := &jwt.Claims{}
		_, _, err = githubjwt.NewParser().ParseUnverified(resp.GetToken(), claims)
		require.NoError(t, err)

		require.Equal(t, tt.subj, claims.Subject)
	}
}

func (suite *RwApiTestSuite) checkList(t *testing.T, user *entities.User, codeAssist bool, expectedIDs []string) {
	client := pb.NewMeServiceClient(suite.grpcClient)
	ctx := testutils.AuthorizeGRPC(user.Identity)

	req := &pb.ListPATsRequest{}
	if codeAssist {
		req.CodeAssistsPatMode = utils.PtrFromValue(true)
	}

	resp, err := client.ListPATs(ctx, req)
	require.NoError(t, err)

	require.Equal(t, len(expectedIDs), len(resp.Pats))

	for i, expID := range expectedIDs {
		pat := resp.Pats[i]
		require.Equal(t, expID, pat.Id)
	}
}

func (suite *RwApiTestSuite) createServicePAT(t *testing.T, user *entities.User) (id string, token string) {
	pat, token, err := suite.PatService.Create(
		context.Background(),
		user,
		nil,
		entities.PAT{
			PATParams: entities.PATParams{
				Name:      "service-pat",
				IsService: true,
			},
		},
		true)
	require.NoError(t, err)

	return pat.ID, token
}

func (suite *RwApiTestSuite) createSimplePAT(t *testing.T, user *entities.User) (id string, token string) {
	return suite.createPATWithScope(t, user, nil, nil, "")
}

func (suite *RwApiTestSuite) createPATWithExpiration(t *testing.T, user *entities.User, expiresAt *time.Time) (id string, token string) {
	return suite.createPAT(t, user, nil, nil, expiresAt, false, false, "")
}

func (suite *RwApiTestSuite) createEternalPAT(t *testing.T, user *entities.User) (id string, token string) {
	return suite.createPAT(t, user, nil, nil, nil, true, false, "")
}

func (suite *RwApiTestSuite) createCodeAssistPAT(t *testing.T, user *entities.User, expiresAt *time.Time, ide string) (id string, token string) {
	return suite.createPAT(t, user, nil, nil, expiresAt, false, false, ide)
}

func (suite *RwApiTestSuite) createPATWithScope(
	t *testing.T,
	user *entities.User,
	repoIDs []uint64,
	role *iam.Role,
	caIDE string,
) (id string, token string) {
	return suite.createPAT(t, user, repoIDs, role, nil, false, false, caIDE)
}

func (suite *RwApiTestSuite) createPAT(
	t *testing.T,
	user *entities.User,
	repoIDs []uint64,
	role *iam.Role,
	expiresAt *time.Time,
	isEternal bool,
	jwt bool,
	caIDE string,
) (id string, token string) {
	client := pb.NewMeServiceClient(suite.grpcClient)
	ctx := testutils.AuthorizeGRPC(user.Identity)

	var scope *pb.CreatePATRequest_Scope
	if role != nil || len(repoIDs) != 0 {
		scope = &pb.CreatePATRequest_Scope{}
	}
	if role != nil {
		scope.Role = utils.PtrFromValue(string(*role))
	}
	if len(repoIDs) != 0 {
		scope.RepoIds = functools.Map(repoIDs, grpc_marshalling.IDInverse)
	}
	if isEternal {
		expiresAt = nil
	}
	rand, err := pkgenerator.GetNextID()
	require.NoError(t, err)
	var caScope *pb.CreatePATRequest_CodeAssistScope
	if caIDE != "" {
		caScope = &pb.CreatePATRequest_CodeAssistScope{Ide: caIDE}
	}

	r, err := client.CreatePAT(ctx, &pb.CreatePATRequest{
		Name:        user.Identity.ID + "pat" + strconv.FormatUint(rand, 10),
		Description: utils.PtrFromValue("testpat"),
		ExpiresAt:   commongrpc.TimeToProtocTsNullable(expiresAt),
		Scope:       scope,
		IsEternal:   &isEternal,
		IsJwt:       &jwt,
		CodeAssist:  caScope,
	})
	require.NoError(t, err)

	var resp pb.CreatePATResponse
	require.NoError(t, r.GetResponse().UnmarshalTo(&resp))

	if jwt {
		require.Regexp(t, "eyJ[A-Za-z0-9-_]+\\.(?:eyJ[A-Za-z0-9-_]+)?\\.[A-Za-z0-9-_]{2,}(?:(?:\\.[A-Za-z0-9-_]{2,}){2})?", resp.Token)
	} else {
		require.Regexp(t, "^pv1_[0-9a-zA-Z]{64}_[0-9]+$", resp.Token)
	}
	if isEternal {
		require.Nil(t, resp.PatInfo.ExpiresAt)
	} else if expiresAt != nil {
		// approximately equal
		require.Greater(t, 1*time.Millisecond, expiresAt.Sub(*commongrpc.ProtocTsToTimeNullable(resp.PatInfo.ExpiresAt)))
	} else { // default expiration time used
		expectedTime := time.Now().Add(time.Duration(24*suite.cfg.PAT.DefaultExpirationDays) * time.Hour)
		// approximately equal
		require.Greater(t, 1*time.Minute, expectedTime.Sub(*commongrpc.ProtocTsToTimeNullable(resp.PatInfo.ExpiresAt)))
	}
	require.NotNil(t, resp.GetPatInfo().GetCreatedAt())
	require.LessOrEqual(t, commongrpc.ProtocTsToTime(resp.GetPatInfo().GetCreatedAt()), time.Now())

	return resp.PatInfo.Id, resp.Token
}
