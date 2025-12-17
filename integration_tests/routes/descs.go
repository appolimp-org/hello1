package routes

import (
	"common/utils"
	"common/utils/rolesgenerator/iam"
	"fmt"
	"github.com/cockroachdb/errors"
	"github.com/fatih/color"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v2"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"sync"
	"testing"
)

const goldenFileName = "routes.golden.yaml"

type AppDesc struct {
	Services []ServiceInfoDesc `yaml:"services"`
}

type ServiceInfoDesc struct {
	Name    string           `yaml:"name"`
	Methods []MethodInfoDesc `yaml:"methods"`
}

type MethodInfoDesc struct {
	Name            string           `yaml:"name"`
	Permissions     []iam.Permission `yaml:"permissions"`
	HasAuthUnitTest bool             `yaml:"has_auth_unit_test"`
}

func GoldenFileName() string {
	return filepath.Join(utils.SrcDir(), goldenFileName)
}

var globalGoldenMu sync.Mutex

func LoadGolden(t *testing.T) AppDesc {
	gldAppDesc := AppDesc{}
	bytesContent, err := os.ReadFile(GoldenFileName())
	if !errors.Is(err, os.ErrNotExist) {
		require.NoError(t, err)
		yaml.Unmarshal(bytesContent, &gldAppDesc)
	}

	// sanity check
	var corrupt bool
	serviceNames := make(map[string]struct{})
	for _, svc := range gldAppDesc.Services {
		if _, ok := serviceNames[svc.Name]; ok {
			color.NoColor = false
			fmt.Print(color.RedString("[>] service section %s is recorded more than once\n", svc.Name))
			corrupt = true
		}
		serviceNames[svc.Name] = struct{}{}
		methodNames := make(map[string]struct{})
		for _, method := range svc.Methods {
			if _, ok := methodNames[method.Name]; ok {
				color.NoColor = false
				fmt.Print(color.RedString("[>] method %s.%s is recorded more than once\n", svc.Name, method.Name))
				corrupt = true
			}
			methodNames[method.Name] = struct{}{}
		}
	}
	if corrupt {
		t.Errorf("\n\ngolden file is corrupt. see messages above")
	}
	gldAppDesc.sort()
	return gldAppDesc
}

func AddMethodToGolden(t *testing.T, methodName string) {
	globalGoldenMu.Lock()
	defer globalGoldenMu.Unlock()

	desc := LoadGolden(t)
	parts := strings.SplitN(methodName, "/", 3)
	require.Equal(t, 3, len(parts))
	serviceName := parts[1]
	shortMethodName := parts[2]
	for serviceIdx := 0; serviceIdx <= len(desc.Services); serviceIdx++ {
		if serviceIdx != len(desc.Services) && serviceName > desc.Services[serviceIdx].Name {
			continue
		}
		if serviceIdx == len(desc.Services) || serviceName != desc.Services[serviceIdx].Name {
			service := ServiceInfoDesc{
				Name: serviceName,
			}
			method := MethodInfoDesc{
				Name:            shortMethodName,
				HasAuthUnitTest: true,
			}
			service.Methods = append(service.Methods, method)
			desc.Services = slices.Insert(desc.Services, serviceIdx, service)
			desc.Save(t)
			return
		}
		for methodIdx := 0; methodIdx <= len(desc.Services[serviceIdx].Methods); methodIdx++ {
			if methodIdx != len(desc.Services[serviceIdx].Methods) &&
				shortMethodName > desc.Services[serviceIdx].Methods[methodIdx].Name {
				continue
			}
			if methodIdx == len(desc.Services[serviceIdx].Methods) ||
				shortMethodName != desc.Services[serviceIdx].Methods[methodIdx].Name {
				method := MethodInfoDesc{
					Name:            shortMethodName,
					HasAuthUnitTest: true,
				}
				desc.Services[serviceIdx].Methods = slices.Insert(desc.Services[serviceIdx].Methods, methodIdx, method)
				desc.Save(t)
				return
			}
			if !desc.Services[serviceIdx].Methods[methodIdx].HasAuthUnitTest {
				desc.Services[serviceIdx].Methods[methodIdx].HasAuthUnitTest = true
				desc.Save(t)
				return
			}
			return
		}
	}

}

func AddPermToGolden(t *testing.T, methodName string, perm iam.Permission) {
	AddMethodToGolden(t, methodName)

	globalGoldenMu.Lock()
	defer globalGoldenMu.Unlock()

	desc := LoadGolden(t)
	parts := strings.SplitN(methodName, "/", 3)
	require.Equal(t, 3, len(parts))
	serviceName := parts[1]
	shortMethodName := parts[2]

	for serviceIdx := 0; serviceIdx <= len(desc.Services); serviceIdx++ {
		if serviceIdx != len(desc.Services) && serviceName > desc.Services[serviceIdx].Name {
			continue
		}
		require.Equal(t, serviceName, desc.Services[serviceIdx].Name)
		for methodIdx := 0; methodIdx <= len(desc.Services[serviceIdx].Methods); methodIdx++ {
			if methodIdx != len(desc.Services[serviceIdx].Methods) &&
				shortMethodName > desc.Services[serviceIdx].Methods[methodIdx].Name {
				continue
			}
			require.Equal(t, shortMethodName, desc.Services[serviceIdx].Methods[methodIdx].Name)
			permIdx, ok := slices.BinarySearch(desc.Services[serviceIdx].Methods[methodIdx].Permissions, perm)
			if !ok {
				desc.Services[serviceIdx].Methods[methodIdx].Permissions = slices.Insert(desc.Services[serviceIdx].Methods[methodIdx].Permissions, permIdx, perm)
				desc.Save(t)
				return
			}
			return
		}
		return
	}
}

func (desc *AppDesc) sort() {
	sort.Slice(desc.Services, func(i, j int) bool {
		return desc.Services[i].Name < desc.Services[j].Name
	})
	for serviceIdx := 0; serviceIdx < len(desc.Services); serviceIdx++ {
		sort.Slice(desc.Services[serviceIdx].Methods, func(i, j int) bool {
			return desc.Services[serviceIdx].Methods[i].Name < desc.Services[serviceIdx].Methods[j].Name
		})
		for methodIdx := 0; methodIdx < len(desc.Services[serviceIdx].Methods); methodIdx++ {
			sort.Slice(desc.Services[serviceIdx].Methods[methodIdx].Permissions, func(i, j int) bool {
				return desc.Services[serviceIdx].Methods[methodIdx].Permissions[i] < desc.Services[serviceIdx].Methods[methodIdx].Permissions[j]
			})
		}
	}
}

func (desc *AppDesc) Save(t *testing.T) {
	desc.sort()
	bin, err := yaml.Marshal(desc)
	require.NoError(t, err)
	err = os.WriteFile(GoldenFileName(), bin, os.ModePerm)
	require.NoError(t, err)
}
