package integrationtests

import (
	"bytes"
	"common/functools"
	grpc2 "common/grpc"
	"common/testutils/yarequire"
	"common/utils"
	"common/utils/rolesgenerator/iam"
	"context"
	"encoding/json"
	"fmt"
	"gitcore/internal/access/common"
	"gitcore/internal/consts"
	"gitcore/internal/entities"
	except "gitcore/internal/exceptions"
	"gitcore/internal/generated/mocks"
	"gitcore/internal/interfaces"
	"gitcore/internal/pullrequests/neuroreview"
	"gitcore/internal/services/scheduler"
	"gitcore/internal/testutils"
	pb "private_api/generated/yandex/cloud/priv/gitcore/v1"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
	"go.uber.org/fx"
	"go.uber.org/mock/gomock"
	"google.golang.org/protobuf/types/known/fieldmaskpb"
)

type comradeClientProxy struct {
	suite *MockComradeTestSuite
}

func (p *comradeClientProxy) RequestModelSync(
	ctx context.Context, authenticator interfaces.Authenticator, systemPrompt string, userPrompt string,
) (response string, err error) {
	return p.suite.mockComradeClient.RequestModelSync(ctx, authenticator, systemPrompt, userPrompt)
}

func (p *comradeClientProxy) RequestModel(
	ctx context.Context, authenticator interfaces.Authenticator, systemPrompt string, userPrompt string,
) (responseID string, err error) {
	return p.suite.mockComradeClient.RequestModel(ctx, authenticator, systemPrompt, userPrompt)
}

func (p *comradeClientProxy) GetResponse(
	ctx context.Context, authenticator interfaces.Authenticator, responseID string,
) (response string, completed bool, err error) {
	return p.suite.mockComradeClient.GetResponse(ctx, authenticator, responseID)
}

func (p *comradeClientProxy) CreateConversation(ctx context.Context, authenticator interfaces.Authenticator, messages []entities.RestorableConversationMessage, conversationTitle string) (conversationID string, err error) {
	return p.suite.mockComradeClient.CreateConversation(ctx, authenticator, messages, "")
}

type MockComradeTestSuite struct {
	IntegrationTestSuite
	mockComradeClient                 *mocks.MockComradeClient
	mockCtrl                          *gomock.Controller
	mockReporter                      *testutils.MockReporter
	restorableConversationsRepository interfaces.RestorableConversationsRepository
}

func (suite *MockComradeTestSuite) SetupSuite() {
	suite.SetupFlags()
	suite.SetupServer(
		fx.Decorate(func() interfaces.ComradeClient {
			return &comradeClientProxy{suite: suite}
		}),
	)
	suite.SetupDefaultQuotaLimits()
	suite.SetupUsers(Create)
	suite.SetupOrganizations(Create)
	suite.SetupSmallRepos(Create)
}

func (suite *MockComradeTestSuite) BeforeTest(_, _ string) {
	suite.mockCtrl, suite.mockReporter = testutils.NewMockController(suite.T())
	suite.mockComradeClient = mocks.NewMockComradeClient(suite.mockCtrl)
}

func (suite *MockComradeTestSuite) AfterTest(_, _ string) {
	if suite.mockReporter != nil {
		suite.mockReporter.Finish(suite.mockCtrl)
	}
}

func (suite *MockComradeTestSuite) TearDownSuite() {
	suite.IntegrationTestSuite.TearDownSuite()
	suite.CancelAllWorkflows()
}

func TestMockComradeTestSuite(t *testing.T) {
	suite.Run(t, new(MockComradeTestSuite))
}

func (suite *MockComradeTestSuite) TestNeuroReview() {
	t := suite.T()

	repoID, _, _ := suite.makeRandomRepo(t, suite.users.Kopatych)
	repo, err := suite.RepoRepo.GetRepositoryByID(context.Background(), repoID)
	require.NoError(t, err)

	suite.ensureDefaultMaster(repo)
	suite.mustBash(repo, `
		git checkout master
		
		# Создаем базовые файлы в master
		cat > main.go << 'EOF'
package main

import "fmt"

func main() {
	fmt.Println("Hello from master!")
}
EOF
		
		cat > utils.go << 'EOF'
package utils

// Helper function
func Helper() string {
	return "helper"
}

// Deprecated function - will be removed
func OldHelper() string {
	return "old"
}
EOF
		
		mkdir -p internal/config
		cat > internal/config/config.go << 'EOF'
package config

const (
	Version = "1.0.0"
	Debug   = true
)
EOF
		
		git add .
		git commit -m "Add base files to master"
		
		# Переключаемся на новую ветку и делаем изменения
		git checkout -b neuroreview-complex-test-branch
		
		# 1. Модифицируем существующий файл (main.go)
		cat > main.go << 'EOF'
package main

import (
	"fmt"
	"os"
)

func main() {
	const my_constant = 42 // bad naming style
	unused_var := "hello"  // unused variable
	
	if len(os.Args) > 1 {
		fmt.Printf("Hello %s! Constant: %d\n", os.Args[1], my_constant)
	} else {
		fmt.Println("Hello, World!")
	}
}
EOF
		
		# 2. Создаем новый файл
		cat > validator.go << 'EOF'
package validator

import "strings"

// ValidateEmail validates email format
func ValidateEmail(email string) bool {
	return strings.Contains(email, "@") // simplistic validation
}

// TODO: Add proper regex validation
func validateDomain(domain string) bool {
	return len(domain) > 0 // too simple
}
EOF
		
		# 3. Модифицируем utils.go (удаляем старую функцию, добавляем новую)
		cat > utils.go << 'EOF'
package utils

import "strings"

// Helper function - improved version
func Helper() string {
	return "improved_helper"
}

// NewFunction adds new functionality
func NewFunction(input string) string {
	return strings.ToUpper(input) // potential issue with Unicode
}
EOF
		
		# 4. Удаляем файл config.go
		rm internal/config/config.go
		
		# 5. Создаем новый config в другом месте
		cat > config.go << 'EOF'
package main

// Configuration constants
const (
	VERSION = "2.0.0"  // should be lowercase
	debug   = false    // inconsistent naming
)
EOF
		
		git add .
		git commit -m "Complex changes: modify main.go and utils.go, add validator.go and config.go, remove internal/config/config.go"
	`)

	suite.addRole(t, suite.users.Kopatych, repo, iam.Roles.RepositoriesMaintainer)
	pr := suite.makePullRequest(suite.users.Kopatych, &makePrOptions{
		Repo:   repo,
		Source: "neuroreview-complex-test-branch",
		Target: "master",
		Title:  "Complex changes for neuroreview testing",
	})

	commentClient := pb.NewPRCommentServiceClient(suite.grpcClient)
	authCtx := testutils.AuthorizeGRPC(suite.users.Kopatych.Identity)

	_, err = commentClient.Create(authCtx, &pb.CreateCommentRequest{
		PrId:    grpc2.MarshalID(pr.ID),
		Body:    "Константы в Go должны быть в стиле CamelCase: myConstant или MyConstant",
		Type:    pb.PRCommentType_PR_COMMENT_TYPE_CODE_ASSISTANT,
		Publish: true,
		Anchor: &pb.CreateCommentRequest_ShortAnchor{
			Path: "main.go",
			Position: &pb.DiffPos{
				From: 9,
				To:   9,
				Side: pb.DiffPos_SOURCE,
			},
		},
	})
	require.NoError(t, err)

	_, err = commentClient.Create(authCtx, &pb.CreateCommentRequest{
		PrId:    grpc2.MarshalID(pr.ID),
		Body:    "Слишком простая валидация email. Используйте регулярное выражение для корректной проверки",
		Type:    pb.PRCommentType_PR_COMMENT_TYPE_CODE_ASSISTANT,
		Publish: true,
		Anchor: &pb.CreateCommentRequest_ShortAnchor{
			Path: "validator.go",
			Position: &pb.DiffPos{
				From: 7,
				To:   7,
				Side: pb.DiffPos_SOURCE,
			},
		},
	})
	require.NoError(t, err)

	aiResponse := map[string]interface{}{
		"problems": []map[string]interface{}{
			{
				"file":      "main.go",
				"line":      10,
				"operation": "+",
				"comment":   "Неиспользуемая переменная unused_var. Удалите её или используйте.",
			},
			{
				"file":      "validator.go",
				"line":      12,
				"operation": "+",
				"comment":   "Функция validateDomain не экспортирована и слишком упрощена. Добавьте proper валидацию доменов.",
			},
			{
				"file":      "utils.go",
				"line":      10,
				"operation": "-",
				"comment":   "Удаленная функция OldHelper использовалась в других местах. Убедитесь, что все вызовы обновлены.",
			},
			{
				"file":      "utils.go",
				"line":      12,
				"operation": "+",
				"comment":   "strings.ToUpper может некорректно работать с Unicode символами. Рассмотрите использование golang.org/x/text/cases.",
			},
			{
				"file":      "config.go",
				"line":      5,
				"operation": "+",
				"comment":   "Константа VERSION должна быть в стиле version для приватных или Version для экспортируемых.",
			},
			{
				"file":      "nonexistent.go",
				"line":      42,
				"operation": "+",
				"comment":   "Этот комментарий для несуществующего файла должен создаться без anchor.",
			},
		},
		"summary": `❗ Требуются доработки

**Описание изменений:**
...

**Основные проблемы:**
...
`,
	}

	aiResponseJSON, err := json.Marshal(aiResponse)
	require.NoError(t, err)

	expectedDiff := `diff --git a/config.go b/config.go
new file mode 100644
--- /dev/null
+++ b/config.go
@@ -0,0 +1,7 @@
           1 + package main
           2 + 
           3 + // Configuration constants
           4 + const (
           5 +     VERSION = "2.0.0"  // should be lowercase
           6 +     debug   = false    // inconsistent naming
           7 + )
diff --git a/internal/config/config.go b/internal/config/config.go
deleted file mode 100644
--- a/internal/config/config.go
+++ /dev/null
@@ -1,6 +0,0 @@
     1       - package config
     2       - 
     3       - const (
     4       -     Version = "1.0.0"
     5       -     Debug   = true
     6       - )
diff --git a/main.go b/main.go
@@ -1,7 +1,17 @@
     1     1   package main
     2     2   
     3       - import "fmt"
           3 + import (
           4 +     "fmt"
           5 +     "os"
           6 + )
     4     7   
     5     8   func main() {
     6       -     fmt.Println("Hello from master!")
           9 +     const my_constant = 42 // bad naming style
          10 +     unused_var := "hello"  // unused variable
          11 +     
          12 +     if len(os.Args) > 1 {
          13 +         fmt.Printf("Hello %s! Constant: %d\n", os.Args[1], my_constant)
          14 +     } else {
          15 +         fmt.Println("Hello, World!")
          16 +     }
     7    17   }
diff --git a/utils.go b/utils.go
@@ -1,11 +1,13 @@
     1     1   package utils
     2     2   
     3       - // Helper function
           3 + import "strings"
           4 + 
           5 + // Helper function - improved version
     4     6   func Helper() string {
     5       -     return "helper"
           7 +     return "improved_helper"
     6     8   }
     7     9   
     8       - // Deprecated function - will be removed
     9       - func OldHelper() string {
    10       -     return "old"
          10 + // NewFunction adds new functionality
          11 + func NewFunction(input string) string {
          12 +     return strings.ToUpper(input) // potential issue with Unicode
    11    13   }
diff --git a/validator.go b/validator.go
new file mode 100644
--- /dev/null
+++ b/validator.go
@@ -0,0 +1,13 @@
           1 + package validator
           2 + 
           3 + import "strings"
           4 + 
           5 + // ValidateEmail validates email format
           6 + func ValidateEmail(email string) bool {
           7 +     return strings.Contains(email, "@") // simplistic validation
           8 + }
           9 + 
          10 + // TODO: Add proper regex validation
          11 + func validateDomain(domain string) bool {
          12 +     return len(domain) > 0 // too simple
          13 + }
`

	actualSystemPrompt := ""
	actualUserPrompt := ""

	aiReponseID := "123"

	suite.mockComradeClient.EXPECT().
		RequestModel(
			gomock.Any(),
			gomock.Any(),
			gomock.Any(),
			gomock.Any(),
		).
		Do(func(_ any, _ any, systemPrompt string, userPrompt string) {
			actualSystemPrompt = systemPrompt
			actualUserPrompt = userPrompt
		}).
		Return(aiReponseID, nil).
		Times(1)

	requestCounter := 0
	suite.mockComradeClient.EXPECT().
		GetResponse(
			gomock.Any(),
			gomock.Any(),
			aiReponseID,
		).DoAndReturn(func(_ any, _ any, _ any) (string, bool, error) {
		requestCounter++
		if requestCounter >= 3 {
			return string(aiResponseJSON), true, nil
		}
		return "", false, nil
	}).
		Times(3)

	client := pb.NewPRServiceClient(suite.grpcClient)

	operation, err := client.NeuroReview(authCtx, &pb.NeuroReviewRequest{
		PrId: grpc2.MarshalID(pr.ID),
	})
	require.NoError(t, err)
	require.NotNil(t, operation)

	suite.WaitForWorkflows(t, entities.WorkflowTypes.NeuroReview)

	t.Run("validate neuroreview prompts", func(t *testing.T) {
		userPromptLines := strings.Split(strings.TrimSpace(actualUserPrompt), "\n")
		randStartTag := userPromptLines[0]
		randEndTag := userPromptLines[len(userPromptLines)-1]

		params := map[string]string{
			"UserDataStartTag": randStartTag,
			"UserDataEndTag":   randEndTag,
			"Diff":             expectedDiff,
		}
		expectedSystemPromptBytes := bytes.Buffer{}
		err := neuroreview.SystemPromptTemplate.Execute(&expectedSystemPromptBytes, params)
		require.NoError(t, err)
		expectedSystemPrompt := strings.TrimSpace(expectedSystemPromptBytes.String())

		expectedUserPrompt := randStartTag + "\n\n" + expectedDiff + "\n\n" + randEndTag

		require.Equal(t, expectedSystemPrompt, actualSystemPrompt)
		require.Equal(t, expectedUserPrompt, actualUserPrompt)
	})

	t.Run("validate neuroreview comments", func(t *testing.T) {
		comments := suite.getPRComments(t, pr.ID)
		require.Len(t, comments, 9)

		for _, expected := range aiResponse["problems"].([]map[string]any) {
			file := expected["file"].(string)
			line := expected["line"].(int)
			operation := expected["operation"].(string)
			comment := expected["comment"].(string)

			if file == "nonexistent.go" {
				reviewComment := functools.First(comments, func(c *entities.PullRequestComment) bool {
					return c.Anchor == nil && c.Body == comment && c.Type == entities.PullRequestCommentTypes.CodeAssistant
				})
				require.NotNil(t, reviewComment)
				continue
			}

			reviewComment := functools.First(comments, func(c *entities.PullRequestComment) bool {
				return c.Anchor != nil && c.Anchor.Path == file && c.Anchor.From != nil && *c.Anchor.From == line
			})
			require.NotNil(t, reviewComment)
			require.Equal(t, entities.PullRequestCommentTypes.CodeAssistant, (*reviewComment).Type)
			require.Equal(t, comment, (*reviewComment).Body)

			expectedSide := entities.PullRequestCommentSides.Source
			if operation == "-" {
				expectedSide = entities.PullRequestCommentSides.Target
			}
			require.NotNil(t, (*reviewComment).Anchor.Side)
			require.Equal(t, expectedSide, *(*reviewComment).Anchor.Side)
		}
	})

	t.Run("validate neuroreview summary comment", func(t *testing.T) {
		comments := suite.getPRComments(t, pr.ID)

		summaryComment := comments[0]
		require.NotNil(t, summaryComment)
		require.Equal(t, `❗ Требуются доработки

**Описание изменений:**
...

**Основные проблемы:**
...

{% cut "Просмотрены файлы:" %}

config.go\
internal/config/config.go\
main.go\
utils.go\
validator.go

{% endcut %}
`, (*summaryComment).Body)
	})

	t.Run("validate neuroreview merge check", func(t *testing.T) {
		res, err := client.ListMergeChecks(authCtx, &pb.ListMergeChecksRequest{
			PrId: grpc2.MarshalID(pr.ID),
		})
		require.NoError(t, err)
		require.NotNil(t, res.MergeChecks.NeuroReview)
		require.Equal(t, pb.MergeCheck_SUCCESS, res.MergeChecks.NeuroReview.Check.Status)
		require.False(t, res.MergeChecks.NeuroReview.Outdated)
		require.Equal(t, grpc2.MarshalID(pr.Iteration), res.MergeChecks.NeuroReview.Iteration)
	})

	t.Run("validate feed event", func(t *testing.T) {
		res, err := client.ListFeed(authCtx, &pb.ListPullRequestFeedRequest{
			PrId: grpc2.MarshalID(pr.ID),
		})
		require.NoError(t, err)
		require.Len(t, res.Events, 10)
		neuroReviewStartEvent := res.Events[7]
		require.Equal(t, pb.PullRequestFeedItem_NEURO_REVIEW_START, neuroReviewStartEvent.EventType)
		details := neuroReviewStartEvent.Details.GetNeuroReviewStart()
		require.NotNil(t, details)
		require.Equal(t, grpc2.MarshalID(suite.users.Kopatych.ID), details.UserId)
		require.Equal(t, grpc2.MarshalID(pr.Iteration), details.Iteration.Id)

	})

	t.Run("second run is not allowed", func(t *testing.T) {
		operation, err := client.NeuroReview(authCtx, &pb.NeuroReviewRequest{
			PrId: grpc2.MarshalID(pr.ID),
		})
		yarequire.ProtoExceptionTemplate(t, err, except.NeuroReviewAlreadyCompleted)
		require.Nil(t, operation)
	})

	suite.mustBash(repo, `
		git checkout neuroreview-complex-test-branch
		cat > main.go << 'EOF'
package main

import "fmt"

func main() {
	fmt.Println("Hello from neuroreview-complex-test-branch!")
}
EOF
		git add .
		git commit -m "Add new commit"
	`)

	neuroReviewIteration := pr.Iteration
	pr, err = suite.PullRequestService.Get(context.Background(), pr.ID)
	require.NoError(t, err)

	t.Run("validate neuroreview merge check is outdated", func(t *testing.T) {
		res, err := client.ListMergeChecks(authCtx, &pb.ListMergeChecksRequest{
			PrId: grpc2.MarshalID(pr.ID),
		})
		require.NoError(t, err)
		require.NotNil(t, res.MergeChecks.NeuroReview)
		require.Equal(t, pb.MergeCheck_SUCCESS, res.MergeChecks.NeuroReview.Check.Status)
		require.True(t, res.MergeChecks.NeuroReview.Outdated)
		require.Equal(t, grpc2.MarshalID(neuroReviewIteration), res.MergeChecks.NeuroReview.Iteration)
	})

	t.Run("validate skipped review", func(t *testing.T) {

		suite.mustBash(repo, `
		git checkout master
		git checkout -b neuroreview-only-binary-branch
		head -c 2048 </dev/urandom > binary
		git add .
		git commit -m "Add new commit"
	`)

		onlyBinaryPR := suite.makePullRequest(suite.users.Kopatych, &makePrOptions{
			Repo:   repo,
			Source: "neuroreview-only-binary-branch",
			Target: "master",
			Title:  "Changes",
		})

		operation, err := client.NeuroReview(authCtx, &pb.NeuroReviewRequest{
			PrId: grpc2.MarshalID(onlyBinaryPR.ID),
		})
		require.NoError(t, err)
		require.NotNil(t, operation)

		suite.WaitForWorkflows(t, entities.WorkflowTypes.NeuroReview)

		comments := suite.getPRComments(t, onlyBinaryPR.ID)
		require.Len(t, comments, 1)
		require.Equal(t, entities.PullRequestCommentTypes.CodeAssistant, (*comments[0]).Type)
		require.Equal(t, `🔍 Нет изменений для ревью

{% cut "Пропущены файлы:" %}

binary

{% endcut %}
`, (*comments[0]).Body)
	})

	t.Run("validate generated files are skipped", func(t *testing.T) {
		suite.mustBash(repo, `
		git checkout master
		git checkout -b neuroreview-generated-files-branch
		cat > service.pb.go << 'EOF'
// Code generated by protoc-gen-go. DO NOT EDIT.
// versions:
EOF

		cat > service.pb.validate.go << 'EOF'
// Code generated by protoc-gen-validate. DO NOT EDIT.
// source: service.proto
EOF

		git add .
		git commit -m "Add new commit"
	`)

		generatedFilesPR := suite.makePullRequest(suite.users.Kopatych, &makePrOptions{
			Repo:   repo,
			Source: "neuroreview-generated-files-branch",
			Target: "master",
			Title:  "Changes",
		})

		operation, err := client.NeuroReview(authCtx, &pb.NeuroReviewRequest{
			PrId: grpc2.MarshalID(generatedFilesPR.ID),
		})
		require.NoError(t, err)
		require.NotNil(t, operation)

		suite.WaitForWorkflows(t, entities.WorkflowTypes.NeuroReview)

		comments := suite.getPRComments(t, generatedFilesPR.ID)
		require.Len(t, comments, 1)
		require.Equal(t, entities.PullRequestCommentTypes.CodeAssistant, (*comments[0]).Type)
		require.Equal(t, `🔍 Нет изменений для ревью

{% cut "Пропущены файлы:" %}

service.pb.go\
service.pb.validate.go

{% endcut %}
`, (*comments[0]).Body)
	})
}

func (suite *MockComradeTestSuite) TestNeuroReview_ErrorHandling() {
	t := suite.T()

	repoID, _, _ := suite.makeRandomRepo(t, suite.users.Kopatych)
	repo, err := suite.RepoRepo.GetRepositoryByID(context.Background(), repoID)
	require.NoError(t, err)

	suite.ensureDefaultMaster(repo)
	suite.mustBash(repo, `
		git checkout master
		
		# Создаем базовые файлы в master
		cat > main.go << 'EOF'
package main

import "fmt"

func main() {
	fmt.Println("Hello from master!")
}
EOF
		
		git add .
		git commit -m "Add base files to master"
		
		# Переключаемся на новую ветку и делаем изменения
		git checkout -b neuroreview-test-branch
		
		# Модифицируем существующий файл (main.go)
		cat > main.go << 'EOF'
package main

import (
	"fmt"
	"os"
)

func main() {
	const my_constant = 42 // bad naming style
	unused_var := "hello"  // unused variable
	
	if len(os.Args) > 1 {
		fmt.Printf("Hello %s! Constant: %d\n", os.Args[1], my_constant)
	} else {
		fmt.Println("Hello, World!")
	}
}
EOF
				
		git add .
		git commit -m "Changes: modify main.go"
	`)

	pr := suite.makePullRequest(suite.users.Kopatych, &makePrOptions{
		Repo:   repo,
		Source: "neuroreview-test-branch",
		Target: "master",
		Title:  "Changes for neuroreview testing",
	})

	aiResponse := map[string]interface{}{
		"problems": []map[string]interface{}{
			{
				"file":    "main.go",
				"line":    10,
				"comment": "Неиспользуемая переменная unused_var. Удалите её или используйте.",
			},
		},
		"summary": `❗ Требуются доработки

**Описание изменений:**
...

**Основные проблемы:**
...
`,
	}

	aiResponseJSON, err := json.Marshal(aiResponse)
	require.NoError(t, err)

	authCtx := testutils.AuthorizeGRPC(suite.users.Kopatych.Identity)
	client := pb.NewPRServiceClient(suite.grpcClient)

	t.Run("limit failure", func(t *testing.T) {
		suite.mockComradeClient.EXPECT().
			RequestModel(
				gomock.Any(),
				gomock.Any(),
				gomock.Any(),
				gomock.Any(),
			).
			Return("", except.LLMRequestLimitExceeded.Build()).
			Times(1)

		operation, err := client.NeuroReview(authCtx, &pb.NeuroReviewRequest{
			PrId: grpc2.MarshalID(pr.ID),
		})
		require.NoError(t, err)
		require.NotNil(t, operation)

		// workflow succeeded
		suite.WaitForWorkflows(t, entities.WorkflowTypes.NeuroReview)

		// no comments added
		comments := suite.getPRComments(t, pr.ID)
		require.Len(t, comments, 0)

		// mergecheck has exception status and contains error description
		res, err := client.ListMergeChecks(authCtx, &pb.ListMergeChecksRequest{
			PrId: grpc2.MarshalID(pr.ID),
		})
		require.NoError(t, err)
		require.NotNil(t, res.MergeChecks.NeuroReview)
		require.Equal(t, pb.MergeCheck_EXCEPTION, res.MergeChecks.NeuroReview.Check.Status)
		require.NotNil(t, res.MergeChecks.NeuroReview.Check.DisplayMessage)
		require.Equal(t, except.LLMRequestLimitExceeded.MessageID, res.MergeChecks.NeuroReview.Check.DisplayMessage.MessageId)
		require.False(t, res.MergeChecks.NeuroReview.Outdated)
		require.Equal(t, grpc2.MarshalID(pr.Iteration), res.MergeChecks.NeuroReview.Iteration)
	})

	t.Run("limit failure on get response", func(t *testing.T) {
		aiResponseID := "123"
		suite.mockComradeClient.EXPECT().
			RequestModel(
				gomock.Any(),
				gomock.Any(),
				gomock.Any(),
				gomock.Any(),
			).
			Return(aiResponseID, nil).
			Times(1)

		suite.mockComradeClient.EXPECT().
			GetResponse(
				gomock.Any(),
				gomock.Any(),
				aiResponseID,
			).
			Return("", true, except.LLMRequestLimitExceeded.Build()).
			Times(1)

		operation, err := client.NeuroReview(authCtx, &pb.NeuroReviewRequest{
			PrId: grpc2.MarshalID(pr.ID),
		})
		require.NoError(t, err)
		require.NotNil(t, operation)

		// workflow succeeded
		suite.WaitForWorkflows(t, entities.WorkflowTypes.NeuroReview)

		// no comments added
		comments := suite.getPRComments(t, pr.ID)
		require.Len(t, comments, 0)

		// mergecheck has exception status and contains error description
		res, err := client.ListMergeChecks(authCtx, &pb.ListMergeChecksRequest{
			PrId: grpc2.MarshalID(pr.ID),
		})
		require.NoError(t, err)
		require.NotNil(t, res.MergeChecks.NeuroReview)
		require.Equal(t, pb.MergeCheck_EXCEPTION, res.MergeChecks.NeuroReview.Check.Status)
		require.NotNil(t, res.MergeChecks.NeuroReview.Check.DisplayMessage)
		require.Equal(t, except.LLMRequestLimitExceeded.MessageID, res.MergeChecks.NeuroReview.Check.DisplayMessage.MessageId)
		require.False(t, res.MergeChecks.NeuroReview.Outdated)
		require.Equal(t, grpc2.MarshalID(pr.Iteration), res.MergeChecks.NeuroReview.Iteration)
	})

	t.Run("guard failure, ok after retry", func(t *testing.T) {
		counter := 0
		aiResponseID := "123"
		suite.mockComradeClient.EXPECT().
			RequestModel(
				gomock.Any(),
				gomock.Any(),
				gomock.Any(),
				gomock.Any(),
			).
			DoAndReturn(func(_ any, _ any, _ any, _ any) (string, error) {
				counter++
				return aiResponseID, nil
			}).
			Times(2)

		suite.mockComradeClient.EXPECT().
			GetResponse(
				gomock.Any(),
				gomock.Any(),
				aiResponseID,
			).
			DoAndReturn(func(_ any, _ any, _ any) (string, bool, error) {
				if counter >= 2 {
					return string(aiResponseJSON), true, nil
				}
				return "", true, except.LLMRequestInvalid.Build()
			}).
			Times(2)

		operation, err := client.NeuroReview(authCtx, &pb.NeuroReviewRequest{
			PrId: grpc2.MarshalID(pr.ID),
		})
		require.NoError(t, err)
		require.NotNil(t, operation)

		// workflow succeeded
		suite.WaitForWorkflows(t, entities.WorkflowTypes.NeuroReview)

		// 2 comments added
		comments := suite.getPRComments(t, pr.ID)
		require.Len(t, comments, 2)

		// mergecheck has success status
		res, err := client.ListMergeChecks(authCtx, &pb.ListMergeChecksRequest{
			PrId: grpc2.MarshalID(pr.ID),
		})
		require.NoError(t, err)
		require.NotNil(t, res.MergeChecks.NeuroReview)
		require.Equal(t, pb.MergeCheck_SUCCESS, res.MergeChecks.NeuroReview.Check.Status)
		require.Nil(t, res.MergeChecks.NeuroReview.Check.DisplayMessage)
		require.False(t, res.MergeChecks.NeuroReview.Outdated)
		require.Equal(t, grpc2.MarshalID(pr.Iteration), res.MergeChecks.NeuroReview.Iteration)
	})

	suite.mustBash(repo, `
		git checkout master
		git checkout -b neuroreview-test-branch-2
		cat > main.go << 'EOF'
package main

func main() {
	// some changes
}
EOF
		git add .
		git commit -m "Add new commit"
	`)

	pr = suite.makePullRequest(suite.users.Kopatych, &makePrOptions{
		Repo:   repo,
		Source: "neuroreview-test-branch-2",
		Target: "master",
		Title:  "Changes for neuroreview testing",
	})

	t.Run("total guard failure", func(t *testing.T) {
		aiResponseID := "123"
		suite.mockComradeClient.EXPECT().
			RequestModel(
				gomock.Any(),
				gomock.Any(),
				gomock.Any(),
				gomock.Any(),
			).
			DoAndReturn(func(_ any, _ any, _ any, _ any) (string, error) {
				return aiResponseID, nil
			}).
			Times(3)

		suite.mockComradeClient.EXPECT().
			GetResponse(
				gomock.Any(),
				gomock.Any(),
				aiResponseID,
			).
			DoAndReturn(func(_ any, _ any, _ any) (string, bool, error) {
				return "", true, except.LLMRequestInvalid.Build()
			}).
			Times(3)

		operation, err := client.NeuroReview(authCtx, &pb.NeuroReviewRequest{
			PrId: grpc2.MarshalID(pr.ID),
		})
		require.NoError(t, err)
		require.NotNil(t, operation)

		// workflow succeeded
		suite.WaitForWorkflows(t, entities.WorkflowTypes.NeuroReview)

		// no comments added
		comments := suite.getPRComments(t, pr.ID)
		require.Len(t, comments, 0)

		// mergecheck has success status
		res, err := client.ListMergeChecks(authCtx, &pb.ListMergeChecksRequest{
			PrId: grpc2.MarshalID(pr.ID),
		})
		require.NoError(t, err)
		require.NotNil(t, res.MergeChecks.NeuroReview)
		require.Equal(t, pb.MergeCheck_EXCEPTION, res.MergeChecks.NeuroReview.Check.Status)
		require.Nil(t, res.MergeChecks.NeuroReview.Check.DisplayMessage)
		require.False(t, res.MergeChecks.NeuroReview.Outdated)
		require.Equal(t, grpc2.MarshalID(pr.Iteration), res.MergeChecks.NeuroReview.Iteration)
	})

	t.Run("model response interpretation failure, ok after retry", func(t *testing.T) {
		counter := 0
		aiResponseID := "123"
		suite.mockComradeClient.EXPECT().
			RequestModel(
				gomock.Any(),
				gomock.Any(),
				gomock.Any(),
				gomock.Any(),
			).
			DoAndReturn(func(_ any, _ any, _ any, _ any) (string, error) {
				counter++
				return aiResponseID, nil
			}).
			Times(2)

		suite.mockComradeClient.EXPECT().
			GetResponse(
				gomock.Any(),
				gomock.Any(),
				aiResponseID,
			).
			DoAndReturn(func(_ any, _ any, _ any) (string, bool, error) {
				if counter >= 2 {
					return string(aiResponseJSON), true, nil
				}
				return "something invalid", true, nil
			}).
			Times(2)

		operation, err := client.NeuroReview(authCtx, &pb.NeuroReviewRequest{
			PrId: grpc2.MarshalID(pr.ID),
		})
		require.NoError(t, err)
		require.NotNil(t, operation)

		// workflow succeeded
		suite.WaitForWorkflows(t, entities.WorkflowTypes.NeuroReview)

		// no comments added
		comments := suite.getPRComments(t, pr.ID)
		require.Len(t, comments, 2)

		// mergecheck has success status
		res, err := client.ListMergeChecks(authCtx, &pb.ListMergeChecksRequest{
			PrId: grpc2.MarshalID(pr.ID),
		})
		require.NoError(t, err)
		require.NotNil(t, res.MergeChecks.NeuroReview)
		require.Equal(t, pb.MergeCheck_SUCCESS, res.MergeChecks.NeuroReview.Check.Status)
		require.Nil(t, res.MergeChecks.NeuroReview.Check.DisplayMessage)
		require.False(t, res.MergeChecks.NeuroReview.Outdated)
		require.Equal(t, grpc2.MarshalID(pr.Iteration), res.MergeChecks.NeuroReview.Iteration)
	})

	suite.mustBash(repo, `
		git checkout master
		git checkout -b neuroreview-test-branch-3
		cat > main.go << 'EOF'
package main

func main() {
	// some changes
}
EOF
		git add .
		git commit -m "Add new commit"
	`)

	pr = suite.makePullRequest(suite.users.Kopatych, &makePrOptions{
		Repo:   repo,
		Source: "neuroreview-test-branch-3",
		Target: "master",
		Title:  "Changes for neuroreview testing",
	})

	t.Run("total model response interpretation failure", func(t *testing.T) {
		aiResponseID := "123"
		suite.mockComradeClient.EXPECT().
			RequestModel(
				gomock.Any(),
				gomock.Any(),
				gomock.Any(),
				gomock.Any(),
			).
			DoAndReturn(func(_ any, _ any, _ any, _ any) (string, error) {
				return aiResponseID, nil
			}).
			Times(3)

		suite.mockComradeClient.EXPECT().
			GetResponse(
				gomock.Any(),
				gomock.Any(),
				aiResponseID,
			).
			DoAndReturn(func(_ any, _ any, _ any) (string, bool, error) {
				return "something invalid", true, nil
			}).
			Times(3)

		operation, err := client.NeuroReview(authCtx, &pb.NeuroReviewRequest{
			PrId: grpc2.MarshalID(pr.ID),
		})
		require.NoError(t, err)
		require.NotNil(t, operation)

		// workflow failed
		s, ok := suite.Scheduler.(*scheduler.SchedulerService)
		require.True(t, ok)
		err = s.WaitWorkflowByType(context.Background(), entities.WorkflowTypes.NeuroReview)
		require.Error(t, err)

		// no comments added
		comments := suite.getPRComments(t, pr.ID)
		require.Len(t, comments, 0)

		// mergecheck has exception status
		res, err := client.ListMergeChecks(authCtx, &pb.ListMergeChecksRequest{
			PrId: grpc2.MarshalID(pr.ID),
		})
		require.NoError(t, err)
		require.NotNil(t, res.MergeChecks.NeuroReview)
		require.Equal(t, pb.MergeCheck_EXCEPTION, res.MergeChecks.NeuroReview.Check.Status)
		require.Nil(t, res.MergeChecks.NeuroReview.Check.DisplayMessage)
		require.False(t, res.MergeChecks.NeuroReview.Outdated)
		require.Equal(t, grpc2.MarshalID(pr.Iteration), res.MergeChecks.NeuroReview.Iteration)
	})

}

func (suite *MockComradeTestSuite) TestNeuroReview_Access() {
	t := suite.T()

	_, orgSlug, repoSlug := suite.makeRandomRepo(t, suite.users.Kopatych)

	repo, err := suite.RepoRepo.GetRepository(context.Background(), orgSlug, repoSlug)
	require.NoError(t, err)

	org, err := suite.OrgRepo.GetOrganization(context.Background(), orgSlug)
	require.NoError(t, err)

	suite.mustBash(repo, `
		git checkout -b master
		cat > main.go << 'EOF'
package main
EOF

		git add .
		git commit -m "Initial commit"

		git checkout -b neuroreview-test-branch
		cat > main.go << 'EOF'
package main

func main() {
	// some changes
}
EOF
		git add .
		git commit -m "Add new commit"
	`)

	pr := suite.makePullRequest(suite.users.Admin, &makePrOptions{
		Repo:   repo,
		Source: "neuroreview-test-branch",
		Target: "master",
		Title:  "Changes",
	})

	authCtx := testutils.AuthorizeGRPC(suite.users.Admin.Identity)
	client := pb.NewPRServiceClient(suite.grpcClient)

	availableActions, err := client.GetAvailableActions(authCtx, &pb.GetAvailablePRActionsRequest{
		Id: grpc2.MarshalID(pr.ID),
	})
	require.NoError(t, err)
	require.True(t, availableActions.NeuroReview)

	orgClient := pb.NewOrgServiceClient(suite.grpcClient)
	_, err = orgClient.UpdateProfile(authCtx, &pb.UpdateOrgProfileRequest{
		Id:             grpc2.MarshalID(org.ID),
		Fl152Compliant: true,
		Visibility:     pb.ProfileVisibility_PROFILE_PRIVATE,
		UpdateMask: &fieldmaskpb.FieldMask{
			Paths: []string{"fl152_compliant", "visibility"},
		},
	})
	require.NoError(t, err)

	availableActions, err = client.GetAvailableActions(authCtx, &pb.GetAvailablePRActionsRequest{
		Id: grpc2.MarshalID(pr.ID),
	})
	require.NoError(t, err)
	require.True(t, availableActions.NeuroReview)

	_, err = client.NeuroReview(authCtx, &pb.NeuroReviewRequest{
		PrId: grpc2.MarshalID(pr.ID),
	})
	yarequire.ProtoExceptionTemplate(t, err, except.OrganizationIsFL152Compliant)
}

func (suite *MockComradeTestSuite) TestNeuroReview_Customization() {
	t := suite.T()

	repoID, _, _ := suite.makeRandomRepo(t, suite.users.Kopatych)
	repo, err := suite.RepoRepo.GetRepositoryByID(context.Background(), repoID)
	require.NoError(t, err)

	suite.ensureDefaultMaster(repo)
	suite.mustBash(repo, fmt.Sprintf(`
		git checkout master
		
		cat > main.go << 'EOF'
package main

import "fmt"

func main() {
	fmt.Println("Hello from master!")
}
EOF
		
		cat > %s << 'EOF'
Проект использует следующие технологии...
EOF

		git add .
		git commit -m "Init"
		
		# Переключаемся на новую ветку и делаем изменения
		git checkout -b neuroreview-test-branch
		
		# Модифицируем существующий файл (main.go)
		cat > main.go << 'EOF'
package main

import (
	"fmt"
	"os"
)

func main() {
	const my_constant = 42 // bad naming style
	unused_var := "hello"  // unused variable
	
	if len(os.Args) > 1 {
		fmt.Printf("Hello %%s! Constant: %%d\n", os.Args[1], my_constant)
	} else {
		fmt.Println("Hello, World!")
	}
}
EOF
				
		git add .
		git commit -m "Changes: modify main.go"
	`, consts.Agents))

	pr := suite.makePullRequest(suite.users.Kopatych, &makePrOptions{
		Repo:   repo,
		Source: "neuroreview-test-branch",
		Target: "master",
		Title:  "Changes for neuroreview testing",
	})

	aiResponse := map[string]interface{}{
		"problems": []map[string]interface{}{
			{
				"file":    "main.go",
				"line":    10,
				"comment": "Неиспользуемая переменная unused_var. Удалите её или используйте.",
			},
		},
		"summary": `❗ Требуются доработки

**Описание изменений:**
...

**Основные проблемы:**
...
`,
	}

	aiResponseJSON, err := json.Marshal(aiResponse)
	require.NoError(t, err)

	authCtx := testutils.AuthorizeGRPC(suite.users.Kopatych.Identity)
	client := pb.NewPRServiceClient(suite.grpcClient)

	actualUserPrompt := ""

	aiReponseID := "123"

	suite.mockComradeClient.EXPECT().
		RequestModel(
			gomock.Any(),
			gomock.Any(),
			gomock.Any(),
			gomock.Any(),
		).
		Do(func(_ any, _ any, systemPrompt string, userPrompt string) {
			actualUserPrompt = userPrompt
		}).
		Return(aiReponseID, nil).
		Times(1)

	suite.mockComradeClient.EXPECT().
		GetResponse(
			gomock.Any(),
			gomock.Any(),
			aiReponseID,
		).DoAndReturn(func(_ any, _ any, _ any) (string, bool, error) {
		return string(aiResponseJSON), true, nil
	}).
		Times(1)

	operation, err := client.NeuroReview(authCtx, &pb.NeuroReviewRequest{
		PrId: grpc2.MarshalID(pr.ID),
	})
	require.NoError(t, err)
	require.NotNil(t, operation)

	suite.WaitForWorkflows(t, entities.WorkflowTypes.NeuroReview)

	require.Contains(t, actualUserPrompt, "Проект использует следующие технологии...")
}

func (suite *MockComradeTestSuite) getPRComments(t *testing.T, prID uint64) []*entities.PullRequestComment {
	ctx := context.Background()

	client := pb.NewPRCommentServiceClient(suite.grpcClient)
	authCtx := testutils.AuthorizeGRPC(suite.users.Kopatych.Identity)

	pr, err := suite.PullRequestRepo.Get(context.Background(), prID)
	require.NoError(t, err)

	response, err := client.List(authCtx, &pb.ListCommentsRequest{
		PrId:     grpc2.MarshalID(prID),
		PageSize: utils.PtrFromValue(uint64(100)),
	})
	require.NoError(t, err)

	var comments []*entities.PullRequestComment
	for _, pbComment := range response.Comments {
		commentID, err := grpc2.ParseID(pbComment.Id)
		require.NoError(t, err)

		comment, err := suite.PullRequestCommentService.Get(ctx, pr.RepoID, prID, commentID)
		require.NoError(t, err)
		comments = append(comments, comment)
	}

	return comments
}

func (suite *RwApiTestSuite) TestCollectReviewData() {
	t := suite.T()
	ctx := context.Background()

	repoID, _, _ := suite.makeRandomRepo(t, suite.users.Kopatych)
	repo, err := suite.RepoService.Get(ctx, repoID)
	require.NoError(t, err)

	suite.mustBash(repo, `
	git checkout -b master
	
	echo "package main" > main.go
	echo "package test" > test.go
	echo "Hello World" > README.md
	echo "Some text" > big_diff.txt
	echo "unchanged file" > unchanged.txt
	git add .
	git commit -m "Initial commit"
	
	# Create test branch with various file changes
	git checkout -b test-collect-review-data
	
	# 1. Modified file
	cat > main.go << 'EOF'
package main

import "fmt"

func main() {
	fmt.Println("Modified!")
}
EOF
		
	# 2. New file
	cat > utils.go << 'EOF'
package utils

func Helper() string {
	return "helper"
}
EOF
		
	# 3. Deleted file
	rm README.md
	
	# 4. Binary file
	printf '\x89\x50\x4e\x47\x0d\x0a\x1a\x0a\x00\x00\x00\x0d\x49\x48\x44\x52' > image.png
	
	# 5. Generated file (package-lock.json)
	cat > package-lock.json << 'EOF'
{"lockfileVersion":3,"packages":{"":{"dependencies":{"lodash":"^4.17.21"}}}}
EOF
		
	# 6. Renamed file
	git mv test.go test_renamed.go

	# 7. Large file
	yes "This is a line of text to make a large file for testing purposes. Lorem ipsum dolor sit amet." | head -n 100000 > large.txt

	# 8. Big diff file
	yes "This is a line of text to make a large file for testing purposes. Lorem ipsum dolor sit amet.\n" | head -n 3000 > big_diff.txt
	
	git add .
	git commit -m "Test various file types"
`)

	pr := suite.makePullRequest(suite.users.Kopatych, &makePrOptions{
		Repo:   repo,
		Source: "test-collect-review-data",
		Target: "master",
		Title:  "Test PR with various file types",
	})

	authenticator := common.NewIAMTokenAuthenticator(
		testutils.FakeIAMAuthToken(suite.users.Kopatych.Identity),
		&suite.users.Kopatych.Identity,
	)
	result, err := suite.NeuroReviewService.CollectReviewData(
		ctx,
		suite.users.Kopatych.ID,
		authenticator,
		repo.ID,
		pr.ID,
		pr.Iteration,
		nil,
	)
	require.NoError(t, err)

	require.ElementsMatch(t, []string{"utils.go", "test_renamed.go", "README.md", "main.go"}, result.CollectedFiles)
	require.ElementsMatch(t, []string{"image.png", "package-lock.json", "large.txt", "big_diff.txt"}, result.SkippedFiles)

	expectedDiff := `diff --git a/README.md b/README.md
deleted file mode 100644
--- a/README.md
+++ /dev/null
@@ -1,1 +0,0 @@
     1       - Hello World
diff --git a/big_diff.txt b/big_diff.txt
<Diff size limit exceeded>
diff --git a/image.png b/image.png
new file mode 100644
Binary files /dev/null and b/image.png differ
diff --git a/large.txt b/large.txt
new file mode 100644
<File is too large>
diff --git a/main.go b/main.go
@@ -1,1 +1,7 @@
     1     1   package main
           2 + 
           3 + import "fmt"
           4 + 
           5 + func main() {
           6 +     fmt.Println("Modified!")
           7 + }
diff --git a/package-lock.json b/package-lock.json
new file mode 100644
<File is generated>
diff --git a/test.go b/test_renamed.go
rename from test.go
rename to test_renamed.go
diff --git a/utils.go b/utils.go
new file mode 100644
--- /dev/null
+++ b/utils.go
@@ -0,0 +1,5 @@
           1 + package utils
           2 + 
           3 + func Helper() string {
           4 +     return "helper"
           5 + }
`
	require.Equal(t, expectedDiff, result.Diff)
}
