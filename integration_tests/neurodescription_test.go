package integrationtests

import (
	"bytes"
	grpc2 "common/grpc"
	"common/testutils/yarequire"
	"common/utils"
	"context"
	"fmt"
	"gitcore/internal/consts"
	"gitcore/internal/entities"
	except "gitcore/internal/exceptions"
	"gitcore/internal/pullrequests/neurodescription"
	"gitcore/internal/services/scheduler"
	"gitcore/internal/testutils"
	pb "private_api/generated/yandex/cloud/priv/gitcore/v1"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
	"google.golang.org/protobuf/types/known/fieldmaskpb"
)

func (suite *MockComradeTestSuite) TestNeuroDescription() {
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

		cat > %s << 'EOF'
Проект использует следующие технологии...
EOF

		git add .
		git commit -m "Add base files to master"
		
		git checkout -b neurodescription-complex-test-branch
		
		# 1. Модифицируем существующий файл (main.go)
		cat > main.go << 'EOF'
package main

import (
	"fmt"
	"os"
)

func main() {
	const my_constant = 42
	unused_var := "hello"
	
	if len(os.Args) > 1 {
		fmt.Printf("Hello %%s! Constant: %%d\n", os.Args[1], my_constant)
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
	return strings.Contains(email, "@")
}

// TODO: Add proper regex validation
func validateDomain(domain string) bool {
	return len(domain) > 0
}
EOF
		
		# 3. Модифицируем utils.go
		cat > utils.go << 'EOF'
package utils

import "strings"

// Helper function - improved version
func Helper() string {
	return "improved_helper"
}

// NewFunction adds new functionality
func NewFunction(input string) string {
	return strings.ToUpper(input)
}
EOF
		
		# 4. Удаляем файл config.go
		rm internal/config/config.go
		
		# 5. Создаем новый config в другом месте
		cat > config.go << 'EOF'
package main

// Configuration constants
const (
	VERSION = "2.0.0"
	debug   = false
)
EOF
		
		git add .
		git commit -m "Complex changes for neurodescription testing"
	`, consts.Agents))

	pr := suite.makePullRequest(suite.users.Kopatych, &makePrOptions{
		Repo:   repo,
		Source: "neurodescription-complex-test-branch",
		Target: "master",
		Title:  "Complex changes for neurodescription testing",
	})

	aiResponse := `# Рефакторинг кодовой базы и улучшение конфигурации

- **Улучшена функция main.go**: добавлена поддержка аргументов командной строки для персонализированного приветствия
- **Добавлена валидация email**: реализована функция ValidateEmail для базовой проверки формата email
- **Обновлена функция Helper**: изменен возвращаемый текст для более информативного ответа
- **Добавлена функция преобразования строк**: новая функция NewFunction для перевода строк в верхний регистр
- **Перенесена конфигурация**: константы конфигурации перемещены из internal/config в корневой пакет и обновлены до версии 2.0.0
- **Удалена устаревшая функция**: убрана deprecated функция OldHelper из utils.go
`

	expectedDiff := `diff --git a/config.go b/config.go
new file mode 100644
--- /dev/null
+++ b/config.go
@@ -0,0 +1,7 @@
           1 + package main
           2 + 
           3 + // Configuration constants
           4 + const (
           5 +     VERSION = "2.0.0"
           6 +     debug   = false
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
           9 +     const my_constant = 42
          10 +     unused_var := "hello"
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
          12 +     return strings.ToUpper(input)
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
           7 +     return strings.Contains(email, "@")
           8 + }
           9 + 
          10 + // TODO: Add proper regex validation
          11 + func validateDomain(domain string) bool {
          12 +     return len(domain) > 0
          13 + }
`

	actualSystemPrompt := ""
	actualUserPrompt := ""

	aiResponseID := "123"

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
		Return(aiResponseID, nil).
		Times(1)

	requestCounter := 0
	suite.mockComradeClient.EXPECT().
		GetResponse(
			gomock.Any(),
			gomock.Any(),
			aiResponseID,
		).DoAndReturn(func(_ any, _ any, _ any) (string, bool, error) {
		requestCounter++
		if requestCounter >= 3 {
			return aiResponse, true, nil
		}
		return "", false, nil
	}).
		Times(3)

	authCtx := testutils.AuthorizeGRPC(suite.users.Kopatych.Identity)
	client := pb.NewPRServiceClient(suite.grpcClient)

	t.Run("validate PR description neuro describe fields", func(t *testing.T) {
		pr, err := client.Get(authCtx, &pb.GetPullRequestRequest{
			Identity: &pb.GetPullRequestRequest_Id{
				Id: grpc2.MarshalID(pr.ID),
			},
		})
		require.NoError(t, err)
		require.NotNil(t, pr)
		require.Empty(t, pr.Description)
		require.Nil(t, pr.NeuroDescribeInfo.OperationId)
		require.Nil(t, pr.NeuroDescribeInfo.Status)
	})

	operation, err := client.NeuroDescribe(authCtx, &pb.NeuroDescribeRequest{
		PrId: grpc2.MarshalID(pr.ID),
	})
	require.NoError(t, err)
	require.NotNil(t, operation)

	suite.WaitForWorkflows(t, entities.WorkflowTypes.NeuroDescription)

	t.Run("validate prompts", func(t *testing.T) {
		userPromptLines := strings.Split(strings.TrimSpace(actualUserPrompt), "\n")
		randStartTag := userPromptLines[4]
		randEndTag := userPromptLines[len(userPromptLines)-1]

		params := map[string]string{
			"UserDataStartTag": randStartTag,
			"UserDataEndTag":   randEndTag,
		}
		expectedSystemPromptBytes := bytes.Buffer{}
		err := neurodescription.SingleDescriptionSystemPromptTemplate.Execute(&expectedSystemPromptBytes, params)
		require.NoError(t, err)
		expectedSystemPrompt := strings.TrimSpace(expectedSystemPromptBytes.String())

		expectedUserPrompt := "Проект использует следующие технологии...\n\n\n\n" + randStartTag + "\n\n" + expectedDiff + "\n\n" + randEndTag

		require.Equal(t, expectedSystemPrompt, actualSystemPrompt)
		require.Equal(t, expectedUserPrompt, actualUserPrompt)
	})

	t.Run("validate PR description was updated", func(t *testing.T) {
		pr, err := client.Get(authCtx, &pb.GetPullRequestRequest{
			Identity: &pb.GetPullRequestRequest_Id{
				Id: grpc2.MarshalID(pr.ID),
			},
		})
		require.NoError(t, err)
		require.NotNil(t, pr)
		require.Equal(t, aiResponse, pr.Description)
		require.NotNil(t, pr.NeuroDescribeInfo.OperationId)
		require.NotNil(t, pr.NeuroDescribeInfo.Status)
		require.Equal(t, pb.NeuroDescribeInfo_GENERATED, *(pr.NeuroDescribeInfo.Status))
	})

	t.Run("validate operation", func(t *testing.T) {
		pr, err := client.Get(authCtx, &pb.GetPullRequestRequest{
			Identity: &pb.GetPullRequestRequest_Id{
				Id: grpc2.MarshalID(pr.ID),
			},
		})
		require.NoError(t, err)
		require.NotNil(t, pr)
		require.NotNil(t, pr.NeuroDescribeInfo.OperationId)

		opClient := pb.NewOperationServiceClient(suite.grpcClient)
		op, err := opClient.Get(authCtx, &pb.GetOperationRequest{
			Id: *pr.NeuroDescribeInfo.OperationId,
		})
		require.NoError(t, err)
		require.NotNil(t, op)
		require.True(t, op.Done)
		require.Nil(t, (*op).GetError())

		meta, err := op.Metadata.UnmarshalNew()
		require.NoError(t, err)

		yarequire.ProtoEqual(t, &pb.NeuroDescribeMetadata{
			UserId:    grpc2.MarshalID(suite.users.Kopatych.ID),
			Iteration: pr.Iteration,
			Status:    pb.OperationMetadata_SUCCESS,
		}, meta)
	})

	t.Run("validate PR feed", func(t *testing.T) {
		feed, err := client.ListFeed(authCtx, &pb.ListPullRequestFeedRequest{
			PrId: grpc2.MarshalID(pr.ID),
		})
		require.NoError(t, err)
		require.NotNil(t, feed)
		require.Equal(t, 1, len(feed.Events))
		require.Equal(t, pb.PullRequestFeedItem_PR_UPDATED, feed.Events[0].EventType)
		require.Equal(t, pb.PullRequestFeedItem_UpdateDetails_TYPE_CODE_ASSISTANT, feed.Events[0].Details.GetUpdate().GetType())
	})

	t.Run("manual description update", func(t *testing.T) {
		_, err := client.Update(authCtx, &pb.UpdatePullRequestRequest{
			Id:          grpc2.MarshalID(pr.ID),
			Description: utils.PtrFromValue("Manual description update"),
			UpdateMask: &fieldmaskpb.FieldMask{
				Paths: []string{"description"},
			},
		})
		require.NoError(t, err)

		pr, err := client.Get(authCtx, &pb.GetPullRequestRequest{
			Identity: &pb.GetPullRequestRequest_Id{
				Id: grpc2.MarshalID(pr.ID),
			},
		})
		require.NoError(t, err)
		require.NotNil(t, pr)
		require.NotNil(t, pr.NeuroDescribeInfo.OperationId)
		require.Nil(t, pr.NeuroDescribeInfo.Status)

		feed, err := client.ListFeed(authCtx, &pb.ListPullRequestFeedRequest{
			PrId: pr.Id,
		})
		require.NoError(t, err)
		require.NotNil(t, feed)
		require.Equal(t, 2, len(feed.Events))
		require.Equal(t, pb.PullRequestFeedItem_PR_UPDATED, feed.Events[0].EventType)
		require.Equal(t, pb.PullRequestFeedItem_UpdateDetails_TYPE_DEFAULT, feed.Events[0].Details.GetUpdate().GetType())
	})
}

func (suite *MockComradeTestSuite) TestNeuroDescription_OnPRCreation() {
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

		cat > %s << 'EOF'
Проект использует следующие технологии...
EOF

		git add .
		git commit -m "Add base files to master"
		
		git checkout -b neurodescription-complex-test-branch
		
		# 1. Модифицируем существующий файл (main.go)
		cat > main.go << 'EOF'
package main

import (
	"fmt"
	"os"
)

func main() {
	const my_constant = 42
	unused_var := "hello"
	
	if len(os.Args) > 1 {
		fmt.Printf("Hello %%s! Constant: %%d\n", os.Args[1], my_constant)
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
	return strings.Contains(email, "@")
}

// TODO: Add proper regex validation
func validateDomain(domain string) bool {
	return len(domain) > 0
}
EOF
		
		# 3. Модифицируем utils.go
		cat > utils.go << 'EOF'
package utils

import "strings"

// Helper function - improved version
func Helper() string {
	return "improved_helper"
}

// NewFunction adds new functionality
func NewFunction(input string) string {
	return strings.ToUpper(input)
}
EOF
		
		# 4. Удаляем файл config.go
		rm internal/config/config.go
		
		# 5. Создаем новый config в другом месте
		cat > config.go << 'EOF'
package main

// Configuration constants
const (
	VERSION = "2.0.0"
	debug   = false
)
EOF
		
		git add .
		git commit -m "Complex changes for neurodescription testing"
	`, consts.Agents))

	aiResponse := `# Рефакторинг кодовой базы и улучшение конфигурации

- **Улучшена функция main.go**: добавлена поддержка аргументов командной строки для персонализированного приветствия
- **Добавлена валидация email**: реализована функция ValidateEmail для базовой проверки формата email
- **Обновлена функция Helper**: изменен возвращаемый текст для более информативного ответа
- **Добавлена функция преобразования строк**: новая функция NewFunction для перевода строк в верхний регистр
- **Перенесена конфигурация**: константы конфигурации перемещены из internal/config в корневой пакет и обновлены до версии 2.0.0
- **Удалена устаревшая функция**: убрана deprecated функция OldHelper из utils.go
`

	expectedDiff := `diff --git a/config.go b/config.go
new file mode 100644
--- /dev/null
+++ b/config.go
@@ -0,0 +1,7 @@
           1 + package main
           2 + 
           3 + // Configuration constants
           4 + const (
           5 +     VERSION = "2.0.0"
           6 +     debug   = false
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
           9 +     const my_constant = 42
          10 +     unused_var := "hello"
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
          12 +     return strings.ToUpper(input)
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
           7 +     return strings.Contains(email, "@")
           8 + }
           9 + 
          10 + // TODO: Add proper regex validation
          11 + func validateDomain(domain string) bool {
          12 +     return len(domain) > 0
          13 + }
`

	actualSystemPrompt := ""
	actualUserPrompt := ""

	aiResponseID := "123"

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
		Return(aiResponseID, nil).
		Times(1)

	requestCounter := 0
	suite.mockComradeClient.EXPECT().
		GetResponse(
			gomock.Any(),
			gomock.Any(),
			aiResponseID,
		).DoAndReturn(func(_ any, _ any, _ any) (string, bool, error) {
		requestCounter++
		if requestCounter >= 3 {
			return aiResponse, true, nil
		}
		return "", false, nil
	}).
		Times(3)

	authCtx := testutils.AuthorizeGRPC(suite.users.Kopatych.Identity)
	client := pb.NewPRServiceClient(suite.grpcClient)

	op, err := client.Create(authCtx, &pb.CreatePullRequestRequest{
		RepoId:        grpc2.MarshalID(repo.ID),
		Source:        "neurodescription-complex-test-branch",
		Target:        "master",
		Title:         "Complex changes for neurodescription testing",
		NeuroDescribe: true,
	})
	require.NoError(t, err)

	pr := &pb.PullRequest{}
	err = op.GetResponse().UnmarshalTo(pr)
	require.NoError(t, err)

	suite.WaitForWorkflows(t, entities.WorkflowTypes.NeuroDescription)

	t.Run("validate prompts", func(t *testing.T) {
		userPromptLines := strings.Split(strings.TrimSpace(actualUserPrompt), "\n")
		randStartTag := userPromptLines[4]
		randEndTag := userPromptLines[len(userPromptLines)-1]

		params := map[string]string{
			"UserDataStartTag": randStartTag,
			"UserDataEndTag":   randEndTag,
		}
		expectedSystemPromptBytes := bytes.Buffer{}
		err := neurodescription.SingleDescriptionSystemPromptTemplate.Execute(&expectedSystemPromptBytes, params)
		require.NoError(t, err)
		expectedSystemPrompt := strings.TrimSpace(expectedSystemPromptBytes.String())

		expectedUserPrompt := "Проект использует следующие технологии...\n\n\n\n" + randStartTag + "\n\n" + expectedDiff + "\n\n" + randEndTag

		require.Equal(t, expectedSystemPrompt, actualSystemPrompt)
		require.Equal(t, expectedUserPrompt, actualUserPrompt)
	})

	t.Run("validate PR description was set", func(t *testing.T) {
		pr, err := client.Get(authCtx, &pb.GetPullRequestRequest{
			Identity: &pb.GetPullRequestRequest_Id{
				Id: pr.Id,
			},
		})
		require.NoError(t, err)
		require.NotNil(t, pr)
		require.Equal(t, aiResponse, pr.Description)
		require.NotNil(t, pr.NeuroDescribeInfo.OperationId)
		require.NotNil(t, pr.NeuroDescribeInfo.Status)
		require.Equal(t, pb.NeuroDescribeInfo_GENERATED, *(pr.NeuroDescribeInfo.Status))
	})

	t.Run("validate operation", func(t *testing.T) {
		pr, err := client.Get(authCtx, &pb.GetPullRequestRequest{
			Identity: &pb.GetPullRequestRequest_Id{
				Id: pr.Id,
			},
		})
		require.NoError(t, err)
		require.NotNil(t, pr)
		require.NotNil(t, pr.NeuroDescribeInfo.OperationId)

		opClient := pb.NewOperationServiceClient(suite.grpcClient)
		op, err := opClient.Get(authCtx, &pb.GetOperationRequest{
			Id: *pr.NeuroDescribeInfo.OperationId,
		})
		require.NoError(t, err)
		require.NotNil(t, op)
		require.True(t, op.Done)
		require.Nil(t, (*op).GetError())

		meta, err := op.Metadata.UnmarshalNew()
		require.NoError(t, err)

		yarequire.ProtoEqual(t, &pb.NeuroDescribeMetadata{
			UserId:    grpc2.MarshalID(suite.users.Kopatych.ID),
			Iteration: pr.Iteration,
			Status:    pb.OperationMetadata_SUCCESS,
		}, meta)
	})

	t.Run("validate PR feed", func(t *testing.T) {
		feed, err := client.ListFeed(authCtx, &pb.ListPullRequestFeedRequest{
			PrId: pr.Id,
		})
		require.NoError(t, err)
		require.NotNil(t, feed)
		require.Equal(t, 1, len(feed.Events))
		require.Equal(t, pb.PullRequestFeedItem_PR_UPDATED, feed.Events[0].EventType)
		require.Equal(t, pb.PullRequestFeedItem_UpdateDetails_TYPE_CODE_ASSISTANT, feed.Events[0].Details.GetUpdate().GetType())
	})

	t.Run("manual description update", func(t *testing.T) {
		_, err := client.Update(authCtx, &pb.UpdatePullRequestRequest{
			Id:          pr.Id,
			Description: utils.PtrFromValue("Manual description update"),
			UpdateMask: &fieldmaskpb.FieldMask{
				Paths: []string{"description"},
			},
		})
		require.NoError(t, err)

		pr, err := client.Get(authCtx, &pb.GetPullRequestRequest{
			Identity: &pb.GetPullRequestRequest_Id{
				Id: pr.Id,
			},
		})
		require.NoError(t, err)
		require.NotNil(t, pr)
		require.NotNil(t, pr.NeuroDescribeInfo.OperationId)
		require.Nil(t, pr.NeuroDescribeInfo.Status)

		feed, err := client.ListFeed(authCtx, &pb.ListPullRequestFeedRequest{
			PrId: pr.Id,
		})
		require.NoError(t, err)
		require.NotNil(t, feed)
		require.Equal(t, 2, len(feed.Events))
		require.Equal(t, pb.PullRequestFeedItem_PR_UPDATED, feed.Events[0].EventType)
		require.Equal(t, pb.PullRequestFeedItem_UpdateDetails_TYPE_DEFAULT, feed.Events[0].Details.GetUpdate().GetType())
	})
}

func (suite *MockComradeTestSuite) TestNeuroDescription_LargePR() {
	t := suite.T()

	repoID, _, _ := suite.makeRandomRepo(t, suite.users.Kopatych)
	repo, err := suite.RepoRepo.GetRepositoryByID(context.Background(), repoID)
	require.NoError(t, err)

	suite.ensureDefaultMaster(repo)
	suite.mustBash(repo, fmt.Sprintf(`
		git checkout master
		
		# Create base files
		echo "package main" > main.go

		cat > %s << 'EOF'
Проект использует следующие технологии...
EOF

		git add .
		git commit -m "Initial commit"
		
		# Create branch with large files
		git checkout -b large-pr-branch
		
		# Create first large file
		for i in {1..2000}; do
			echo "a"
		done > file1.go
		
		# Create second large file
		for i in {1..2000}; do
			echo "b"
		done > file2.go
		
		git add .
		git commit -m "Add large files for testing"
	`, consts.Agents))

	pr := suite.makePullRequest(suite.users.Kopatych, &makePrOptions{
		Repo:   repo,
		Source: "large-pr-branch",
		Target: "master",
		Title:  "Large PR",
	})

	// AI responses for chunks
	chunkSummary1 := "В файле file1.go добавлено 2000 строк с тестовыми данными."
	chunkSummary2 := "В файле file2.go добавлено 2000 строк с тестовыми данными."

	// Final AI response after combining summaries
	finalResponse := `# Добавление больших тестовых файлов

- **Добавлен файл file1.go**: содержит 2000 строк с тестовыми данными
- **Добавлен файл file2.go**: содержит 2000 строк с тестовыми данными
`

	authCtx := testutils.AuthorizeGRPC(suite.users.Kopatych.Identity)
	client := pb.NewPRServiceClient(suite.grpcClient)

	// Variables to capture prompts
	var chunk1SystemPrompt, chunk1UserPrompt string
	var chunk2SystemPrompt, chunk2UserPrompt string
	var finalSystemPrompt, finalUserPrompt string

	// Mock for chunk 1 request
	chunkResponseID1 := "chunk_1"
	suite.mockComradeClient.EXPECT().
		RequestModel(
			gomock.Any(),
			gomock.Any(),
			gomock.Any(),
			gomock.Any(),
		).
		Do(func(_ any, _ any, systemPrompt string, userPrompt string) {
			chunk1SystemPrompt = systemPrompt
			chunk1UserPrompt = userPrompt
		}).
		Return(chunkResponseID1, nil).
		Times(1)

	// Mock for chunk 1 response
	suite.mockComradeClient.EXPECT().
		GetResponse(
			gomock.Any(),
			gomock.Any(),
			chunkResponseID1,
		).
		Return(chunkSummary1, true, nil).
		Times(1)

	// Mock for chunk 2 request
	chunkResponseID2 := "chunk_2"
	suite.mockComradeClient.EXPECT().
		RequestModel(
			gomock.Any(),
			gomock.Any(),
			gomock.Any(),
			gomock.Any(),
		).
		Do(func(_ any, _ any, systemPrompt string, userPrompt string) {
			chunk2SystemPrompt = systemPrompt
			chunk2UserPrompt = userPrompt
		}).
		Return(chunkResponseID2, nil).
		Times(1)

	// Mock for chunk 2 response
	suite.mockComradeClient.EXPECT().
		GetResponse(
			gomock.Any(),
			gomock.Any(),
			chunkResponseID2,
		).
		Return(chunkSummary2, true, nil).
		Times(1)

	// Mock for final request
	finalResponseID := "final"
	suite.mockComradeClient.EXPECT().
		RequestModel(
			gomock.Any(),
			gomock.Any(),
			gomock.Any(),
			gomock.Any(),
		).
		Do(func(_ any, _ any, systemPrompt string, userPrompt string) {
			finalSystemPrompt = systemPrompt
			finalUserPrompt = userPrompt
		}).
		Return(finalResponseID, nil).
		Times(1)

	// Mock for final response
	suite.mockComradeClient.EXPECT().
		GetResponse(
			gomock.Any(),
			gomock.Any(),
			finalResponseID,
		).
		Return(finalResponse, true, nil).
		Times(1)

	operation, err := client.NeuroDescribe(authCtx, &pb.NeuroDescribeRequest{
		PrId: grpc2.MarshalID(pr.ID),
	})
	require.NoError(t, err)
	require.NotNil(t, operation)

	suite.WaitForWorkflows(t, entities.WorkflowTypes.NeuroDescription)

	t.Run("validate chunk 1 prompts", func(t *testing.T) {
		chunk1UserPromptLines := strings.Split(strings.TrimSpace(chunk1UserPrompt), "\n")
		randStartTag := chunk1UserPromptLines[4]
		randEndTag := chunk1UserPromptLines[len(chunk1UserPromptLines)-1]

		require.Contains(t, chunk1UserPrompt, "file1.go")

		params := map[string]string{
			"UserDataStartTag": randStartTag,
			"UserDataEndTag":   randEndTag,
		}
		expectedSystemPromptBytes := bytes.Buffer{}
		err := neurodescription.ChunkSummarySystemPromptTemplate.Execute(&expectedSystemPromptBytes, params)
		require.NoError(t, err)
		expectedSystemPrompt := strings.TrimSpace(expectedSystemPromptBytes.String())

		require.Equal(t, expectedSystemPrompt, chunk1SystemPrompt)

		require.Contains(t, chunk1UserPrompt, "Проект использует следующие технологии...")
	})

	t.Run("validate chunk 2 prompts", func(t *testing.T) {
		chunk2UserPromptLines := strings.Split(strings.TrimSpace(chunk2UserPrompt), "\n")
		randStartTag := chunk2UserPromptLines[4]
		randEndTag := chunk2UserPromptLines[len(chunk2UserPromptLines)-1]

		require.Contains(t, chunk2UserPrompt, "file2.go")

		params := map[string]string{
			"UserDataStartTag": randStartTag,
			"UserDataEndTag":   randEndTag,
		}
		expectedSystemPromptBytes := bytes.Buffer{}
		err := neurodescription.ChunkSummarySystemPromptTemplate.Execute(&expectedSystemPromptBytes, params)
		require.NoError(t, err)
		expectedSystemPrompt := strings.TrimSpace(expectedSystemPromptBytes.String())

		require.Equal(t, expectedSystemPrompt, chunk2SystemPrompt)

		require.Contains(t, chunk2UserPrompt, "Проект использует следующие технологии...")
	})

	t.Run("validate final prompts", func(t *testing.T) {
		finalUserPromptLines := strings.Split(strings.TrimSpace(finalUserPrompt), "\n")
		randStartTag := finalUserPromptLines[4]
		randEndTag := finalUserPromptLines[len(finalUserPromptLines)-1]

		params := map[string]string{
			"UserDataStartTag": randStartTag,
			"UserDataEndTag":   randEndTag,
		}

		expectedSystemPromptBytes := bytes.Buffer{}
		err := neurodescription.FinalDescriptionSystemPromptTemplate.Execute(&expectedSystemPromptBytes, params)
		require.NoError(t, err)
		expectedSystemPrompt := strings.TrimSpace(expectedSystemPromptBytes.String())

		require.Equal(t, expectedSystemPrompt, finalSystemPrompt)

		expectedUserPrompt := "Проект использует следующие технологии...\n\n\n\n" +
			randStartTag + "\n\n" + chunkSummary1 + "\n\n" + chunkSummary2 + "\n\n" + randEndTag
		require.Equal(t, expectedUserPrompt, finalUserPrompt)
	})

	t.Run("validate PR description was updated with final summary", func(t *testing.T) {
		prRepo := suite.PullRequestRepoFactory.Build(repo.ID)
		updatedPR, err := prRepo.Get(context.Background(), pr.ID)
		require.NoError(t, err)
		require.Equal(t, finalResponse, updatedPR.Description)
	})

	t.Run("validate operation status is success", func(t *testing.T) {
		prRepo := suite.PullRequestRepoFactory.Build(repo.ID)
		updatedPR, err := prRepo.Get(context.Background(), pr.ID)
		require.NoError(t, err)

		require.NotNil(t, updatedPR.NeuroDescription.OperationID)

		operation, err := suite.OpService.Get(context.Background(), *updatedPR.NeuroDescription.OperationID)
		require.NoError(t, err)
		require.Equal(t, entities.OperationStatuses.Success, operation.Status)
	})
}

func (suite *MockComradeTestSuite) TestNeuroDescription_ErrorHandling() {
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
		git checkout -b neurodescription-test-branch
		
		# Модифицируем существующий файл (main.go)
		cat > main.go << 'EOF'
package main

import (
	"fmt"
	"os"
)

func main() {
	const my_constant = 42
	unused_var := "hello"
	
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
		Source: "neurodescription-test-branch",
		Target: "master",
		Title:  "Changes for neurodescription testing",
	})

	aiResponse := `# Улучшение функции main

- **Добавлена поддержка аргументов командной строки**: программа теперь принимает аргументы для персонализированного приветствия
`

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

		operation, err := client.NeuroDescribe(authCtx, &pb.NeuroDescribeRequest{
			PrId: grpc2.MarshalID(pr.ID),
		})
		require.NoError(t, err)
		require.NotNil(t, operation)

		// workflow succeeded
		suite.WaitForWorkflows(t, entities.WorkflowTypes.NeuroDescription)

		pr, err := client.Get(authCtx, &pb.GetPullRequestRequest{
			Identity: &pb.GetPullRequestRequest_Id{
				Id: grpc2.MarshalID(pr.ID),
			},
		})
		require.NoError(t, err)
		require.NotNil(t, pr)
		require.NotNil(t, pr.NeuroDescribeInfo.OperationId)
		require.Nil(t, pr.NeuroDescribeInfo.Status)
		require.Empty(t, pr.Description)

		// operation has failed status
		opClient := pb.NewOperationServiceClient(suite.grpcClient)
		op, err := opClient.Get(authCtx, &pb.GetOperationRequest{
			Id: *pr.NeuroDescribeInfo.OperationId,
		})
		require.NoError(t, err)
		require.NotNil(t, op)
		require.True(t, op.Done)
		require.NotNil(t, (*op).GetError())
		require.Equal(t, except.LLMRequestLimitExceeded.MessageTemplate.Template, (*op).GetError().Message)

		meta, err := op.Metadata.UnmarshalNew()
		require.NoError(t, err)

		yarequire.ProtoEqual(t, &pb.NeuroDescribeMetadata{
			UserId:    grpc2.MarshalID(suite.users.Kopatych.ID),
			Iteration: pr.Iteration,
			Status:    pb.OperationMetadata_FAILED,
		}, meta)
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
					return aiResponse, true, nil
				}
				return "", true, except.LLMRequestInvalid.Build()
			}).
			Times(2)

		operation, err := client.NeuroDescribe(authCtx, &pb.NeuroDescribeRequest{
			PrId: grpc2.MarshalID(pr.ID),
		})
		require.NoError(t, err)
		require.NotNil(t, operation)

		// workflow succeeded
		suite.WaitForWorkflows(t, entities.WorkflowTypes.NeuroDescription)

		// PR description updated
		pr, err := client.Get(authCtx, &pb.GetPullRequestRequest{
			Identity: &pb.GetPullRequestRequest_Id{
				Id: grpc2.MarshalID(pr.ID),
			},
		})
		require.NoError(t, err)
		require.NotNil(t, pr)
		require.NotNil(t, pr.NeuroDescribeInfo.OperationId)
		require.NotNil(t, pr.NeuroDescribeInfo.Status)
		require.Equal(t, pb.NeuroDescribeInfo_GENERATED, *(pr.NeuroDescribeInfo.Status))
		require.Equal(t, aiResponse, pr.Description)

		// operation has success status
		opClient := pb.NewOperationServiceClient(suite.grpcClient)
		op, err := opClient.Get(authCtx, &pb.GetOperationRequest{
			Id: *pr.NeuroDescribeInfo.OperationId,
		})
		require.NoError(t, err)
		require.NotNil(t, op)
		require.True(t, op.Done)
		require.Nil(t, (*op).GetError())

		meta, err := op.Metadata.UnmarshalNew()
		require.NoError(t, err)

		yarequire.ProtoEqual(t, &pb.NeuroDescribeMetadata{
			UserId:    grpc2.MarshalID(suite.users.Kopatych.ID),
			Iteration: pr.Iteration,
			Status:    pb.OperationMetadata_SUCCESS,
		}, meta)
	})

	suite.mustBash(repo, `
		git checkout master
		git checkout -b neurodescription-test-branch-2
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
		Source: "neurodescription-test-branch-2",
		Target: "master",
		Title:  "Changes for neurodescription testing",
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

		operation, err := client.NeuroDescribe(authCtx, &pb.NeuroDescribeRequest{
			PrId: grpc2.MarshalID(pr.ID),
		})
		require.NoError(t, err)
		require.NotNil(t, operation)

		// workflow failed
		s, ok := suite.Scheduler.(*scheduler.SchedulerService)
		require.True(t, ok)
		err = s.WaitWorkflowByType(context.Background(), entities.WorkflowTypes.NeuroDescription)
		require.Error(t, err)

		// PR description not updated
		pr, err := client.Get(authCtx, &pb.GetPullRequestRequest{
			Identity: &pb.GetPullRequestRequest_Id{
				Id: grpc2.MarshalID(pr.ID),
			},
		})
		require.NoError(t, err)
		require.NotNil(t, pr)
		require.NotNil(t, pr.NeuroDescribeInfo.OperationId)
		require.Nil(t, pr.NeuroDescribeInfo.Status)
		require.Empty(t, pr.Description)

		// operation has failed status
		opClient := pb.NewOperationServiceClient(suite.grpcClient)
		op, err := opClient.Get(authCtx, &pb.GetOperationRequest{
			Id: *pr.NeuroDescribeInfo.OperationId,
		})
		require.NoError(t, err)
		require.NotNil(t, op)
		require.True(t, op.Done)
		require.Nil(t, (*op).GetError())

		meta, err := op.Metadata.UnmarshalNew()
		require.NoError(t, err)

		yarequire.ProtoEqual(t, &pb.NeuroDescribeMetadata{
			UserId:    grpc2.MarshalID(suite.users.Kopatych.ID),
			Iteration: pr.Iteration,
			Status:    pb.OperationMetadata_FAILED,
		}, meta)
	})
}
