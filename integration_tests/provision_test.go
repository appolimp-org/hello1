package integrationtests

import (
	grpc2 "common/grpc"
	"context"
	"fmt"
	grpc_marshalling "gitcore/internal/grpcserver/marshalling"
	"gitcore/internal/interfaces"
	"gitcore/internal/testutils"
	pb "private_api/generated/yandex/cloud/priv/gitcore/v1"
	"testing"

	"github.com/go-git/go-git/v5/plumbing"

	"github.com/stretchr/testify/require"

	yautils "common/utils"
	"common/utils/rolesgenerator/iam"
	"gitcore/internal/entities"
	"gitcore/internal/utils"
)

// Mergo won't merge unexported (private) fields but will do recursively any exported one !
type makePrOptions struct {
	Repo        *entities.Repository
	Title       string
	Description string
	Source      string
	Target      string
	Reviewers   []uint64
	Publish     *bool
	SourceRepo  *entities.Repository
}

type makeFauxPrOptions struct {
	Repo       *entities.Repository
	ID         uint64
	PublicID   uint64
	Title      string
	Source     string
	Target     string
	Status     entities.PRStatus
	SourceRepo *entities.Repository
}

// if you just need to create PR, use it
func (suite *RwApiTestSuite) makeFauxPullRequest(repoID uint64, author *entities.User, opts *makeFauxPrOptions) *entities.PullRequest {
	t := suite.T()
	repo := suite.Params.PullRequestRepoFactory.Build(repoID)

	repository, err := suite.Params.RepoRepo.GetRepositoryByID(context.Background(), repoID)
	require.NoError(t, err)

	opts, err = utils.MergeOptions(opts, &makeFauxPrOptions{
		Title:  "some description",
		Source: "branch",
		Target: "master",
		Status: entities.PRStatuses.Draft,
		Repo:   repository,
	})
	require.NoError(t, err)

	if opts.SourceRepo == nil {
		opts.SourceRepo = opts.Repo
	}

	pr := &entities.PullRequest{
		Title:        opts.Title,
		PublicID:     opts.PublicID,
		ID:           opts.ID,
		RepoID:       opts.Repo.ID,
		SourceRepoID: opts.SourceRepo.ID,
		AuthorID:     author.ID,
		SourceBranch: opts.Source,
		TargetBranch: opts.Target,
		Status:       opts.Status,
		Settings:     *entities.NewPRSettings(),
	}
	prID, err := repo.Create(context.Background(), pr, plumbing.ZeroHash)

	require.NoError(t, err)
	pr, err = suite.PullRequestService.Get(context.Background(), prID)

	require.NoError(t, err)
	return pr
}

func (suite *IntegrationTestSuite) makePullRequest(author *entities.User, opts *makePrOptions) *entities.PullRequest {
	t := suite.T()

	opts, err := utils.MergeOptions(opts, &makePrOptions{
		Repo:    suite.repos.Alpha,
		Title:   "TASK-1 add README.md",
		Source:  "branch",
		Target:  "master",
		Publish: yautils.PtrFromValue(true),
	})
	require.NoError(t, err)

	suite.addRole(t, author, opts.Repo, iam.Roles.RepositoriesContributor)

	prParams := interfaces.CreatePRParams{
		Title:        opts.Title,
		Description:  opts.Description,
		SourceBranch: opts.Source,
		TargetBranch: opts.Target,
		ReviewerIDs:  opts.Reviewers,
	}
	if opts.SourceRepo != nil {
		prParams.ForkRepoID = &opts.SourceRepo.ID
	}

	if opts.Publish != nil && *opts.Publish {
		prParams.Publish = true
	}

	prID, err := suite.PullRequestService.Create(
		context.Background(),
		prParams,
		opts.Repo,
		author,
		suite.getFakeAuthenticator(author.Identity),
		entities.NotifyOptions{},
	)

	require.NoError(t, err)
	pr, err := suite.PullRequestService.Get(context.Background(), prID)

	require.NoError(t, err)
	return pr
}

func (suite *RwApiTestSuite) makePullRequestAndReviewers(t *testing.T, reviewersCount int) (*entities.PullRequest, []*entities.User) {
	pr := suite.makePullRequest(suite.users.Kopatych, nil)

	var allCreatedUsers []*entities.User
	for i := 1; i <= reviewersCount; i++ {
		user := suite.RandomUserFixture()
		allCreatedUsers = append(allCreatedUsers, user)

		suite.addReviewer(t, pr, user)
	}
	return pr, allCreatedUsers
}

func (suite *RwApiTestSuite) addReviewer(t *testing.T, pr *entities.PullRequest, user *entities.User) {
	suite.addRole(t, suite.users.Kopatych, suite.repos.Alpha, iam.Roles.RepositoriesAdmin) // request
	suite.addRole(t, user, suite.repos.Alpha, iam.Roles.RepositoriesDeveloper)             // reviewer

	_, err := suite.PullRequestService.AssignReviewer(context.Background(), pr, suite.users.Kopatych, user, true, entities.NotifyOptions{})
	require.NoError(t, err)
}

type makePrCommentOptions struct {
	Body           string
	Draft          bool
	NeedResolution bool
	ParentID       *uint64
	Anchor         *entities.PullRequestCommentAnchor
	Type           entities.PullRequestCommentType
}

func (suite *RwApiTestSuite) makePrCommentGRPC(t *testing.T, author *entities.User, pr *entities.PullRequest, opts *makePrCommentOptions) *entities.PullRequestComment {
	client := pb.NewPRCommentServiceClient(suite.grpcClient)

	opts, err := utils.MergeOptions(opts, &makePrCommentOptions{
		Body: "Comment",
	})
	require.NoError(t, err)

	request := &pb.CreateCommentRequest{
		PrId:                grpc2.MarshalID(pr.ID),
		Body:                opts.Body,
		Publish:             !opts.Draft,
		NeedResolution:      opts.NeedResolution,
		NotificationOptions: testutils.SkipNotificationPb,
		Type:                grpc_marshalling.ConvertPRCommentTypeInverse(opts.Type),
	}
	if opts.ParentID != nil {
		request.ParentId = yautils.PtrFromValue(grpc2.MarshalID(*opts.ParentID))
	}
	if opts.Anchor != nil {
		request.Anchor = &pb.CreateCommentRequest_ShortAnchor{
			Path: opts.Anchor.Path,
		}
		if opts.Anchor.To != nil || opts.Anchor.From != nil || opts.Anchor.Side != nil {
			side := utils.OrElse(opts.Anchor.Side, entities.PullRequestCommentSides.Source)
			request.Anchor.Position = &pb.DiffPos{
				From: int32(utils.OrElse(opts.Anchor.From, 0)),
				To:   int32(utils.OrElse(opts.Anchor.To, 0)),
				Side: grpc_marshalling.DiffPosSideInverse(side),
			}
		}
	}

	ctx := testutils.AuthorizeGRPC(author.Identity)
	op, err := client.Create(ctx, request)
	require.NoError(t, err)
	require.NotNil(t, op)

	comment, err := op.GetResponse().UnmarshalNew()
	require.NoError(t, err)
	cmt, ok := comment.(*pb.PRComment)
	require.True(t, ok)

	commentID, err := grpc2.ParseID(cmt.Id)
	require.NoError(t, err)
	commentEntity, err := suite.PullRequestCommentService.Get(ctx, pr.RepoID, pr.ID, commentID)
	require.NoError(t, err)
	return commentEntity
}

func (suite *RwApiTestSuite) makeSmallPrFeed(user *entities.User, pr *entities.PullRequest) []uint64 {
	parent := suite.makePrCommentGRPC(suite.T(), user, pr, nil)
	child1 := suite.makePrCommentGRPC(suite.T(), user, pr, &makePrCommentOptions{
		Body:     fmt.Sprintf("%v child 1", user.DisplayName),
		Draft:    true,
		ParentID: &parent.ID,
	})
	child2 := suite.makePrCommentGRPC(suite.T(), user, pr, &makePrCommentOptions{
		Body:           fmt.Sprintf("%v child 2", user.DisplayName),
		NeedResolution: true,
		ParentID:       &parent.ID,
	})
	return []uint64{parent.ID, child1.ID, child2.ID}
}

func (suite *RwApiTestSuite) makePrComments(
	t *testing.T,
	author *entities.User,
	pr *entities.PullRequest,
	specs []*makePrCommentOptions,
) []*entities.PullRequestComment {
	comments := make([]*entities.PullRequestComment, 0, len(specs))

	for i, spec := range specs {
		u, err := yautils.GenerateUUID()
		require.NoError(t, err)

		spec, err = utils.MergeOptions(spec, &makePrCommentOptions{
			Body: fmt.Sprintf("Comment #%d; Rand: %s", i, u),
		})
		require.NoError(t, err)

		comments = append(comments, suite.makePrCommentGRPC(t, author, pr, spec))
	}

	return comments
}
