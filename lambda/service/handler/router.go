package handler

import (
	"net/http"

	"github.com/aws/aws-lambda-go/events"
	"github.com/pennsieve/account-service/service/utils"
)

type RouterHandlerFunc func(events.APIGatewayV2HTTPRequest) (events.APIGatewayV2HTTPResponse, error)

// Defines the router interface
type Router interface {
	POST(string, RouterHandlerFunc)
	GET(string, RouterHandlerFunc)
	Start(events.APIGatewayV2HTTPRequest) (events.APIGatewayV2HTTPResponse, error)
}

type LambdaRouter struct {
	getRoutes  map[string]RouterHandlerFunc
	postRoutes map[string]RouterHandlerFunc
}

func NewLambdaRouter() Router {
	return &LambdaRouter{
		make(map[string]RouterHandlerFunc),
		make(map[string]RouterHandlerFunc),
	}
}

func (r *LambdaRouter) POST(routeKey string, handler RouterHandlerFunc) {
	r.postRoutes[routeKey] = handler
}

func (r *LambdaRouter) GET(routeKey string, handler RouterHandlerFunc) {
	r.getRoutes[routeKey] = handler
}

func (r *LambdaRouter) Start(request events.APIGatewayV2HTTPRequest) (events.APIGatewayV2HTTPResponse, error) {
	routeKey := utils.ExtractRoute(request.RouteKey)

	switch request.RequestContext.HTTP.Method {
	case http.MethodPost:
		f, ok := r.postRoutes[routeKey]
		if ok {
			return f(request)
		} else {
			return handleError()
		}
	case http.MethodGet:
		f, ok := r.getRoutes[routeKey]
		if ok {
			return f(request)
		} else {
			return handleError()
		}
	default:
		return events.APIGatewayV2HTTPResponse{
			StatusCode: 422,
			Body:       "LambdaRouter",
		}, ErrUnsupportedPath
	}
}

func handleError() (events.APIGatewayV2HTTPResponse, error) {
	return events.APIGatewayV2HTTPResponse{
		StatusCode: 404,
		Body:       "LambdaRouter",
	}, ErrUnsupportedRoute
}
