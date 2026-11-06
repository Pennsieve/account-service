package runner

import "context"

type Runner interface {
	Run(context.Context) error
}
