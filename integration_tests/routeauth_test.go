package integrationtests

import (
	"common/testutils/update"
	"context"
	"fmt"
	"gitcore/internal/integration_tests/routes"
	"slices"
	"sort"
	"strings"

	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/assert"
	"google.golang.org/grpc"
)

func (suite *RoutesTestSuite) TestAllRoutes() {
	allGrpcMethods := suite.AllGrpcMethods()
	allHTTPMethods := suite.AllHTTPMethods()

	curAppDesc := routes.AppDesc{}

	curAppDesc.Services = append(curAppDesc.Services, routes.ServiceInfoDesc{Name: "HTTP"})
	for _, method := range allHTTPMethods {
		curAppDesc.Services[len(curAppDesc.Services)-1].Methods =
			append(curAppDesc.Services[len(curAppDesc.Services)-1].Methods, routes.MethodInfoDesc{
				Name: method.Path,
			})
	}

	for _, method := range allGrpcMethods {
		if curAppDesc.Services == nil || curAppDesc.Services[len(curAppDesc.Services)-1].Name != method.serviceName {
			curAppDesc.Services = append(curAppDesc.Services, routes.ServiceInfoDesc{Name: method.serviceName})
		}
		curAppDesc.Services[len(curAppDesc.Services)-1].Methods =
			append(curAppDesc.Services[len(curAppDesc.Services)-1].Methods, routes.MethodInfoDesc{
				Name: method.methodName,
			})
	}

	gldAppDesc := routes.LoadGolden(suite.T())

	var absentServices []string
	var absentMethods []string
	var unwantedServices []string
	var unwantedMethdos []string

	curSerIdx, gldSerIdx := 0, -1
	for ; curSerIdx < len(curAppDesc.Services); curSerIdx++ {
		gldSerIdx++

		for ; gldSerIdx < len(gldAppDesc.Services); gldSerIdx++ {
			if curAppDesc.Services[curSerIdx].Name <= gldAppDesc.Services[gldSerIdx].Name {
				break
			}
			unwantedServices = append(unwantedServices, gldAppDesc.Services[gldSerIdx].Name)
		}

		if len(gldAppDesc.Services) == gldSerIdx {
			absentServices = append(absentServices, curAppDesc.Services[curSerIdx].Name)
			gldAppDesc.Services = append(gldAppDesc.Services, curAppDesc.Services[curSerIdx])
			continue
		}

		if curAppDesc.Services[curSerIdx].Name != gldAppDesc.Services[gldSerIdx].Name {
			absentServices = append(absentServices, curAppDesc.Services[curSerIdx].Name)
			gldAppDesc.Services = slices.Insert(gldAppDesc.Services, gldSerIdx, curAppDesc.Services[curSerIdx])
			continue
		}

		curService := curAppDesc.Services[curSerIdx]
		gldService := &gldAppDesc.Services[gldSerIdx]
		curMtdIdx, gldMtdIdx := 0, -1
		for ; curMtdIdx < len(curService.Methods); curMtdIdx++ {
			gldMtdIdx++
			for gldMtdIdx < len(gldService.Methods) {
				if curService.Methods[curMtdIdx].Name <= gldService.Methods[gldMtdIdx].Name {
					break
				}
				unwantedServices = append(unwantedServices, gldService.Methods[gldMtdIdx].Name)
				gldService.Methods = slices.Delete(gldService.Methods, gldMtdIdx, gldMtdIdx+1)
			}

			if len(gldService.Methods) == gldMtdIdx {
				absentMethods = append(absentMethods, curService.Methods[curMtdIdx].Name)
				gldService.Methods = append(gldService.Methods, curService.Methods[curMtdIdx])
				continue
			}

			if curService.Methods[curMtdIdx].Name != gldService.Methods[gldMtdIdx].Name {
				absentMethods = append(absentMethods, curService.Methods[curMtdIdx].Name)
				gldService.Methods = slices.Insert(gldService.Methods, gldMtdIdx, curService.Methods[curMtdIdx])
				continue
			}

			slices.Sort(gldService.Methods[gldMtdIdx].Permissions)
		}
	}

	if update.IsUpdate() {
		gldAppDesc.Save(suite.T())
		return
	}

	hint := fmt.Sprintf("\nPlease, update golden file by running %s with --update flag (or pass GOLDENFILE=1)", suite.T().Name())

	assert.Empty(suite.T(),
		absentServices,
		"Some services ("+strings.Join(absentServices, ",")+") are not present in '"+routes.GoldenFileName()+"'. "+hint,
	)
	assert.Empty(suite.T(),
		unwantedServices,
		"Some services ("+strings.Join(unwantedServices, ",")+") are present '"+routes.GoldenFileName()+"', but not present in code. "+hint,
	)
	assert.Empty(suite.T(),
		absentMethods,
		"Some methods ("+strings.Join(absentMethods, ",")+") are not present in '"+routes.GoldenFileName()+"'. "+hint,
	)
	assert.Empty(suite.T(),
		unwantedMethdos,
		"Some methods ("+strings.Join(unwantedMethdos, ",")+") are present in '"+routes.GoldenFileName()+"', but not present in code. "+hint,
	)

	prevServiceName := ""
	for _, s := range gldAppDesc.Services {
		if prevServiceName != "" && s.Name == prevServiceName {
			assert.True(suite.T(), false, "Some services has duplicate: "+prevServiceName)
		}
		prevServiceName = s.Name
		prevMethodName := ""
		for _, m := range s.Methods {
			if prevMethodName != "" && m.Name == prevMethodName {
				assert.True(suite.T(), false, "Some methods has duplicate: "+prevServiceName+" "+prevMethodName)
			}
			prevMethodName = m.Name
			prevPerm := ""
			for _, p := range m.Permissions {
				if prevPerm != "" && string(p) == prevPerm {
					assert.True(suite.T(), false, "Some perms has duplicate: "+prevServiceName+" "+prevMethodName+" "+prevPerm)
				}
				prevPerm = string(p)
			}
		}
	}
}

func (suite *RoutesTestSuite) AllGrpcMethods() []*grpcMethodProcessorData {
	allMethodsVec := make([]*grpcMethodProcessorData, 0)
	for i, desc := range suite.descs {
		for _, method := range desc.Methods {
			data := grpcMethodProcessorData{
				serviceName: desc.ServiceName,
				methodName:  method.MethodName,
				descHandler: grpcMethodHandler(method.Handler),
				service:     suite.services[i],
			}
			allMethodsVec = append(allMethodsVec, &data)
		}
	}

	sort.Slice(allMethodsVec, func(i int, j int) bool {
		if allMethodsVec[i].serviceName < allMethodsVec[j].serviceName {
			return true
		}
		if allMethodsVec[i].serviceName > allMethodsVec[j].serviceName {
			return false
		}
		return allMethodsVec[i].methodName < allMethodsVec[j].methodName
	})
	return allMethodsVec
}

func (suite *RoutesTestSuite) AllHTTPMethods() []*echo.Route {
	var routes []*echo.Route
	for _, s := range suite.httpServers {
		routes = append(routes, s.Routes()...)
	}
	m := make(map[string]struct{})
	var uniqRoutes []*echo.Route
	for _, r := range routes {
		if _, ok := m[r.Path]; ok {
			continue
		}
		uniqRoutes = append(uniqRoutes, r)
		m[r.Path] = struct{}{}
	}
	sort.Slice(uniqRoutes, func(i int, j int) bool {
		return uniqRoutes[i].Path < uniqRoutes[j].Path
	})
	return uniqRoutes
}

type grpcMethodProcessorData struct {
	serviceName string
	methodName  string
	descHandler grpcMethodHandler
	service     any
}

type grpcMethodHandler func(srv any, ctx context.Context, dec func(any) error, interceptor grpc.UnaryServerInterceptor) (any, error)
