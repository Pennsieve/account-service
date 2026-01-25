package handler

import (
	"context"
	"log"
	"net/http"

	"github.com/aws/aws-lambda-go/events"
	"github.com/pennsieve/account-service/service/utils"
)

type RouterHandlerFunc func(context.Context, events.APIGatewayV2HTTPRequest) (events.APIGatewayV2HTTPResponse, error)

// Defines the router interface
type Router interface {
	POST(string, RouterHandlerFunc)
	GET(string, RouterHandlerFunc)
	PATCH(string, RouterHandlerFunc)
	DELETE(string, RouterHandlerFunc)
	Start(context.Context, events.APIGatewayV2HTTPRequest) (events.APIGatewayV2HTTPResponse, error)
}

type LambdaRouter struct {
	getRoutes    map[string]RouterHandlerFunc
	postRoutes   map[string]RouterHandlerFunc
	patchRoutes  map[string]RouterHandlerFunc
	deleteRoutes map[string]RouterHandlerFunc
}

func NewLambdaRouter() Router {
	return &LambdaRouter{
		make(map[string]RouterHandlerFunc),
		make(map[string]RouterHandlerFunc),
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

func (r *LambdaRouter) PATCH(routeKey string, handler RouterHandlerFunc) {
	r.patchRoutes[routeKey] = handler
}

func (r *LambdaRouter) DELETE(routeKey string, handler RouterHandlerFunc) {
	r.deleteRoutes[routeKey] = handler
}

func (r *LambdaRouter) Start(ctx context.Context, request events.APIGatewayV2HTTPRequest) (events.APIGatewayV2HTTPResponse, error) {
	log.Println(request)
	routeKey := utils.ExtractRoute(request.RouteKey)

	switch request.RequestContext.HTTP.Method {
	case http.MethodPost:
		f, ok := r.postRoutes[routeKey]
		if ok {
			return f(ctx, request)
		} else {
			return handleError()
		}
	case http.MethodGet:
		f, ok := r.getRoutes[routeKey]
		if ok {
			return f(ctx, request)
		} else {
			return handleError()
		}
	case http.MethodPatch:
		f, ok := r.patchRoutes[routeKey]
		if ok {
			return f(ctx, request)
		} else {
			return handleError()
		}
	case http.MethodDelete:
		f, ok := r.deleteRoutes[routeKey]
		if ok {
			return f(ctx, request)
		} else {
			return handleError()
		}
	default:
		log.Println(ErrUnsupportedPath.Error())
		return events.APIGatewayV2HTTPResponse{
			StatusCode: http.StatusUnprocessableEntity,
			Body:       ErrUnsupportedPath.Error(),
		}, nil
	}
}

func handleError() (events.APIGatewayV2HTTPResponse, error) {
	log.Println(ErrUnsupportedRoute.Error())
	return events.APIGatewayV2HTTPResponse{
		StatusCode: http.StatusNotFound,
		Body:       ErrUnsupportedRoute.Error(),
	}, nil
}
