.PHONY: help clean test test-ci package publish package-eventbridge publish-eventbridge

LAMBDA_BUCKET ?= "pennsieve-cc-lambda-functions-use1"
WORKING_DIR   ?= "$(shell pwd)"
SERVICE_NAME  ?= "account-service"
PACKAGE_NAME  ?= "${SERVICE_NAME}-${IMAGE_TAG}.zip"
EVENTBRIDGE_PACKAGE_NAME ?= "${SERVICE_NAME}-eventbridge-handler-${IMAGE_TAG}.zip"
CHECK_ACCESS_PACKAGE_NAME ?= "${SERVICE_NAME}-check-access-${IMAGE_TAG}.zip"

.DEFAULT: help

help:
	@echo "Make Help for $(SERVICE_NAME)"
	@echo ""
	@echo "make clean			- spin down containers and remove db files"
	@echo "make test			- run dockerized tests locally"
	@echo "make test-ci			- run dockerized tests for Jenkins"
	@echo "make package			- create venv and package lambda function"
	@echo "make publish			- package and publish lambda function"

# Run dockerized tests (can be used locally)
test:
	mkdir -p test-dynamodb-data
	chmod -R 777 test-dynamodb-data
	docker-compose -f docker-compose.test.yml down --remove-orphans
	docker-compose -f docker-compose.test.yml up --exit-code-from local_tests local_tests
	make clean

# Run dockerized tests (used on Jenkins)
test-ci:
	mkdir -p test-dynamodb-data
	chmod -R 777 test-dynamodb-data
	docker-compose -f docker-compose.test.yml down --remove-orphans
	@IMAGE_TAG=$(IMAGE_TAG) docker-compose -f docker-compose.test.yml up --exit-code-from=ci-tests ci-tests

# Remove folders created by NEO4J docker container
clean: docker-clean
	rm -rf conf
	rm -rf data
	rm -rf plugins

# Spin down active docker containers.
docker-clean:
	docker-compose -f docker-compose.test.yml down

# Build lambda and create ZIP file
package:
	@echo ""
	@echo "***********************"
	@echo "*   Building lambdas  *"
	@echo "***********************"
	@echo ""
	@echo "Building API Lambda..."
	env GOOS=linux GOARCH=arm64 go build -tags lambda.norpc -o $(WORKING_DIR)/bin/api/bootstrap $(WORKING_DIR)/cmd/api; \
	cd $(WORKING_DIR)/bin/api/; \
		zip -r $(WORKING_DIR)/bin/api/$(PACKAGE_NAME) .
	@echo "Building EventBridge Handler Lambda..."
	env GOOS=linux GOARCH=arm64 go build -tags lambda.norpc -o $(WORKING_DIR)/bin/eventbridge/bootstrap $(WORKING_DIR)/cmd/eventbridge-handler; \
	cd $(WORKING_DIR)/bin/eventbridge/; \
		zip -r $(WORKING_DIR)/bin/eventbridge/$(EVENTBRIDGE_PACKAGE_NAME) .
	@echo "Building Check Access Lambda..."
	env GOOS=linux GOARCH=arm64 go build -tags lambda.norpc -o $(WORKING_DIR)/bin/check-access/bootstrap $(WORKING_DIR)/cmd/check-access; \
	cd $(WORKING_DIR)/bin/check-access/; \
		zip -r $(WORKING_DIR)/bin/check-access/$(CHECK_ACCESS_PACKAGE_NAME) .

# Copy Service lambdas to S3 location
publish:
	@make package
	@echo ""
	@echo "*************************"
	@echo "*   Publishing lambdas  *"
	@echo "*************************"
	@echo ""
	@echo "Publishing API Lambda..."
	aws s3 cp $(WORKING_DIR)/bin/api/$(PACKAGE_NAME) s3://$(LAMBDA_BUCKET)/$(SERVICE_NAME)/ --output json
	@echo "Publishing EventBridge Handler Lambda..."
	aws s3 cp $(WORKING_DIR)/bin/eventbridge/$(EVENTBRIDGE_PACKAGE_NAME) s3://$(LAMBDA_BUCKET)/$(SERVICE_NAME)/ --output json
	@echo "Publishing Check Access Lambda..."
	aws s3 cp $(WORKING_DIR)/bin/check-access/$(CHECK_ACCESS_PACKAGE_NAME) s3://$(LAMBDA_BUCKET)/$(SERVICE_NAME)/ --output json
	rm -rf $(WORKING_DIR)/bin/api/$(PACKAGE_NAME) $(WORKING_DIR)/bin/api/bootstrap
	rm -rf $(WORKING_DIR)/bin/eventbridge/$(EVENTBRIDGE_PACKAGE_NAME) $(WORKING_DIR)/bin/eventbridge/bootstrap
	rm -rf $(WORKING_DIR)/bin/check-access/$(CHECK_ACCESS_PACKAGE_NAME) $(WORKING_DIR)/bin/check-access/bootstrap

# Run go mod tidy on modules
tidy:
	go mod tidy
