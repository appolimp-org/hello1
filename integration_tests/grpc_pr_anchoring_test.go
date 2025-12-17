package integrationtests

import (
	"common/cgit"
	"common/testutils/yarequire"
	"common/utils"
	"common/utils/rolesgenerator/iam"
	"fmt"
	"gitcore/internal/entities"
	grpc_marshalling "gitcore/internal/grpcserver/marshalling"
	"gitcore/internal/testutils"
	"google.golang.org/grpc/codes"
	"google.golang.org/protobuf/testing/protocmp"
	"os"
	"path"
	pb "private_api/generated/yandex/cloud/priv/gitcore/v1"
	"slices"
	"testing"

	"github.com/stretchr/testify/require"
)

var content1 = `import (
	"fmt"
)

func main() {
	s := "Hello, World!"

	fmt.Println(s)
	fmt.Println(s)
	fmt.Println(s)

	// TODO: remove
	return
}




hehe



hoho
`

var content2 = `import (
	"fmt"
	"math"
)

func main() {
	s := "Hello, John!"

	num := 9

	fmt.Println(s)
	fmt.Println(s)
	fmt.Println(s)

	sqrt := math.sqrt(num)
	fmt.Println(sqrt)

	// TODO: remove
	return
}




hehe



hoho
`

var content3 = `import (
	"fmt"
	"math"
	"strconv"
	"strings"
)

func main() {
	s := "Hello, John!"

	num := 9

	return
}
`

func commit(t *testing.T, cg *cgit.CGit, filePath, fileContent string) {
	dir := path.Dir(filePath)
	if dir != "." {
		require.NoError(t, os.MkdirAll(dir, 0755))
	}
	file, err := os.Create(filePath)
	require.NoError(t, err)

	_, err = file.WriteString(fileContent)
	require.NoError(t, err)
	require.NoError(t, file.Close())

	cg.Must(t, "add", filePath)
	cg.Must(t, "commit", "-m", "\"new commit\"")
}

func (suite *RwApiTestSuite) initCGit(user *entities.User, slug string) (cgit.CGit, string) {
	t := suite.T()

	_, repoName := path.Split(slug)

	repoURL := fmt.Sprintf("%s%s.git", suite.gitHost, slug)
	tmpDir := testutils.TempDir(t, "", repoName)
	cg := cgit.NewCGit(tmpDir).WithAuthToken(testutils.FakeIAMAuthToken(user.Identity))
	cg.Must(t, "clone", repoURL)
	cg = cgit.NewCGit(path.Join(tmpDir, repoName)).WithAuthToken(testutils.FakeIAMAuthToken(user.Identity))

	return cg, tmpDir
}

func (suite *RwApiTestSuite) TestPRComments_Anchoring() {
	t := suite.T()
	user := suite.users.Kopatych
	slug := suite.repos.Alpha.FullSlug()
	ctx := testutils.AuthorizeGRPC(user.Identity)

	suite.addRole(t, user, suite.repos.Alpha, iam.Roles.RepositoriesDeveloper)

	cg, tmpDir := suite.initCGit(user, slug)
	cg.Must(t, "checkout", "branch")
	filePath := path.Join(tmpDir, "alpha", "newfile.go")
	commit(t, &cg, filePath, content1)
	cg.Must(t, "push")

	pr := suite.makePullRequest(suite.users.Kopatych, &makePrOptions{})
	firstIterationID := pr.Iteration

	commentClient := pb.NewPRCommentServiceClient(suite.grpcClient)
	_, err := commentClient.Create(ctx, &pb.CreateCommentRequest{
		PrId:    grpc_marshalling.IDInverse(pr.ID),
		Body:    "Comment 1",
		Publish: true,
		Anchor: &pb.CreateCommentRequest_ShortAnchor{
			Path: "newfile.go",
			Position: &pb.DiffPos{
				From: 8,
				To:   9,
				Side: pb.DiffPos_SOURCE,
			},
		},
		NotificationOptions: testutils.SkipNotificationPb,
	})
	require.NoError(t, err)

	commit(t, &cg, filePath, content2)
	cg.Must(t, "push")

	prClient := pb.NewPRServiceClient(suite.grpcClient)
	resp, err := prClient.ListIterations(ctx, &pb.ListIterationsRequest{
		PrId: grpc_marshalling.IDInverse(pr.ID),
	})
	require.NoError(t, err)
	require.Len(t, resp.Iterations, 2)

	_, err = commentClient.Create(ctx, &pb.CreateCommentRequest{
		PrId:    grpc_marshalling.IDInverse(pr.ID),
		Body:    "Comment 2",
		Publish: true,
		Anchor: &pb.CreateCommentRequest_ShortAnchor{
			Path: "newfile.go",
			Position: &pb.DiffPos{
				From: 1,
				To:   2,
				Side: pb.DiffPos_SOURCE,
			},
		},
		NotificationOptions: testutils.SkipNotificationPb,
	})
	require.NoError(t, err)

	feed, err := commentClient.List(ctx, &pb.ListCommentsRequest{
		PrId:       grpc_marshalling.IDInverse(pr.ID),
		OnlyDrafts: false,
	})
	require.NoError(t, err)
	require.Len(t, feed.Comments, 2)

	anchor := feed.Comments[0].Anchor
	require.EqualValues(t, 1, anchor.Position.From)
	require.EqualValues(t, 2, anchor.Position.To)
	require.EqualValues(t, 1, anchor.Hunk.ToStart)
	require.EqualValues(t, 5, anchor.Hunk.ToCount)
	require.False(t, *feed.Comments[1].IsOutdated)

	feed2, err := commentClient.List(ctx, &pb.ListCommentsRequest{
		PrId:       grpc_marshalling.IDInverse(pr.ID),
		OnlyDrafts: false,
		Iteration:  utils.PtrFromValue(grpc_marshalling.IDInverse(firstIterationID)),
	})
	require.NoError(t, err)
	require.Len(t, feed2.Comments, 1)
	require.Equal(t, feed.Comments[1].Id, feed2.Comments[0].Id)

	anchor2 := feed2.Comments[0].Anchor
	require.EqualValues(t, 8, anchor2.Position.From)
	require.EqualValues(t, 9, anchor2.Position.To)
	require.EqualValues(t, 5, anchor2.Hunk.ToStart)
	require.EqualValues(t, 8, anchor2.Hunk.ToCount)
	require.False(t, *feed2.Comments[0].IsOutdated)
}

func (suite *RwApiTestSuite) TestPRComments_InvalidAnchor() {
	t := suite.T()

	user := suite.users.Kopatych
	repo := suite.repos.Alpha
	suite.addRole(t, user, repo, iam.Roles.RepositoriesDeveloper)

	pr := suite.makePullRequest(user, &makePrOptions{})

	commentClient := pb.NewPRCommentServiceClient(suite.grpcClient)
	_, err := commentClient.Create(testutils.AuthorizeGRPC(user.Identity), &pb.CreateCommentRequest{
		PrId:    grpc_marshalling.IDInverse(pr.ID),
		Body:    "Comment 1",
		Publish: true,
		Anchor: &pb.CreateCommentRequest_ShortAnchor{
			Path: "this file doesn't exist",
			Position: &pb.DiffPos{
				From: 1,
				To:   2,
				Side: pb.DiffPos_SOURCE,
			},
		},
		NotificationOptions: testutils.SkipNotificationPb,
	})
	yarequire.ProtoStatusEqual(t, codes.NotFound, err)

	suite.mustBash(suite.repos.Alpha, `
		git checkout branch
		echo "content" > file.txt
		git add . && git commit -m "file.txt"
	`)
}

func (suite *RwApiTestSuite) TestPRComments_AnchorRegion() {
	t := suite.T()

	user := suite.users.Kopatych
	repo := suite.repos.Alpha
	suite.addRole(t, user, repo, iam.Roles.RepositoriesDeveloper)

	slug := suite.repos.Alpha.FullSlug()

	cg, tmpDir := suite.initCGit(user, slug)
	filePath := path.Join(tmpDir, "alpha", "newfile.go")
	cg.Must(t, "checkout", "master")
	commit(t, &cg, filePath, content1)
	cg.Must(t, "push")
	cg.Must(t, "checkout", "-b", "new_branch")
	commit(t, &cg, filePath, content2)
	cg.Must(t, "push", "--set-upstream", "origin", "new_branch")

	pr := suite.makePullRequest(user, &makePrOptions{
		Source: "new_branch",
	})
	client := pb.NewPRCommentServiceClient(suite.grpcClient)

	testcases := []struct {
		name         string
		anchor       *pb.CreateCommentRequest_ShortAnchor
		expectedHunk *pb.Hunk
	}{
		{
			name: "no hunk",
			anchor: &pb.CreateCommentRequest_ShortAnchor{
				Path: "newfile.go",
			},
			expectedHunk: nil,
		},
		{
			name: "on target, changes inside",
			anchor: &pb.CreateCommentRequest_ShortAnchor{
				Path: "newfile.go",
				Position: &pb.DiffPos{
					From: 6,
					To:   6,
					Side: pb.DiffPos_TARGET,
				},
			},
			expectedHunk: &pb.Hunk{
				FromStart: 3,
				FromCount: 7,
				ToStart:   4,
				ToCount:   9,
				Patch:     " )\n \n func main() {\n-\ts := \"Hello, World!\"\n+\ts := \"Hello, John!\"\n+\n+\tnum := 9\n \n \tfmt.Println(s)\n \tfmt.Println(s)\n",
			},
		},
		{
			name: "on source, additions inside",
			anchor: &pb.CreateCommentRequest_ShortAnchor{
				Path: "newfile.go",
				Position: &pb.DiffPos{
					From: 3,
					To:   3,
					Side: pb.DiffPos_SOURCE,
				},
			},
			expectedHunk: &pb.Hunk{
				FromStart: 1,
				FromCount: 5,
				ToStart:   1,
				ToCount:   6,
				Patch:     " import (\n \t\"fmt\"\n+\t\"math\"\n )\n \n func main() {\n",
			},
		},
		{
			name: "on source, start on changes, end on additions",
			anchor: &pb.CreateCommentRequest_ShortAnchor{
				Path: "newfile.go",
				Position: &pb.DiffPos{
					From: 12,
					To:   13,
					Side: pb.DiffPos_SOURCE,
				},
			},
			expectedHunk: &pb.Hunk{
				FromStart: 7,
				FromCount: 5,
				ToStart:   9,
				ToCount:   8,
				Patch:     "+\tnum := 9\n \n \tfmt.Println(s)\n \tfmt.Println(s)\n \tfmt.Println(s)\n \n+\tsqrt := math.sqrt(num)\n+\tfmt.Println(sqrt)\n",
			},
		},
		{
			name: "on source, end on changes, additions inside",
			anchor: &pb.CreateCommentRequest_ShortAnchor{
				Path: "newfile.go",
				Position: &pb.DiffPos{
					From: 5,
					To:   5,
					Side: pb.DiffPos_SOURCE,
				},
			},
			expectedHunk: &pb.Hunk{
				FromStart: 2,
				FromCount: 5,
				ToStart:   2,
				ToCount:   7,
				Patch:     " \t\"fmt\"\n+\t\"math\"\n )\n \n func main() {\n-\ts := \"Hello, World!\"\n+\ts := \"Hello, John!\"\n+\n",
			},
		},
		{
			name: "unchanged, target",
			anchor: &pb.CreateCommentRequest_ShortAnchor{
				Path: "go/example.go",
				Position: &pb.DiffPos{
					From: 5,
					To:   5,
					Side: pb.DiffPos_TARGET,
				},
			},
			expectedHunk: &pb.Hunk{
				FromStart: 2,
				FromCount: 7,
				ToStart:   2,
				ToCount:   7,
				Patch:     " \n import (\n \t\"harvesterd/intf\"\n \t\"sync\"\n \t\"sync/atomic\"\n )\n \n",
			},
		},
		{
			name: "unchanged, source",
			anchor: &pb.CreateCommentRequest_ShortAnchor{
				Path: "go/example.go",
				Position: &pb.DiffPos{
					From: 5,
					To:   5,
					Side: pb.DiffPos_SOURCE,
				},
			},
			expectedHunk: &pb.Hunk{
				FromStart: 2,
				FromCount: 7,
				ToStart:   2,
				ToCount:   7,
				Patch:     " \n import (\n \t\"harvesterd/intf\"\n \t\"sync\"\n \t\"sync/atomic\"\n )\n \n",
			},
		},
		{
			name: "far out of scope, target",
			anchor: &pb.CreateCommentRequest_ShortAnchor{
				Path: "newfile.go",
				Position: &pb.DiffPos{
					From: 19,
					To:   19,
					Side: pb.DiffPos_TARGET,
				},
			},
			expectedHunk: &pb.Hunk{
				FromStart: 16,
				FromCount: 7,
				ToStart:   22,
				ToCount:   7,
				Patch:     " \n \n \n hehe\n \n \n \n",
			},
		},
		{
			name: "far out of scope, source",
			anchor: &pb.CreateCommentRequest_ShortAnchor{
				Path: "newfile.go",
				Position: &pb.DiffPos{
					From: 25,
					To:   25,
					Side: pb.DiffPos_SOURCE,
				},
			},
			expectedHunk: &pb.Hunk{
				FromStart: 16,
				FromCount: 7,
				ToStart:   22,
				ToCount:   7,
				Patch:     " \n \n \n hehe\n \n \n \n",
			},
		},
	}

	for i, tc := range testcases {
		t.Run(tc.name, func(t *testing.T) {
			op, err := client.Create(testutils.AuthorizeGRPC(user.Identity), &pb.CreateCommentRequest{
				PrId:                grpc_marshalling.IDInverse(pr.ID),
				Body:                fmt.Sprintf("Comment %d", i+1),
				Publish:             true,
				Anchor:              tc.anchor,
				NotificationOptions: testutils.SkipNotificationPb,
			})
			require.NoError(t, err)

			resp := new(pb.PRComment)
			require.NoError(t, op.GetResponse().UnmarshalTo(resp))
			require.Equal(t, tc.expectedHunk, resp.Anchor.Hunk)
		})
	}
}

func (suite *RwApiTestSuite) TestPRComments_AnchoringTarget() {
	user := suite.users.Kopatych
	slug := suite.repos.Alpha.FullSlug()
	ctx := testutils.AuthorizeGRPC(user.Identity)

	commentClient := pb.NewPRCommentServiceClient(suite.grpcClient)
	prClient := pb.NewPRServiceClient(suite.grpcClient)

	testcases := []struct {
		name           string
		f              func(t *testing.T, cg *cgit.CGit, filePath string)
		from, to       int
		expFrom, expTo int
		expIsOutdated  bool
	}{
		{
			name: "unchanged",
			f: func(t *testing.T, cg *cgit.CGit, filePath string) {
				commit(t, cg, filePath, content3)
				cg.Must(t, "push")
			},
			from:          8,
			to:            8,
			expFrom:       8,
			expTo:         8,
			expIsOutdated: false,
		},
		{
			name: "target_changed",
			f: func(t *testing.T, cg *cgit.CGit, filePath string) {
				cg.Must(t, "checkout", "master")
				commit(t, cg, filePath, content2)
				cg.Must(t, "push")
				cg.Must(t, "checkout", "new_branch")
				cg.Must(t, "rebase", "master")
				cg.Must(t, "push", "-f")
			},
			from:          8,
			to:            8,
			expFrom:       11,
			expTo:         11,
			expIsOutdated: false,
		},
	}

	for _, tc := range testcases {
		suite.Run(tc.name, func() {
			t := suite.T()
			suite.addRole(t, user, suite.repos.Alpha, iam.Roles.RepositoriesDeveloper)

			cg, tmpDir := suite.initCGit(user, slug)
			filePath := path.Join(tmpDir, "alpha", "newfile.go")

			commit(t, &cg, filePath, content1)
			cg.Must(t, "push")
			cg.Must(t, "checkout", "-b", "new_branch")
			commit(t, &cg, path.Join(tmpDir, "alpha", "smth.txt"), "smth")
			commit(t, &cg, filePath, content2)
			cg.Must(t, "push", "--set-upstream", "origin", "new_branch")

			pr := suite.makePullRequest(user, &makePrOptions{
				Source: "new_branch",
			})
			op, err := commentClient.Create(ctx, &pb.CreateCommentRequest{
				PrId:    grpc_marshalling.IDInverse(pr.ID),
				Body:    "Comment!",
				Publish: true,
				Anchor: &pb.CreateCommentRequest_ShortAnchor{
					Path: "newfile.go",
					Position: &pb.DiffPos{
						From: int32(tc.from),
						To:   int32(tc.to),
						Side: pb.DiffPos_TARGET,
					},
				},
				NotificationOptions: testutils.SkipNotificationPb,
			})
			require.NoError(t, err)

			resp := new(pb.PRComment)
			require.NoError(t, op.GetResponse().UnmarshalTo(resp))

			tc.f(t, &cg, filePath)

			diff, err := prClient.GetFileChanges(ctx, &pb.GetFileChangesRequest{
				Paths: []*pb.DiffPath{
					{
						Target: "newfile.go",
						Source: "newfile.go",
					},
				},
				PrId: grpc_marshalling.IDInverse(pr.ID),
			})
			require.NoError(t, err)
			require.Len(t, diff.Changes, 1)
			require.Len(t, diff.Changes[0].Comments, 1)

			pos := diff.Changes[0].Comments[0].Pos
			require.NotNil(t, pos)
			require.EqualValues(t, tc.expFrom, pos.From)
			require.EqualValues(t, tc.expTo, pos.To)
			require.EqualValues(t, tc.expIsOutdated, pos.Outdated)
		})
	}
}

func (suite *RwApiTestSuite) TestPRComments_IterationsReAnchoring() {
	var base = ".\nb\n."
	var ver1 = ".\na\n."
	var ver2 = ".\naa\n."
	var ver21 = ".\naa\n..."
	var ver3 = ".\naaa\n."

	user := suite.users.Kopatych
	slug := suite.repos.Alpha.FullSlug()
	ctx := testutils.AuthorizeGRPC(user.Identity)

	TARGET := pb.DiffPos_TARGET
	SOURCE := pb.DiffPos_SOURCE

	commentClient := pb.NewPRCommentServiceClient(suite.grpcClient)
	prClient := pb.NewPRServiceClient(suite.grpcClient)

	testcases := []struct {
		name         string
		pushVersions []string
		reqsIters    []int
		reqsSides    []pb.DiffPos_Side
		expSides     []pb.DiffPos_Side
		expOutdated  []bool
	}{
		{
			name:         "all on target",
			pushVersions: []string{ver2, ver3},
			reqsIters:    []int{0, 1, 2},
			reqsSides:    []pb.DiffPos_Side{TARGET, TARGET, TARGET},
			expSides:     []pb.DiffPos_Side{TARGET, TARGET, TARGET},
			expOutdated:  []bool{true, true, true},
		},
		{
			name:         "line changes on every iter",
			pushVersions: []string{ver2, ver3},
			reqsIters:    []int{0, 1, 2},
			reqsSides:    []pb.DiffPos_Side{SOURCE, SOURCE, SOURCE},
			expSides:     []pb.DiffPos_Side{TARGET, SOURCE, SOURCE},
			expOutdated:  []bool{false, true, false},
		},
		{
			name:         "line changes on 2 of 3 iters",
			pushVersions: []string{ver2, ver21},
			reqsIters:    []int{0, 1, 2},
			reqsSides:    []pb.DiffPos_Side{SOURCE, SOURCE, SOURCE},
			expSides:     []pb.DiffPos_Side{TARGET, SOURCE, SOURCE},
			expOutdated:  []bool{false, false, false},
		},
	}

	for _, tc := range testcases {
		suite.Run(tc.name, func() {
			t := suite.T()
			suite.addRole(t, user, suite.repos.Alpha, iam.Roles.RepositoriesDeveloper)

			cg, tmpDir := suite.initCGit(user, slug)
			filePath := path.Join(tmpDir, "alpha", "newfile.txt")

			commit(t, &cg, filePath, base)
			cg.Must(t, "push")
			cg.Must(t, "checkout", "-b", "new_branch")
			commit(t, &cg, filePath, ver1)
			cg.Must(t, "push", "--set-upstream", "origin", "new_branch")

			pr := suite.makePullRequest(user, &makePrOptions{
				Source: "new_branch",
			})

			// push and publish
			for _, ver := range tc.pushVersions {
				commit(t, &cg, filePath, ver)
				cg.Must(t, "push")
			}

			iterations, err := prClient.ListIterations(ctx, &pb.ListIterationsRequest{
				PrId: grpc_marshalling.IDInverse(pr.ID),
			})
			require.NoError(t, err)

			for i, iter := range tc.reqsIters {
				_, err := commentClient.Create(ctx, &pb.CreateCommentRequest{
					PrId:    grpc_marshalling.IDInverse(pr.ID),
					Body:    fmt.Sprintf("Comment %d", i),
					Publish: true,
					Anchor: &pb.CreateCommentRequest_ShortAnchor{
						Path: "newfile.txt",
						Position: &pb.DiffPos{
							From: int32(2),
							To:   int32(2),
							Side: tc.reqsSides[i],
						},
					},
					Iteration:           &iterations.Iterations[iter].Id,
					NotificationOptions: testutils.SkipNotificationPb,
				})
				require.NoError(t, err)
			}

			diff, err := prClient.GetFileChanges(ctx, &pb.GetFileChangesRequest{
				IterId:     &iterations.Iterations[2].Id,
				FromIterId: &iterations.Iterations[0].Id,
				PrId:       grpc_marshalling.IDInverse(pr.ID),
				Paths: []*pb.DiffPath{
					{
						Target: "newfile.txt",
						Source: "newfile.txt",
					},
				},
			})
			require.NoError(t, err)
			require.Len(t, diff.Changes, 1)
			change := diff.Changes[0]

			// yarequire.ProtoDumpFixture(t, change)
			yarequire.ProtoCompareWithFixture(t, change,
				protocmp.IgnoreFields(&pb.GetFileChangesResponse_Comment{}, "comment_id"),
			)

			slices.Reverse(change.Comments)
			for i, comment := range change.Comments {
				require.Equal(t, tc.expSides[i], comment.Pos.Side, i)
				require.Equal(t, tc.expOutdated[i], comment.Pos.Outdated, i)
			}
		})
	}
}
