package compute

import (
	"context"
	"log"
	"net/http"

	"github.com/aws/aws-lambda-go/events"
	"github.com/pennsieve/account-service/internal/errors"
	"github.com/pennsieve/account-service/internal/utils"
)

type ComputeRouterHandlerFunc func(context.Context, events.APIGatewayV2HTTPRequest) (events.APIGatewayV2HTTPResponse, error)

// Defines the compute router interface
type ComputeRouter interface {
	POST(string, ComputeRouterHandlerFunc)
	GET(string, ComputeRouterHandlerFunc)
	PUT(string, ComputeRouterHandlerFunc)
	DELETE(string, ComputeRouterHandlerFunc)
	Start(context.Context, events.APIGatewayV2HTTPRequest) (events.APIGatewayV2HTTPResponse, error)
}

type ComputeLambdaRouter struct {
	getRoutes    map[string]ComputeRouterHandlerFunc
	postRoutes   map[string]ComputeRouterHandlerFunc
	putRoutes    map[string]ComputeRouterHandlerFunc
	deleteRoutes map[string]ComputeRouterHandlerFunc
}

func NewComputeLambdaRouter() ComputeRouter {
	return &ComputeLambdaRouter{
		getRoutes:    make(map[string]ComputeRouterHandlerFunc),
		postRoutes:   make(map[string]ComputeRouterHandlerFunc),
		putRoutes:    make(map[string]ComputeRouterHandlerFunc),
		deleteRoutes: make(map[string]ComputeRouterHandlerFunc),
	}
}

func (r *ComputeLambdaRouter) POST(routeKey string, handler ComputeRouterHandlerFunc) {
	r.postRoutes[routeKey] = handler
}

func (r *ComputeLambdaRouter) GET(routeKey string, handler ComputeRouterHandlerFunc) {
	r.getRoutes[routeKey] = handler
}

func (r *ComputeLambdaRouter) PUT(routeKey string, handler ComputeRouterHandlerFunc) {
	r.putRoutes[routeKey] = handler
}

func (r *ComputeLambdaRouter) DELETE(routeKey string, handler ComputeRouterHandlerFunc) {
	r.deleteRoutes[routeKey] = handler
}

func (r *ComputeLambdaRouter) Start(ctx context.Context, request events.APIGatewayV2HTTPRequest) (events.APIGatewayV2HTTPResponse, error) {
	log.Println(request)
	routeKey := utils.ExtractRoute(request.RouteKey)

	switch request.RequestContext.HTTP.Method {
	case http.MethodPost:
		f, ok := r.postRoutes[routeKey]
		if ok {
			return f(ctx, request)
		} else {
			return handleComputeError()
		}
	case http.MethodGet:
		f, ok := r.getRoutes[routeKey]
		if ok {
			return f(ctx, request)
		} else {
			return handleComputeError()
		}
	case http.MethodPut:
		f, ok := r.putRoutes[routeKey]
		if ok {
			return f(ctx, request)
		} else {
			return handleComputeError()
		}
	case http.MethodDelete:
		f, ok := r.deleteRoutes[routeKey]
		if ok {
			return f(ctx, request)
		} else {
			return handleComputeError()
		}
	default:
		log.Println(errors.ErrUnsupportedPath.Error())
		return events.APIGatewayV2HTTPResponse{
			StatusCode: http.StatusUnprocessableEntity,
			Body:       errors.ErrUnsupportedPath.Error(),
		}, nil
	}
}

func handleComputeError() (events.APIGatewayV2HTTPResponse, error) {
	log.Println(errors.ErrUnsupportedRoute.Error())
	return events.APIGatewayV2HTTPResponse{
		StatusCode: http.StatusNotFound,
		Body:       errors.ErrUnsupportedRoute.Error(),
	}, nil
}