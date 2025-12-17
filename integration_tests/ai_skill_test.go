package integrationtests

import (
	"common/testutils/yarequire"
	"common/utils/rolesgenerator/iam"
	_ "embed"
	"gitcore/internal/entities"
	except "gitcore/internal/exceptions"
	grpc_marshalling "gitcore/internal/grpcserver/marshalling"
	"gitcore/internal/interfaces"
	"gitcore/internal/testutils"
	pb "private_api/generated/yandex/cloud/priv/gitcore/v1"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
)

func (suite *RwApiTestSuite) TestGRPCAISkills() {
	t := suite.T()

	// Create system skills repository
	systemRepo := suite.makeRepo(suite.users.Admin, &interfaces.CreateRepositoryArgs{
		OrgID:   suite.orgs.Yandex.ID,
		Name:    "system-skills",
		Slug:    "system-skills",
		IsEmpty: true,
	})

	systemRepo2 := suite.makeRepo(suite.users.Admin, &interfaces.CreateRepositoryArgs{
		OrgID:   suite.orgs.Yandex.ID,
		Name:    "system-skills2",
		Slug:    "system-skills2",
		IsEmpty: true,
	})

	systemRepo3 := suite.makeRepo(suite.users.Admin, &interfaces.CreateRepositoryArgs{
		OrgID:   suite.orgs.Yandex.ID,
		Name:    "system-skills3",
		Slug:    "system-skills3",
		IsEmpty: true,
	})

	privateRepoName := "ai-import-private-repo"
	// Create private repository without access for krosh
	privateRepo := suite.makeRepo(suite.users.Pikachu, &interfaces.CreateRepositoryArgs{
		OrgID:      suite.orgs.Pokemon.ID,
		Name:       privateRepoName,
		Slug:       privateRepoName,
		Visibility: entities.Visibilities.Private,
		IsEmpty:    true,
	})
	suite.addRole(t, suite.users.Admin, privateRepo, iam.Roles.Admin)

	// Configure it as system skills repo
	suite.cfg.AISkills.SystemSkillsRepoID = systemRepo.ID

	aiSystemOrg := suite.orgs.Yandex.Slug
	aiPrivateUserOrg := suite.orgs.Pokemon.Slug

	// Push skills and settings to system repo
	suite.mustBash(systemRepo2, `
git checkout -b main
mkdir -p .sourcecraft/ai/skills
mkdir -p .sourcecraft/ai/skill-settings

# Skill: code-review (with choice, string, bool inputs)
cat > .sourcecraft/ai/skills/code-review.yaml << 'EOF'
name: Code Review
description: AI-powered code review assistant
instructions: |
  Review the code for quality and best practices.
inputs:
  language:
    type: choice
    description: Programming language
    options:
      - go
      - python
      - java
    default: python
  style:
    type: string 
    description: Code style guide
    default: standard
  strict:
    type: bool
    description: Enable strict mode
    default: false
EOF

cat > .sourcecraft/ai/skills/recursive-not-allowed.yaml << 'EOF'
import: `+aiSystemOrg+`/system-skills3
EOF

cat > .sourcecraft/ai/skills/code-review5.yaml << 'EOF'
name: Code Review
description: AI-powered code review assistant
instructions: |
  Review the code for quality and best practices.
inputs:
  language:
    type: choice
    description: Programming language
    options:
      - go
      - python
      - java
    default: python
  style:
    type: string 
    description: Code style guide
    default: standard
  strict:
    type: bool
    description: Enable strict mode
    default: false
EOF

		# Skill: documentation (no inputs)
		cat > .sourcecraft/ai/skills/documentation.yaml << 'EOF'
name: Documentation Generator
description: Generate documentation
instructions: |
  Generate comprehensive documentation.
EOF

		# Settings for code-review (override language, add instructions)
		cat > .sourcecraft/ai/skill-settings/code-review.yaml << 'EOF'
inputs:
  language: go
instructions: |
  System-level review rules apply.
EOF

git add .sourcecraft
git commit -m "Add AI skills"
	`)

	suite.mustBash(systemRepo3, `
git checkout -b main

mkdir -p .sourcecraft/ai/skills
mkdir -p .sourcecraft/ai/skill-settings
		
cat > .sourcecraft/ai/skills/recursive-not-allowed.yaml << 'EOF'
name: Code Review
description: AI-powered code review assistant
instructions: |
  Review the code for quality and best practices.
inputs:
  language:
    type: choice
    description: Programming language
    options:
      - go
      - python
      - java
    default: python
  style:
    type: string 
    description: Code style guide
    default: standard
  strict:
    type: bool
    description: Enable strict mode
    default: false
EOF

git add .sourcecraft
git commit -m "Add AI skills"
		`)

	// Push skills and settings to system repo
	suite.mustBash(systemRepo, `
		git checkout -b main

		mkdir -p .sourcecraft/ai/skills
		mkdir -p .sourcecraft/ai/skill-settings

		# Skill: code-review (with choice, string, bool inputs)
		cat > .sourcecraft/ai/skills/code-review.yaml << 'EOF'
name: Code Review
description: AI-powered code review assistant
instructions: |
  Review the code for quality and best practices.
inputs:
  language:
    type: choice
    description: Programming language
    options:
      - go
      - python
      - java
    default: python
  style:
    type: string
    description: Code style guide
    default: standard
  strict:
    type: bool
    description: Enable strict mode
    default: false
EOF

		# Skill: bug-fix (with choice input)
		cat > .sourcecraft/ai/skills/bug-fix.yaml << 'EOF'
name: Bug Fix Assistant
description: Help fix bugs in code
instructions: |
  Analyze the bug and suggest fixes.
inputs:
  severity:
    type: choice
    description: Bug severity level
    options:
      - low
      - medium
      - high
      - critical
    default: medium
EOF

		# Skill: documentation (no inputs)
		cat > .sourcecraft/ai/skills/documentation.yaml << 'EOF'
name: Documentation Generator
description: Generate documentation
instructions: |
  Generate comprehensive documentation.
EOF

		# Settings for code-review (override language, add instructions)
		cat > .sourcecraft/ai/skill-settings/code-review.yaml << 'EOF'
inputs:
  language: go
instructions: |
  System-level review rules apply.
EOF

		# Settings for bug-fix (add instructions)
		cat > .sourcecraft/ai/skill-settings/bug-fix.yaml << 'EOF'
instructions: |
  System bug fix guidelines.
EOF

# import skill not exist 
cat > .sourcecraft/ai/skills/skill_not_exist.yaml << 'EOF'
import: `+aiSystemOrg+`/system-skills2
EOF
# import skill for not exist repo
cat > .sourcecraft/ai/skills/not-exist-repo.yaml << 'EOF'
import: `+aiSystemOrg+`/not-exist-repo
EOF
# import skill - recursive not allowed
cat > .sourcecraft/ai/skills/recursive-not-allowed.yaml << 'EOF'
import: `+aiSystemOrg+`/system-skills2
EOF
# Settings for code-review3 should be used from local repo
cat > .sourcecraft/ai/skill-settings/code-review3.yaml << 'EOF'
inputs:
  language: go
instructions: |
  System-level review rules apply.
EOF

# skill name must be code-review5
cat > .sourcecraft/ai/skills/code-review5.yaml << 'EOF'
import: `+aiSystemOrg+`/system-skills2
EOF
# Settings for code-review5 should be used from local repo
cat > .sourcecraft/ai/skill-settings/code-review5.yaml << 'EOF'
inputs:
  language: go
instructions: |
  System-level review rules apply.
EOF

		git add .sourcecraft
		git commit -m "Add AI skills"
	`)

	// Create user repository 1 with custom skill and duplicate
	userRepo1 := suite.makeRepo(suite.users.Krosh, &interfaces.CreateRepositoryArgs{
		OrgID:   suite.orgs.Yandex.ID,
		Name:    "user-repo-1",
		Slug:    "user-repo-1",
		IsEmpty: true,
	})

	suite.mustBash(userRepo1, `
		git checkout -b main

		mkdir -p .sourcecraft/ai/skills
		mkdir -p .sourcecraft/ai/skill-settings

		# Custom skill unique to user repo
		cat > .sourcecraft/ai/skills/custom-skill.yaml << 'EOF'
name: Custom Skill
description: User-defined custom skill
instructions: |
  Custom instructions for this skill.
inputs:
  format:
    type: string
    description: Output format
    default: text
EOF

		# Duplicate skill (should be ignored, only definition matters, not content)
		cat > .sourcecraft/ai/skills/code-review.yaml << 'EOF'
name: User Code Review
description: This should be ignored
instructions: |
  This skill definition should be ignored.
inputs:
  fake:
    type: string
    description: Fake input
EOF

		# Settings for code-review (override style, add instructions)
		cat > .sourcecraft/ai/skill-settings/code-review.yaml << 'EOF'
inputs:
  style: strict
instructions: |
  User-level review customizations.
EOF

		# Settings for custom-skill
		cat > .sourcecraft/ai/skill-settings/custom-skill.yaml << 'EOF'
inputs:
  format: markdown
instructions: |
  User custom instructions.
EOF


# private-skill in private-repo not listed
cat > .sourcecraft/ai/skill-settings/private-skill.yaml << 'EOF'
import: `+aiPrivateUserOrg+`/`+privateRepoName+`
EOF


# Skill userRepo2
cat > .sourcecraft/ai/skills/userRepo2.yaml << 'EOF'
import: `+suite.orgs.Yandex.Slug+`/user-repo-2
EOF

		git add .sourcecraft
		git commit -m "Add user AI skills"
	`)

	// Create user repository 2 with invalid settings
	userRepo2 := suite.makeRepo(suite.users.Krosh, &interfaces.CreateRepositoryArgs{
		OrgID:   suite.orgs.Yandex.ID,
		Name:    "user-repo-2",
		Slug:    "user-repo-2",
		IsEmpty: true,
	})

	suite.mustBash(userRepo2, `
		git checkout -b main

		mkdir -p .sourcecraft/ai/skill-settings

		# Settings with invalid values (wrong types, invalid choices)
		cat > .sourcecraft/ai/skill-settings/code-review.yaml << 'EOF'
inputs:
  language: rust        # invalid choice (not in options)
  style: valid_string   # valid string
  strict: not_a_bool    # invalid bool value
instructions: |
  Invalid settings test.
EOF

# Skill userRepo2
cat > .sourcecraft/ai/skills/userRepo2.yaml << 'EOF'
name: userRepo2 Skill
description: Should be imported
instructions: |
  imported instructions.
EOF

		git add .sourcecraft
		git commit -m "Add invalid settings"
	`)

	suite.mustBash(privateRepo, `
		git checkout -b main

		mkdir -p .sourcecraft/ai/skills

		cat > .sourcecraft/ai/skills/private-skill.yaml << 'EOF'
name: Private Skill
description: Should not be accessible
instructions: |
  Private instructions.
EOF

cat > .sourcecraft/ai/skills/private-skill2.yaml << 'EOF'
name: Private Skill2
description: Should not be accessible
instructions: |
  Private instructions.
EOF

		git add .sourcecraft
		git commit -m "Add private skill"
	`)

	client := pb.NewAISkillServiceClient(suite.grpcClient)
	kroshCtx := testutils.AuthorizeGRPC(suite.users.Krosh.Identity)

	t.Run("list without context - only system skills", func(t *testing.T) {
		resp, err := client.ListSkills(kroshCtx, &pb.ListAISkillsRequest{})
		require.NoError(t, err)

		// yarequire.ProtoDumpFixture(t, resp)
		yarequire.ProtoCompareWithFixture(t, resp)
	})

	t.Run("list with repo context - system and repo skills", func(t *testing.T) {
		resp, err := client.ListSkills(kroshCtx, &pb.ListAISkillsRequest{
			Context: &pb.AISkillContext{
				Identifier: &pb.AISkillContext_RepoId{
					RepoId: grpc_marshalling.IDInverse(userRepo1.ID),
				},
			},
		})
		require.NoError(t, err)

		// yarequire.ProtoDumpFixture(t, resp)
		yarequire.ProtoCompareWithFixture(t, resp)
	})

	t.Run("invalid settings ignored", func(t *testing.T) {
		resp, err := client.ListSkills(kroshCtx, &pb.ListAISkillsRequest{
			Context: &pb.AISkillContext{
				Identifier: &pb.AISkillContext_RepoId{
					RepoId: grpc_marshalling.IDInverse(userRepo2.ID),
				},
			},
		})
		require.NoError(t, err)

		// yarequire.ProtoDumpFixture(t, resp)
		yarequire.ProtoCompareWithFixture(t, resp)
	})

	t.Run("permission denied for inaccessible repo context", func(t *testing.T) {
		_, err := client.ListSkills(kroshCtx, &pb.ListAISkillsRequest{
			Context: &pb.AISkillContext{
				Identifier: &pb.AISkillContext_RepoId{
					RepoId: grpc_marshalling.IDInverse(privateRepo.ID),
				},
			},
		})

		yarequire.ProtoStatusEqual(t, codes.PermissionDenied, err)
	})

	t.Run("render instructions", func(t *testing.T) {
		resp, err := client.RenderInstructions(kroshCtx, &pb.RenderAISkillInstructionsRequest{
			Slug: "bug-fix",
			Inputs: []*pb.RenderAISkillInstructionsRequest_Input{
				{Name: "severity", Value: "high"},
			},
		})
		require.NoError(t, err)

		assert.Equal(t, "Analyze the bug and suggest fixes.\n\nSystem bug fix guidelines.\n", resp.Instructions)
	})

	t.Run("render instructions - skill not found", func(t *testing.T) {
		_, err := client.RenderInstructions(kroshCtx, &pb.RenderAISkillInstructionsRequest{
			Slug: "non-existent-skill",
		})

		yarequire.ProtoStatusEqual(t, codes.NotFound, err)
	})

	t.Run("render instructions with repo context", func(t *testing.T) {
		resp, err := client.RenderInstructions(kroshCtx, &pb.RenderAISkillInstructionsRequest{
			Slug: "custom-skill",
			Context: &pb.AISkillContext{
				Identifier: &pb.AISkillContext_RepoId{
					RepoId: grpc_marshalling.IDInverse(userRepo1.ID),
				},
			},
			Inputs: []*pb.RenderAISkillInstructionsRequest_Input{
				{Name: "format", Value: "json"},
			},
		})
		require.NoError(t, err)

		assert.Equal(t, "Custom instructions for this skill.\n\nUser custom instructions.\n", resp.Instructions)
	})

	t.Run("no skills in repo context", func(t *testing.T) {
		emptyRepo := suite.makeRepo(suite.users.Krosh, &interfaces.CreateRepositoryArgs{
			OrgID:   suite.orgs.Yandex.ID,
			Name:    "empty-repo",
			Slug:    "empty-repo",
			IsEmpty: true,
		})

		resp, err := client.ListSkills(kroshCtx, &pb.ListAISkillsRequest{
			Context: &pb.AISkillContext{
				Identifier: &pb.AISkillContext_RepoId{
					RepoId: grpc_marshalling.IDInverse(emptyRepo.ID),
				},
			},
		})
		require.NoError(t, err)

		require.Len(t, resp.Skills, 4)
	})
}

func (suite *RwApiTestSuite) TestGRPCAISkills_Templating() {
	t := suite.T()

	// Create system skills repository
	systemRepo := suite.makeRepo(suite.users.Admin, &interfaces.CreateRepositoryArgs{
		OrgID:   suite.orgs.Yandex.ID,
		Name:    "system-skills",
		Slug:    "system-skills",
		IsEmpty: true,
	})

	// Configure it as system skills repo
	suite.cfg.AISkills.SystemSkillsRepoID = systemRepo.ID

	// Push skills and settings to system repo
	suite.mustBash(systemRepo, `
		git checkout -b main

		mkdir -p .sourcecraft/ai/skills
		mkdir -p .sourcecraft/ai/skill-settings

		# Skill: code-review (with choice, string, bool inputs)
		cat > .sourcecraft/ai/skills/code-review.yaml << 'EOF'
name: Code Review
description: AI-powered code review assistant
instructions: |
  Review the code for quality and best practices.
  {{ instructions }}
  Some text here.
  {% if smth %}
  Smth is true.
  {% else %}
  Smth is false.
  {% endif %}
  Strict is {{ strict }}.
  Style is 'formal' - {{ style == 'formal' }}
inputs:
  language:
    required: true
    type: choice
    description: Programming language
    options:
      - go
      - python
      - java
  style:
    type: string
    description: Code style guide
  strict:
    required: true
    type: bool
    description: Enable strict mode
    default: false
  smth:
    type: bool
EOF

		git add .sourcecraft
		git commit -m "Add AI skills"
	`)

	// Create user repository with extra instructions
	userRepo1 := suite.makeRepo(suite.users.Krosh, &interfaces.CreateRepositoryArgs{
		OrgID:   suite.orgs.Yandex.ID,
		Name:    "user-repo-1",
		Slug:    "user-repo-1",
		IsEmpty: true,
	})

	suite.mustBash(userRepo1, `
		git checkout -b main

		mkdir -p .sourcecraft/ai/skill-settings

		# Settings for code-review
		cat > .sourcecraft/ai/skill-settings/code-review.yaml << 'EOF'
instructions: |
  User-level review customizations {{ language }}.
EOF
		git add .sourcecraft
		git commit -m "commit"
	`)

	// Create another user repository with invalid template
	userRepo2 := suite.makeRepo(suite.users.Krosh, &interfaces.CreateRepositoryArgs{
		OrgID:   suite.orgs.Yandex.ID,
		Name:    "user-repo-2",
		Slug:    "user-repo-2",
		IsEmpty: true,
	})

	suite.mustBash(userRepo2, `
		git checkout -b main

		mkdir -p .sourcecraft/ai/skill-settings

		# Settings for code-review
		cat > .sourcecraft/ai/skill-settings/code-review.yaml << 'EOF'
instructions: |
  Invalid template: {{ language }.
EOF

# Skill userRepo2
cat > .sourcecraft/ai/skills/userRepo2.yaml << 'EOF'
name: userRepo2 Skill
description: Should be imported
instructions: |
  imported instructions.
EOF

		git add .sourcecraft
		git commit -m "commit"
	`)

	client := pb.NewAISkillServiceClient(suite.grpcClient)
	kroshCtx := testutils.AuthorizeGRPC(suite.users.Krosh.Identity)

	t.Run("substituted instructions parameter", func(t *testing.T) {
		resp, err := client.ListSkills(kroshCtx, &pb.ListAISkillsRequest{
			Context: &pb.AISkillContext{
				Identifier: &pb.AISkillContext_RepoId{
					RepoId: grpc_marshalling.IDInverse(userRepo1.ID),
				},
			},
		})
		require.NoError(t, err)

		require.Len(t, resp.Skills, 1)
		require.Equal(t, "Code Review", resp.Skills[0].Name)
		require.Equal(t, `Review the code for quality and best practices.
User-level review customizations {{ language }}.

Some text here.
{% if smth %}
Smth is true.
{% else %}
Smth is false.
{% endif %}
Strict is {{ strict }}.
Style is 'formal' - {{ style == 'formal' }}
`, resp.Skills[0].InstructionsTemplate)
	})

	t.Run("render instructions - missing required input", func(t *testing.T) {
		_, err := client.RenderInstructions(kroshCtx, &pb.RenderAISkillInstructionsRequest{
			Slug: "code-review",
			Context: &pb.AISkillContext{
				Identifier: &pb.AISkillContext_RepoId{
					RepoId: grpc_marshalling.IDInverse(userRepo1.ID),
				},
			},
		})
		yarequire.ProtoStatusEqual(t, codes.InvalidArgument, err)
		yarequire.ProtoExceptionTemplate(t, err, except.AISkillInputRequired, "language")
	})

	t.Run("render instructions - invalid input value", func(t *testing.T) {
		// invalid choice
		_, err := client.RenderInstructions(kroshCtx, &pb.RenderAISkillInstructionsRequest{
			Slug: "code-review",
			Inputs: []*pb.RenderAISkillInstructionsRequest_Input{
				{Name: "language", Value: "rust"},
			},
			Context: &pb.AISkillContext{
				Identifier: &pb.AISkillContext_RepoId{
					RepoId: grpc_marshalling.IDInverse(userRepo1.ID),
				},
			},
		})
		yarequire.ProtoStatusEqual(t, codes.InvalidArgument, err)
		yarequire.ProtoExceptionTemplate(t, err, except.AISkillInputInvalid, "language", "expected one of go, python, java, got rust")

		// invalid bool
		_, err = client.RenderInstructions(kroshCtx, &pb.RenderAISkillInstructionsRequest{
			Slug: "code-review",
			Inputs: []*pb.RenderAISkillInstructionsRequest_Input{
				{Name: "language", Value: "python"},
				{Name: "strict", Value: "not_a_bool"},
			},
			Context: &pb.AISkillContext{
				Identifier: &pb.AISkillContext_RepoId{
					RepoId: grpc_marshalling.IDInverse(userRepo1.ID),
				},
			},
		})
		yarequire.ProtoStatusEqual(t, codes.InvalidArgument, err)
		yarequire.ProtoExceptionTemplate(t, err, except.AISkillInputInvalid, "strict", "expected boolean value, got not_a_bool")
	})

	t.Run("render - ok", func(t *testing.T) {
		resp, err := client.RenderInstructions(kroshCtx, &pb.RenderAISkillInstructionsRequest{
			Slug: "code-review",
			Inputs: []*pb.RenderAISkillInstructionsRequest_Input{
				{Name: "language", Value: "python"},
				{Name: "style", Value: "formal"},
			},
			Context: &pb.AISkillContext{
				Identifier: &pb.AISkillContext_RepoId{
					RepoId: grpc_marshalling.IDInverse(userRepo1.ID),
				},
			},
		})
		require.NoError(t, err)

		assert.Equal(t, `Review the code for quality and best practices.
User-level review customizations python.

Some text here.

Smth is false.

Strict is false.
Style is 'formal' - true
`, resp.Instructions)
	})

	t.Run("render instructions - invalid template", func(t *testing.T) {
		_, err := client.RenderInstructions(kroshCtx, &pb.RenderAISkillInstructionsRequest{
			Slug: "code-review",
			Inputs: []*pb.RenderAISkillInstructionsRequest_Input{
				{Name: "language", Value: "python"},
			},
			Context: &pb.AISkillContext{
				Identifier: &pb.AISkillContext_RepoId{
					RepoId: grpc_marshalling.IDInverse(userRepo2.ID),
				},
			},
		})
		yarequire.ProtoStatusEqual(t, codes.InvalidArgument, err)
		yarequire.ProtoExceptionTemplate(t, err, except.TemplateSyntaxError, "line 2:30; line 4:14; line 5:0; line 5:1; line 5:11")
	})
}
