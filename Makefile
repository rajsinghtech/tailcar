.PHONY: help
help: ## Display this help
	@awk 'BEGIN {FS = ":.*##"; printf "\nUsage:\n  make \033[36m<target>\033[0m\n"} /^[a-zA-Z_0-9-]+:.*?##/ { printf "  \033[36m%-15s\033[0m %s\n", $$1, $$2 } /^##@/ { printf "\n\033[1m%s\033[0m\n", substr($$0, 5) } ' $(MAKEFILE_LIST)

##@ Development

.PHONY: manifests
manifests: controller-gen ## Generate WebhookConfiguration, ClusterRole and CustomResourceDefinition objects
	$(CONTROLLER_GEN) rbac:roleName=manager-role crd webhook paths="./..." output:crd:artifacts:config=config/crd/bases
	@echo "Copying CRDs to helm chart..."
	@cp config/crd/bases/*.yaml helm/tailcar/templates/crds/

.PHONY: generate
generate: controller-gen ## Generate code containing DeepCopy, DeepCopyInto, and DeepCopyObject method implementations
	$(CONTROLLER_GEN) object:headerFile="hack/boilerplate.go.txt" paths="./..."

.PHONY: fmt
fmt: ## Run go fmt against code
	go fmt ./...

.PHONY: vet
vet: ## Run go vet against code
	go vet ./...

.PHONY: lint
lint: golangci-lint ## Run golangci-lint
	$(GOLANGCI_LINT) run ./...

.PHONY: lint-fix
lint-fix: golangci-lint ## Run golangci-lint and auto-fix issues
	$(GOLANGCI_LINT) run --fix ./...

.PHONY: test
test: manifests generate fmt vet ## Run tests
	go test ./... -coverprofile cover.out

##@ Build

.PHONY: build
build: generate fmt vet ## Build manager binary
	go build -o bin/manager cmd/manager/main.go

.PHONY: run
run: manifests generate fmt vet ## Run from your host
	go run ./cmd/manager/main.go

.PHONY: docker-build
docker-build: ## Build docker image
	docker build -t tailcar:latest .

.PHONY: docker-push
docker-push: ## Push docker image
	docker push tailcar:latest

##@ Deployment

.PHONY: install
install: manifests ## Install CRDs into the K8s cluster specified in ~/.kube/config
	kubectl apply -f config/crd/bases

.PHONY: uninstall
uninstall: manifests ## Uninstall CRDs from the K8s cluster specified in ~/.kube/config
	kubectl delete -f config/crd/bases

.PHONY: deploy
deploy: manifests ## Deploy controller to the K8s cluster specified in ~/.kube/config
	cd config/manager && kubectl apply -k .

.PHONY: undeploy
undeploy: ## Undeploy controller from the K8s cluster specified in ~/.kube/config
	cd config/manager && kubectl delete -k .

##@ Helm

.PHONY: helm-lint
helm-lint: ## Lint helm chart
	helm lint helm/tailcar

.PHONY: helm-template
helm-template: ## Template helm chart for debugging
	helm template tailcar helm/tailcar

.PHONY: helm-package
helm-package: manifests ## Package helm chart
	helm package helm/tailcar -d dist/

.PHONY: helm-install
helm-install: manifests ## Install helm chart
	helm install tailcar helm/tailcar --namespace tailcar-system --create-namespace

.PHONY: helm-upgrade
helm-upgrade: manifests ## Upgrade helm chart
	helm upgrade tailcar helm/tailcar --namespace tailcar-system

.PHONY: helm-uninstall
helm-uninstall: ## Uninstall helm chart
	helm uninstall tailcar --namespace tailcar-system

##@ Tools

CONTROLLER_GEN = $(shell pwd)/bin/controller-gen
.PHONY: controller-gen
controller-gen: ## Download controller-gen locally if necessary
	$(call go-get-tool,$(CONTROLLER_GEN),sigs.k8s.io/controller-tools/cmd/controller-gen@v0.16.5)

GOLANGCI_LINT = $(shell pwd)/bin/golangci-lint
.PHONY: golangci-lint
golangci-lint: ## Download golangci-lint locally if necessary
	@[ -f $(GOLANGCI_LINT) ] || { \
	set -e ;\
	echo "Downloading golangci-lint" ;\
	curl -sSfL https://raw.githubusercontent.com/golangci/golangci-lint/master/install.sh | sh -s -- -b $(shell dirname $(GOLANGCI_LINT)) v2.2.0 ;\
	}

define go-get-tool
@[ -f $(1) ] || { \
set -e ;\
TMP_DIR=$$(mktemp -d) ;\
cd $$TMP_DIR ;\
go mod init tmp ;\
echo "Downloading $(2)" ;\
GOBIN=$(shell dirname $(1)) go install $(2) ;\
rm -rf $$TMP_DIR ;\
}
endef
