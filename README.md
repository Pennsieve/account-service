# template-serverless-service

A template for building a new Pennsieve Serverless Service

## How to use this template to create a new `foo-service` repository

* This [repo's page on GitHub](https://github.com/Pennsieve/template-serverless-service)
  has a "Use this template" button. Click the button and select "Create a new repository".
  Name the new repository `foo-service`
* In the new `foo-service` repo search for the `TODO` comments to find names/strings that need to be updated.
  Usually the update is simply to change `template-serverless-servce` to `foo-service` or `TemplateServerlessService`
  to `FooService`, etc.
* The `TODO` comments mostly only identify where a name is first declared. So fix any compilation errors with the new
  names.
* Replace this README with one for Foo service!

## Preferred Go version and libraries

* Go 1.21
* Structured logging: [log/slog](https://pkg.go.dev/log/slog) from the standard library.
* Postgres Driver: [jackc/pgx](https://github.com/jackc/pgx) unless you need to interact with a Pennsieve Go library
  that still uses [lib/pq](https://github.com/lib/pq) in which case, use that.
* Testing utility: [stretchr/testify](https://pkg.go.dev/github.com/stretchr/testify)

## Additional steps
* Create and populate prod and non-prod `foo-service` directories in [Pennsieve/infrastructure](https://github.com/Pennsieve/infrastructure)
* Create prod and non-prod `foo-service` service-deploy jobs in Jenkins.
* Add new endpoint in [Pennsieve/pennsieve-go-api](https://github.com/Pennsieve/pennsieve-go-api).
